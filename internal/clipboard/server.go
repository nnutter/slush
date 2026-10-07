// Clipboard forwarding server.
//
// Remote programs speak a small line-based protocol over one TCP
// connection per operation:
//
//	SLUSH1 <token> HELLO            -> OK slush-clipboard 1
//	SLUSH1 <token> COPY <nbytes>\n<bytes>   -> OK
//	SLUSH1 <token> PASTE            -> OK <nbytes>\n<bytes>
//	SLUSH1 <token> OPEN <nbytes>\n<target>  -> OK
//
// Failures answer "ERR <message>". Anything that does not parse as a
// header (including other protocols pointed at this port by mistake)
// gets an ERR instead of hanging, so misconfiguration is loud.
//
// The server binds 127.0.0.1 only: the reverse tunnel forwards the
// remote loopback here, and nothing else needs to reach it.
package clipboard

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/nnutter/slush/internal/desktop"
)

// portIsBound reports whether something is listening on ":"+port by
// trying to bind that address. Binding never completes a TCP
// handshake, so it is safe against servers that misbehave when
// prodded with a bare connection.
func portIsBound(port int) bool {
	ln, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

const (
	// DefaultPort is the loopback port used by slush sessions.
	DefaultPort = 2489

	clipboardMagic   = "SLUSH1"
	clipboardVersion = "1"

	// maxHeaderLine bounds a single request line; payloads are
	// bounded separately by maxClipboardPayload.
	maxHeaderLine = 4096
	// maxClipboardPayload caps one clipboard transfer so a buggy
	// client cannot make the server grow without bound.
	maxClipboardPayload = 64 << 20

	clipboardIOTimeout = 30 * time.Second
)

// Server is a clipboard forwarding listener. Use StartServer to create one.
type Server struct {
	listener net.Listener
	token    string
}

// EnsurePortFree returns an error if port cannot be bound. It binds rather
// than dials so readiness checks cannot wedge a half-open connection.
func EnsurePortFree(port int) error {
	if portIsBound(port) {
		return fmt.Errorf("clipboard server already running on :%d; stop it before using slush", port)
	}
	return nil
}

// StartServer listens on 127.0.0.1:port and serves the clipboard protocol
// until Stop is called.
func StartServer(port int, token string) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return nil, fmt.Errorf("listen clipboard on 127.0.0.1:%d: %w", port, err)
	}
	server := &Server{listener: ln, token: token}
	go server.serve()
	return server, nil
}

// Stop terminates the clipboard server listener.
func (s *Server) Stop() {
	if s == nil {
		return
	}
	_ = s.listener.Close()
}

func (s *Server) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

// handle serves a single request on conn and then closes it.
func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(clipboardIOTimeout))

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		writeResponse(conn, "ERR read header")
		return
	}
	if len(line) > maxHeaderLine {
		writeResponse(conn, "ERR header too long")
		return
	}

	verb, token, length, err := parseClipboardHeader(strings.TrimSuffix(line, "\n"))
	if err != nil {
		writeResponse(conn, "ERR "+err.Error())
		return
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
		writeResponse(conn, "ERR unauthorized")
		return
	}

	switch verb {
	case "HELLO":
		writeResponse(conn, "OK slush-clipboard "+clipboardVersion)
	case "COPY":
		body, err := readPayload(reader, length)
		if err != nil {
			writeResponse(conn, "ERR "+err.Error())
			return
		}
		if err := desktop.Copy(body); err != nil {
			writeResponse(conn, "ERR "+err.Error())
			return
		}
		writeResponse(conn, "OK")
	case "PASTE":
		body, err := desktop.Paste()
		if err != nil {
			writeResponse(conn, "ERR "+err.Error())
			return
		}
		writeResponse(conn, fmt.Sprintf("OK %d", len(body)))
		_, _ = conn.Write(body)
	case "OPEN":
		target, err := readPayload(reader, length)
		if err != nil {
			writeResponse(conn, "ERR "+err.Error())
			return
		}
		if err := desktop.OpenURL(string(target)); err != nil {
			writeResponse(conn, "ERR "+err.Error())
			return
		}
		writeResponse(conn, "OK")
	default:
		writeResponse(conn, "ERR unknown verb")
	}
}

// parseClipboardHeader parses "SLUSH1 <token> <VERB> [<nbytes>]".
func parseClipboardHeader(line string) (verb, token string, length int, err error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", "", 0, fmt.Errorf("malformed header")
	}
	if fields[0] != clipboardMagic {
		return "", "", 0, fmt.Errorf("unknown protocol")
	}
	token = fields[1]
	verb = fields[2]

	wantLength := verb == "COPY" || verb == "OPEN"
	if wantLength && len(fields) != 4 {
		return "", "", 0, fmt.Errorf("malformed header")
	}
	if !wantLength && len(fields) != 3 {
		return "", "", 0, fmt.Errorf("malformed header")
	}
	if wantLength {
		length, err = strconv.Atoi(fields[3])
		if err != nil || length < 0 || length > maxClipboardPayload {
			return "", "", 0, fmt.Errorf("bad length")
		}
	}
	switch verb {
	case "HELLO", "COPY", "PASTE", "OPEN":
		return verb, token, length, nil
	default:
		return "", "", 0, fmt.Errorf("unknown verb")
	}
}

// readPayload reads exactly n bytes following a request header.
func readPayload(reader *bufio.Reader, n int) ([]byte, error) {
	if n > maxClipboardPayload {
		return nil, fmt.Errorf("bad length")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, fmt.Errorf("short payload")
	}
	return body, nil
}

func writeResponse(conn net.Conn, response string) {
	_, _ = fmt.Fprintf(conn, "%s\n", response)
}

// GenerateToken returns a random per-session token so only holders of
// the wrapped remote environment can drive the server.
func GenerateToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
