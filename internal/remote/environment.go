package remote

import (
	"fmt"
	"strings"
)

// Environment identifies the forwarding endpoint and authentication for a
// remote session. ControlPath selects its held SSH connection.
type Environment struct {
	ControlPath string
	Token       string
	Port        int
	AgentSocket string
}

// DirExpr is the shell expression for the provisioned slush directory.
const DirExpr = `${XDG_CACHE_HOME:-$HOME/.cache}/slush`

const shimDirExpr = `"${XDG_CACHE_HOME:-$HOME/.cache}/slush/bin"`

// EnvPrefix exports the shim path, clipboard token, and browser handler.
func EnvPrefix(token string) string {
	return fmt.Sprintf("export SLUSH=1 SLUSH_TOKEN=%s BROWSER=slush-open PATH=%s:$PATH; ", token, shimDirExpr)
}

// interactiveShellCommand restores the session environment after user startup.
const interactiveShellCommand = `case "${SHELL:-/bin/sh}" in */bash) exec "$SHELL" --rcfile "${XDG_CACHE_HOME:-$HOME/.cache}/slush/bashrc" -i ;; */zsh) export SLUSH_ZDOTDIR="${ZDOTDIR-}" ZDOTDIR="${XDG_CACHE_HOME:-$HOME/.cache}/slush/zsh"; exec "$SHELL" -l ;; *) exec "${SHELL:-/bin/sh}" -l ;; esac`

// SSHCommand wraps a shell command or starts an integrated interactive shell.
func SSHCommand(command []string, token string) (string, bool) {
	interactive := len(command) == 0
	tail := strings.Join(command, " ")
	if interactive {
		tail = interactiveShellCommand
	}
	return EnvPrefix(token) + tail, interactive
}

// MoshCommand wraps an argv command for execution by mosh-server.
func MoshCommand(command []string, env Environment) []string {
	script := EnvPrefix(env.Token)
	if env.AgentSocket != "" {
		script += "export SSH_AUTH_SOCK=" + ShellQuote(env.AgentSocket) + "; "
	}
	var tail []string
	if len(command) == 0 {
		script += interactiveShellCommand
	} else {
		script += `exec "$@"`
		tail = append([]string{"sh"}, command...)
	}
	return append([]string{"sh", "-c", script}, tail...)
}
