package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalClipboardRoundTrip(t *testing.T) {
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

	payload := []byte("local client round trip")
	require.NoError(t, localClipboardCopy("test-token", payload))
	got, err := localClipboardPaste("test-token")
	require.NoError(t, err)
	assert.Equal(t, payload, got)

	require.Error(t, localClipboardCopy("wrong-token", payload))
	_, err = localClipboardPaste("wrong-token")
	require.Error(t, err)
}

func TestLocalClipboardServerDown(t *testing.T) {
	useEphemeralClipboardPort(t)

	// Nothing listens: bind and close to find a free port, then call.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	_ = ln.Close()
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	previous := clipboardPort
	clipboardPort = port
	t.Cleanup(func() { clipboardPort = previous })

	require.Error(t, localClipboardCopy("test-token", []byte("x")))
	_, err = localClipboardPaste("test-token")
	require.Error(t, err)
}

func TestServerErrorText(t *testing.T) {
	assert.Equal(t, "boom", serverErrorText("ERR boom"))
	assert.Equal(t, "OK", serverErrorText("OK"))
	assert.Equal(t, "", serverErrorText(""))
}
