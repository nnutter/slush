package remote

import (
	"fmt"
	"strings"
)

// Install writes clipboard and URL commands to a remote directory without
// creating a session or changing its credentials or shell startup files.
func Install(sshPath, host, directory string, connectionOptions []string) error {
	expression, err := installDirectory(directory)
	if err != nil {
		return err
	}
	script := "umask 022\n" +
		`command -v python3 >/dev/null 2>&1 || { echo 'remote has no python3; clipboard shims need it' >&2; exit 1; }` + "\n" +
		"bin=" + expression + "\n" + shimInstallScript()
	_, err = SSHExec(sshPath, host, script, strings.NewReader(shimScript), connectionOptions)
	return err
}

func installDirectory(directory string) (string, error) {
	if directory == "" || strings.ContainsRune(directory, '\x00') {
		return "", fmt.Errorf("install directory must be a nonempty path without NUL bytes")
	}
	if directory == "~" {
		return `"${HOME:?}"`, nil
	}
	if suffix, ok := strings.CutPrefix(directory, "~/"); ok {
		return `"${HOME:?}"/` + ShellQuote(suffix), nil
	}
	if strings.HasPrefix(directory, "~") {
		return "", fmt.Errorf("install directory does not support ~otheruser; use ~ or ~/path")
	}
	if !strings.HasPrefix(directory, "/") {
		directory = "./" + directory
	}
	return ShellQuote(directory), nil
}
