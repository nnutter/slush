package command

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/nnutter/slush/internal/remote"
	"github.com/nnutter/slush/internal/session"
)

func newInstallCommand(options func(*cobra.Command, []string) (session.Options, error)) *cobra.Command {
	install := &cobra.Command{
		Use:               "install [options] [user@]host directory",
		Short:             "Install clipboard and URL wrappers in a remote directory",
		Long:              "Install wrappers without starting a session or changing shell startup files.\nThe directory must be on the remote process PATH. Quote '~/path' to expand ~ remotely.\nExisting wrapper names are replaced. Python 3 is required on the remote host.",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeHost,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, flag := range []string{"transport", "forward-agent", "local-forward", "remote-forward"} {
				if cmd.Flags().Changed(flag) {
					return fmt.Errorf("--%s is not supported by install; installation uses SSH without forwarding", flag)
				}
			}
			request, err := options(cmd, args)
			if err != nil {
				return err
			}
			sshPath, err := exec.LookPath("ssh")
			if err != nil {
				return fmt.Errorf("find ssh: %w", err)
			}
			var connectionOptions []string
			if request.SSHPort != 0 {
				connectionOptions = append(connectionOptions, "-p", fmt.Sprint(request.SSHPort))
			}
			if request.IdentityFile != "" {
				connectionOptions = append(connectionOptions, "-i", request.IdentityFile)
			}
			if request.ConfigFile != "" {
				connectionOptions = append(connectionOptions, "-F", request.ConfigFile)
			}
			if err := remote.Install(sshPath, request.Host, args[1], connectionOptions); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Installed slush wrappers on %s in %s\n", request.Host, args[1])
			return err
		},
	}
	install.Flags().SetInterspersed(false)
	return install
}
