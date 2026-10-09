package main

import (
	"os"

	"github.com/nnutter/slush/internal/command"
)

func main() {
	if err := command.Execute(os.Args[1:]); err != nil {
		os.Exit(command.ExitCode(err))
	}
}
