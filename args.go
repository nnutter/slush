package main

import (
	"fmt"
	"slices"
	"strings"
)

// sshReverseTunnel is OpenSSH -R syntax: [bind_address:]port:host:hostport
// with an implicit bind on the remote side.
const sshReverseTunnel = "2489:127.0.0.1:2489"

// etReverseTunnel is Eternal Terminal source:destination port syntax.
// ET defaults both sides to localhost. Do not use hostnames here: values with
// more than one ':' are parsed as ssh-style and require four fields.
const etReverseTunnel = "2489:2489"

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
	return "ssh"
}

func reverseTunnelFlag(mode clientMode) string {
	if mode == modeET {
		return "-r"
	}
	return "-R"
}

func reverseTunnelArg(mode clientMode) string {
	if mode == modeET {
		return etReverseTunnel
	}
	return sshReverseTunnel
}

// withReverseTunnel returns args with the Lemonade reverse tunnel injected
// unless an identical tunnel is already present for flag (e.g. "-R" or "-r").
func withReverseTunnel(args []string, flag, tunnel string) []string {
	if hasReverseTunnel(args, flag, tunnel) {
		return slices.Clone(args)
	}
	return slices.Concat([]string{flag, tunnel}, args)
}

func hasReverseTunnel(args []string, flag, tunnel string) bool {
	longFlag := reverseTunnelLongFlag(flag)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == flag || (longFlag != "" && arg == longFlag):
			if i+1 < len(args) && args[i+1] == tunnel {
				return true
			}
			i++
		case isCombinedShortTunnel(arg, flag, tunnel):
			return true
		case longFlag != "" && arg == longFlag+"="+tunnel:
			return true
		}
	}
	return false
}

func reverseTunnelLongFlag(flag string) string {
	if flag == "-r" {
		return "--reversetunnel"
	}
	return ""
}

func isCombinedShortTunnel(arg, flag, tunnel string) bool {
	return len(arg) > len(flag) &&
		arg[:len(flag)] == flag &&
		arg[len(flag):] == tunnel
}

// moshDestination returns the [user@]host operand from mosh-style args.
func moshDestination(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 >= len(args) {
				return "", fmt.Errorf("missing mosh destination host")
			}
			return args[i+1], nil
		}
		if !strings.HasPrefix(arg, "-") {
			return arg, nil
		}
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			continue
		}
		if isMoshCombinedShortOpt(arg) {
			continue
		}
		if moshFlagTakesArg(arg) {
			i++
			continue
		}
	}
	return "", fmt.Errorf("missing mosh destination host")
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

// withMoshSSHControlPath ensures mosh's bootstrap ssh reuses the ControlMaster
// socket that holds the Lemonade reverse tunnel.
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
