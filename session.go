package main

import (
	"fmt"
	"os/exec"

	"github.com/nnutter/slush/internal/clipboard"
	"github.com/nnutter/slush/internal/process"
)

var clipboardPort = clipboard.DefaultPort

func runClientWithOptions(options sessionOptions, token string) (int, error) {
	if options.mode == modeMosh {
		return runMoshSession(options, token)
	}
	return runSSHSession(options, token)
}

func runSSHSession(options sessionOptions, token string) (int, error) {
	rest := options.args
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
	if interactive && process.StdinIsTerminal() {
		clientArgs = append([]string{"-t"}, clientArgs...)
	}
	return runTunneledSession(sshPath, host, options.connOpts, clientArgs, options.forwards, token, withSSHControlPath)
}
