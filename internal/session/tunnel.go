package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nnutter/slush/internal/process"
	"github.com/nnutter/slush/internal/remote"
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

// runMoshSession keeps an ssh tunnel up for clipboard and any -L/-R forwards,
// and runs mosh for the interactive session. mosh cannot carry port forwards
// itself because it tears down its bootstrap ssh connection after start.
func runMoshSession(options sessionOptions, token string) (int, error) {
	args := options.args
	host, err := moshDestination(args)
	if err != nil {
		return 0, err
	}
	moshPath, err := exec.LookPath("mosh")
	if err != nil {
		return 0, fmt.Errorf("mosh not found on PATH: %w", err)
	}
	prepare := func(args []string, params sessionParams) ([]string, error) {
		clientArgs, err := withRemoteEnvMosh(args, params)
		if err != nil {
			return nil, err
		}
		quoted := make([]string, len(options.connOpts))
		for i, option := range options.connOpts {
			quoted[i] = remote.ShellQuote(option)
		}
		// Mosh's default IP discovery disables multiplexing with -S none.
		// Its short-lived bootstrap must not create a second agent socket.
		clientArgs = append([]string{"--no-ssh-pty", "--ssh=ssh -oForwardAgent=no " + strings.Join(quoted, " ")}, clientArgs...)
		return withMoshSSHControlPath(clientArgs, params), nil
	}
	return runTunneledSession(moshPath, host, options, args, token, prepare)
}

// sessionParams carries per-session values into client arg preparation
// and remote provisioning.
type sessionParams struct {
	controlPath string
	token       string
	agentSocket string
}

// runTunneledSession starts a background ssh ControlMaster with the given
// forwards (plus clipboard), provisions the remote clipboard shims, runs
// clientPath, then tears the tunnel down. Provisioning failures degrade
// to a plain session with a warning; validate (not the session) is
// where clipboard forwarding is enforced.
func runTunneledSession(
	clientPath, sshHost string,
	options sessionOptions, clientArgs []string,
	token string,
	prepareArgs func([]string, sessionParams) ([]string, error),
) (int, error) {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return 0, fmt.Errorf("ssh not found on PATH: %w", err)
	}
	teardown, params, err := establishSession(sshPath, sshHost, options, token)
	if err != nil {
		return 0, err
	}
	defer teardown()

	if prepareArgs != nil {
		clientArgs, err = prepareArgs(clientArgs, params)
		if err != nil {
			return 0, err
		}
	}
	return process.Run(clientPath, clientArgs, clientPath == sshPath)
}

// establishSession starts the tunnel master and provisions the remote
// shims, returning teardown and the session parameters. Callers
// run the interactive client (sessions) or probes (validate) over it.
// connOpts are ssh connection options (port, identity, ...) for the
// master; port forwards travel separately in forwards.
func establishSession(sshPath, sshHost string, options sessionOptions, token string) (func(), sessionParams, error) {
	dir, err := os.MkdirTemp("", "slush-ssh-")
	if err != nil {
		return nil, sessionParams{}, fmt.Errorf("create temp dir: %w", err)
	}
	controlPath := filepath.Join(dir, "control")

	tunnel, err := startSSHTunnel(sshPath, sshHost, controlPath, options.connOpts, options.forwards)
	if err != nil {
		// Cleanup must not replace the setup error.
		_ = os.RemoveAll(dir)
		return nil, sessionParams{}, err
	}
	params := sessionParams{controlPath: controlPath, token: token}
	if options.forwardAgent {
		params.agentSocket, err = checkRemoteAgent(sshPath, sshHost, controlPath)
		if err != nil {
			tunnel.Stop()
			_ = os.RemoveAll(dir)
			return nil, sessionParams{}, err
		}
	}
	teardown := func() {
		tunnel.Stop()
		// Best-effort directory cleanup must not change the client exit status.
		_ = os.RemoveAll(dir)
	}

	if err := remote.Provision(sshPath, sshHost, remote.Environment{ControlPath: params.controlPath, Token: params.token, Port: options.protocolPort}); err != nil {
		fmt.Fprintf(os.Stderr, "slush: clipboard provisioning: %v (continuing without clipboard forwarding)\n", err)
	}
	return teardown, params, nil
}

// startSSHTunnel opens an ssh master child with clipboard and any extra -L/-R
// forwards, waiting until the control socket exists (forwards and auth OK).
func startSSHTunnel(sshPath, host, controlPath string, connOpts, forwards []string) (*sshTunnel, error) {
	args := []string{
		"-N",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ControlMaster=yes",
		"-o", "ControlPath=" + controlPath,
		"-o", "ControlPersist=no",
	}
	args = append(args, connOpts...)
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
