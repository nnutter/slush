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
package main

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
)

const (
	defaultClipboardPort = 2489

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

// clipboardPort is the local TCP port the server listens on. Tests may
// override it; production always uses defaultClipboardPort.
var clipboardPort = defaultClipboardPort

// clipboardServer is a clipboard forwarding listener. Use
// startClipboardServer to create one.
type clipboardServer struct {
	listener net.Listener
	token    string
}

// ensureClipboardPortFree returns an error if the clipboard port cannot
// be bound. It never dials: like lemonade's server, endpoints behind
// this port may misbehave when prodded with a bare connection.
func ensureClipboardPortFree() error {
	if portIsBound(clipboardPort) {
		return fmt.Errorf("clipboard server already running on :%d; stop it before using slush", clipboardPort)
	}
	return nil
}

// startClipboardServer listens on 127.0.0.1:clipboardPort and serves
// the clipboard protocol until Stop is called.
func startClipboardServer(token string) (*clipboardServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(clipboardPort))
	if err != nil {
		return nil, fmt.Errorf("listen clipboard on 127.0.0.1:%d: %w", clipboardPort, err)
	}
	server := &clipboardServer{listener: ln, token: token}
	go server.serve()
	return server, nil
}

// Stop terminates the clipboard server listener.
func (s *clipboardServer) Stop() {
	if s == nil {
		return
	}
	_ = s.listener.Close()
}

func (s *clipboardServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

// handle serves a single request on conn and then closes it.
func (s *clipboardServer) handle(conn net.Conn) {
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
		if err := copyToClipboard(body); err != nil {
			writeResponse(conn, "ERR "+err.Error())
			return
		}
		writeResponse(conn, "OK")
	case "PASTE":
		body, err := pasteFromClipboard()
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
		if err := openOnClient(string(target)); err != nil {
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

// generateClipboardToken returns a random per-session token so only
// holders of the wrapped remote environment can drive the server.
func generateClipboardToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
