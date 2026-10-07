package command

import (
	"fmt"
	"slices"
	"strings"
)

// clipboardReverseTunnel is OpenSSH -R syntax:
// [bind_address:]port:host:hostport with an implicit bind on the
// remote side. It carries the clipboard protocol to the local server.
const clipboardReverseTunnel = "2489:127.0.0.1:2489"

type clientMode int

const (
	modeSSH clientMode = iota
	modeMosh
)

// remoteSlushDirExpr is the shell expression for the provisioned
// slush directory. It must match shim.py's shim_dir.
const remoteSlushDirExpr = `${XDG_CACHE_HOME:-$HOME/.cache}/slush`

// remoteShimDirExpr is the provisioned shim directory expression.
const remoteShimDirExpr = `"${XDG_CACHE_HOME:-$HOME/.cache}/slush/bin"`

// remoteEnvPrefix returns a POSIX shell prefix exporting the slush
// session environment: clipboard token, shim PATH shadow, and BROWSER
// so gh/git/xdg-open callers reach slush-open. It carries no user
// data (the token is hex), so callers can safely prepend it to remote
// commands. The trailing "; " separates it from the command.
func remoteEnvPrefix(token string) string {
	return fmt.Sprintf("export SLUSH=1 SLUSH_TOKEN=%s BROWSER=slush-open PATH=%s:$PATH; ",
		token, remoteShimDirExpr)
}

// sshHostOperand returns the [user@]host operand from ssh-style args,
// skipping flags (and their arguments) as well as -L/-R forwards.
func sshHostOperand(args []string) (string, error) {
	idx, err := sshHostIndex(args)
	if err != nil {
		return "", err
	}
	return args[idx], nil
}

// splitSSHRemoteCommand splits ssh-style args into the head (options
// through the host operand) and the remote command following it.
func splitSSHRemoteCommand(args []string) (head, cmd []string, err error) {
	idx, err := sshHostIndex(args)
	if err != nil {
		return nil, nil, err
	}
	return slices.Clone(args[:idx+1]), slices.Clone(args[idx+1:]), nil
}

// Bash login profiles (notably macOS path_helper) can put native tools
// ahead of the injected PATH. Restore the session environment after those
// profiles run. Zsh uses startup wrappers via ZDOTDIR for the same reason;
// other shells retain their usual login startup.
const interactiveShellCommand = `case "${SHELL:-/bin/sh}" in */bash) exec "$SHELL" --rcfile "${XDG_CACHE_HOME:-$HOME/.cache}/slush/bashrc" -i ;; */zsh) export SLUSH_ZDOTDIR="${ZDOTDIR-}" ZDOTDIR="${XDG_CACHE_HOME:-$HOME/.cache}/slush/zsh"; exec "$SHELL" -l ;; *) exec "${SHELL:-/bin/sh}" -l ;; esac`

// withRemoteEnvSSH wraps the remote command (or a fresh login shell
// when the user gave none) with the slush session environment.
// It reports whether the session is interactive (no user command).
func withRemoteEnvSSH(rest []string, token string) ([]string, bool, error) {
	head, cmd, err := splitSSHRemoteCommand(rest)
	if err != nil {
		return nil, false, err
	}
	tail := strings.Join(cmd, " ")
	interactive := len(cmd) == 0
	if interactive {
		tail = interactiveShellCommand
	}
	return append(head, remoteEnvPrefix(token)+tail), interactive, nil
}

// sshHostIndex returns the position of the [user@]host operand.
func sshHostIndex(args []string) (int, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 >= len(args) {
				return 0, fmt.Errorf("missing ssh destination host")
			}
			return i + 1, nil
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if arg == "-" {
				continue
			}
			return i, nil
		}
		if isCombinedSSHFlag(arg) {
			continue
		}
		if sshFlagTakesArg(arg) {
			i++
			continue
		}
		// Anything else starting with '-' is a flag or a bundle of
		// single-letter flags; none of them is the host operand.
	}
	return 0, fmt.Errorf("missing ssh destination host")
}

// sshFlagTakesArg reports whether an ssh flag consumes the next argument.
// It matches whole flags only; see isCombinedSSHFlag for -Xvalue forms.
func sshFlagTakesArg(flag string) bool {
	switch flag {
	case "-b", "-c", "-D", "-E", "-F", "-I", "-J", "-L", "-R",
		"-S", "-W", "-Q", "-l", "-i", "-m", "-o", "-p",
		"-G", "-w":
		return true
	default:
		return false
	}
}

// isCombinedSSHFlag reports whether arg is a combined short option
// (-p2222, -luser) that carries its value inline and consumes no
// further argument.
func isCombinedSSHFlag(arg string) bool {
	return len(arg) > 2 && arg[0] == '-' && arg[1] != '-' &&
		sshFlagTakesArg(arg[:2])
}

