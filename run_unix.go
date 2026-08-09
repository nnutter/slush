//go:build unix

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// runSession runs the remote client (ssh or et) with the given args, attaching
// the current terminal as completely as possible while remaining the parent so
// callers can clean up after Wait returns.
func runSession(binPath string, args []string) (int, error) {
	cmd := exec.Command(binPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdinFD := int(os.Stdin.Fd())
	tty := term.IsTerminal(stdinFD)

	var previousForeground int
	if tty {
		// Avoid stopping ourselves when moving the terminal's foreground group.
		signal.Ignore(syscall.SIGTTIN, syscall.SIGTTOU, syscall.SIGTSTP)

		var err error
		previousForeground, err = terminalForegroundPgid(stdinFD)
		if err != nil {
			return 0, fmt.Errorf("get terminal foreground process group: %w", err)
		}
	}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start %s: %w", binPath, err)
	}

	if tty {
		if err := setTerminalForegroundPgid(stdinFD, cmd.Process.Pid); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return 0, fmt.Errorf("set terminal foreground process group: %w", err)
		}
		defer func() {
			_ = setTerminalForegroundPgid(stdinFD, previousForeground)
		}()
	}

	stopForwarding := forwardSignals(cmd.Process)
	defer stopForwarding()

	return exitCode(cmd.Wait()), nil
}

func terminalForegroundPgid(fd int) (int, error) {
	return unix.IoctlGetInt(fd, unix.TIOCGPGRP)
}

func setTerminalForegroundPgid(fd int, pgid int) error {
	return unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, pgid)
}

// forwardSignals sends terminal- and lifecycle-related signals to the child
// process. Returns a stop function.
func forwardSignals(proc *os.Process) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGWINCH)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-ch:
				_ = proc.Signal(sig)
			case <-done:
				return
			}
		}
	}()

	return func() {
		signal.Stop(ch)
		close(done)
	}
}
