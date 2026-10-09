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
	path, err := clientPath(options.Transport)
	if err != nil {
		return 0, err
	}
	connection, err := Open(options, token)
	if err != nil {
		return 0, err
	}
	defer connection.Close()
	if options.Transport == Auto {
		path, err = clientPath(connection.Transport())
		if err != nil {
			return 0, err
		}
	}
	return connection.runClient(path)
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
	host, err := sessionHost(native)
	if err != nil {
		return nil, err
	}
	close, params, err := establishSession(sshPath, host, native, token)
	if err != nil {
		return nil, err
	}
	connection := &Session{options: native, sshPath: sshPath, host: host, params: params, close: close}
	if options.Transport == Auto {
		mode, err := connection.autoTransport()
		if err != nil {
			connection.Close()
			return nil, err
		}
		options.Transport = mode
		connection.options = options.nativeOptions()
	}
	return connection, nil
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

func (s *Session) autoTransport() (Transport, error) {
	if _, err := exec.LookPath("mosh"); err != nil {
		return SSH, nil
	}
	// Probe the bootstrap's normal PATH, not the shim-prefixed session PATH.
	// A missing master must fail instead of opening a new SSH connection.
	output, err := remote.SSHExec(s.sshPath, s.host,
		"if command -v mosh-server >/dev/null 2>&1; then printf mosh; else printf ssh; fi", nil,
		append([]string{"-o", "ControlMaster=no", "-o", "ProxyCommand=false", "-S", s.params.controlPath}, s.options.connOpts...))
	if err != nil {
		return SSH, fmt.Errorf("detect remote Mosh availability: %w", err)
	}
	switch strings.TrimSpace(output) {
	case "mosh":
		return Mosh, nil
	case "ssh":
		return SSH, nil
	default:
		return SSH, fmt.Errorf("unexpected remote Mosh availability response: %q", output)
	}
}

func (s *Session) moshClientArgs() ([]string, error) {
	clientArgs, err := withRemoteEnvMosh(s.options.args, s.params)
	if err != nil {
		return nil, err
	}
	quoted := make([]string, len(s.options.connOpts))
	for i, option := range s.options.connOpts {
		quoted[i] = remote.ShellQuote(option)
	}
	// Mosh's default IP discovery disables multiplexing with -S none.
	// Its short-lived bootstrap must not create a second agent socket.
	clientArgs = append([]string{"--no-ssh-pty", "--ssh=ssh -oForwardAgent=no " + strings.Join(quoted, " ")}, clientArgs...)
	return withMoshSSHControlPath(clientArgs, s.params), nil
}

func (s *Session) runClient(path string) (int, error) {
	var args []string
	var err error
	if s.options.mode == Mosh {
		args, err = s.moshClientArgs()
	} else {
		args, err = s.sshClientArgs()
	}
	if err != nil {
		return 0, err
	}
	return process.Run(path, args, path == s.sshPath)
}

func (s *Session) sshClientArgs() ([]string, error) {
	clientArgs, interactive, err := withRemoteEnvSSH(s.options.args, s.params.token)
	if err != nil {
		return nil, err
	}
	if interactive && process.StdinIsTerminal() {
		clientArgs = append([]string{"-t"}, clientArgs...)
	}
	return withSSHControlPath(clientArgs, s.params), nil
}

func sessionHost(options sessionOptions) (string, error) {
	if options.mode == Mosh {
		return moshDestination(options.args)
	}
	return sshHostOperand(options.args)
}

func clientPath(mode Transport) (string, error) {
	name := "ssh"
	if mode == Mosh {
		name = "mosh"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not found on PATH: %w", name, err)
	}
	return path, nil
}
