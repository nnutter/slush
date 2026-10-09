package command

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nnutter/slush/internal/remote"
)

func TestTransportSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("subprocess fixtures require a POSIX shell")
	}
	for _, test := range []struct {
		name, mode, wantClient, wantError, probePrefix string
		localMosh, remoteMosh                          bool
		probeExit, moshExit, wantExit                  int
	}{
		{name: "default prefers Mosh", localMosh: true, remoteMosh: true, wantClient: "mosh"},
		{name: "both available", mode: "auto", localMosh: true, remoteMosh: true, wantClient: "mosh"},
		{name: "local missing", mode: "auto", remoteMosh: true, wantClient: "ssh"},
		{name: "remote missing", mode: "auto", localMosh: true, wantClient: "ssh"},
		{name: "unexpected probe output", mode: "auto", localMosh: true, remoteMosh: true, probePrefix: "login notice\n", wantExit: 1, wantError: "unexpected remote Mosh availability response"},
		{name: "probe connection failure", mode: "auto", localMosh: true, probeExit: 255, wantExit: 1, wantError: "detect remote Mosh availability"},
		{name: "SSH skips detection", mode: "ssh", localMosh: true, remoteMosh: true, probeExit: 255, wantClient: "ssh"},
		{name: "forced Mosh skips detection", mode: "mosh", localMosh: true, probeExit: 255, wantClient: "mosh", moshExit: 42, wantExit: 42},
		{name: "Mosh failure never retries", mode: "auto", localMosh: true, remoteMosh: true, wantClient: "mosh", moshExit: 42, wantExit: 42},
	} {
		t.Run(test.name, func(t *testing.T) {
			useEphemeralClipboardPort(t)
			bin := t.TempDir()
			remoteBin := t.TempDir()
			if test.remoteMosh {
				writeFakeClipTool(t, remoteBin, "mosh-server", "exit 0")
			}
			require.NoError(t, os.Symlink("/bin/cat", filepath.Join(bin, "cat")))
			clientLog := filepath.Join(t.TempDir(), "clients")
			writeFakeSSHSession(t, bin, fakeSSHSession{})
			sshSession := filepath.Join(bin, "ssh-session")
			require.NoError(t, os.Rename(filepath.Join(bin, "ssh"), sshSession))
			// Execute the availability command in a separate remote PATH.
			// The shell, not the fixture, determines whether the server exists.
			writeFakeClipTool(t, bin, "ssh", `case "$*" in
  *BatchMode=yes*)
    case "$*" in
      *mosh-server*)
    if [ `+strconv.Itoa(test.probeExit)+` -ne 0 ]; then exit `+strconv.Itoa(test.probeExit)+`; fi
    for command in "$@"; do :; done
    printf '%s' `+remote.ShellQuote(test.probePrefix)+`
    PATH=`+remote.ShellQuote(remoteBin)+` /bin/sh -c "$command"
    exit $? ;;
    esac
    exec `+remote.ShellQuote(sshSession)+` "$@" ;;
  *SLUSH=1*) printf 'ssh\n' >> `+remote.ShellQuote(clientLog)+` ;;
esac
exec `+remote.ShellQuote(sshSession)+` "$@"`)
			if test.localMosh {
				writeFakeClipTool(t, bin, "mosh", "printf 'mosh\\n' >> "+remote.ShellQuote(clientLog)+"\nexit "+strconv.Itoa(test.moshExit))
			}
			t.Setenv("PATH", bin)
			args := []string{"user@host", "true"}
			if test.mode != "" {
				args = append([]string{"--transport", test.mode}, args...)
			}
			err := Execute(args)
			assert.Equal(t, test.wantExit, ExitCode(err))
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else if test.wantExit == 0 {
				require.NoError(t, err)
			}
			clients, readErr := os.ReadFile(clientLog)
			if test.wantClient == "" {
				require.ErrorIs(t, readErr, os.ErrNotExist)
			} else {
				require.NoError(t, readErr)
				assert.Equal(t, test.wantClient+"\n", string(clients))
			}
			requirePortFree(t, clipboardPort)
		})
	}
}
