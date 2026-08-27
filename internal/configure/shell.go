package configure

import (
	"fmt"
	"strings"
)

// managedShellBlock is the block appended to the shell rc. The key is read
// from its 0600-perm file rather than embedded, so a world-readable rc never
// exposes the secret. (2BA_API_KEY is not a legal POSIX env name, so the
// tools' native OPENAI_* names are exported instead.)
func managedShellBlock(e *Env) string {
	var b strings.Builder
	fmt.Fprintln(&b, BlockBegin)
	fmt.Fprintf(&b, "export OPENAI_API_BASE=%q\n", e.APIBase)
	fmt.Fprintf(&b, "export OPENAI_API_KEY=\"$(cat %q 2>/dev/null)\"\n", e.KeyFile)
	fmt.Fprintf(&b, "# pick the default model for aider with: aider --model openai/%s\n", e.Model)
	fmt.Fprintln(&b, BlockEnd)
	return b.String()
}

// ConfigureShellEnv appends the managed OPENAI_* block to the first shell rc,
// or replaces a previously written one.
func ConfigureShellEnv(e *Env) {
	rc := firstShellRC()
	if rc == "" {
		e.warnf("no shell rc found — skipping env setup")
		return
	}
	e.backup(rc)
	if e.DryRun {
		e.logf("would add 2ba.ai env to %s", rc)
		return
	}
	if hasExactLine(rc, BlockBegin) {
		ok, _ := stripManagedBlock(rc)
		if !ok {
			e.warnf("not modifying %s — resolve the stray marker and re-run", rc)
			return
		}
	}
	if err := appendBlock(rc, managedShellBlock(e)); err != nil {
		e.warnf("could not write %s: %v", rc, err)
		return
	}
	e.logf("OPENAI_* env added to %s (reload your shell, or source it)", rc)
}
