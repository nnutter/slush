package command

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture executes a POSIX shell script")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	bin := t.TempDir()
	home := t.TempDir()
	log := filepath.Join(t.TempDir(), "ssh-options")
	t.Setenv("HOME", home)
	t.Setenv("INSTALL_LOG", log)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Record the SSH connection arguments, then execute the remote script.
	script := `#!/bin/sh
while [ "$#" -gt 1 ]; do
  printf '%s\n' "$1" >> "$INSTALL_LOG"
  shift
done
exec /bin/sh -c "$1"
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o755))
	identity := filepath.Join(t.TempDir(), "identity")
	config := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(identity, nil, 0o600))
	require.NoError(t, os.WriteFile(config, nil, 0o600))
	command := newCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"install", "-p", "2222", "-i", identity, "-F", config, "user@host"})
	require.NoError(t, command.Execute())
	assert.Contains(t, output.String(), "Installed slush wrappers")
	assert.Contains(t, output.String(), "~/.slush/bin")
	assert.Contains(t, output.String(), "PATH")
	args, err := os.ReadFile(log)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"-p", "2222", "-i", identity, "-F", config,
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "user@host",
	}, strings.Split(strings.TrimSpace(string(args)), "\n"))
	cmd := exec.CommandContext(t.Context(), filepath.Join(home, ".slush", "bin", "xdg-open"), "--dry-run", "https://example.com")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	assert.Equal(t, "OPEN https://example.com\n", string(out))
	_, err = os.Stat(filepath.Join(home, ".cache", "slush", "slush-env"))
	require.True(t, os.IsNotExist(err), "install must not create session credentials")
}
