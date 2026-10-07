package command

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/nnutter/slush/internal/remote"
)

func checkLocalAgent() error {
	path, err := exec.LookPath("ssh-add")
	if err != nil {
		return fmt.Errorf("--forward-agent needs ssh-add to check the local SSH agent: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, "-l").CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("local SSH agent check timed out")
	}
	if err == nil {
		return nil
	}
	// ssh-add returns 1 for an accessible agent with no identities. Forwarding
	// that agent is valid: the user can load keys while the session is running.
	if status, ok := errors.AsType[*exec.ExitError](err); ok && status.ExitCode() == 1 {
		return nil
	}
	return fmt.Errorf("local SSH agent is unavailable: %s; start ssh-agent and check SSH_AUTH_SOCK", strings.TrimSpace(string(output)))
}

// checkRemoteAgent creates the agent listener through the held ControlMaster.
// OpenSSH owns this listener for the SSH connection, not the command channel.
// It therefore remains usable after this check and mosh bootstrap both exit.
func checkRemoteAgent(sshPath, host, controlPath string) (string, error) {
	output, err := remote.SSHExec(sshPath, host, `test -S "$SSH_AUTH_SOCK" && printf '%s' "$SSH_AUTH_SOCK"`, nil,
		[]string{"-A", "-o", "ControlMaster=no", "-S", controlPath})
	if err != nil {
		return "", fmt.Errorf("remote SSH agent is unavailable; check sshd AllowAgentForwarding: %w", err)
	}
	path := strings.TrimSpace(output)
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\r\n") {
		return "", fmt.Errorf("remote SSH agent returned an invalid socket path %q", path)
	}
	return path, nil
}
