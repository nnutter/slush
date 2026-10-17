package remote

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// localInstallSSH executes the actual remote installer in a local POSIX shell.
func localInstallSSH(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture executes a POSIX shell script")
	}
	path := filepath.Join(t.TempDir(), "ssh")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nfor arg do script=$arg; done\nexec /bin/sh -c \"$script\"\n"), 0o755))
	return path
}

func TestInstallWrappers(t *testing.T) {
	ssh := localInstallSSH(t)
	_ = python3Path(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("SLUSH_TOKEN", "")
	t.Setenv("SLUSH_PORT", "")
	cache := filepath.Join(home, ".cache", "slush")
	require.NoError(t, os.MkdirAll(cache, 0o700))

	bin := filepath.Join(home, ".slush", "bin")
	require.NoError(t, Install(ssh, "host", nil))
	require.NoError(t, Install(ssh, "host", nil), "reinstallation must work")

	// An existing server does not inherit slush credentials. Each invocation
	// must read the current session file, including after a new connection.
	for _, token := range []string{"first-session", "next-session"} {
		port, received := startFakeClipboardServer(t, token, []byte("local clipboard"))
		require.NoError(t, os.WriteFile(filepath.Join(cache, "slush-env"),
			fmt.Appendf(nil, "SLUSH_TOKEN=%s\nSLUSH_PORT=%d\n", token, port), 0o600))
		for _, operation := range []struct {
			name, input, output, verb, payload string
			args                               []string
		}{
			{name: "pbcopy", input: "remote clipboard", verb: "COPY", payload: "remote clipboard"},
			{name: "wl-paste", output: "local clipboard", verb: "PASTE"},
			{name: "slush-open", verb: "OPEN", payload: "https://example.com", args: []string{"https://example.com"}},
		} {
			cmd := exec.CommandContext(t.Context(), filepath.Join(bin, operation.name), operation.args...)
			cmd.Stdin = strings.NewReader(operation.input)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s: %s", operation.name, output)
			assert.Equal(t, operation.output, string(output))
			request := receiveRequest(t, received)
			assert.Equal(t, operation.verb, request.verb)
			assert.Equal(t, operation.payload, string(request.body))
		}
	}
}

func TestInstallFixedLocation(t *testing.T) {
	ssh := localInstallSSH(t)
	_ = python3Path(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, Install(ssh, "host", nil))
	bin := filepath.Join(home, ".slush", "bin")
	cmd := exec.CommandContext(t.Context(), filepath.Join(bin, "open"), "--dry-run", "https://example.com")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	assert.Equal(t, "OPEN https://example.com\n", string(out))
}

func TestInstallFailures(t *testing.T) {
	ssh := localInstallSSH(t)
	_ = python3Path(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".slush", "bin")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".slush"), 0o755))
	require.NoError(t, os.WriteFile(bin, []byte("existing file"), 0o600))
	require.Error(t, Install(ssh, "host", nil))
	contents, err := os.ReadFile(bin)
	require.NoError(t, err)
	assert.Equal(t, "existing file", string(contents))
	require.NoError(t, os.Remove(bin))
	require.ErrorContains(t, Install("/nonexistent/ssh", "host", nil), "ssh host")
	t.Setenv("PATH", t.TempDir())
	require.ErrorContains(t, Install(ssh, "host", nil), "remote has no python3")
	_, err = os.Stat(bin)
	require.True(t, os.IsNotExist(err), "missing Python must not leave wrappers")
}
