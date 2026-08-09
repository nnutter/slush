package main

import (
	"net"
	"strconv"
	"testing"
)

// useEphemeralLemonadePort points lemonadePort at a free local port for the
// duration of the test so concurrent/local lemonade instances do not conflict.
func useEphemeralLemonadePort(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		_ = ln.Close()
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		_ = ln.Close()
		t.Fatalf("parse port: %v", err)
	}
	_ = ln.Close()

	previous := lemonadePort
	lemonadePort = port
	t.Cleanup(func() { lemonadePort = previous })
}
