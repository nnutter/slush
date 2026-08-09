package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if err := ensureLemonadePortFree(); err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}

	server, err := startLemonade()
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}
	defer server.Stop()

	mode, args, err := takeModeFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}

	code, err := runClient(mode, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}
	return code
}

func runClient(mode clientMode, args []string) (int, error) {
	switch mode {
	case modeMosh:
		return runMoshSession(args)
	case modeET:
		return runETSession(args)
	default:
		return runSSHSession(args)
	}
}

func runSSHSession(args []string) (int, error) {
	binPath, err := exec.LookPath("ssh")
	if err != nil {
		return 0, fmt.Errorf("ssh not found on PATH: %w", err)
	}
	// -L/-R stay in args for ssh; only Lemonade is injected when missing.
	return runSession(binPath, withReverseTunnel(args))
}
