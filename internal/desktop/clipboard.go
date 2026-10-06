// Package desktop integrates with native clipboard and URL-open tools.
//
// Backends shell out to the platform's canonical tools so slush honors
// the user's local configuration instead of reimplementing desktop
// integration: pbcopy/pbpaste/open on macOS, wl-copy/wl-paste (with
// xclip/xsel fallbacks) and xdg-open on Linux.
package desktop

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Copy writes data to the local system clipboard.
func Copy(data []byte) error {
	name, args, err := copyCommand()
	if err != nil {
		return err
	}
	return runWithStdin(name, args, data)
}

// Paste reads the local system clipboard.
func Paste() ([]byte, error) {
	name, args, err := pasteCommand()
	if err != nil {
		return nil, err
	}
	return runOutput(name, args)
}

// OpenURL opens target with the local desktop handler. Only URLs
// are supported: remote file paths cannot be opened on the client
// without file syncing, which slush deliberately does not do.
func OpenURL(target string) error {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return fmt.Errorf("empty open target")
	}
	if !hasURLScheme(trimmed) {
		return fmt.Errorf("not a URL %q; remote files cannot be opened locally (use Zed remote: zed ssh://host/path)", trimmed)
	}
	name, args, err := openCommand()
	if err != nil {
		return err
	}
	return runWithStdin(name, append(args, trimmed), nil)
}

// hasURLScheme reports whether target starts with a URI scheme
// ("scheme://...").
func hasURLScheme(target string) bool {
	scheme, rest, ok := strings.Cut(target, "://")
	if !ok || scheme == "" || rest == "" {
		return false
	}
	for i, r := range scheme {
		valid := r == '+' || r == '-' || r == '.' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')
		if !valid || (i == 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// ClipboardBackendName names the selected clipboard backend for display.
func ClipboardBackendName() string {
	name, _, err := copyCommand()
	if err != nil {
		return "unavailable: " + err.Error()
	}
	return name
}

// copyCommand selects the native clipboard-write tool.
func copyCommand() (string, []string, error) {
	switch runtime.GOOS {
	case "darwin":
		return lookCommand("pbcopy")
	case "windows":
		return lookCommand("powershell", "-NoProfile", "-Command", "$input | Set-Clipboard")
	default:
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			if name, args, err := lookCommand("wl-copy"); err == nil {
				return name, args, nil
			}
		}
		if name, args, err := lookCommand("xclip", "-selection", "clipboard"); err == nil {
			return name, args, nil
		}
		if name, args, err := lookCommand("xsel", "--clipboard", "--input"); err == nil {
			return name, args, nil
		}
		return "", nil, fmt.Errorf("no clipboard tool found (install wl-clipboard or xclip)")
	}
}

// pasteCommand selects the native clipboard-read tool.
func pasteCommand() (string, []string, error) {
	switch runtime.GOOS {
	case "darwin":
		return lookCommand("pbpaste")
	case "windows":
		return lookCommand("powershell", "-NoProfile", "-Command", "Get-Clipboard")
	default:
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			if name, args, err := lookCommand("wl-paste"); err == nil {
				return name, args, nil
			}
		}
		if name, args, err := lookCommand("xclip", "-selection", "clipboard", "-o"); err == nil {
			return name, args, nil
		}
		if name, args, err := lookCommand("xsel", "--clipboard", "--output"); err == nil {
			return name, args, nil
		}
		return "", nil, fmt.Errorf("no clipboard tool found (install wl-clipboard or xclip)")
	}
}

// openCommand selects the native URL-open tool.
func openCommand() (string, []string, error) {
	switch runtime.GOOS {
	case "darwin":
		return lookCommand("open")
	case "windows":
		return lookCommand("rundll32", "url.dll,FileProtocolHandler")
	default:
		return lookCommand("xdg-open")
	}
}

// lookCommand resolves name on PATH and reports a helpful error when
// the native tool is missing.
func lookCommand(name string, args ...string) (string, []string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", nil, fmt.Errorf("%s not found on PATH: %w", name, err)
	}
	return path, args, nil
}

func runWithStdin(name string, args []string, data []byte) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func runOutput(name string, args []string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}
