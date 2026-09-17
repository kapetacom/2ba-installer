package configure

import (
	"os"
	"path/filepath"
)

// Hermes Agent (NousResearch/hermes-agent) is a Python CLI that reads its
// config from ~/.hermes/config.yaml. Unlike opencode/zcode/pi the file is
// YAML, not JSON, and the docs make a point of preserving user comments
// (the installer comment block in install.sh is a notable example). To avoid
// round-tripping the file through a YAML library and silently dropping
// user comments, the installer takes the same approach as
// InstructContinue / InstructCursor: print a copy-pasteable snippet for
// the user to add to their config.yaml. The key/value names below are the
// verbatim names from the upstream providers doc
// (hermes-agent.nousresearch.com/docs/integrations/providers).

// HermesHome returns the Hermes home directory, respecting $HERMES_HOME. It
// is the single source of truth for this path; the detect package reuses
// it instead of keeping a second copy.
func HermesHome() string {
	if v := os.Getenv("HERMES_HOME"); v != "" {
		return v
	}
	return filepath.Join(home(), ".hermes")
}

// hermesConfigFile is the user-scope config Hermes reads.
func hermesConfigFile() string {
	return filepath.Join(HermesHome(), "config.yaml")
}

// InstructHermes prints the YAML block the user should append to
// ~/.hermes/config.yaml to point Hermes at 2ba. The snippet uses the
// installer's own key names verbatim; a user's existing entries are not
// touched (Hermes reads the same file). On a missing home AND a missing
// `hermes` binary on PATH the function is a no-op so the same detection
// rule other instruct-mode services follow applies.
func InstructHermes(e *Env) {
	homeDir := HermesHome()
	if !dirExists(homeDir) && !onPath("hermes") {
		// No install to instruct: stay silent to match InstructContinue's
		// "no .continue dir" branch — printing a hint to a user who has
		// never installed Hermes adds noise.
		return
	}
	if e.Model == "" || e.APIKey == "" {
		e.warnf("hermes: refusing to print instructions with an empty model or API key")
		return
	}
	e.warnf("Hermes detected — add the snippet below to %s (merge into any existing top-level keys, don't paste a duplicate):", hermesConfigFile())
	// The block uses documented key names verbatim. `transport` defaults
	// to chat_completions when omitted; setting it explicitly avoids
	// ambiguity on future Hermes releases. `default_model` on a provider
	// makes the provider the source of truth for the entry's models.
	// Note: the installer prints this every run — Hermes reads YAML, and
	// duplicate top-level keys resolve last-wins per YAML 1.2. The user
	// is responsible for merging; we cannot tell from here whether the
	// snippet was already pasted. Same trade-off as InstructContinue.
	e.hintf("providers:")
	e.hintf("  2ba:")
	e.hintf("    api: %s", e.APIBase)
	e.hintf("    api_key: %s", e.APIKey)
	e.hintf("    transport: chat_completions")
	e.hintf("    default_model: %s", e.Model)
	e.hintf("model:")
	e.hintf("  provider: 2ba")
	e.hintf("  default: 2ba:%s", e.Model)
	// Hermes sources per-model capabilities from models.dev; amber is not
	// listed there. The hint below names the documented
	// `model_overrides.<provider>.<model>` block the user must add by hand
	// (or the simpler `supports_vision: true` on the top-level `model:`).
	e.hintf("# amber is not in models.dev — declare capabilities by hand:")
	e.hintf("#   model_overrides:")
	e.hintf("#     2ba:")
	e.hintf("#       %s:", e.Model)
	e.hintf("#         supports_vision: true")
	e.hintf("#         supports_reasoning: true")
	e.hintf("# or, equivalently, under the top-level model: section:")
	e.hintf("#   model:")
	e.hintf("#     supports_vision: true")
}
