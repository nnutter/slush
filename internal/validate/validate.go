// The validate subcommand: `slush validate [--transport auto|ssh|mosh] host`.
//
// Validate connects like a session (tunnel, provisioning) and then
// runs probes instead of an interactive client: shim version,
// transport, session environment, clipboard round-trips in both
// directions, open dry-run, and a platform report. Each check prints
// `ok - <name>` or `FAIL - <name>: <detail>`; the exit code is nonzero
// when any check fails. It is the enforcement point for clipboard
// forwarding: sessions degrade with a warning, validate fails loudly.

package validate

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/nnutter/slush/internal/desktop"
	"github.com/nnutter/slush/internal/protocol"
	"github.com/nnutter/slush/internal/remote"
	"github.com/nnutter/slush/internal/session"
)

// validator accumulates check results. Report writes are best effort;
// output failures must not replace remote integration check results.
type validator struct {
	out    io.Writer
	failed int
	passed int
}

func (v *validator) fail(name, detail string) {
	v.failed++
	_, _ = fmt.Fprintf(v.out, "FAIL - %s: %s\n", name, detail)
}

func (v *validator) ok(name string) {
	v.passed++
	_, _ = fmt.Fprintf(v.out, "ok - %s\n", name)
}

func (v *validator) skip(name, reason string) {
	v.passed++
	_, _ = fmt.Fprintf(v.out, "ok - %s (skipped: %s)\n", name, reason)
}

// Run checks remote integration using shared session setup and reports results.
func Run(options session.Options, out io.Writer) error {
	token, err := protocol.GenerateToken()
	if err != nil {
		return err
	}
	v := &validator{out: out}

	if err := protocol.EnsurePortFree(options.ProtocolPort); err != nil {
		v.fail("clipboard server", err.Error())
		return fmt.Errorf("1 check failed")
	}
	server, err := protocol.StartServer(options.ProtocolPort, token)
	if err != nil {
		v.fail("clipboard server", err.Error())
		return fmt.Errorf("1 check failed")
	}
	defer server.Stop()
	v.ok(fmt.Sprintf("clipboard server on 127.0.0.1:%d", options.ProtocolPort))

	_, err = exec.LookPath("ssh")
	if err != nil {
		v.fail("tunnel", "ssh not found on PATH")
		return fmt.Errorf("1 check failed")
	}
	connection, err := session.Open(options, token)
	if err != nil {
		v.fail("tunnel", err.Error())
		return fmt.Errorf("1 check failed")
	}
	defer connection.Close()
	v.ok("tunnel with clipboard forward")

	probe := connection.Probe

	if options.ForwardAgent {
		checkAgent(v, probe)
	}
	checkShims(v, probe)
	checkTransport(v, probe, options.ProtocolPort)
	checkSessionEnv(v, connection)
	checkClipboardRoundTrips(v, probe, options.ProtocolPort, token)
	checkOpen(v, probe)

	// Platform report.
	remoteUname, _ := probe(`uname -sm`)
	_, _ = fmt.Fprintf(out, "info - client %s clipboard %s; remote %s\n",
		runtime.GOOS, desktop.ClipboardBackendName(), strings.TrimSpace(remoteUname))
	v.ok("platform report")

	if v.failed > 0 {
		return fmt.Errorf("%d check(s) failed", v.failed)
	}
	_, _ = fmt.Fprintf(out, "validate: all %d checks passed\n", v.passed)
	return nil
}

func checkAgent(v *validator, probe func(string) (string, error)) {
	output, err := probe(`test -S "$SSH_AUTH_SOCK" && { ssh-add -l >/dev/null 2>&1; agent_status=$?; test "$agent_status" -le 1; } && printf agent-ready`)
	if err != nil || !isReadyAgent(output) {
		v.fail("SSH agent", fmt.Sprintf("remote agent is unavailable; check sshd AllowAgentForwarding: output %q, error %v", strings.TrimSpace(output), err))
	} else {
		v.ok("SSH agent forwarding")
	}
}

func isReadyAgent(output string) bool {
	return strings.TrimSpace(output) == "agent-ready"
}

func checkShims(v *validator, probe func(string) (string, error)) {
	// Shim version: proves provisioning landed.
	if versionOut, err := probe(`cat ` + remote.DirExpr + `/VERSION 2>/dev/null || echo MISSING`); err != nil {
		v.fail("shims", err.Error())
	} else if strings.TrimSpace(versionOut) != remote.Version {
		v.fail("shims", fmt.Sprintf("remote has %q, want %q", strings.TrimSpace(versionOut), remote.Version))
	} else {
		v.ok(fmt.Sprintf("shims version %s", remote.Version))
	}
}

