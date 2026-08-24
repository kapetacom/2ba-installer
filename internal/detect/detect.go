// Package detect reports which of the supported coding tools are present on
// this machine, so the installer can pre-select them. The predicates mirror
// the historical install.sh so behaviour is unchanged.
package detect

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Services is the set of supported targets and whether each should be
// configured.
type Services struct {
	Shell    bool
	Opencode bool
	Windsurf bool
	Kimi     bool
	Continue bool
	Cursor   bool

	// ShellRC is the first existing shell rc file ("" if none).
	ShellRC string
}

// ShellRCNames is the priority order used to pick the shell rc to edit.
var ShellRCNames = []string{".zshrc", ".bashrc", ".profile"}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

func xdgConfig() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	return filepath.Join(home(), ".config")
}

func kimiCodeHome() string {
	if v := os.Getenv("KIMI_CODE_HOME"); v != "" {
		return v
	}
	return filepath.Join(home(), ".kimi-code")
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// onPath reports whether a command is on PATH.
func onPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// FirstShellRC returns the first existing rc in priority order, or "".
func FirstShellRC() string {
	h := home()
	for _, n := range ShellRCNames {
		p := filepath.Join(h, n)
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// Detect inspects the environment and returns which services are present.
func Detect() Services {
	var s Services
	h := home()

	s.Shell = FirstShellRC() != ""
	s.ShellRC = FirstShellRC()

	s.Opencode = dirExists(filepath.Join(xdgConfig(), "opencode"))
	s.Windsurf = dirExists(filepath.Join(h, ".codeium", "windsurf"))
	s.Kimi = dirExists(filepath.Join(h, ".kimi")) || dirExists(kimiCodeHome()) ||
		onPath("kimi")
	s.Continue = dirExists(filepath.Join(h, ".continue"))
	s.Cursor = dirExists(filepath.Join(h, ".cursor")) || onPath("cursor")

	return s
}

// Has reports whether the named service is present in s.
func (s Services) Has(name string) bool {
	switch name {
	case "shell":
		return s.Shell
	case "opencode":
		return s.Opencode
	case "windsurf":
		return s.Windsurf
	case "kimi":
		return s.Kimi
	case "continue":
		return s.Continue
	case "cursor":
		return s.Cursor
	}
	return false
}
