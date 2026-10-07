package remote

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeProvisionSSH installs an ssh stand-in driven by environment:
// FAKE_VERSION (reported installed version), FAKE_NO_PYTHON3=1 (report
// no python3), FAKE_MUX_FAIL=1 (fail any exec carrying -S),
// FAKE_ALWAYS_FAIL=1 (fail everything). Calls append to calls.log;
// install stdin lands in stdin.sh.
func writeFakeProvisionSSH(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture executes a POSIX shell script")
	}
	script := `#!/bin/sh
echo "=== CALL ===" >> "$FAKE_SSH_DIR/calls.log"
echo "$*" >> "$FAKE_SSH_DIR/calls.log"
for arg in "$@"; do
  if [ "$arg" = "-S" ]; then
    if [ "$FAKE_MUX_FAIL" = 1 ]; then
      exit 7
    fi
  fi
done
if [ "$FAKE_ALWAYS_FAIL" = 1 ]; then
  exit 1
fi
stdin=$(cat)
printf '%s' "$stdin" > "$FAKE_SSH_DIR/stdin.sh"
if [ -n "$stdin" ]; then
  echo INSTALLED
  exit 0
fi
if [ "$FAKE_NO_PYTHON3" = 1 ]; then
  echo NO_PYTHON3
fi
echo "${FAKE_VERSION:-MISSING}"
`
	path := filepath.Join(dir, "ssh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}

func provisionTestEnv(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("FAKE_SSH_DIR", dir)
	t.Setenv("FAKE_VERSION", "MISSING")
	t.Setenv("FAKE_MUX_FAIL", "")
	t.Setenv("FAKE_NO_PYTHON3", "")
	t.Setenv("FAKE_ALWAYS_FAIL", "")
}

func sshCalls(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "calls.log"))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var calls []string
	for _, chunk := range strings.Split(string(raw), "=== CALL ===\n") {
		if strings.TrimSpace(chunk) != "" {
			calls = append(calls, chunk)
		}
	}
	return calls
}

func TestProvisionInstallsWhenStale(t *testing.T) {
	binDir := t.TempDir()
	sshDir := t.TempDir()
	writeFakeProvisionSSH(t, binDir)
	provisionTestEnv(t, sshDir)

	params := Environment{ControlPath: filepath.Join(sshDir, "control"), Token: "tok123", Port: 2489}
	require.NoError(t, Provision(filepath.Join(binDir, "ssh"), "user@host", params))

	calls := sshCalls(t, sshDir)
	require.Len(t, calls, 2, "check plus install")
	assert.Contains(t, calls[0], "user@host")
	assert.NotContains(t, calls[0], "slush-shim")

	uploaded, err := os.ReadFile(filepath.Join(sshDir, "stdin.sh"))
	require.NoError(t, err)
	// The fake reads stdin through $(...), which strips trailing
	// newlines; restore the one shim.py ends with before comparing.
	assert.Equal(t, shimScript, string(uploaded)+"\n")
	assert.Contains(t, calls[1], "slush-shim")
}

func TestProvisionSkipsWhenCurrent(t *testing.T) {
	binDir := t.TempDir()
	sshDir := t.TempDir()
	writeFakeProvisionSSH(t, binDir)
	provisionTestEnv(t, sshDir)
	t.Setenv("FAKE_VERSION", Version)

	params := Environment{ControlPath: filepath.Join(sshDir, "control"), Token: "tok123", Port: 2489}
	require.NoError(t, Provision(filepath.Join(binDir, "ssh"), "user@host", params))

	calls := sshCalls(t, sshDir)
	require.Len(t, calls, 1, "check only, no install")
	uploaded, err := os.ReadFile(filepath.Join(sshDir, "stdin.sh"))
	require.NoError(t, err)
	assert.Empty(t, string(uploaded))
}

func TestProvisionFailsWithoutPython(t *testing.T) {
	binDir := t.TempDir()
	sshDir := t.TempDir()
	writeFakeProvisionSSH(t, binDir)
	provisionTestEnv(t, sshDir)
	t.Setenv("FAKE_NO_PYTHON3", "1")

	params := Environment{ControlPath: filepath.Join(sshDir, "control"), Token: "tok123", Port: 2489}
	err := Provision(filepath.Join(binDir, "ssh"), "user@host", params)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "python3")
}

func TestProvisionFallsBackWithoutMux(t *testing.T) {
	binDir := t.TempDir()
	sshDir := t.TempDir()
	writeFakeProvisionSSH(t, binDir)
	provisionTestEnv(t, sshDir)
	t.Setenv("FAKE_VERSION", Version)
	t.Setenv("FAKE_MUX_FAIL", "1")

	params := Environment{ControlPath: filepath.Join(sshDir, "control"), Token: "tok123", Port: 2489}
	require.NoError(t, Provision(filepath.Join(binDir, "ssh"), "user@host", params))

	calls := sshCalls(t, sshDir)
	require.Len(t, calls, 2, "mux attempt plus direct fallback")
	assert.Contains(t, calls[0], "-S")
	assert.NotContains(t, calls[1], "-S")
}

func TestProvisionExecError(t *testing.T) {
	binDir := t.TempDir()
	sshDir := t.TempDir()
	writeFakeProvisionSSH(t, binDir)
	provisionTestEnv(t, sshDir)
	t.Setenv("FAKE_ALWAYS_FAIL", "1")

	params := Environment{ControlPath: filepath.Join(sshDir, "control"), Token: "tok123", Port: 2489}
	err := Provision(filepath.Join(binDir, "ssh"), "user@host", params)
	require.Error(t, err)
}

func TestShellQuote(t *testing.T) {
	assert.Equal(t, "'abc123'", ShellQuote("abc123"))
	assert.Equal(t, "'a'\\''b'", ShellQuote("a'b"))
	assert.Equal(t, "''", ShellQuote(""))
}
