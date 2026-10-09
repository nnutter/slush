//go:build windows

package process

import (
	"fmt"
	"os"
	"os/exec"
)

// Run runs the remote client (ssh, mosh, or et) with stdio attached. Windows
// lacks the Unix TTY process-group handoff used for near-transparent
// interactive sessions.
func Run(binPath string, args []string, _ bool) (int, error) {
	cmd := exec.Command(binPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start %s: %w", binPath, err)
	}
	return exitCode(cmd.Wait()), nil
}
