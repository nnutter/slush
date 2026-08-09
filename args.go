package main

import "slices"

const reverseTunnel = "2489:127.0.0.1:2489"

// withReverseTunnel returns args with the Lemonade reverse tunnel injected
// unless an identical tunnel is already present for flag (e.g. "-R" or "-r").
func withReverseTunnel(args []string, flag string) []string {
	if hasReverseTunnel(args, flag) {
		return slices.Clone(args)
	}
	return slices.Concat([]string{flag, reverseTunnel}, args)
}

func hasReverseTunnel(args []string, flag string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == flag:
			if i+1 < len(args) && args[i+1] == reverseTunnel {
				return true
			}
			i++
		case isCombinedShortTunnel(arg, flag):
			return true
		}
	}
	return false
}

func isCombinedShortTunnel(arg, flag string) bool {
	return len(arg) > len(flag) &&
		arg[:len(flag)] == flag &&
		arg[len(flag):] == reverseTunnel
}
