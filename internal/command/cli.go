package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/fang/v2"
	"github.com/nnutter/slush/internal/protocol"
	"github.com/nnutter/slush/internal/session"
	"github.com/spf13/cobra"
)

var clipboardPort = protocol.DefaultPort

// remoteExitError carries a completed remote command's exit status. The remote
// command already owns its diagnostics, so Fang must not print another error.
type remoteExitError int

func (e remoteExitError) Error() string {
	return fmt.Sprintf("remote command exited with status %d", e)
}

// ExitCode maps a command error to the process exit status.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if status, ok := errors.AsType[remoteExitError](err); ok {
		return int(status)
	}
	return 1
}

// Execute runs the slush CLI with the supplied arguments.
func Execute(args []string) error {
	command := newCommand()
	command.SetArgs(args)
	return fang.Execute(context.Background(), command, fang.WithErrorHandler(func(w io.Writer, styles fang.Styles, err error) {
		if _, remoteExit := errors.AsType[remoteExitError](err); !remoteExit {
			fang.DefaultErrorHandler(w, styles, err)
		}
	}))
}

func newCommand() *cobra.Command {
	var mode clientMode
	var mosh, forwardAgent bool
	var port tcpPort
	var identity, config string
	var local, remote forwardValues

	options := func(cmd *cobra.Command, args []string) (session.Options, error) {
		if mosh {
			if cmd.Flags().Changed("transport") {
				return session.Options{}, fmt.Errorf("choose either --transport or --mosh, not both")
			}
			mode = modeMosh
		}
		if strings.HasPrefix(args[0], "-") || strings.ContainsAny(args[0], "\r\n\t ") {
			return session.Options{}, fmt.Errorf("invalid host %q; use an SSH alias or [user@]host", args[0])
		}
		if forwardAgent {
			if err := session.CheckLocalAgent(); err != nil {
				return session.Options{}, err
			}
		}
		for _, option := range []struct{ flag, path string }{{"-i", identity}, {"-F", config}} {
			if option.path == "" {
				continue
			}
			info, err := os.Stat(option.path)
			if err != nil {
				return session.Options{}, fmt.Errorf("%s file %q: %w", option.flag, option.path, err)
			}
			if !info.Mode().IsRegular() {
				return session.Options{}, fmt.Errorf("%s file %q must be a regular file", option.flag, option.path)
			}
		}
		return session.Options{
			Transport: session.Transport(mode), Host: args[0], Command: args[1:],
			SSHPort: uint16(port), IdentityFile: identity, ConfigFile: config,
			ForwardAgent: forwardAgent, LocalForwards: local.configuration(),
			RemoteForwards: remote.configuration(), ProtocolPort: clipboardPort,
		}, nil
	}

	hostRequired := func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("missing host; use %s [options] [user@]host", cmd.CommandPath())
		}
		return nil
	}
	root := &cobra.Command{
		Use:   "slush [options] [user@]host [command...]",
		Short: "SSH or mosh sessions with clipboard and URL forwarding",
		Long:  "Connect with SSH or mosh using the same connection and forwarding options.\nFlags after the host belong to the remote command, not slush.\nForward shorthand: PORT, PORT:PORT, PORT:HOST:PORT, or BIND:PORT:HOST:PORT.\nOmitted addresses mean localhost. A single port is used at both ends.\nBracket IPv6 addresses. Repeat -L or -R for multiple forwards.",
		Args:  hostRequired,
		RunE: func(cmd *cobra.Command, args []string) error {
			request, err := options(cmd, args)
			if err != nil {
				return err
			}
			return executeSession(request)
		},
	}
	root.Flags().SetInterspersed(false)
	flags := root.PersistentFlags()
	flags.Var(&mode, "transport", "Terminal transport: ssh or mosh")
	flags.BoolVar(&mosh, "mosh", false, "Use mosh (shorthand for --transport mosh)")
	flags.BoolVarP(&forwardAgent, "forward-agent", "A", false, "Forward the local SSH agent for the session lifetime (disabled by default)")
	flags.VarP(&port, "port", "p", "Connection port for SSH (otherwise use SSH configuration)")
	flags.StringVarP(&identity, "identity", "i", "", "Identity file for SSH in either transport")
	flags.StringVarP(&config, "config", "F", "", "Configuration file for SSH in either transport")
	flags.VarP(&local, "local-forward", "L", "Make a remote TCP port available locally; omitted addresses are localhost, one port is symmetric")
	flags.VarP(&remote, "remote-forward", "R", "Make a local TCP port available on the remote; omitted addresses are localhost, one port is symmetric")
	_ = root.RegisterFlagCompletionFunc("transport", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"ssh", "mosh"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = root.MarkPersistentFlagFilename("identity")
	_ = root.MarkPersistentFlagFilename("config")

	validate := &cobra.Command{
		Use:   "validate [options] [user@]host",
		Short: "Check provisioning, clipboard forwarding, and session integration",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := hostRequired(cmd, args); err != nil {
				return err
			}
			if len(args) != 1 {
				return fmt.Errorf("validate accepts one host, not a remote command")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			request, err := options(cmd, args)
			if err != nil {
				return err
			}
			token, err := protocol.GenerateToken()
			if err != nil {
				return err
			}
			return runValidateChecksWithOptions(request, token, cmd.OutOrStdout())
		},
	}
	validate.Flags().SetInterspersed(false)
	root.AddCommand(validate)
	return root
}

func executeSession(options session.Options) error {
	code, err := session.Run(options)
	if err != nil {
		return err
	}
	if code != 0 {
		return remoteExitError(code)
	}
	return nil
}
