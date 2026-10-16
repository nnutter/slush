package remote

import "strings"

// Install writes clipboard and URL commands to ~/.slush/bin without
// creating a session or changing its credentials or shell startup files.
func Install(sshPath, host string, connectionOptions []string) error {
	script := "umask 022\n" +
		`command -v python3 >/dev/null 2>&1 || { echo 'remote has no python3; clipboard shims need it' >&2; exit 1; }` + "\n" +
		`bin="${HOME:?}/.slush/bin"` + "\n" + shimInstallScript()
	_, err := SSHExec(sshPath, host, script, strings.NewReader(shimScript), connectionOptions)
	return err
}
