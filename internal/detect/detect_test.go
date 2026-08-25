package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, ".kimi-code"))

	_ = os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".zshrc"), []byte("#rc\n"), 0o600)
	_ = os.MkdirAll(filepath.Join(home, ".codeium", "windsurf"), 0o755)
	_ = os.MkdirAll(filepath.Join(home, ".kimi"), 0o755)
	_ = os.MkdirAll(filepath.Join(home, ".continue"), 0o755)
	_ = os.MkdirAll(filepath.Join(home, ".cursor"), 0o755)
	_ = os.MkdirAll(filepath.Join(home, ".zcode"), 0o755)

	s := Detect()
	if !s.Shell || !s.Opencode || !s.Windsurf || !s.Kimi || !s.Continue || !s.Cursor || !s.Zcode {
		t.Errorf("expected all services detected, got %+v", s)
	}
	if s.ShellRC != filepath.Join(home, ".zshrc") {
		t.Errorf("ShellRC = %q, want .zshrc", s.ShellRC)
	}
}

func TestFirstShellRCPriority(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_ = os.WriteFile(filepath.Join(home, ".profile"), []byte("x"), 0o600)
	if got := FirstShellRC(); got != filepath.Join(home, ".profile") {
		t.Errorf("FirstShellRC = %q, want .profile", got)
	}

	_ = os.WriteFile(filepath.Join(home, ".zshrc"), []byte("x"), 0o600)
	if got := FirstShellRC(); got != filepath.Join(home, ".zshrc") {
		t.Errorf("FirstShellRC = %q, want .zshrc (higher priority)", got)
	}
}

func TestHas(t *testing.T) {
	s := Services{Shell: true, Kimi: true, Zcode: true}
	if !s.Has("shell") || !s.Has("kimi") || !s.Has("zcode") {
		t.Error("Has should report present services")
	}
	if s.Has("cursor") || s.Has("bogus") {
		t.Error("Has should be false for absent/unknown services")
	}
}