// withSSHControlPath ensures the ssh client reuses the ControlMaster
// socket that holds the port forwards. Ours sorts first so a user
// -o ControlPath cannot silently detach the client from the tunnel.
func withSSHControlPath(args []string, params sessionParams) []string {
	return slices.Concat([]string{"-o", "ControlPath=" + params.controlPath}, args)
}

// withReverseTunnel returns args with the clipboard reverse tunnel
// injected unless an identical -R tunnel is already present.
func withReverseTunnel(args []string) []string {
	if hasSSHForward(args, "-R", clipboardReverseTunnel) {
		return slices.Clone(args)
	}
	return slices.Concat([]string{"-R", clipboardReverseTunnel}, args)
}

func hasSSHForward(args []string, flag, spec string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == flag:
			if i+1 < len(args) && args[i+1] == spec {
				return true
			}
			i++
		case isCombinedShortTunnel(arg, flag, spec):
			return true
		}
	}
	return false
}

func isCombinedShortTunnel(arg, flag, tunnel string) bool {
	return len(arg) > len(flag) &&
		arg[:len(flag)] == flag &&
		arg[len(flag):] == tunnel
}

// moshDestination returns the [user@]host operand from mosh-style args.
func moshDestination(args []string) (string, error) {
	host, _, err := destinationHostAt(args, "mosh", moshFlagTakesArg, isMoshCombinedShortOpt)
	return host, err
}

// splitMoshCommand splits mosh-style args into the leading options,
// the host, and the remote command. The options never contain the
// "--" separator: at most one is kept, normalized to exactly one in
// withRemoteEnvMosh.
func splitMoshCommand(args []string) (pre []string, host string, cmd []string, err error) {
	host, idx, err := destinationHostAt(args, "mosh", moshFlagTakesArg, isMoshCombinedShortOpt)
	if err != nil {
		return nil, "", nil, err
	}
	pre = slices.Clone(args[:idx])
	if len(pre) > 0 && pre[len(pre)-1] == "--" {
		pre = pre[:len(pre)-1]
	}
	return pre, host, slices.Clone(args[idx+1:]), nil
}

// withRemoteEnvMosh rebuilds mosh args with the session environment.
// mosh-server execs its command directly (no shell), so the wrapper
// is explicit argv: sh -c 'export ...; exec ...'. Exactly one "--"
// separates options from the host: mosh's option parser abbreviates
// -c to --client, so an unprotected `sh -c` would misparse.
func withRemoteEnvMosh(args []string, params sessionParams) ([]string, error) {
	pre, host, cmd, err := splitMoshCommand(args)
	if err != nil {
		return nil, err
	}
	script := remoteEnvPrefix(params.token)
	if params.agentSocket != "" {
		script += "export SSH_AUTH_SOCK=" + shellQuote(params.agentSocket) + "; "
	}
	tail := []string{}
	if len(cmd) == 0 {
		script += interactiveShellCommand
	} else {
		script += `exec "$@"`
		tail = append([]string{"sh"}, cmd...)
	}
	out := append(pre, "--", host, "sh", "-c", script)
	return append(out, tail...), nil
}

// destinationHostAt is destinationHost that also reports the host's
// position so callers can split options from the remote command.
func destinationHostAt(
	args []string,
	client string,
	flagTakesArg func(string) bool,
	isCombinedShort func(string) bool,
) (string, int, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 >= len(args) {
				return "", 0, fmt.Errorf("missing %s destination host", client)
			}
			return args[i+1], i + 1, nil
		}
		if !strings.HasPrefix(arg, "-") {
			return arg, i, nil
		}
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			continue
		}
		if isCombinedShort(arg) {
			continue
		}
		if flagTakesArg(arg) {
			i++
			continue
		}
	}
	return "", 0, fmt.Errorf("missing %s destination host", client)
}

func moshFlagTakesArg(flag string) bool {
	switch flag {
	case "--client", "--server", "--predict", "--family",
		"--port", "-p", "--ssh", "--bind-server",
		"--experimental-remote-ip":
		return true
	default:
		return false
	}
}

func isMoshCombinedShortOpt(arg string) bool {
	return strings.HasPrefix(arg, "-p") && arg != "-p" && !strings.HasPrefix(arg, "--")
}

// withMoshSSHControlPath supplies the held master's path to bootstrap SSH.
// Mosh's default IP-discovery proxy overrides it with -S none.
func withMoshSSHControlPath(args []string, params sessionParams) []string {
	opt := "-o ControlPath=" + params.controlPath
	out := slices.Clone(args)
	for i, arg := range out {
		if arg == "--ssh" && i+1 < len(out) {
			out[i+1] = out[i+1] + " " + opt
			return out
		}
		if after, found := strings.CutPrefix(arg, "--ssh="); found {
			out[i] = "--ssh=" + after + " " + opt
			return out
		}
	}
	return slices.Concat([]string{"--ssh=ssh " + opt}, args)
}