func checkTransport(v *validator, probe func(string) (string, error), port int) {
	// Transport: a real TCP dial through the forward. Safe: the
	// server answers ERR to a bare connect and moves on.
	transportScript := fmt.Sprintf(`python3 -c 'import socket; socket.create_connection(("127.0.0.1", %d), timeout=5).close(); print("listening")' 2>&1`,
		port)
	if transportOut, err := probe(transportScript); err != nil {
		v.fail("transport", err.Error())
	} else if strings.TrimSpace(transportOut) != "listening" {
		if strings.Contains(transportOut, "No such file") || strings.Contains(transportOut, "not found") {
			v.skip("transport", "no python3 on remote")
		} else {
			v.fail("transport", strings.TrimSpace(transportOut))
		}
	} else {
		v.ok("transport over reverse forward")
	}
}

func checkClipboardRoundTrips(v *validator, probe func(string) (string, error), port int, token string) {
	// Round trip remote -> local.
	payloadOut := fmt.Sprintf("slush-validate-%d", time.Now().UnixNano())
	if _, err := probe(fmt.Sprintf(`printf '%%s' '%s' | pbcopy`, payloadOut)); err != nil {
		v.fail("copy remote->local", err.Error())
	} else if pasted, err := protocol.Paste(port, token); err != nil {
		v.fail("copy remote->local", err.Error())
	} else if string(pasted) != payloadOut {
		v.fail("copy remote->local", fmt.Sprintf("got %q back", pasted))
	} else {
		v.ok("copy remote->local")
	}

	// Round trip local -> remote.
	payloadIn := fmt.Sprintf("slush-validate-%d", time.Now().UnixNano())
	if err := protocol.Copy(port, token, []byte(payloadIn)); err != nil {
		v.fail("paste local->remote", err.Error())
	} else if pastedOut, err := probe(`pbpaste`); err != nil {
		v.fail("paste local->remote", err.Error())
	} else if pastedOut != payloadIn {
		v.fail("paste local->remote", fmt.Sprintf("got %q back", pastedOut))
	} else {
		v.ok("paste local->remote")
	}
}

func checkOpen(v *validator, probe func(string) (string, error)) {
	// Open dry-run: proves arg parsing without opening anything.
	if openOut, err := probe(`slush-open --dry-run https://example.com/validate`); err != nil {
		v.fail("open dry-run", err.Error())
	} else if strings.TrimSpace(openOut) != "OPEN https://example.com/validate" {
		v.fail("open dry-run", fmt.Sprintf("got %q", strings.TrimSpace(openOut)))
	} else {
		v.ok("open dry-run")
	}
}

// checkSessionEnv verifies the transport's wrapper by inspecting its output.
func checkSessionEnv(v *validator, connection *session.Session) {
	script, err := connection.WrappedCommand(`printf '%s\n' "$SLUSH" "$SLUSH_TOKEN" "$BROWSER"`)
	if err != nil {
		v.fail("session env", err.Error())
		return
	}
	out, err := connection.Probe(script)
	if err != nil {
		v.fail("session env", err.Error())
		return
	}
	verifyEnvOutput(v, out, connection.Environment().Token)
	if connection.Transport() == session.Mosh {
		return
	}
	// PATH shadow: pbcopy must resolve into the shim dir.
	whichOut, err := connection.Probe(`command -v pbcopy`)
	if err != nil {
		v.fail("session env", err.Error())
		return
	}
	if !strings.Contains(whichOut, "/slush/bin/") {
		v.fail("session env", fmt.Sprintf("pbcopy resolves to %q", strings.TrimSpace(whichOut)))
		return
	}
	v.ok("session env PATH shadow")
}

// verifyEnvOutput checks wrapped printenv output without echoing secrets.
func verifyEnvOutput(v *validator, out, token string) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if !slices.Equal(lines, []string{"1", token, "slush-open"}) {
		v.fail("session env", fmt.Sprintf("unexpected wrapper output %q", redactToken(out, token)))
		return
	}
	v.ok("session env exports")
}

func redactToken(out, token string) string {
	if token == "" {
		return out
	}
	return strings.ReplaceAll(out, token, "<token>")
}
