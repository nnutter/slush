package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeValidateSSH installs an ssh stand-in that emulates the
// remote side locally: -N holds a fake master, -O answers control
// ops, and anything else runs as the remote command with HOME
// isolated to FAKE_HOME. Provision scripts and probes (including the
// real shim) execute for real against the test's clipboard server.
func writeFakeValidateSSH(t *testing.T, dir string) {
	t.Helper()
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
    exit 1
  fi
done

if [ -n "$is_master" ]; then
  saw_tunnel=
  prev=
  for arg in "$@"; do
    if [ "$prev" = "-R" ] && [ "$arg" = "2489:127.0.0.1:2489" ]; then
      saw_tunnel=1
    fi
    prev=
    if [ "$arg" = "-R" ]; then prev=-R; fi
  done
  if [ -z "$saw_tunnel" ] || [ -z "$controlpath" ]; then
    echo "unexpected master args: $*" >&2
    exit 1
  fi
  : > "$controlpath"
  exec sleep 300
fi

# Emulated remote exec: skip ssh flags, first bare word is the host,
# the rest is the remote script, run locally with isolated HOME.
stdin=$(cat)
prev=
host=
script=
for arg in "$@"; do
  if [ -n "$prev" ]; then
    prev=
    continue
  fi
  case "$arg" in
    -o|-S) prev=1; continue ;;
    -*) continue ;;
  esac
  if [ -z "$host" ]; then
    host="$arg"
    continue
  fi
  if [ -z "$script" ]; then
    script="$arg"
  else
    script="$script $arg"
  fi
done
if [ -z "$host" ] || [ -z "$script" ]; then
  echo "cannot find host/script in: $*" >&2
  exit 1
fi
printf '%s' "$stdin" | HOME="$FAKE_HOME" PATH="$FAKE_HOME/.cache/slush/bin:/usr/local/bin:/usr/bin:/bin" sh -c "$script"
`
	path := filepath.Join(dir, "ssh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}

func validateTestEnv(t *testing.T, binDir string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("FAKE_HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	clipFile := filepath.Join(t.TempDir(), "clipboard")
	writeFakeClipTool(t, binDir, "wl-copy", `cat > "$FAKE_CLIP_FILE"`)
	writeFakeClipTool(t, binDir, "wl-paste", `cat "$FAKE_CLIP_FILE"`)
	writeFakeClipTool(t, binDir, "pbcopy", `cat > "$FAKE_CLIP_FILE"`)
	writeFakeClipTool(t, binDir, "pbpaste", `cat "$FAKE_CLIP_FILE"`)
	t.Setenv("FAKE_CLIP_FILE", clipFile)
	t.Setenv("WAYLAND_DISPLAY", "test-display")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRunValidateEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("emulated remote is a shell script")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	useEphemeralClipboardPort(t)

	binDir := t.TempDir()
	writeFakeValidateSSH(t, binDir)
	validateTestEnv(t, binDir)

	code := run([]string{"validate", "user@host"})
	assert.Equal(t, 0, code)

	// Provisioning really landed in the isolated HOME.
	version, err := os.ReadFile(filepath.Join(os.Getenv("FAKE_HOME"), ".cache", "slush", "VERSION"))
	require.NoError(t, err)
	assert.Equal(t, shimVersion, strings.TrimSpace(string(version)))
}

func TestRunValidateEndToEndMosh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("emulated remote is a shell script")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	useEphemeralClipboardPort(t)

	binDir := t.TempDir()
	writeFakeValidateSSH(t, binDir)
	validateTestEnv(t, binDir)

	code := run([]string{"--mosh", "validate", "user@host"})
	assert.Equal(t, 0, code)
}

func TestRunValidateEndToEndET(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("emulated remote is a shell script")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	useEphemeralClipboardPort(t)

	binDir := t.TempDir()
	writeFakeValidateSSH(t, binDir)
	validateTestEnv(t, binDir)

	code := run([]string{"--et", "validate", "user@host"})
	assert.Equal(t, 0, code)
}

func TestRunValidateUsage(t *testing.T) {
	useEphemeralClipboardPort(t)
	code := run([]string{"validate"})
	assert.Equal(t, 1, code)
}
