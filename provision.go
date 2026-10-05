// Remote shim provisioning.
//
// Each session installs (when stale) a small argv[0]-dispatched python
// shim as ~/.cache/slush/bin/{pbcopy,pbpaste,wl-copy,...} plus a
// per-session env file with the clipboard token. Installs are
// idempotent and version-checked; the shims speak the clipboard
// protocol from server.go, so swapping their runtime later needs no
// protocol change.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

//go:embed shim.py
var shimScript string

// shimVersion bumps whenever shim.py changes so remotes reinstall.
const shimVersion = "1"

// shimNames are installed as symlinks to the argv[0]-dispatched shim.
var shimNames = []string{
	"pbcopy", "pbpaste",
	"wl-copy", "wl-paste",
	"xclip", "xsel",
	"xdg-open", "open", "slush-open", "sensible-browser",
}

const provisionTimeout = 60 * time.Second

// provisionRemote ensures the remote's shims are current and writes the
// session env file. When controlPath names a live ControlMaster the
// execs ride it; otherwise they connect directly. Any failure is an
// error for the caller to report while continuing degraded.
func provisionRemote(sshPath, host string, params sessionParams) error {
	version, missingPython, err := writeSessionEnv(sshPath, host, params)
	if err != nil {
		return err
	}
	if missingPython {
		return fmt.Errorf("remote has no python3; clipboard shims need it")
	}
	if version == shimVersion {
		return nil
	}
	return installShims(sshPath, host, params)
}

// writeSessionEnv refreshes the per-session env file and reports the
// installed shim version plus whether python3 is missing remotely.
func writeSessionEnv(sshPath, host string, params sessionParams) (string, bool, error) {
	script := fmt.Sprintf("umask 077\n"+`dir="${XDG_CACHE_HOME:-$HOME/.cache}/slush"`+"\n"+`mkdir -p "$dir/bin" || exit 1`+"\n"+
		`: >> "$dir/slush-env" || exit 1`+"\n"+`chmod 600 "$dir/slush-env" || exit 1`+"\n"+
		`printf 'SLUSH_TOKEN=%%s\nSLUSH_PORT=%%d\n' %s %d > "$dir/slush-env" || exit 1`+"\n"+`if ! command -v python3 >/dev/null 2>&1; then echo NO_PYTHON3; fi`+"\n"+`cat "$dir/VERSION" 2>/dev/null || echo MISSING`+"\n",
		shellQuote(params.token), clipboardPort)

	out, err := runProvisionExec(sshPath, host, params.controlPath, script, nil)
	if err != nil {
		return "", false, err
	}
	version := ""
	missingPython := false
	for line := range strings.Lines(strings.TrimSpace(out)) {
		switch strings.TrimSpace(line) {
		case "NO_PYTHON3":
			missingPython = true
		case "":
		default:
			version = strings.TrimSpace(line)
		}
	}
	return version, missingPython, nil
}

// installShims uploads the shim script and links every shim name.
func installShims(sshPath, host string, params sessionParams) error {
	links := make([]string, 0, len(shimNames))
	for _, name := range shimNames {
		links = append(links, fmt.Sprintf(`ln -sf slush-shim "$dir/bin/%s" || exit 1`, name))
	}
	script := `dir="${XDG_CACHE_HOME:-$HOME/.cache}/slush"` + "\n" +
		`mkdir -p "$dir/bin" || exit 1` + "\n" +
		`cat > "$dir/bin/slush-shim" || exit 1` + "\n" +
		`chmod +x "$dir/bin/slush-shim" || exit 1` + "\n" +
		strings.Join(links, "\n") + "\n" +
		fmt.Sprintf(`printf '%%s' %s > "$dir/VERSION" || exit 1`, shellQuote(shimVersion)) + "\n" +
		`echo INSTALLED` + "\n"

	out, err := runProvisionExec(sshPath, host, params.controlPath, script, strings.NewReader(shimScript))
	if err != nil {
		return err
	}
	if !strings.Contains(out, "INSTALLED") {
		return fmt.Errorf("unexpected install output: %q", out)
	}
	return nil
}

// runProvisionExec runs script on host via ssh, riding controlPath's
// master when possible and falling back to a direct connection.
// stdin feeds the remote stdin (used to upload the shim).
func runProvisionExec(sshPath, host, controlPath, script string, stdin *strings.Reader) (string, error) {
	if controlPath == "" {
		return sshExec(sshPath, host, script, stdin, nil)
	}
	mux := []string{"-o", "ControlMaster=no", "-S", controlPath}
	if out, err := sshExec(sshPath, host, script, stdin, mux); err == nil {
		return out, nil
	}
	return sshExec(sshPath, host, script, stdin, nil)
}

func sshExec(sshPath, host, script string, stdin *strings.Reader, extra []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	defer cancel()

	args := append([]string{}, extra...)
	args = append(args,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		host, script,
	)
	cmd := exec.CommandContext(ctx, sshPath, args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ssh %s: %w", host, err)
	}
	return string(out), nil
}

// shellQuote renders s safe as one shell word; only used for
// slush-generated hex and version constants, never user data.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
