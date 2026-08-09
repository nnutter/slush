//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

// runSSH runs ssh with stdio attached. Windows lacks the Unix TTY process-group
// handoff used for near-transparent interactive sessions.
func runSSH(sshPath string, args []string) (int, error) {
	cmd := exec.Command(sshPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start ssh: %w", err)
	}
	return exitCode(cmd.Wait()), nil
}
