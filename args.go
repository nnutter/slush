package main

import "slices"

const reverseTunnel = "2489:127.0.0.1:2489"

// withReverseTunnel returns args with the Lemonade reverse tunnel injected
// unless an identical -R tunnel is already present.
func withReverseTunnel(args []string) []string {
	if hasReverseTunnel(args) {
		return slices.Clone(args)
	}
	return slices.Concat([]string{"-R", reverseTunnel}, args)
}

func hasReverseTunnel(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-R":
			if i+1 < len(args) && args[i+1] == reverseTunnel {
				return true
			}
			i++
		case len(arg) > 2 && arg[:2] == "-R" && arg[2:] == reverseTunnel:
			return true
		}
	}
	return false
}
