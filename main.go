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
	if mode == modeMosh {
		return runMoshSession(args)
	}

	binName := clientBinary(mode)
	binPath, err := exec.LookPath(binName)
	if err != nil {
		return 0, fmt.Errorf("%s not found on PATH: %w", binName, err)
	}
	return runSession(binPath, withReverseTunnel(args, reverseTunnelFlag(mode), reverseTunnelArg(mode)))
}
