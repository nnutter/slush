package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWarnsWhenLemonadeAlreadyRunning(t *testing.T) {
	useEphemeralLemonadeAddr(t)

	ln, err := net.Listen("tcp", lemonadeAddr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	code := run([]string{"-G", "example.com"})
	assert.Equal(t, 1, code)
}

func TestRunEndToEndWithFakeBinaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	useEphemeralLemonadeAddr(t)

	binDir := t.TempDir()
	writeFakeSSH(t, binDir)
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"user@host", "true"})
	assert.Equal(t, 0, code)
	requirePortFree(t, lemonadeAddr)
}

func TestRunPropagatesSSHExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	useEphemeralLemonadeAddr(t)

	binDir := t.TempDir()
	writeFakeSSHExit(t, binDir, 42)
	writeFakeLemonade(t, binDir)
	t.Setenv("PATH", binDir)

	require.NoError(t, ensureLemonadePortFree())

	code := run([]string{"user@host"})
	assert.Equal(t, 42, code)
	requirePortFree(t, lemonadeAddr)
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

func writeFakeLemonade(t *testing.T, dir string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "fakel.go")
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
	addr := os.Getenv("SLUSH_LEMONADE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:2489"
	}
	ln, err := net.Listen("tcp", addr)
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
