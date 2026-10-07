package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/nnutter/slush/internal/clipboard"
	"github.com/nnutter/slush/internal/process"
)

// clipboardPort is the application endpoint. Production uses DefaultPort;
// tests select an ephemeral port for isolated sessions.
var clipboardPort = clipboard.DefaultPort

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	mode, args, err := takeModeFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}

	// `slush validate` (and `slush --mosh validate ...`) runs probes
	// instead of a session. A host literally named validate still
	// works via `slush -- validate`.
	if len(args) > 0 && args[0] == "validate" {
		return runValidate(mode, args[1:])
	}

	token, err := clipboard.GenerateToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}

	if err := clipboard.EnsurePortFree(clipboardPort); err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}

	server, err := clipboard.StartServer(clipboardPort, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}
	defer server.Stop()

	code, err := runClient(mode, args, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slush: %v\n", err)
		return 1
	}
	return code
}

func runClient(mode clientMode, args []string, token string) (int, error) {
	options, err := sessionOptionsFromArgs(mode, args)
	if err != nil {
		return 0, err
	}
	return runClientWithOptions(options, token)
}

func runClientWithOptions(options sessionOptions, token string) (int, error) {
	switch options.mode {
	case modeMosh:
		return runMoshSession(options, token)
	default:
		return runSSHSession(options, token)
	}
}

func runSSHSession(options sessionOptions, token string) (int, error) {
	rest := options.args
	host, err := sshHostOperand(rest)
	if err != nil {
		return 0, err
	}
	// Connection options ride the tunnel master; forwards travel separately.
	clientArgs, interactive, err := withRemoteEnvSSH(rest, token)
	if err != nil {
		return 0, err
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return 0, fmt.Errorf("ssh not found on PATH: %w", err)
	}
	if interactive && process.StdinIsTerminal() {
		// A wrapped command is always present now, so ssh would
		// skip PTY allocation on its own; restore it for shells.
		clientArgs = append([]string{"-t"}, clientArgs...)
	}
	// Like --mosh: forwards ride a held ControlMaster child that
	// cannot outlive slush, and the interactive client reuses it. An
	// explicit master (not auto) keeps concurrent sessions from
	// stealing each other's forwards.
	return runTunneledSession(sshPath, host, options.connOpts, clientArgs, options.forwards, token, withSSHControlPath)
}
