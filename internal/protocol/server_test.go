package protocol

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useEphemeralClipboardPort points clipboardPort at a free local port for
// the duration of the test.
func useEphemeralClipboardPort(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	_ = ln.Close()

	previous := clipboardPort
	clipboardPort = port
	t.Cleanup(func() { clipboardPort = previous })
}

func TestParseClipboardHeader(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantVerb  string
		wantToken string
		wantLen   int
		wantErr   string
	}{
		{name: "hello", line: "SLUSH1 tok HELLO", wantVerb: "HELLO", wantToken: "tok"},
		{name: "paste", line: "SLUSH1 tok PASTE", wantVerb: "PASTE", wantToken: "tok"},
		{name: "copy", line: "SLUSH1 tok COPY 12", wantVerb: "COPY", wantToken: "tok", wantLen: 12},
		{name: "open", line: "SLUSH1 tok OPEN 7", wantVerb: "OPEN", wantToken: "tok", wantLen: 7},
		{name: "empty copy", line: "SLUSH1 tok COPY 0", wantVerb: "COPY", wantToken: "tok"},
		{name: "empty", line: "", wantErr: "malformed header"},
		{name: "wrong magic", line: "LEMONADE tok HELLO", wantErr: "unknown protocol"},
		{name: "unknown verb", line: "SLUSH1 tok FROB", wantErr: "unknown verb"},
		{name: "copy missing length", line: "SLUSH1 tok COPY", wantErr: "malformed header"},
		{name: "paste with length", line: "SLUSH1 tok PASTE 3", wantErr: "malformed header"},
		{name: "negative length", line: "SLUSH1 tok COPY -1", wantErr: "bad length"},
		{name: "huge length", line: "SLUSH1 tok COPY 99999999999", wantErr: "bad length"},
		{name: "http garbage", line: "GET / HTTP/1.0", wantErr: "unknown protocol"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verb, token, length, err := parseClipboardHeader(tt.line)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantVerb, verb)
			assert.Equal(t, tt.wantToken, token)
			assert.Equal(t, tt.wantLen, length)
		})
	}
}

func TestGenerateClipboardToken(t *testing.T) {
	first, err := GenerateToken()
	require.NoError(t, err)
	assert.Len(t, first, 32)

	second, err := GenerateToken()
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
}

func TestEnsureClipboardPortFree(t *testing.T) {
	useEphemeralClipboardPort(t)
	require.NoError(t, EnsurePortFree(clipboardPort))

	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(clipboardPort))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	err = EnsurePortFree(clipboardPort)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

// clipboardExchange dials the test server, sends one request, and
// returns the response line plus any trailing payload (for PASTE).
func clipboardExchange(t *testing.T, header string, body []byte) (string, []byte) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(clipboardPort), 5*time.Second)
	require.NoError(t, err)
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	_, err = fmt.Fprintf(conn, "%s\n", header)
	require.NoError(t, err)
	if body != nil {
		_, err = conn.Write(body)
		require.NoError(t, err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	line = strings.TrimSuffix(line, "\n")

	var rest []byte
	if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "OK" {
		n, err := strconv.Atoi(fields[1])
		require.NoError(t, err)
		rest = make([]byte, n)
		_, err = io.ReadFull(reader, rest)
		require.NoError(t, err)
	}
	return line, rest
}

func TestClipboardHello(t *testing.T) {
	useEphemeralClipboardPort(t)
	server, err := startClipboardServer("test-token")
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	line, _ := clipboardExchange(t, "SLUSH1 test-token HELLO", nil)
	assert.Equal(t, "OK slush-clipboard 1", line)
}

func TestClipboardUnauthorized(t *testing.T) {
	useEphemeralClipboardPort(t)
	server, err := startClipboardServer("test-token")
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	line, _ := clipboardExchange(t, "SLUSH1 wrong-token HELLO", nil)
	assert.Equal(t, "ERR unauthorized", line)

	line, _ = clipboardExchange(t, "SLUSH1 wrong-token PASTE", nil)
	assert.Equal(t, "ERR unauthorized", line)
}

func TestClipboardMalformed(t *testing.T) {
	useEphemeralClipboardPort(t)
	server, err := startClipboardServer("test-token")
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	for _, header := range []string{"", "HELLO", "GET / HTTP/1.0", "SLUSH1 test-token FROB", "noise-without-newline-terminator-x"} {
		line, _ := clipboardExchange(t, header, nil)
		assert.True(t, strings.HasPrefix(line, "ERR "), "header %q: %q", header, line)
	}
}

// TestClipboardServerToleratesBareConnect ensures a plain TCP connect
// with no request never wedges the server: unlike lemonade's one-slot
// connection channel, each connection is independent, so probes and
// port scans cannot deadlock the next real request.
func TestClipboardServerToleratesBareConnect(t *testing.T) {
	useEphemeralClipboardPort(t)
	server, err := startClipboardServer("test-token")
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(clipboardPort), 5*time.Second)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	line, _ := clipboardExchange(t, "SLUSH1 test-token HELLO", nil)
	assert.Equal(t, "OK slush-clipboard 1", line)
}

