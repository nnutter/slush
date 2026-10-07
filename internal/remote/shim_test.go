package remote

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClipboardRequest records one shim request seen by the test server.
type fakeClipboardRequest struct {
	verb string
	body []byte
}

// startFakeClipboardServer speaks just enough of the clipboard protocol
// for shim tests: COPY records, PASTE answers cannedBody, OPEN records.
func startFakeClipboardServer(t *testing.T, token string, cannedBody []byte) (int, chan fakeClipboardRequest) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	received := make(chan fakeClipboardRequest, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(conn)
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				fields := strings.Fields(strings.TrimSpace(line))
				if len(fields) < 3 || fields[0] != "SLUSH1" || fields[1] != token {
					fmt.Fprintf(conn, "ERR unauthorized\n")
					return
				}
				var body []byte
				if len(fields) == 4 {
					n, err := strconv.Atoi(fields[3])
					if err != nil {
						fmt.Fprintf(conn, "ERR bad length\n")
						return
					}
					body = make([]byte, n)
					if _, err := readExactly(reader, body); err != nil {
						return
					}
				}
				received <- fakeClipboardRequest{verb: fields[2], body: body}
				if fields[2] == "PASTE" {
					fmt.Fprintf(conn, "OK %d\n", len(cannedBody))
					conn.Write(cannedBody)
					return
				}
				fmt.Fprintf(conn, "OK\n")
			}()
		}
	}()
	return port, received
}

func readExactly(reader *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := reader.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func python3Path(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}
	return path
}

// runShimName links shim.py under name and runs it like the remote would.
func runShimName(t *testing.T, name string, args []string, stdin string, env map[string]string) (int, string, string) {
	t.Helper()
	python := python3Path(t)

	dir := t.TempDir()
	shimSrc, err := filepath.Abs("shim.py")
	require.NoError(t, err)
	require.NoError(t, os.Symlink(shimSrc, filepath.Join(dir, name)))

	cmd := exec.Command(python, append([]string{filepath.Join(dir, name)}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	code := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run shim: %v", err)
		}
	}
	return code, stdout.String(), stderr.String()
}

func receiveRequest(t *testing.T, ch chan fakeClipboardRequest) fakeClipboardRequest {
	t.Helper()
	select {
	case req := <-ch:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for shim request")
		return fakeClipboardRequest{}
	}
}

func TestShimCopy(t *testing.T) {
	port, received := startFakeClipboardServer(t, "test-token", nil)
	env := map[string]string{"SLUSH_TOKEN": "test-token", "SLUSH_PORT": strconv.Itoa(port)}

	code, _, stderr := runShimName(t, "pbcopy", nil, "hello shim", env)
	require.Equal(t, 0, code, "stderr: %s", stderr)

	req := receiveRequest(t, received)
	assert.Equal(t, "COPY", req.verb)
	assert.Equal(t, []byte("hello shim"), req.body)
}

func TestShimPaste(t *testing.T) {
	port, received := startFakeClipboardServer(t, "test-token", []byte("pasted body"))
	env := map[string]string{"SLUSH_TOKEN": "test-token", "SLUSH_PORT": strconv.Itoa(port)}

	code, stdout, stderr := runShimName(t, "pbpaste", nil, "", env)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Equal(t, "pasted body", stdout)

	req := receiveRequest(t, received)
	assert.Equal(t, "PASTE", req.verb)
}

func TestShimOpen(t *testing.T) {
	port, received := startFakeClipboardServer(t, "test-token", nil)
	env := map[string]string{"SLUSH_TOKEN": "test-token", "SLUSH_PORT": strconv.Itoa(port)}

	code, _, stderr := runShimName(t, "xdg-open", []string{"https://example.com/x"}, "", env)
	require.Equal(t, 0, code, "stderr: %s", stderr)

	req := receiveRequest(t, received)
	assert.Equal(t, "OPEN", req.verb)
	assert.Equal(t, []byte("https://example.com/x"), req.body)
}

func TestShimXclipFlagFlip(t *testing.T) {
	port, received := startFakeClipboardServer(t, "test-token", []byte("x"))
	env := map[string]string{"SLUSH_TOKEN": "test-token", "SLUSH_PORT": strconv.Itoa(port)}

	code, stdout, stderr := runShimName(t, "xclip", []string{"-selection", "clipboard", "-o"}, "", env)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Equal(t, "x", stdout)
	assert.Equal(t, "PASTE", receiveRequest(t, received).verb)

	code, _, stderr = runShimName(t, "xclip", []string{"-selection", "clipboard"}, "copied", env)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	req := receiveRequest(t, received)
	assert.Equal(t, "COPY", req.verb)
	assert.Equal(t, []byte("copied"), req.body)
}

func TestShimOpenDryRun(t *testing.T) {
	// Dry runs never touch the server, so no token or listener needed.
	code, stdout, _ := runShimName(t, "slush-open", []string{"--dry-run", "https://example.com"}, "", nil)
	require.Equal(t, 0, code)
	assert.Equal(t, "OPEN https://example.com", strings.TrimSpace(stdout))
}

func TestShimMissingToken(t *testing.T) {
	python3Path(t)
	code, _, stderr := runShimName(t, "pbcopy", nil, "x", nil)
	require.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "no session token")
}

func TestShimUnknownName(t *testing.T) {
	env := map[string]string{"SLUSH_TOKEN": "test-token", "SLUSH_PORT": "2489"}
	code, _, stderr := runShimName(t, "not-a-shim", nil, "", env)
	require.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "unknown shim name")
}

func TestShimTokenFromEnvFile(t *testing.T) {
	port, received := startFakeClipboardServer(t, "file-token", []byte("ok"))
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cache", "slush"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".cache", "slush", "slush-env"),
		[]byte("SLUSH_TOKEN=file-token\nSLUSH_PORT="+strconv.Itoa(port)+"\n"),
		0o600,
	))

	python := python3Path(t)
	dir := t.TempDir()
	shimSrc, err := filepath.Abs("shim.py")
	require.NoError(t, err)
	require.NoError(t, os.Symlink(shimSrc, filepath.Join(dir, "pbpaste")))
	cmd := exec.Command(python, filepath.Join(dir, "pbpaste"))
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home,
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "SLUSH_TOKEN=", "SLUSH_PORT=")
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "ok", string(out))
	assert.Equal(t, "PASTE", receiveRequest(t, received).verb)
}
