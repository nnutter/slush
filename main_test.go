package main

import (
	"net"
	"net/rpc"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWarnsWhenLemonadeAlreadyRunning(t *testing.T) {
	useEphemeralLemonadePort(t)

	ln, err := net.Listen("tcp", ":"+strconv.Itoa(lemonadePort))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	code := run([]string{"-G", "example.com"})
	assert.Equal(t, 1, code)
}

func TestRunEndToEndWithFakeBinaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeFakeSSH(t, binDir)
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"user@host", "true"})
	assert.Equal(t, 0, code)
	requirePortFree(t, lemonadePort)
}

func TestRunPropagatesSSHExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeFakeSSHExit(t, binDir, 42)
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"user@host"})
	assert.Equal(t, 42, code)
	requirePortFree(t, lemonadePort)
}

// TestStartLemonadeDoesNotPoisonConnCh ensures readiness probing never dials
// the lemonade port. Real lemonade accepts every TCP connection onto a one-slot
// channel and only receives from it inside RPC handlers; a connect/close leaves
// a stale entry and deadlocks the next RPC (and can panic the server).
func TestStartLemonadeDoesNotPoisonConnCh(t *testing.T) {
	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeConnChLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	server, err := startLemonade()
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	done := make(chan error, 1)
	go func() {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(lemonadePort), time.Second)
		if err != nil {
			done <- err
			return
		}
		client := rpc.NewClient(conn)
		defer client.Close()
		var unused struct{}
		done <- client.Call("Health.Ping", struct{}{}, &unused)
	}()

	select {
	case err := <-done:
		require.NoError(t, err, "RPC must succeed; hang/reset means readiness dialed the port")
	case <-time.After(3 * time.Second):
		t.Fatal("RPC hung: readiness probe likely dialed lemonade and filled connCh")
	}
}

func writeFakeSSH(t *testing.T, dir string) {
	t.Helper()
	script := `#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "-R" ]; then
    saw_r=1
    continue
  fi
  if [ -n "$saw_r" ]; then
    if [ "$arg" = "2489:127.0.0.1:2489" ]; then
      exit 0
    fi
    saw_r=
  fi
  case "$arg" in
    -R2489:127.0.0.1:2489) exit 0 ;;
  esac
done
echo "missing reverse tunnel: $*" >&2
exit 1
`
	path := filepath.Join(dir, "ssh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}

func writeFakeSSHExit(t *testing.T, dir string, code int) {
	t.Helper()
	script := "#!/bin/sh\nexit " + strconv.Itoa(code) + "\n"
	path := filepath.Join(dir, "ssh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}

// writeConnChLemonade builds a lemonade stand-in that mirrors production
// lemonade's one-slot connCh + net/rpc accept loop. Dialing without an RPC
// leaves connCh full and deadlocks the next RPC — the bug slush must avoid.
func writeConnChLemonade(t *testing.T, dir string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "connch.go")
	require.NoError(t, os.WriteFile(src, []byte(`package main

import (
	"net"
	"net/rpc"
	"os"
	"os/signal"
	"syscall"
)

var connCh = make(chan net.Conn, 1)

type Health struct{}

func (Health) Ping(_ struct{}, _ *struct{}) error {
	<-connCh
	return nil
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "server" {
		os.Exit(2)
	}
	_ = rpc.Register(Health{})
	port := os.Getenv("SLUSH_LEMONADE_PORT")
	if port == "" {
		port = "2489"
	}
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		os.Exit(1)
	}
	defer ln.Close()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			connCh <- conn
			rpc.ServeConn(conn)
		}
	}()
	<-ch
}
`), 0o644))

	out := filepath.Join(dir, "lemonade")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "build connch lemonade: %s", output)
}

func writeFakeLemonade(t *testing.T, dir string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "fakel.go")
	// Bind ":"+port like production lemonade so portIsBound detects readiness.
	require.NoError(t, os.WriteFile(src, []byte(`package main

import (
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "server" {
		os.Exit(2)
	}
	port := os.Getenv("SLUSH_LEMONADE_PORT")
	if port == "" {
		port = "2489"
	}
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		os.Exit(1)
	}
	defer ln.Close()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	<-ch
}
`), 0o644))

	out := filepath.Join(dir, "lemonade")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "build fake lemonade: %s", output)
}
