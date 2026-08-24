// Package termx decides where the installer should read input from and write
// output to so that the interactive menu works both when the binary is run
// directly (stdin is a terminal) and when it is invoked through a pipe such as
// `curl -fsSL https://2ba.ai/install.sh | sh` (stdin is the script pipe, but
// the controlling terminal is still reachable via /dev/tty).
package termx

import (
	"os"

	"golang.org/x/term"
)

// Interactive reports whether a real terminal is available for the menu:
// either stdin is already a TTY, or /dev/tty can be opened (the usual case
// when a piped-in script execs us). ok is false when we are genuinely
// headless (no controlling terminal), in which case callers should fall back
// to non-interactive behaviour.
func Interactive() (tty *os.File, ok bool) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, true // use os.Stdin/os.Stdout below; nothing to reopen
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, false
	}
	return tty, true
}

// IsTerminal reports whether f is an attached terminal.
func IsTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }
