// The validate subcommand: `slush [--mosh|--et] validate [host...]`.
//
// Validate connects like a session (tunnel, provisioning) and then
// runs probes instead of an interactive client: shim version,
// transport, session environment, clipboard round-trips in both
// directions, open dry-run, and a platform report. Each check prints
// `ok - <name>` or `FAIL - <name>: <detail>`; the exit code is nonzero
// when any check fails. It is the enforcement point for clipboard
// forwarding: sessions degrade with a warning, validate fails loudly.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/nnutter/slush/internal/clipboard"
	"github.com/nnutter/slush/internal/desktop"
)

// validator accumulates check results.
type validator struct {
	out    io.Writer
	failed int
	passed int
}

func (v *validator) ok(name string) {
	v.passed++
	fmt.Fprintf(v.out, "ok - %s\n", name)
}

func (v *validator) skip(name, reason string) {
	v.passed++
	fmt.Fprintf(v.out, "ok - %s (skipped: %s)\n", name, reason)
}

func (v *validator) fail(name, detail string) {
	v.failed++
	fmt.Fprintf(v.out, "FAIL - %s: %s\n", name, detail)
}

// runValidate validates clipboard forwarding against a remote.
func runValidate(mode clientMode, args []string) int {
	token, err := clipboard.GenerateToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}
	if err := runValidateChecks(mode, args, token, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "slush: validate: %v\n", err)
		return 1
	}
	return 0
}

func runValidateChecks(mode clientMode, args []string, token string, out io.Writer) error {
	v := &validator{out: out}

	host, connOpts, forwards, err := validateTarget(mode, args)
	if err != nil {
		v.fail("target", err.Error())
		return fmt.Errorf("1 check failed")
	}

	if err := clipboard.EnsurePortFree(clipboardPort); err != nil {
		v.fail("clipboard server", err.Error())
		return fmt.Errorf("1 check failed")
	}
	server, err := clipboard.StartServer(clipboardPort, token)
	if err != nil {
		v.fail("clipboard server", err.Error())
		return fmt.Errorf("1 check failed")
	}
	defer server.Stop()
	v.ok(fmt.Sprintf("clipboard server on 127.0.0.1:%d", clipboardPort))

	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		v.fail("tunnel", "ssh not found on PATH")
		return fmt.Errorf("1 check failed")
	}
	teardown, controlPath, err := establishSession(sshPath, host, connOpts, forwards, token)
	if err != nil {
		v.fail("tunnel", err.Error())
		return fmt.Errorf("1 check failed")
	}
	defer teardown()
	v.ok("tunnel with clipboard forward")

	probe := func(script string) (string, error) {
		return sshExec(sshPath, host, probeEnvPrefix(token)+script, nil,
			[]string{"-o", "ControlMaster=no", "-S", controlPath})
	}

	// Shim version: proves provisioning landed.
	if versionOut, err := probe(`cat ` + remoteSlushDirExpr + `/VERSION 2>/dev/null || echo MISSING`); err != nil {
		v.fail("shims", err.Error())
	} else if strings.TrimSpace(versionOut) != shimVersion {
		v.fail("shims", fmt.Sprintf("remote has %q, want %q", strings.TrimSpace(versionOut), shimVersion))
	} else {
		v.ok(fmt.Sprintf("shims version %s", shimVersion))
	}

	// Transport: a real TCP dial through the forward. Safe: the
	// server answers ERR to a bare connect and moves on.
	transportScript := fmt.Sprintf(`python3 -c 'import socket; socket.create_connection(("127.0.0.1", %d), timeout=5).close(); print("listening")' 2>&1`,
		clipboardPort)
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

	// Session environment: the wrapper each mode injects.
	checkSessionEnv(v, mode, args, token, probe)

	// Round trip remote -> local.
	payloadOut := fmt.Sprintf("slush-validate-%d", time.Now().UnixNano())
	if _, err := probe(fmt.Sprintf(`printf '%%s' '%s' | pbcopy`, payloadOut)); err != nil {
		v.fail("copy remote->local", err.Error())
	} else if pasted, err := clipboard.Paste(clipboardPort, token); err != nil {
		v.fail("copy remote->local", err.Error())
	} else if string(pasted) != payloadOut {
		v.fail("copy remote->local", fmt.Sprintf("got %q back", pasted))
	} else {
		v.ok("copy remote->local")
	}

	// Round trip local -> remote.
	payloadIn := fmt.Sprintf("slush-validate-%d", time.Now().UnixNano())
	if err := clipboard.Copy(clipboardPort, token, []byte(payloadIn)); err != nil {
		v.fail("paste local->remote", err.Error())
	} else if pastedOut, err := probe(`pbpaste`); err != nil {
		v.fail("paste local->remote", err.Error())
	} else if pastedOut != payloadIn {
		v.fail("paste local->remote", fmt.Sprintf("got %q back", pastedOut))
	} else {
		v.ok("paste local->remote")
	}

	// Open dry-run: proves arg parsing without opening anything.
	if openOut, err := probe(`slush-open --dry-run https://example.com/validate`); err != nil {
		v.fail("open dry-run", err.Error())
	} else if strings.TrimSpace(openOut) != "OPEN https://example.com/validate" {
		v.fail("open dry-run", fmt.Sprintf("got %q", strings.TrimSpace(openOut)))
	} else {
		v.ok("open dry-run")
	}

	// Platform report.
	remoteUname, _ := probe(`uname -sm`)
	fmt.Fprintf(out, "info - client %s clipboard %s; remote %s\n",
		runtime.GOOS, desktop.ClipboardBackendName(), strings.TrimSpace(remoteUname))
	v.ok("platform report")

	if v.failed > 0 {
		return fmt.Errorf("%d check(s) failed", v.failed)
	}
	fmt.Fprintf(out, "validate: all %d checks passed\n", v.passed)
	return nil
}

