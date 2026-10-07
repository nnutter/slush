package process

import (
	"errors"
	"os"
	"os/exec"

	"golang.org/x/term"
)

// StdinIsTerminal reports whether slush's stdin is a terminal.
func StdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}
