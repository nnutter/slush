package command

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/slush/internal/protocol"
)

// run exercises the real command entry point and maps its error to a status.
func run(args []string) int {
	return ExitCode(Execute(args, "dev"))
}

// fakeClipboardForward is the ssh -O forward handler shared by session fakes.
// It accepts only a symmetric loopback forward and records the port when
// SLUSH_TEST_PORTS is set.
const fakeClipboardForward = `
    if [ "$arg" = "forward" ]; then
      if [ -z "$controlpath" ] || [ ! -e "$controlpath" ]; then
        echo "clipboard forward before master: $*" >&2
        exit 1
      fi
      spec=
      prevarg=
      for a in "$@"; do
        if [ "$prevarg" = "-R" ]; then
          spec=$a
          break
        fi
        prevarg=$a
      done
      remote=${spec%%:*}
      rest=${spec#*:}
      hostpart=${rest%:*}
      local=${rest##*:}
      if [ -z "$remote" ] || [ "$remote" != "$local" ] || [ "$hostpart" != "127.0.0.1" ]; then
        echo "unexpected clipboard forward: $spec" >&2
        exit 1
      fi
      if [ -n "$SLUSH_TEST_PORTS" ]; then
        printf '%s\n' "$remote" >> "$SLUSH_TEST_PORTS"
      fi
      exit 0
    fi
`

// fakeClientHold keeps the client alive until SLUSH_TEST_HOLD clipboard
// forwards have been recorded, so overlapping sessions stay bound together.
const fakeClientHold = `
if [ -n "$SLUSH_TEST_HOLD" ]; then
  i=0
  while [ "$i" -lt 100 ]; do
    count=0
    if [ -f "$SLUSH_TEST_PORTS" ]; then
      while IFS= read -r _; do
        count=$((count + 1))
      done < "$SLUSH_TEST_PORTS"
    fi
    if [ "$count" -ge "$SLUSH_TEST_HOLD" ]; then
      break
    fi
    i=$((i + 1))
    /bin/sleep 0.05
  done
fi
`

// trackClipboardPorts records each session's forwarded port.
func trackClipboardPorts(t *testing.T) {
	t.Helper()
	t.Setenv("SLUSH_TEST_PORTS", filepath.Join(t.TempDir(), "ports"))
}

func trackedClipboardPorts(t *testing.T) []int {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("SLUSH_TEST_PORTS"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	var ports []int
	for line := range strings.FieldsSeq(string(data)) {
		port, err := strconv.Atoi(line)
		require.NoError(t, err)
		ports = append(ports, port)
	}
	return ports
}

func requireTrackedPortsReleased(t *testing.T, count int) {
	t.Helper()
	ports := trackedClipboardPorts(t)
	require.Len(t, ports, count)
	for _, port := range ports {
		requirePortFree(t, port)
	}
}

func writeFakeClipTool(t *testing.T, dir, name, body string) {
	t.Helper()
	script := "#!/bin/sh\n" + body + "\n"
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}

func requirePortFree(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if protocol.EnsurePortFree(port) == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.FailNowf(t, "clipboard port is still bound", ":%d still bound", port)
}
