package main

import (
	"net"
	"net/rpc"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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

func TestRunEndToEndSSHWithLocalForward(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeFakeSSHExpecting(t, binDir, "-R", sshReverseTunnel, "-L", "8080:127.0.0.1:8080")
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"-L", "8080:127.0.0.1:8080", "user@host", "true"})
	assert.Equal(t, 0, code)
	requirePortFree(t, lemonadePort)
}

func TestRunEndToEndWithET(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeFakeSSHTunnel(t, binDir)
	writeFakeET(t, binDir)
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"--et", "user@host"})
	assert.Equal(t, 0, code)
	requirePortFree(t, lemonadePort)
}

func TestRunEndToEndETWithLocalForward(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeFakeSSHTunnel(t, binDir, "-L", "8080:127.0.0.1:8080")
	writeFakeET(t, binDir)
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"--et", "-L", "8080:127.0.0.1:8080", "user@host"})
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

func TestRunETNotFound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeFakeSSHTunnel(t, binDir)
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"--et", "user@host"})
	assert.Equal(t, 1, code)
	requirePortFree(t, lemonadePort)
}

func TestRunEndToEndWithMosh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeFakeSSHTunnel(t, binDir)
	writeFakeMosh(t, binDir)
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"--mosh", "user@host"})
	assert.Equal(t, 0, code)
	requirePortFree(t, lemonadePort)
}

func TestRunEndToEndMoshWithLocalForward(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeFakeSSHTunnel(t, binDir, "-L", "8080:127.0.0.1:8080")
	writeFakeMosh(t, binDir)
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"--mosh", "-L", "8080:127.0.0.1:8080", "user@host"})
	assert.Equal(t, 0, code)
	requirePortFree(t, lemonadePort)
}

func TestRunMoshNotFound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeFakeSSHTunnel(t, binDir)
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"--mosh", "user@host"})
	assert.Equal(t, 1, code)
	requirePortFree(t, lemonadePort)
}

func TestRunMoshMissingHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeFakeSSHTunnel(t, binDir)
	writeFakeMosh(t, binDir)
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"--mosh", "-p", "60001"})
	assert.Equal(t, 1, code)
	requirePortFree(t, lemonadePort)
}

func TestRunModeFlagsMutuallyExclusive(t *testing.T) {
	useEphemeralLemonadePort(t)

	binDir := t.TempDir()
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"--et", "--mosh", "user@host"})
	assert.Equal(t, 1, code)
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
	writeFakeSSHExpecting(t, dir, "-R", sshReverseTunnel)
}

func writeFakeSSHExpecting(t *testing.T, dir string, required ...string) {
	t.Helper()
	if len(required)%2 != 0 {
		t.Fatalf("required forwards must be flag/spec pairs, got %v", required)
	}

	var checks strings.Builder
	for i := 0; i < len(required); i += 2 {
		flag := required[i]
		spec := required[i+1]
		checks.WriteString(`
flag='` + flag + `'
spec='` + spec + `'
saw=
prev=
for arg in "$@"; do
  if [ "$prev" = "$flag" ] && [ "$arg" = "$spec" ]; then
    saw=1
    break
  fi
  prev=
  case "$arg" in
    "$flag") prev="$flag" ;;
    ${flag}${spec}) saw=1; break ;;
  esac
done
if [ -z "$saw" ]; then
  echo "missing $flag $spec: $*" >&2
  exit 1
fi
`)
	}

	script := "#!/bin/sh\n" + checks.String() + "exit 0\n"
	path := filepath.Join(dir, "ssh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}

func writeFakeSSHTunnel(t *testing.T, dir string, extraForwards ...string) {
	t.Helper()
	if len(extraForwards)%2 != 0 {
		t.Fatalf("extra forwards must be flag/spec pairs, got %v", extraForwards)
	}

	var extraChecks strings.Builder
	for i := 0; i < len(extraForwards); i += 2 {
		flag := extraForwards[i]
		spec := extraForwards[i+1]
		extraChecks.WriteString(`
saw_extra=
prev=
for arg in "$@"; do
  if [ "$prev" = "` + flag + `" ] && [ "$arg" = "` + spec + `" ]; then
    saw_extra=1
    break
  fi
  prev=
  case "$arg" in
    "` + flag + `") prev="` + flag + `" ;;
    ` + flag + spec + `) saw_extra=1; break ;;
  esac
done
if [ -z "$saw_extra" ]; then
  echo "missing tunnel forward ` + flag + ` ` + spec + `: $*" >&2
  exit 1
fi
`)
	}

	script := `#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "-O" ]; then
    exit 0
  fi
done

saw_f=
saw_n=
saw_tunnel=
controlpath=
prev=
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    case "$arg" in
      ControlPath=*) controlpath=${arg#ControlPath=} ;;
    esac
    prev=
    continue
  fi
  if [ "$prev" = "-R" ]; then
    if [ "$arg" = "2489:127.0.0.1:2489" ]; then
      saw_tunnel=1
    fi
    prev=
    continue
  fi
  case "$arg" in
    -f) saw_f=1 ;;
    -N) saw_n=1 ;;
    -R) prev=-R ;;
    -o) prev=-o ;;
  esac
done

if [ -z "$saw_f" ] || [ -z "$saw_n" ] || [ -z "$saw_tunnel" ] || [ -z "$controlpath" ]; then
  echo "unexpected ssh args: $*" >&2
  exit 1
fi
` + extraChecks.String() + `
: > "$controlpath"
exit 0
`
	path := filepath.Join(dir, "ssh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}

func writeFakeMosh(t *testing.T, dir string) {
	t.Helper()
	script := `#!/bin/sh
saw_host=
saw_control=
for arg in "$@"; do
  case "$arg" in
    -L|-R|-L*|-R*)
      echo "forwards must not be passed to mosh: $*" >&2
      exit 1
      ;;
    user@host) saw_host=1 ;;
    --ssh=*)
      case "$arg" in
        *ControlPath=*) saw_control=1 ;;
      esac
      ;;
  esac
done
if [ -n "$saw_host" ] && [ -n "$saw_control" ]; then
  exit 0
fi
echo "unexpected mosh args: $*" >&2
exit 1
`
	path := filepath.Join(dir, "mosh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}

func writeFakeET(t *testing.T, dir string) {
	t.Helper()
	script := `#!/bin/sh
saw_host=
for arg in "$@"; do
  case "$arg" in
    -L|-R|-L*|-R*|-r|-t|--reversetunnel*|--tunnel*)
      echo "forwards must not be passed to et: $*" >&2
      exit 1
      ;;
    user@host) saw_host=1 ;;
  esac
done
if [ -n "$saw_host" ]; then
  exit 0
fi
echo "unexpected et args: $*" >&2
exit 1
`
	path := filepath.Join(dir, "et")
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
