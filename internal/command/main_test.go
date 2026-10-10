package command

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunEndToEndWithFakeBinaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	trackClipboardPorts(t)

	binDir := t.TempDir()
	writeFakeSSHSession(t, binDir, fakeSSHSession{})
	t.Setenv("PATH", binDir)

	code := run([]string{"user@host", "true"})
	assert.Equal(t, 0, code)
	requireTrackedPortsReleased(t, 1)
}

func TestRunEndToEndSSHInteractive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	trackClipboardPorts(t)

	binDir := t.TempDir()
	writeFakeSSHSession(t, binDir, fakeSSHSession{})
	t.Setenv("PATH", binDir)

	code := run([]string{"user@host"})
	assert.Equal(t, 0, code)
	requireTrackedPortsReleased(t, 1)
}

func TestRunEndToEndSSHWithLocalForward(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	trackClipboardPorts(t)

	binDir := t.TempDir()
	writeFakeSSHSession(t, binDir, fakeSSHSession{masterForwards: []string{"-L", "localhost:8080:127.0.0.1:8080"}})
	t.Setenv("PATH", binDir)

	code := run([]string{"-L", "8080:127.0.0.1:8080", "user@host", "true"})
	assert.Equal(t, 0, code)
	requireTrackedPortsReleased(t, 1)
}

func TestRunPropagatesSSHExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	trackClipboardPorts(t)

	binDir := t.TempDir()
	writeFakeSSHSession(t, binDir, fakeSSHSession{clientExit: 42})
	t.Setenv("PATH", binDir)

	code := run([]string{"user@host"})
	assert.Equal(t, 42, code)
	requireTrackedPortsReleased(t, 1)
}

func TestRunEndToEndWithMosh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	trackClipboardPorts(t)

	binDir := t.TempDir()
	writeFakeSSHTunnel(t, binDir)
	writeFakeMosh(t, binDir)
	t.Setenv("PATH", binDir)

	code := run([]string{"--transport", "mosh", "user@host"})
	assert.Equal(t, 0, code)
	requireTrackedPortsReleased(t, 1)
}

func TestRunEndToEndMoshWithLocalForward(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	trackClipboardPorts(t)

	binDir := t.TempDir()
	writeFakeSSHTunnel(t, binDir, "-L", "localhost:8080:127.0.0.1:8080")
	writeFakeMosh(t, binDir)
	t.Setenv("PATH", binDir)

	code := run([]string{"--transport", "mosh", "-L", "8080:127.0.0.1:8080", "user@host"})
	assert.Equal(t, 0, code)
	requireTrackedPortsReleased(t, 1)
}

func TestRunMoshNotFound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	trackClipboardPorts(t)

	binDir := t.TempDir()
	writeFakeSSHTunnel(t, binDir)
	t.Setenv("PATH", binDir)

	code := run([]string{"--transport", "mosh", "user@host"})
	assert.Equal(t, 1, code)
	requireTrackedPortsReleased(t, 0)
}

func TestRunMoshMissingHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	trackClipboardPorts(t)

	binDir := t.TempDir()
	writeFakeSSHTunnel(t, binDir)
	writeFakeMosh(t, binDir)
	t.Setenv("PATH", binDir)

	code := run([]string{"--transport", "mosh", "-p", "60001"})
	assert.Equal(t, 1, code)
	requireTrackedPortsReleased(t, 0)
}

func TestConcurrentSessionsUseSeparatePorts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helpers are shell scripts")
	}

	trackClipboardPorts(t)
	t.Setenv("SLUSH_TEST_HOLD", "2")

	binDir := t.TempDir()
	writeFakeSSHSession(t, binDir, fakeSSHSession{})
	t.Setenv("PATH", binDir)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Go(func() {
			codes[i] = run([]string{"user@host", "true"})
		})
	}
	wg.Wait()
	assert.Equal(t, []int{0, 0}, codes)
	ports := trackedClipboardPorts(t)
	require.Len(t, ports, 2)
	assert.NotEqual(t, ports[0], ports[1])
	requirePortFree(t, ports[0])
	requirePortFree(t, ports[1])
}

// fakeSSHSession configures the combined ssh fake: one script serves
// the -N master (holds forwards, answers -O check/exit) and the
// interactive client (reuses the master, carries no -R of its own).
type fakeSSHSession struct {
	// extra forwards required on the master, as flag/spec pairs.
	masterForwards []string
	// exit code for the client invocation.
	clientExit int
}

func writeFakeSSHSession(t *testing.T, dir string, cfg fakeSSHSession) {
	t.Helper()
	require.Zero(t, len(cfg.masterForwards)%2, "master forwards must be flag/spec pairs, got %v", cfg.masterForwards)

	var extraChecks strings.Builder
	for i := 0; i < len(cfg.masterForwards); i += 2 {
		flag := cfg.masterForwards[i]
		spec := cfg.masterForwards[i+1]
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
  echo "missing master forward ` + flag + ` ` + spec + `: $*" >&2
  exit 1
fi
`)
	}

	script := `#!/bin/sh
controlpath=
prev=
op=
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    case "$arg" in
      ControlPath=*) controlpath=${arg#ControlPath=} ;;
    esac
    prev=
    continue
  fi
  case "$arg" in
    -o) prev=-o; continue ;;
    -N) is_master=1 ;;
    -O) op=1; continue ;;
  esac
  if [ -n "$op" ]; then
    if [ "$arg" = "check" ]; then
      if [ -n "$controlpath" ] && [ -e "$controlpath" ]; then
        exit 0
      fi
      exit 1
    fi
    if [ "$arg" = "exit" ]; then
      exit 0
    fi
