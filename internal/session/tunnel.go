package session

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nnutter/slush/internal/protocol"
	"github.com/nnutter/slush/internal/remote"
)

const sshTunnelReadyWait = 30 * time.Second

// clipboardForwardAttempts bounds retries when the remote loopback port
// chosen for this session is already taken.
const clipboardForwardAttempts = 8

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

// sessionParams carries per-session values into client arg preparation
// and remote provisioning.
type sessionParams struct {
	controlPath string
	token       string
	agentSocket string
	remotePort  int
}

// establishSession starts the tunnel master and provisions the remote
// shims, returning teardown and the session parameters. Callers
// run the interactive client (sessions) or probes (validate) over it.
// connOpts are ssh connection options (port, identity, ...) for the
// master; port forwards travel separately in forwards.
func establishSession(sshPath, sshHost string, options sessionOptions, token string, server *protocol.Server) (func(), sessionParams, error) {
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
	if err := bindClipboardForward(tunnel, server); err != nil {
		tunnel.Stop()
		_ = os.RemoveAll(dir)
		return nil, sessionParams{}, err
	}
	params := sessionParams{controlPath: controlPath, token: token, remotePort: server.Port()}
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

	if err := remote.Provision(sshPath, sshHost, remote.Environment{ControlPath: params.controlPath, Token: params.token, Port: params.remotePort}); err != nil {
		fmt.Fprintf(os.Stderr, "slush: clipboard provisioning: %v (continuing without clipboard forwarding)\n", err)
	}
	return teardown, params, nil
}

// startSSHTunnel opens an ssh master child with the session's -L/-R
// forwards, waiting until the control socket exists (forwards and auth OK).
// The clipboard reverse forward is added after the master is up.
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

// Stop ends the master via ControlMaster and kills the child if needed.
func (t *sshTunnel) Stop() {
	if t == nil {
		return
	}
	stopSSHTunnel(t.sshPath, t.host, t.controlPath)
	if t.hasProcess() {
		_ = t.cmd.Process.Kill()
	}
	_ = t.wait()
}

func (t *sshTunnel) checkMaster() error {
	cmd := exec.Command(t.sshPath,
		"-o", "ControlPath="+t.controlPath,
		"-O", "check",
		t.host,
	)
	return cmd.Run()
}

func (t *sshTunnel) hasProcess() bool {
	if t.cmd == nil {
		return false
	}
	return t.cmd.Process != nil
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

// bindClipboardForward publishes server's port on the remote loopback.
// The same number is used at both ends so SLUSH_PORT names the listener
// the shim dials and the server that accepts the forwarded connection.
// A busy remote port allocates a new local port and tries again.
func bindClipboardForward(tunnel *sshTunnel, server *protocol.Server) error {
	var last error
	for range clipboardForwardAttempts {
		err := tunnel.forwardClipboard(server.Port())
		if err == nil {
			return nil
		}
		last = err
		if !remoteForwardBusy(err) {
			return err
		}
		if err := server.Relisten(); err != nil {
			return err
		}
	}
	return fmt.Errorf("choose a free clipboard port: %w", last)
}

func (t *sshTunnel) forwardClipboard(port int) error {
	spec := fmt.Sprintf("%d:127.0.0.1:%d", port, port)
	cmd := exec.Command(t.sshPath,
		"-o", "ControlPath="+t.controlPath,
		"-o", "ControlMaster=no",
		"-O", "forward",
		"-R", spec,
		t.host,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return fmt.Errorf("open clipboard forward: %w", err)
		}
		return fmt.Errorf("open clipboard forward: %s", detail)
	}
	return nil
}

// remoteForwardBusy reports OpenSSH's refusal to bind the requested
// remote listen port. Other failures, such as a dead master, must not
// be retried with a new port.
func remoteForwardBusy(err error) bool {
	return err != nil && strings.Contains(err.Error(), "remote port forwarding failed")
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
