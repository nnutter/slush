package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// runMoshSession keeps an ssh tunnel up for Lemonade and any -L/-R forwards,
// and runs mosh for the interactive session. mosh cannot carry port forwards
// itself because it tears down its bootstrap ssh connection after start.
func runMoshSession(args []string) (int, error) {
	forwards, args, err := takeSSHForwards(args)
	if err != nil {
		return 0, err
	}
	host, err := moshDestination(args)
	if err != nil {
		return 0, err
	}
	moshPath, err := exec.LookPath("mosh")
	if err != nil {
		return 0, fmt.Errorf("mosh not found on PATH: %w", err)
	}
	return runTunneledSession(moshPath, host, args, forwards, withMoshSSHControlPath)
}

// runETSession keeps an ssh tunnel up for Lemonade and any -L/-R forwards, and
// runs et for the interactive session. Forwards always use ssh, not et -t/-r.
func runETSession(args []string) (int, error) {
	forwards, args, err := takeSSHForwards(args)
	if err != nil {
		return 0, err
	}
	host, err := etDestination(args)
	if err != nil {
		return 0, err
	}
	etPath, err := exec.LookPath("et")
	if err != nil {
		return 0, fmt.Errorf("et not found on PATH: %w", err)
	}
	return runTunneledSession(etPath, sshHostFromETDestination(host), args, forwards, nil)
}

// runTunneledSession starts a background ssh ControlMaster with the given
// forwards (plus Lemonade), runs clientPath, then tears the tunnel down.
func runTunneledSession(
	clientPath, sshHost string,
	clientArgs, forwards []string,
	prepareArgs func([]string, string) []string,
) (int, error) {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return 0, fmt.Errorf("ssh not found on PATH: %w", err)
	}

	dir, err := os.MkdirTemp("", "slush-ssh-")
	if err != nil {
		return 0, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	controlPath := filepath.Join(dir, "control")

	if err := startSSHTunnel(sshPath, sshHost, controlPath, forwards); err != nil {
		return 0, err
	}
	defer stopSSHTunnel(sshPath, sshHost, controlPath)

	if prepareArgs != nil {
		clientArgs = prepareArgs(clientArgs, controlPath)
	}
	return runSession(clientPath, clientArgs)
}

// startSSHTunnel opens a background ssh master with Lemonade and any extra
// -L/-R forwards. ssh -f returns only after authentication and forward setup.
func startSSHTunnel(sshPath, host, controlPath string, forwards []string) error {
	args := []string{
		"-f",
		"-N",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ControlMaster=yes",
		"-o", "ControlPath=" + controlPath,
	}
	args = append(args, forwards...)
	args = append(args, host)
	args = withReverseTunnel(args)

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