` + fakeClipboardForward + `
    echo "unsupported control command: $arg" >&2
    exit 1
  fi
done

prev=
is_provision=
for arg in "$@"; do
  if [ "$prev" = "-o" ] && [ "$arg" = "BatchMode=yes" ]; then
    is_provision=1
    break
  fi
  prev=
  if [ "$arg" = "-o" ]; then prev=-o; fi
done
if [ -n "$is_provision" ]; then
  stdin=$(cat)
  if [ -n "$stdin" ]; then
    echo INSTALLED
  else
    echo MISSING
  fi
  exit 0
fi

if [ -z "$is_master" ]; then
  # Interactive client: must reuse the master, carry no -R itself,
  # and wrap the remote command with the session environment.
  saw_control=
  saw_host=
  saw_env=
  for arg in "$@"; do
    case "$arg" in
      -R|-R*) echo "client must not carry -R: $*" >&2; exit 1 ;;
      -o) prev_o=1; continue ;;
    esac
    case "$arg" in
      *SLUSH=1*SLUSH_TOKEN=*) saw_env=1 ;;
    esac
    if [ -n "$prev_o" ]; then
      case "$arg" in
        ControlPath=*) saw_control=1 ;;
      esac
      prev_o=
      continue
    fi
    if [ "$arg" = "user@host" ]; then
      saw_host=1
    fi
  done
  if [ -z "$saw_control" ] || [ -z "$saw_host" ] || [ -z "$saw_env" ]; then
    echo "unexpected client args: $*" >&2
    exit 1
  fi
` + fakeClientHold + `
  exit ` + strconv.Itoa(cfg.clientExit) + `
fi

if [ -z "$controlpath" ]; then
  echo "unexpected master args: $*" >&2
  exit 1
fi
` + extraChecks.String() + `
: > "$controlpath"
while true; do
  sleep 60 2>/dev/null
done
`
	path := filepath.Join(dir, "ssh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}

func writeFakeSSHTunnel(t *testing.T, dir string, extraForwards ...string) {
	t.Helper()
	require.Zero(t, len(extraForwards)%2, "extra forwards must be flag/spec pairs, got %v", extraForwards)

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
controlpath=
prev=
op=
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    case "$arg" in
      ControlPath=*) controlpath=${arg#ControlPath=} ;;
    esac
    prev=
    continue
  fi
  case "$arg" in
    -o) prev=-o; continue ;;
    -O) op=1; continue ;;
  esac
  if [ -n "$op" ]; then
    if [ "$arg" = "check" ]; then
      if [ -n "$controlpath" ] && [ -e "$controlpath" ]; then
        exit 0
      fi
      exit 1
    fi
    if [ "$arg" = "exit" ]; then
      exit 0
    fi
` + fakeClipboardForward + `
    echo "unsupported control command: $arg" >&2
    exit 1
  fi
done

prev=
is_provision=
for arg in "$@"; do
  if [ "$prev" = "-o" ] && [ "$arg" = "BatchMode=yes" ]; then
    is_provision=1
    break
  fi
  prev=
  if [ "$arg" = "-o" ]; then prev=-o; fi
done
if [ -n "$is_provision" ]; then
  stdin=$(cat)
  if [ -n "$stdin" ]; then
    echo INSTALLED
  else
    echo MISSING
  fi
  exit 0
fi

saw_n=
prev=
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    case "$arg" in
      ControlPath=*) controlpath=${arg#ControlPath=} ;;
    esac
    prev=
    continue
  fi
  case "$arg" in
    -N) saw_n=1 ;;
    -o) prev=-o ;;
  esac
done

if [ -z "$saw_n" ] || [ -z "$controlpath" ]; then
  echo "unexpected ssh args: $*" >&2
  exit 1
fi
` + extraChecks.String() + `
: > "$controlpath"
# Stay alive as the ControlMaster until slush kills this child.
while true; do
  sleep 60 2>/dev/null
done
`
	path := filepath.Join(dir, "ssh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}

func writeFakeMosh(t *testing.T, dir string) {
	t.Helper()
	script := `#!/bin/sh
saw_host=
saw_control=
saw_dashdash=
saw_sh=
saw_c=
saw_env=
for arg in "$@"; do
  case "$arg" in
    -L|-R|-L*|-R*)
      echo "forwards must not be passed to mosh: $*" >&2
      exit 1
      ;;
    user@host) saw_host=1 ;;
    --) saw_dashdash=1 ;;
    sh) saw_sh=1 ;;
    -c) saw_c=1 ;;
    --ssh=*)
      case "$arg" in
        *ControlPath=*) saw_control=1 ;;
      esac
      ;;
  esac
  case "$arg" in
    *SLUSH=1*SLUSH_TOKEN=*) saw_env=1 ;;
  esac
done
if [ -n "$saw_host" ] && [ -n "$saw_control" ] && [ -n "$saw_dashdash" ] && [ -n "$saw_sh" ] && [ -n "$saw_c" ] && [ -n "$saw_env" ]; then
  exit 0
fi
echo "unexpected mosh args: $*" >&2
exit 1
`
	path := filepath.Join(dir, "mosh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}
