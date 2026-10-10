package command

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

// completeHost reads local SSH files only. Completion must not start a session
// or run SSH configuration commands such as Match exec.
func completeHost(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	config, _ := cmd.Flags().GetString("config")
	if config == "" {
		config = filepath.Join(home, ".ssh", "config")
	}
	hosts := make(map[string]struct{})
	if absolute, err := filepath.Abs(config); err == nil {
		readSSHConfig(absolute, home, hosts, make(map[string]bool), 0)
	}
	readKnownHosts(filepath.Join(home, ".ssh", "known_hosts"), hosts)

	user, prefix, hasUser := strings.Cut(toComplete, "@")
	if hasUser {
		user += "@"
	} else {
		prefix, user = toComplete, ""
	}
	var matches []string
	for host := range hosts {
		if strings.HasPrefix(host, prefix) {
			matches = append(matches, user+host)
		}
	}
	slices.Sort(matches)
	return matches, cobra.ShellCompDirectiveNoFileComp
}

func addSSHHost(hosts map[string]struct{}, host string) {
	if host == "" || strings.HasPrefix(host, "-") || strings.ContainsAny(host, "*?!|@[]\r\n\t ") {
		return
	}
	hosts[host] = struct{}{}
}

// Includes are relative to ~/.ssh, not to the containing config file. Collect
// literal aliases from all blocks without evaluating Host or Match conditions.
func readSSHConfig(path, home string, hosts map[string]struct{}, visited map[string]bool, depth int) {
	// Bound recursion even when symlinked directories hide an include cycle.
	if depth >= 16 {
		return
	}
	path = filepath.Clean(path)
	if visited[path] {
		return
	}
	visited[path] = true
	// #nosec G703 -- Local users select config paths with -F and Include.
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		key, values := sshConfigDirective(scanner.Text())
		switch key {
		case "host":
			for _, host := range values {
				addSSHHost(hosts, host)
			}
		case "include":
			for _, pattern := range values {
				for _, included := range sshIncludePaths(pattern, home) {
					readSSHConfig(included, home, hosts, visited, depth+1)
				}
			}
		}
	}
}

func sshIncludePaths(pattern, home string) []string {
	if suffix, ok := strings.CutPrefix(pattern, "~/"); ok {
		pattern = filepath.Join(home, suffix)
	} else if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(home, ".ssh", pattern)
	}
	paths, _ := filepath.Glob(pattern)
	return paths
}

func sshConfigDirective(line string) (string, []string) {
	fields := sshConfigFields(line)
	if len(fields) == 0 {
		return "", nil
	}
	key, value, _ := strings.Cut(fields[0], "=")
	values := fields[1:]
	if value != "" {
		values = append([]string{value}, values...)
	} else if len(values) > 0 {
		if value, ok := strings.CutPrefix(values[0], "="); ok {
			if value == "" {
				values = values[1:]
			} else {
				values[0] = value
			}
		}
	}
	return strings.ToLower(key), values
}

// sshConfigFields handles quoted arguments and comments without interpreting
// shell expansions. SSH configuration is not a shell script.
func sshConfigFields(line string) []string {
	var fields []string
	var field strings.Builder
	var quote rune
	flush := func() {
		if field.Len() > 0 {
			fields = append(fields, field.String())
			field.Reset()
		}
	}
	for _, char := range line {
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				field.WriteRune(char)
			}
			continue
		}
		switch char {
		case '#':
			flush()
			return fields
		case '"', '\'':
			quote = char
		default:
			if unicode.IsSpace(char) {
				flush()
			} else {
				field.WriteRune(char)
			}
		}
	}
	if quote != 0 {
		return nil
	}
	flush()
	return fields
}

func readKnownHosts(path string, hosts map[string]struct{}) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 0 {
			if fields[0] == "@cert-authority" {
				fields = fields[1:]
			}
		}
		if len(fields) < 3 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		for host := range strings.SplitSeq(fields[0], ",") {
			// OpenSSH stores non-default ports as [host]:port. Slush accepts
			// the host alone and obtains the port from config or -p.
			if bracketed, _, ok := strings.Cut(host, "]:"); ok && strings.HasPrefix(bracketed, "[") {
				host = strings.TrimPrefix(bracketed, "[")
			}
			addSSHHost(hosts, host)
		}
	}
}
