package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureLemonadePortFreeWhenNothingListening(t *testing.T) {
	useEphemeralLemonadePort(t)
	require.NoError(t, ensureLemonadePortFree())
}

func TestEnsureLemonadePortFreeWhenSomethingListening(t *testing.T) {
	useEphemeralLemonadePort(t)

	// Bind like production lemonade ("_"+port) so the free-port check sees it.
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(lemonadePort))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	err = ensureLemonadePortFree()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestStartAndStopLemonade(t *testing.T) {
	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	server, err := startLemonade()
	require.NoError(t, err)
	require.True(t, portIsBound(lemonadePort))

	server.Stop()
	requirePortFree(t, lemonadePort)
}

func TestStartLemonadeMissingBinary(t *testing.T) {
	useEphemeralLemonadePort(t)
	t.Setenv("PATH", emptyPath(t))

	_, err := startLemonade()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func emptyPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		return dir
	}
	return dir + string(os.PathListSeparator) + filepath.Join(dir, "nope")
}

func requirePortFree(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !portIsBound(port) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf(":%d still bound", port)
}
