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

	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: ssh not found on PATH: %v\n", err)
		return 1
	}

	code, err := runSession(sshPath, withReverseTunnel(args))
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}
	return code
}
