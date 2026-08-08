package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureLemonadePortFreeWhenNothingListening(t *testing.T) {
	useEphemeralLemonadeAddr(t)
	require.NoError(t, ensureLemonadePortFree())
}

func TestEnsureLemonadePortFreeWhenSomethingListening(t *testing.T) {
	useEphemeralLemonadeAddr(t)

	ln, err := net.Listen("tcp", lemonadeAddr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	err = ensureLemonadePortFree()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestStartAndStopLemonade(t *testing.T) {
	useEphemeralLemonadeAddr(t)

	binDir := t.TempDir()
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	server, err := startLemonade()
	require.NoError(t, err)
	require.True(t, isAcceptingConnections(lemonadeAddr))

	server.Stop()
	requirePortFree(t, lemonadeAddr)
}

func TestStartLemonadeMissingBinary(t *testing.T) {
	useEphemeralLemonadeAddr(t)
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

func requirePortFree(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !isAcceptingConnections(addr) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s still accepting connections", addr)
}
