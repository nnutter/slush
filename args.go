package main

import (
	"fmt"
	"slices"
	"strings"
)

// sshReverseTunnel is OpenSSH -R syntax: [bind_address:]port:host:hostport
// with an implicit bind on the remote side.
const sshReverseTunnel = "2489:127.0.0.1:2489"

type clientMode int

const (
	modeSSH clientMode = iota
	modeET
	modeMosh
)

// takeModeFlags consumes leading slush mode flags and returns the selected
// client mode plus remaining args.
func takeModeFlags(args []string) (clientMode, []string, error) {
	mode := modeSSH
	rest := args
	for len(rest) > 0 {
		switch rest[0] {
		case "--et":
			if mode == modeMosh {
				return 0, nil, fmt.Errorf("--et and --mosh are mutually exclusive")
			}
			mode = modeET
			rest = rest[1:]
		case "--mosh":
			if mode == modeET {
				return 0, nil, fmt.Errorf("--et and --mosh are mutually exclusive")
			}
			mode = modeMosh
			rest = rest[1:]
		default:
			return mode, rest, nil
		}
	}
	return mode, rest, nil
}

func clientBinary(mode clientMode) string {
	if mode == modeET {
		return "et"
	}
	if mode == modeMosh {
		return "mosh"
	}
	return "ssh"
}

// takeSSHForwards removes OpenSSH-style -L/-R forward options from args.
// Combined forms (-Lspec / -Rspec) are normalized to separate flag and spec.
func takeSSHForwards(args []string) (forwards, rest []string, err error) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i:]...)
			return forwards, rest, nil
		}
		if flag, spec, ok := splitCombinedForward(arg); ok {
			if spec == "" {
				return nil, nil, fmt.Errorf("%s requires an argument", flag)
			}
			forwards = append(forwards, flag, spec)
			continue
		}
		if arg == "-L" || arg == "-R" {
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("%s requires an argument", arg)
			}
			forwards = append(forwards, arg, args[i+1])
			i++
			continue
		}
		rest = append(rest, arg)
	}
	return forwards, rest, nil
}

func splitCombinedForward(arg string) (flag, spec string, ok bool) {
	for _, f := range []string{"-L", "-R"} {
		if strings.HasPrefix(arg, f) && arg != f {
			return f, arg[len(f):], true
		}
	}
	return "", "", false
}

// withReverseTunnel returns args with the Lemonade reverse tunnel injected
// unless an identical -R tunnel is already present.
func withReverseTunnel(args []string) []string {
	if hasSSHForward(args, "-R", sshReverseTunnel) {
		return slices.Clone(args)
	}
	return slices.Concat([]string{"-R", sshReverseTunnel}, args)
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
	return destinationHost(args, "mosh", moshFlagTakesArg, isMoshCombinedShortOpt)
}

// etDestination returns the [user@]host[:port] operand from et-style args.
func etDestination(args []string) (string, error) {
	return destinationHost(args, "et", etFlagTakesArg, func(string) bool { return false })
}

func destinationHost(
	args []string,
	client string,
	flagTakesArg func(string) bool,
	isCombinedShort func(string) bool,
) (string, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 >= len(args) {
				return "", fmt.Errorf("missing %s destination host", client)
			}
			return args[i+1], nil
		}
		if !strings.HasPrefix(arg, "-") {
			return arg, nil
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
	return "", fmt.Errorf("missing %s destination host", client)
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

func etFlagTakesArg(flag string) bool {
	switch flag {
	case "-c", "--command",
		"-s", "--serverpath",
		"-p", "--prefix", "--port",
		"-t", "--tunnel",
		"-r", "--reversetunnel",
		"-j", "--jumphost",
		"-w", "--keepalive", "--ping-interval",
		"-x", "--ssh-option",
		"--log-level", "--loglevel",
		"--max-log-size", "--max-log-count":
		return true
	default:
		return false
	}
}

// sshHostFromETDestination strips an optional ET :port suffix so the remainder
// is a valid ssh destination ([user@]host).
func sshHostFromETDestination(dest string) string {
	userPrefix := ""
	hostPart := dest
	if user, host, ok := strings.Cut(dest, "@"); ok {
		userPrefix = user + "@"
		hostPart = host
	}

	if strings.HasPrefix(hostPart, "[") {
		if idx := strings.LastIndex(hostPart, "]:"); idx >= 0 {
			return userPrefix + hostPart[:idx+1]
		}
		return dest
	}

	host, _, ok := strings.Cut(hostPart, ":")
	if !ok {
		return dest
	}
	return userPrefix + host
}

// withMoshSSHControlPath ensures mosh's bootstrap ssh reuses the ControlMaster
// socket that holds the port forwards.
func withMoshSSHControlPath(args []string, controlPath string) []string {
	opt := "-o ControlPath=" + controlPath
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
