package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/nnutter/slush/internal/clipboard"
	"github.com/stretchr/testify/require"
)

// emptyPath returns a PATH with no usable binaries: an empty dir, plus
// an extra missing entry on unix so bare command names still fail.
func emptyPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		return dir
	}
	return dir + string(os.PathListSeparator) + filepath.Join(dir, "nope")
}

// useEphemeralClipboardPort selects an isolated application endpoint.
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

func writeFakeClipTool(t *testing.T, dir, name, body string) {
	t.Helper()
	script := "#!/bin/sh\n" + body + "\n"
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}

func ensureClipboardPortFree() error {
	return clipboard.EnsurePortFree(clipboardPort)
}

func requirePortFree(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if clipboard.EnsurePortFree(port) == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf(":%d still bound", port)
}
