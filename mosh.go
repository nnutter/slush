package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// runMoshSession keeps an ssh reverse tunnel up for Lemonade and runs mosh for
// the interactive session. mosh cannot carry port forwards itself because it
// tears down its bootstrap ssh connection after start.
func runMoshSession(args []string) (int, error) {
	host, err := moshDestination(args)
	if err != nil {
		return 0, err
	}

	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return 0, fmt.Errorf("ssh not found on PATH: %w", err)
	}
	moshPath, err := exec.LookPath("mosh")
	if err != nil {
		return 0, fmt.Errorf("mosh not found on PATH: %w", err)
	}

	dir, err := os.MkdirTemp("", "slush-ssh-")
	if err != nil {
		return 0, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	controlPath := filepath.Join(dir, "control")

	if err := startSSHTunnel(sshPath, host, controlPath); err != nil {
		return 0, err
	}
	defer stopSSHTunnel(sshPath, host, controlPath)

	return runSession(moshPath, withMoshSSHControlPath(args, controlPath))
}

// startSSHTunnel opens a background ssh master with the Lemonade reverse
// tunnel. ssh -f returns only after authentication and forward setup succeed.
func startSSHTunnel(sshPath, host, controlPath string) error {
	args := withReverseTunnel([]string{
		"-f",
		"-N",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ControlMaster=yes",
		"-o", "ControlPath=" + controlPath,
		host,
	}, "-R", sshReverseTunnel)

	cmd := exec.Command(sshPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("start ssh tunnel: %w", err)
	}
	return nil
}

func stopSSHTunnel(sshPath, host, controlPath string) {
	cmd := exec.Command(sshPath,
		"-o", "ControlPath="+controlPath,
		"-O", "exit",
		host,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}