// probeEnvPrefix exports the session token, port, and shim PATH for
// probes. Unlike the session wrapper it is explicit (not inherited),
// so checks do not depend on wrapping.
func probeEnvPrefix(token string) string {
	return remoteEnvPrefix(token) + fmt.Sprintf("SLUSH_PORT=%d ", clipboardPort)
}

// validateTarget resolves the tunnel host, connection options, and
// forwards for the validate mode. Probes always run over the ssh
// tunnel, whatever the session transport. Only ssh mode carries
// connection options today; mosh/et tunnel limitations match sessions.
func validateTarget(mode clientMode, args []string) (host string, connOpts, forwards []string, err error) {
	options, err := sessionOptionsFromArgs(mode, args)
	if err != nil {
		return "", nil, nil, err
	}
	rest, forwards := options.args, options.forwards
	switch mode {
	case modeMosh:
		host, err := moshDestination(rest)
		if err != nil {
			return "", nil, nil, err
		}
		return host, nil, forwards, nil
	case modeET:
		host, err := etDestination(rest)
		if err != nil {
			return "", nil, nil, err
		}
		return sshHostFromETDestination(host), nil, forwards, nil
	default:
		host, err := sshHostOperand(rest)
		if err != nil {
			return "", nil, nil, err
		}
		return host, options.connOpts, forwards, nil
	}
}

// checkSessionEnv verifies the per-mode environment wrapper by
// executing the wrapped shape and inspecting the result.
func checkSessionEnv(v *validator, mode clientMode, args []string, token string, probe func(string) (string, error)) {
	switch mode {
	case modeET:
		v.skip("session env", "et carries no wrapped environment (shims work by absolute path)")
		return
	case modeMosh:
		wrapped, err := withRemoteEnvMosh([]string{"probehost", "sh", "-c", `printf '%s\n' "$SLUSH" "$SLUSH_TOKEN" "$BROWSER"`}, token)
		if err != nil {
			v.fail("session env", err.Error())
			return
		}
		// The wrapper shape is fixed: ... -- host sh -c <script> <tail>.
		// Run what mosh-server would run: sh -c '<script>' <tail>.
		dashIdx := indexOf(wrapped, "--")
		if dashIdx < 0 || len(wrapped) < dashIdx+4 || wrapped[dashIdx+2] != "sh" || wrapped[dashIdx+3] != "-c" {
			v.fail("session env", "cannot locate wrapper script")
			return
		}
		script, tail := wrapped[dashIdx+4], wrapped[dashIdx+5:]
		quotedTail := make([]string, len(tail))
		for i, arg := range tail {
			quotedTail[i] = shellQuote(arg)
		}
		script = "sh -c " + shellQuote(script) + " " + strings.Join(quotedTail, " ")
		out, err := probe(script)
		if err != nil {
			v.fail("session env", err.Error())
			return
		}
		verifyEnvOutput(v, out, token)
	default:
		wrapped, _, err := withRemoteEnvSSH(append(sshHeadFor(args), `printf '%s\n' "$SLUSH" "$SLUSH_TOKEN" "$BROWSER"`), token)
		if err != nil {
			v.fail("session env", err.Error())
			return
		}
		out, err := probe(wrapped[len(wrapped)-1])
		if err != nil {
			v.fail("session env", err.Error())
			return
		}
		verifyEnvOutput(v, out, token)
		// PATH shadow: pbcopy must resolve into the shim dir.
		whichOut, err := probe(`command -v pbcopy`)
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
}

// sshHeadFor rebuilds the options-plus-host head from validate's ssh
// args so the env check wraps the same shape the session would. A
// dummy command keeps the split working when the user gave none.
func sshHeadFor(args []string) []string {
	_, rest, err := takeSSHForwards(args)
	if err != nil {
		return nil
	}
	head, _, err := splitSSHRemoteCommand(append(rest, "true"))
	if err != nil {
		return nil
	}
	return head
}

// verifyEnvOutput checks wrapped printenv output without echoing secrets.
func verifyEnvOutput(v *validator, out, token string) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || lines[0] != "1" || lines[1] != token || lines[2] != "slush-open" {
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

func indexOf(haystack []string, needle string) int {
	for i, s := range haystack {
		if s == needle {
			return i
		}
	}
	return -1
}
