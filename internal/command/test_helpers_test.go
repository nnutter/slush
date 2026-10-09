package command

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/slush/internal/protocol"
)

// run exercises the real command entry point and maps its error to a status.
func run(args []string) int {
	return ExitCode(Execute(args))
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
	return protocol.EnsurePortFree(clipboardPort)
}

func requirePortFree(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if protocol.EnsurePortFree(port) == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.FailNowf(t, "clipboard port is still bound", ":%d still bound", port)
}
