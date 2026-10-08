package session

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/nnutter/slush/internal/process"
	"github.com/nnutter/slush/internal/protocol"
	"github.com/nnutter/slush/internal/remote"
)

// Run owns the local protocol server and terminal session until completion.
func Run(options Options) (int, error) {
	token, err := protocol.GenerateToken()
	if err != nil {
		return 0, err
	}
	if err := protocol.EnsurePortFree(options.ProtocolPort); err != nil {
		return 0, err
	}
	server, err := protocol.StartServer(options.ProtocolPort, token)
	if err != nil {
		return 0, err
	}
	defer server.Stop()
	return runClientWithOptions(options.nativeOptions(), token)
}

// Session is a prepared SSH connection shared by terminal clients and probes.
type Session struct {
	options sessionOptions
	sshPath string
	host    string
	params  sessionParams
	close   func()
}

// Open establishes forwarding and provisions remote integration. The caller
// owns the local protocol server and must close the session after use.
func Open(options Options, token string) (*Session, error) {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return nil, fmt.Errorf("ssh not found on PATH: %w", err)
	}
	native := options.nativeOptions()
	close, params, err := establishSession(sshPath, options.Host, native, token)
	if err != nil {
		return nil, err
	}
	return &Session{options: native, sshPath: sshPath, host: options.Host, params: params, close: close}, nil
}

// Close removes forwarding and terminates the held SSH connection.
func (s *Session) Close() { s.close() }

// Environment returns the forwarding credentials and remote socket paths.
func (s *Session) Environment() remote.Environment {
	return remote.Environment{
		ControlPath: s.params.controlPath, Token: s.params.token,
		Port: s.options.protocolPort, AgentSocket: s.params.agentSocket,
	}
}

// Probe executes a remote script through the held connection with shim exports.
func (s *Session) Probe(script string) (string, error) {
	prefix := remote.EnvPrefix(s.params.token) + fmt.Sprintf("SLUSH_PORT=%d ", s.options.protocolPort)
	return remote.SSHExec(s.sshPath, s.host, prefix+script, nil,
		append([]string{"-o", "ControlMaster=no", "-S", s.params.controlPath}, s.options.connOpts...))
}

// Transport reports the selected terminal transport.
func (s *Session) Transport() Transport { return s.options.mode }

// WrappedCommand prepares a remote shell command with the terminal transport's
// environment wrapper. It does not execute the command.
func (s *Session) WrappedCommand(command string) (string, error) {
	if s.options.mode == Mosh {
		wrapped := remote.MoshCommand([]string{"sh", "-c", command}, remote.Environment{Token: s.params.token})
		quoted := make([]string, len(wrapped)-3)
		for i, arg := range wrapped[3:] {
			quoted[i] = remote.ShellQuote(arg)
		}
		return "sh -c " + remote.ShellQuote(wrapped[2]) + " " + strings.Join(quoted, " "), nil
	}
	head, _, err := splitSSHRemoteCommand(append(s.options.args, "true"))
	if err != nil {
		return "", err
	}
	wrapped, _, err := withRemoteEnvSSH(append(head, command), s.params.token)
	if err != nil {
		return "", err
	}
	return wrapped[len(wrapped)-1], nil
}

func runClientWithOptions(options sessionOptions, token string) (int, error) {
	if options.mode == Mosh {
		return runMoshSession(options, token)
	}
	return runSSHSession(options, token)
}

func runSSHSession(options sessionOptions, token string) (int, error) {
	rest := options.args
	host, err := sshHostOperand(rest)
	if err != nil {
		return 0, err
	}
	clientArgs, interactive, err := withRemoteEnvSSH(rest, token)
	if err != nil {
		return 0, err
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return 0, fmt.Errorf("ssh not found on PATH: %w", err)
	}
	if interactive && process.StdinIsTerminal() {
		clientArgs = append([]string{"-t"}, clientArgs...)
	}
	return runTunneledSession(sshPath, host, options, clientArgs, token,
		func(args []string, params sessionParams) ([]string, error) {
			return withSSHControlPath(args, params), nil
		})
}
