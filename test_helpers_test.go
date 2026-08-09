package main

import (
	"net"
	"testing"
)

// useEphemeralLemonadeAddr points lemonadeAddr at a free local port for the
// duration of the test so concurrent/local lemonade instances do not conflict.
func useEphemeralLemonadeAddr(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	previous := lemonadeAddr
	lemonadeAddr = addr
	t.Cleanup(func() { lemonadeAddr = previous })
}
