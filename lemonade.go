package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const (
	defaultLemonadePort = 2489
	lemonadeStartWait   = 5 * time.Second
)

// lemonadePort is the local Lemonade TCP port. Tests may override it.
// Production lemonade always listens on ":"+port (all interfaces).
var lemonadePort = defaultLemonadePort

// lemonadeServer is a Lemonade server process started by slush.
type lemonadeServer struct {
	cmd *exec.Cmd
}

// ensureLemonadePortFree returns an error if the Lemonade port cannot be bound.
//
// Must not dial the port: lemonade's server puts every accepted connection on a
// one-slot channel and only receives from it inside RPC handlers. A plain TCP
// connect/close leaves a stale entry there and deadlocks the next real request.
func ensureLemonadePortFree() error {
	if portIsBound(lemonadePort) {
		return fmt.Errorf("lemonade already running on :%d; stop it before using slush", lemonadePort)
	}
	return nil
}

// startLemonade starts `lemonade server -allow 127.0.0.1` and waits until the
// Lemonade port is bound (without dialing it).
func startLemonade() (*lemonadeServer, error) {
	bin, err := exec.LookPath("lemonade")
	if err != nil {
		return nil, fmt.Errorf("lemonade not found on PATH: %w", err)
	}

	cmd := exec.Command(bin, "server", "-allow", "127.0.0.1", "--port="+strconv.Itoa(lemonadePort))
	// Tests use a fake binary that honors SLUSH_LEMONADE_PORT when set.
	cmd.Env = append(os.Environ(), "SLUSH_LEMONADE_PORT="+strconv.Itoa(lemonadePort))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start lemonade: %w", err)
	}

	server := &lemonadeServer{cmd: cmd}
	if err := waitForPortBound(lemonadePort, lemonadeStartWait); err != nil {
		server.Stop()
		return nil, fmt.Errorf("lemonade did not become ready: %w", err)
	}
	return server, nil
}

// Stop terminates the Lemonade server process.
func (s *lemonadeServer) Stop() {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Kill()
	_ = s.cmd.Wait()
}

// portIsBound reports whether something is listening on ":"+port by trying to
// bind that address. Binding never completes a TCP handshake, so it is safe
// against lemonade's connection-channel quirk.
func portIsBound(port int) bool {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

func waitForPortBound(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if portIsBound(port) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for :%d", port)
}
