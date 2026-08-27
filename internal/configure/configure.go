// Package configure writes (and, on --uninstall, removes) the 2ba
// configuration for each supported coding tool. The on-disk formats,
// idempotency rules, and no-clobber behaviour intentionally match the
// historical install.sh.
package configure

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kapetacom/2ba-installer/internal/ui"
)

// BlockBegin/BlockEnd delimit the installer-managed block inside shell rc and
// kimi TOML files.
const (
	BlockBegin = "# >>> 2ba.ai (managed) >>>"
	BlockEnd   = "# <<< 2ba.ai (managed) <<<"
)

// Env carries everything the configurators need.
type Env struct {
	Model     string
	APIBase   string
	APIOrigin string
	APIKey    string // real key, embedded where the format requires it
	KeyFile   string // path to the 0600 key file (referenced by the shell rc)
	DryRun    bool
	Out       io.Writer // default os.Stdout
}

// NewEnv returns an Env writing to os.Stdout.
func NewEnv(model, apiBase, apiOrigin, apiKey, keyFile string, dryRun bool) *Env {
	return &Env{
		Model:     model,
		APIBase:   apiBase,
		APIOrigin: apiOrigin,
		APIKey:    apiKey,
		KeyFile:   keyFile,
		DryRun:    dryRun,
		Out:       os.Stdout,
	}
}

func (e *Env) logf(format string, a ...any)  { fmt.Fprintln(e.Out, ui.Log(fmt.Sprintf(format, a...))) }
func (e *Env) warnf(format string, a ...any) { fmt.Fprintln(e.Out, ui.Warn(fmt.Sprintf(format, a...))) }
func (e *Env) notef(format string, a ...any) { fmt.Fprintln(e.Out, ui.Note(fmt.Sprintf(format, a...))) }
func (e *Env) hintf(format string, a ...any) { fmt.Fprintln(e.Out, ui.Hint(fmt.Sprintf(format, a...))) }

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// onPath reports whether a command is on PATH.
func onPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

// xdgConfig returns $XDG_CONFIG_HOME or ~/.config.
func xdgConfig() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config")
}

func kimiCodeHome() string {
	if v := os.Getenv("KIMI_CODE_HOME"); v != "" {
		return v
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".kimi-code")
}

// firstShellRC returns the first existing of ~/.zshrc, ~/.bashrc, ~/.profile.
func firstShellRC() string {
	h, _ := os.UserHomeDir()
	for _, n := range []string{".zshrc", ".bashrc", ".profile"} {
		p := filepath.Join(h, n)
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// backup copies path to path+".bak.2ba" once (never overwrites an existing
// backup). No-op when the source is missing. In dry-run it only logs.
func (e *Env) backup(path string) {
	bak := path + ".bak.2ba"
	if !fileExists(path) {
		return
	}
	if fileExists(bak) {
		return
	}
	if e.DryRun {
		e.logf("would back up %s", path)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	st, _ := os.Stat(path)
	perm := os.FileMode(0o644)
	if st != nil {
		perm = st.Mode().Perm()
	}
	_ = os.WriteFile(bak, data, perm)
}

// hasExactLine reports whether path contains a line exactly equal to line.
func hasExactLine(path, line string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, l := range bytes.Split(data, []byte("\n")) {
		if string(l) == line {
			return true
		}
	}
	return false
}

// containsSubstring reports whether path contains substr anywhere.
func containsSubstring(path, substr string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte(substr))
}

// stripManagedBlock removes the managed block (BlockBegin through BlockEnd,
// inclusive) from path. It requires both markers on their own lines; if the
// end marker is missing it refuses to touch the file and returns ok=false.
// The file is rewritten in place (permissions preserved).
func stripManagedBlock(path string) (ok bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	lines := bytes.Split(data, []byte("\n"))
	begin, end := -1, -1
	for i, l := range lines {
		s := string(l)
		if s == BlockBegin && begin == -1 {
			begin = i
		}
		if s == BlockEnd && begin != -1 && end == -1 {
			end = i
		}
	}
	if begin == -1 || end == -1 || end < begin {
		return false, nil // no well-formed block to remove
	}
	kept := append(append([][]byte{}, lines[:begin]...), lines[end+1:]...)
	if len(kept) == 1 && len(kept[0]) == 0 {
		kept = nil
	}
	_ = os.WriteFile(path, bytes.Join(kept, []byte("\n")), 0o644) // perms preserved on existing file
	return true, nil
}

// appendBlock appends block to path, ensuring a separating newline first if
// the file lacks a trailing one. It preserves the file's existing permissions
// (a new file is created 0600); callers that need to tighten perms chmod
// explicitly afterwards.
func appendBlock(path, block string) error {
	var prefix string
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 && data[len(data)-1] != '\n' {
		prefix = "\n"
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(prefix + block)
	return err
}
