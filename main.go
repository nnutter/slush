package main

import (
	"os"

	"github.com/nnutter/slush/internal/command"
)

var version = "dev"

func main() {
	if err := command.Execute(os.Args[1:], version); err != nil {
		os.Exit(command.ExitCode(err))
	}
}