func TestClipboardCopyRejectsWithoutBackend(t *testing.T) {
	useEphemeralClipboardPort(t)
	t.Setenv("PATH", emptyPath(t))
	t.Setenv("WAYLAND_DISPLAY", "test-display")

	server, err := startClipboardServer("test-token")
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	line, _ := clipboardExchange(t, "SLUSH1 test-token COPY 5", []byte("hello"))
	assert.True(t, strings.HasPrefix(line, "ERR "), line)
}

func TestClipboardOpenRejectsPaths(t *testing.T) {
	useEphemeralClipboardPort(t)
	server, err := startClipboardServer("test-token")
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	for _, target := range []string{"/remote/file.txt", "example.com", ""} {
		line, _ := clipboardExchange(t,
			fmt.Sprintf("SLUSH1 test-token OPEN %d", len(target)), []byte(target))
		assert.True(t, strings.HasPrefix(line, "ERR "), "target %q: %q", target, line)
	}
}

// Fake clipboard backends below need shell scripts, so round-trip and
// open tests skip Windows like the other fake-binary tests.

func TestClipboardCopyPasteRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake backend helpers are shell scripts")
	}
	useEphemeralClipboardPort(t)

	binDir := t.TempDir()
	clipFile := filepath.Join(t.TempDir(), "clipboard")
	writeFakeClipTool(t, binDir, "wl-copy", `cat > "$FAKE_CLIP_FILE"`)
	writeFakeClipTool(t, binDir, "wl-paste", `cat "$FAKE_CLIP_FILE"`)
	writeFakeClipTool(t, binDir, "pbcopy", `cat > "$FAKE_CLIP_FILE"`)
	writeFakeClipTool(t, binDir, "pbpaste", `cat "$FAKE_CLIP_FILE"`)
	t.Setenv("FAKE_CLIP_FILE", clipFile)
	t.Setenv("WAYLAND_DISPLAY", "test-display")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	server, err := startClipboardServer("test-token")
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	payload := []byte("hello clipboard\nsecond line")
	line, _ := clipboardExchange(t,
		fmt.Sprintf("SLUSH1 test-token COPY %d", len(payload)), payload)
	require.Equal(t, "OK", line)

	gotLine, gotBody := clipboardExchange(t, "SLUSH1 test-token PASTE", nil)
	require.True(t, strings.HasPrefix(gotLine, "OK "), gotLine)
	assert.Equal(t, payload, gotBody)
}

func TestClipboardOpenURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake backend helpers are shell scripts")
	}
	useEphemeralClipboardPort(t)

	binDir := t.TempDir()
	openLog := filepath.Join(t.TempDir(), "opened")
	writeFakeClipTool(t, binDir, "xdg-open", `printf '%s\n' "$@" >> "$FAKE_OPEN_LOG"`)
	writeFakeClipTool(t, binDir, "open", `printf '%s\n' "$@" >> "$FAKE_OPEN_LOG"`)
	t.Setenv("FAKE_OPEN_LOG", openLog)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	server, err := startClipboardServer("test-token")
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	target := "https://example.com/x?y=1"
	line, _ := clipboardExchange(t,
		fmt.Sprintf("SLUSH1 test-token OPEN %d", len(target)), []byte(target))
	require.Equal(t, "OK", line)

	logged, err := os.ReadFile(openLog)
	require.NoError(t, err)
	assert.Equal(t, target+"\n", string(logged))
}

func TestClipboardLinuxBackendFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake backend helpers are shell scripts")
	}
	if runtime.GOOS != "linux" {
		t.Skip("linux backend fallback chain only")
	}
	useEphemeralClipboardPort(t)

	// No WAYLAND_DISPLAY and only xclip present: the chain must fall
	// through to xclip instead of failing.
	binDir := t.TempDir()
	clipFile := filepath.Join(t.TempDir(), "clipboard")
	writeFakeClipTool(t, binDir, "xclip", `if [ "$*" = "-selection clipboard -o" ]; then cat "$FAKE_CLIP_FILE"; else cat > "$FAKE_CLIP_FILE"; fi`)
	t.Setenv("FAKE_CLIP_FILE", clipFile)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	server, err := startClipboardServer("test-token")
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	payload := []byte("fallback payload")
	line, _ := clipboardExchange(t,
		fmt.Sprintf("SLUSH1 test-token COPY %d", len(payload)), payload)
	require.Equal(t, "OK", line)

	gotLine, gotBody := clipboardExchange(t, "SLUSH1 test-token PASTE", nil)
	require.True(t, strings.HasPrefix(gotLine, "OK "), gotLine)
	assert.Equal(t, payload, gotBody)
}

func writeFakeClipTool(t *testing.T, dir, name, body string) {
	t.Helper()
	script := "#!/bin/sh\n" + body + "\n"
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}
