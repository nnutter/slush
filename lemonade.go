package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"
)

const (
	defaultLemonadeAddr = "127.0.0.1:2489"
	lemonadeStartWait   = 5 * time.Second
	lemonadeDialTimeout = 100 * time.Millisecond
)

// lemonadeAddr is the local Lemonade listen address. Tests may override it.
var lemonadeAddr = defaultLemonadeAddr

// lemonadeServer is a Lemonade server process started by slush.
type lemonadeServer struct {
	cmd *exec.Cmd
}

// ensureLemonadePortFree returns an error if something is already
// accepting connections on the Lemonade address.
func ensureLemonadePortFree() error {
	if !isAcceptingConnections(lemonadeAddr) {
		return nil
	}
	return fmt.Errorf("lemonade already running on %s; stop it before using slush", lemonadeAddr)
}

// startLemonade starts `lemonade server -allow 127.0.0.1` and waits until
// it accepts connections on lemonadeAddr.
func startLemonade() (*lemonadeServer, error) {
	bin, err := exec.LookPath("lemonade")
	if err != nil {
		return nil, fmt.Errorf("lemonade not found on PATH: %w", err)
	}

	cmd := exec.Command(bin, "server", "-allow", "127.0.0.1")
	// Production lemonade always binds 2489; tests use a fake binary that
	// honors SLUSH_LEMONADE_ADDR when set.
	cmd.Env = append(os.Environ(), "SLUSH_LEMONADE_ADDR="+lemonadeAddr)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start lemonade: %w", err)
	}

	server := &lemonadeServer{cmd: cmd}
	if err := waitForAcceptingConnections(lemonadeAddr, lemonadeStartWait); err != nil {
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

func isAcceptingConnections(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, lemonadeDialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func waitForAcceptingConnections(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isAcceptingConnections(addr) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", addr)
}
