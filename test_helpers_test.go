package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
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
