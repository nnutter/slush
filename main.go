package main

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/term"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	mode, args, err := takeModeFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}

	// `slush validate` (and `slush --mosh validate ...`) runs probes
	// instead of a session. A host literally named validate still
	// works via `slush -- validate`.
	if len(args) > 0 && args[0] == "validate" {
		return runValidate(mode, args[1:])
	}

	token, err := generateClipboardToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}

	if err := ensureClipboardPortFree(); err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}

	server, err := startClipboardServer(token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}
	defer server.Stop()

	code, err := runClient(mode, args, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}
	return code
}

func runClient(mode clientMode, args []string, token string) (int, error) {
	switch mode {
	case modeMosh:
		return runMoshSession(args, token)
	case modeET:
		return runETSession(args, token)
	default:
		return runSSHSession(args, token)
	}
}

func runSSHSession(args []string, token string) (int, error) {
	forwards, rest, err := takeSSHForwards(args)
	if err != nil {
		return 0, err
	}
	host, err := sshHostOperand(rest)
	if err != nil {
		return 0, err
	}
	clientArgs, interactive, err := withRemoteEnvSSH(rest, token)
	if err != nil {
		return 0, err
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return 0, fmt.Errorf("ssh not found on PATH: %w", err)
	}
	if interactive && stdinIsTerminal() {
		// A wrapped command is always present now, so ssh would
		// skip PTY allocation on its own; restore it for shells.
		clientArgs = append([]string{"-t"}, clientArgs...)
	}
	// Like --et/--mosh: forwards ride a held ControlMaster child that
	// cannot outlive slush, and the interactive client reuses it. An
	// explicit master (not auto) keeps concurrent sessions from
	// stealing each other's forwards.
	return runTunneledSession(sshPath, host, clientArgs, forwards, token, withSSHControlPath)
}

// stdinIsTerminal reports whether slush's stdin is a terminal.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
