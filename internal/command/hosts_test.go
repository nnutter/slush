package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostCompletion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// No completion request may invoke SSH or a Match exec command.
	t.Setenv("PATH", t.TempDir())
	sshDir := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(filepath.Join(sshDir, "conf.d"), 0o700))
	files := map[string]string{
		"config": `# Host lab-comment
Host lab-main lab-shared *.example !lab-excluded lab-?
HostName lab-not-an-alias
Include "conf.d/*.conf" missing.conf
Match exec "must-not-run"
Include ~/extra.conf
Host="lab-equals"
hOsT = "lab-spaced"
Host =lab-attached
Host "unterminated
`,
		"conf.d/aliases.conf": `Host "lab-included" # Host lab-comment
Include config
Include "~/with space.conf"
`,
		"known_hosts": `# ignored
lab-known,lab-shared ssh-ed25519 KEY
[lab-port]:2222 ssh-ed25519 KEY
[2001:db8::1]:2222 ssh-ed25519 KEY
192.0.2.1 ssh-ed25519 KEY
|1|HASH|HASH ssh-ed25519 KEY
@cert-authority lab-ca,*.example ssh-ed25519 KEY
@revoked lab-revoked ssh-ed25519 KEY
malformed
`,
	}
	for path, contents := range files {
		require.NoError(t, os.WriteFile(filepath.Join(sshDir, path), []byte(contents), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(home, "extra.conf"), []byte("Host lab-extra\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, "with space.conf"), []byte("Host lab-space\n"), 0o600))
	customConfig := filepath.Join(home, "custom.conf")
	require.NoError(t, os.WriteFile(customConfig, []byte("Host lab-custom\nInclude custom-aliases\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "custom-aliases"), []byte("Host lab-custom-included\n"), 0o600))

	all := []string{"lab-attached", "lab-ca", "lab-equals", "lab-extra", "lab-included", "lab-known", "lab-main", "lab-port", "lab-shared", "lab-space", "lab-spaced"}
	for _, test := range []struct {
		name string
		args []string
		want []string
	}{
		{"session", []string{"lab"}, all},
		{"validate", []string{"validate", "lab"}, all},
		{"prefix", []string{"lab-k"}, []string{"lab-known"}},
		{"username", []string{"alice@lab-k"}, []string{"alice@lab-known"}},
		{"IPv6", []string{"2001:"}, []string{"2001:db8::1"}},
		{"IP", []string{"192."}, []string{"192.0.2.1"}},
		{"custom config", []string{"-F", customConfig, "lab"}, []string{"lab-ca", "lab-custom", "lab-custom-included", "lab-known", "lab-port", "lab-shared"}},
		{"validate custom config", []string{"validate", "-F", customConfig, "lab-c"}, []string{"lab-ca", "lab-custom", "lab-custom-included"}},
		{"missing config", []string{"-F", filepath.Join(home, "missing"), "lab"}, []string{"lab-ca", "lab-known", "lab-port", "lab-shared"}},
		{"remote command", []string{"lab-main", "lab"}, nil},
		{"remote flag", []string{"lab-main", "-"}, nil},
		{"validation extra argument", []string{"validate", "lab-main", "lab"}, nil},
		{"no match", []string{"unknown"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := newCommand()
			var out, diagnostics bytes.Buffer
			command.SetOut(&out)
			command.SetErr(&diagnostics)
			command.SetArgs(append([]string{"__completeNoDesc"}, test.args...))
			require.NoError(t, command.Execute())
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			require.NotEmpty(t, lines)
			assert.Equal(t, ":4", lines[len(lines)-1], "disable filename completion")
			assert.Equal(t, strings.Join(test.want, "\n"), strings.Join(lines[:len(lines)-1], "\n"))
		})
	}
}

func TestHostCompletionWithoutSSHFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	command := newCommand()
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"__completeNoDesc", "validate", ""})
	require.NoError(t, command.Execute())
	assert.Equal(t, ":4\n", out.String())
}
