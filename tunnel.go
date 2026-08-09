package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const sshTunnelReadyWait = 30 * time.Second

// sshTunnel is a background ssh ControlMaster held as a child process (not
// ssh -f) so it cannot outlive slush and leave remote listen ports stuck.
type sshTunnel struct {
	cmd         *exec.Cmd
	waitCh      chan error
	waitOnce    sync.Once
	waitErr     error
	sshPath     string
	host        string
	controlPath string
}

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

	tunnel, err := startSSHTunnel(sshPath, sshHost, controlPath, forwards)
	if err != nil {
		return 0, err
	}
	defer tunnel.Stop()

	if prepareArgs != nil {
		clientArgs = prepareArgs(clientArgs, controlPath)
	}
	return runSession(clientPath, clientArgs)
}

// startSSHTunnel opens an ssh master child with Lemonade and any extra -L/-R
// forwards, waiting until the control socket exists (forwards and auth OK).
func startSSHTunnel(sshPath, host, controlPath string, forwards []string) (*sshTunnel, error) {
	args := []string{
		"-N",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ControlMaster=yes",
		"-o", "ControlPath=" + controlPath,
		"-o", "ControlPersist=no",
	}
	args = append(args, forwards...)
	args = append(args, host)
	args = withReverseTunnel(args)

	cmd := exec.Command(sshPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ssh tunnel: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	tunnel := &sshTunnel{
		cmd:         cmd,
		waitCh:      waitCh,
		sshPath:     sshPath,
		host:        host,
		controlPath: controlPath,
	}
	if err := tunnel.waitUntilReady(sshTunnelReadyWait); err != nil {
		tunnel.Stop()
		return nil, err
	}
	return tunnel, nil
}

func (t *sshTunnel) waitUntilReady(timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-t.waitCh:
			t.noteWait(err)
			if err != nil {
				return fmt.Errorf("start ssh tunnel: %w", err)
			}
			return fmt.Errorf("start ssh tunnel: ssh exited before becoming ready")
		case <-deadline:
			return fmt.Errorf("start ssh tunnel: timed out waiting for control socket")
		case <-ticker.C:
			// -O check succeeds only after the master is up; with
			// ExitOnForwardFailure that includes forward setup.
			if t.checkMaster() == nil {
				return nil
			}
		}
	}
}

func (t *sshTunnel) checkMaster() error {
	cmd := exec.Command(t.sshPath,
		"-o", "ControlPath="+t.controlPath,
		"-O", "check",
		t.host,
	)
	return cmd.Run()
}

func (t *sshTunnel) noteWait(err error) {
	t.waitOnce.Do(func() {
		t.waitErr = err
	})
}

func (t *sshTunnel) wait() error {
	t.waitOnce.Do(func() {
		t.waitErr = <-t.waitCh
	})
	return t.waitErr
}

// Stop ends the master via ControlMaster and kills the child if needed.
func (t *sshTunnel) Stop() {
	if t == nil {
		return
	}
	stopSSHTunnel(t.sshPath, t.host, t.controlPath)
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	_ = t.wait()
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
