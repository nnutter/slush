// Local client for the clipboard protocol in server.go. Used by
// validate to drive round-trips without shelling out.
package clipboard

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// Copy sends data to the local clipboard server.
func Copy(port int, token string, data []byte) error {
	status, _, err := clipboardCall(port, token, "COPY", data)
	if err != nil {
		return err
	}
	if status != "OK" {
		return fmt.Errorf("copy: %s", serverErrorText(status))
	}
	return nil
}

// Paste reads back from the local clipboard server.
func Paste(port int, token string) ([]byte, error) {
	status, body, err := clipboardCall(port, token, "PASTE", nil)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(status, "OK") {
		return nil, fmt.Errorf("paste: %s", serverErrorText(status))
	}
	return body, nil
}

// clipboardCall performs one request against 127.0.0.1:port,
// returning the response line and any trailing payload.
func clipboardCall(port int, token, verb string, payload []byte) (string, []byte, error) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 5*time.Second)
	if err != nil {
		return "", nil, fmt.Errorf("dial clipboard server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(clipboardIOTimeout))

	if payload == nil {
		if _, err := fmt.Fprintf(conn, "%s %s %s\n", clipboardMagic, token, verb); err != nil {
			return "", nil, fmt.Errorf("send header: %w", err)
		}
	} else {
		if _, err := fmt.Fprintf(conn, "%s %s %s %d\n%s", clipboardMagic, token, verb, len(payload), payload); err != nil {
			return "", nil, fmt.Errorf("send request: %w", err)
		}
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", nil, fmt.Errorf("read response: %w", err)
	}
	line = strings.TrimSuffix(line, "\n")

	var body []byte
	if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "OK" {
		if n, err := strconv.Atoi(fields[1]); err == nil && n >= 0 {
			body = make([]byte, n)
			if _, err := io.ReadFull(reader, body); err != nil {
				return "", nil, fmt.Errorf("read payload: %w", err)
			}
		}
	}
	return line, body, nil
}

// serverErrorText trims the ERR prefix for display.
func serverErrorText(status string) string {
	if rest, ok := strings.CutPrefix(status, "ERR "); ok {
		return rest
	}
	return status
}
