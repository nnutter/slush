package clipboard

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Protocol tests share an ephemeral endpoint, independent of application
// configuration. These helpers keep the existing fixtures at that endpoint.
var clipboardPort = DefaultPort

func startClipboardServer(token string) (*Server, error) {
	return StartServer(clipboardPort, token)
}

// emptyPath supplies no usable backend commands.
func emptyPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		return dir
	}
	return dir + string(os.PathListSeparator) + filepath.Join(dir, "nope")
}
