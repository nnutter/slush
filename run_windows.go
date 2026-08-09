//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

// runSession runs the remote client with stdio attached. Windows lacks the Unix
// TTY process-group handoff used for near-transparent interactive sessions.
func runSession(binPath string, args []string) (int, error) {
	cmd := exec.Command(binPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start %s: %w", binPath, err)
	}
	return exitCode(cmd.Wait()), nil
}
