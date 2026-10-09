package command

import (
	"bytes"
	"testing"

	"charm.land/fang/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandRejectsInvalidOptions(t *testing.T) {
	for _, test := range []struct {
		name   string
		args   []string
		detail string
	}{
		{"missing host", nil, "missing host"},
		{"unknown option", []string{"--ssh-option", "host"}, "unknown flag"},
		{"unsupported transport", []string{"--transport", "et", "host"}, "choose ssh or mosh"},
		{"invalid port", []string{"-p", "65536", "host"}, "between 1 and 65535"},
		{"bad forward", []string{"-L", "8080:db:nope", "host"}, "destination port"},
		{"missing forward", []string{"-R"}, "needs an argument"},
		{"removed mosh shorthand", []string{"--mosh", "host"}, "unknown flag"},
		{"missing identity", []string{"-i", "does-not-exist", "host"}, "file"},
		{"validate remote command", []string{"validate", "host", "command"}, "one host"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := newCommand()
			command.SetArgs(test.args)
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SilenceUsage, command.SilenceErrors = true, true
			require.ErrorContains(t, command.Execute(), test.detail)
		})
	}
}

func TestCommandHelpDoesNotStartSession(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("NO_COLOR", "1")
	for _, args := range [][]string{{"--help"}, {"validate", "--help"}} {
		command := newCommand()
		var out bytes.Buffer
		command.SetOut(&out)
		command.SetErr(&out)
		command.SetArgs(args)
		require.NoError(t, fang.Execute(t.Context(), command))
		assert.Contains(t, out.String(), "--transport")
		assert.NotContains(t, out.String(), "Ssh")
		assert.Regexp(t, `--local-forward\s+Make a remote TCP port available locally`, out.String())
		assert.Regexp(t, `--remote-forward\s+Make a local TCP port available on the remote`, out.String())
	}
}
