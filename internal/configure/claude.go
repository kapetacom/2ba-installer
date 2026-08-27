package configure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Claude Code has no provider catalog of its own: it is pointed at a single
// Anthropic-compatible gateway through ANTHROPIC_* environment variables,
// which the installer manages inside the "env" block of
// <config dir>/settings.json. 2ba's gateway (Heimdall) serves the Messages
// API at <origin>/v1/messages, and Claude Code appends /v1/messages to
// ANTHROPIC_BASE_URL itself, so the stored base is the origin without /v1.
//
// The key goes in as ANTHROPIC_AUTH_TOKEN (Bearer) — Heimdall also accepts
// x-api-key, but Bearer is 2ba's canonical scheme. ANTHROPIC_SMALL_FAST_MODEL
// is pinned to the same model because Claude Code otherwise falls back to a
// haiku model id for background tasks, which 2ba does not serve.
//
// ANTHROPIC_BASE_URL is a single global knob with no 2ba-specific slot, so we
// only write it when it is unset; a settings file already pointing at another
// gateway is left untouched.

// ClaudeConfigDir returns $CLAUDE_CONFIG_DIR or ~/.claude. It is the single
// source of truth for this path; the detect package reuses it instead of
// keeping a second copy.
func ClaudeConfigDir() string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	return filepath.Join(home(), ".claude")
}

// claudeSettingsFile is the user-scope settings file Claude Code reads.
func claudeSettingsFile() string {
	return filepath.Join(ClaudeConfigDir(), "settings.json")
}

// claudeBaseURL returns the ANTHROPIC_BASE_URL for the configured API base:
// Claude Code appends /v1/messages itself, so strip an OpenAI-style /v1.
func claudeBaseURL(apiBase string) string {
	return strings.TrimSuffix(trimAPIBase(apiBase), "/v1")
}

// claudeManagedEnvVars are the settings env keys the installer owns.
// ANTHROPIC_API_KEY is deliberately not among them: it is the user's real
// Anthropic key, and uninstall must survive it.
var claudeManagedEnvVars = []string{
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_MODEL",
	"ANTHROPIC_SMALL_FAST_MODEL",
}

// ConfigureClaude points Claude Code at 2ba via the env block of
// settings.json. An existing 2ba configuration is left as-is; a settings
// file already pointing ANTHROPIC_BASE_URL at another gateway is left
// untouched.
func ConfigureClaude(e *Env) {
	home := ClaudeConfigDir()
	if !dirExists(home) && !onPath("claude") {
		e.warnf("Claude Code not detected (no %s) — install it, run it once, then re-run this installer", home)
		return
	}
	if e.Model == "" || e.APIKey == "" {
		e.warnf("claude: refusing to write with an empty model or API key")
		return
	}
	cfg := claudeSettingsFile()

	obj := map[string]any{}
	if exists, ok := loadJSONObject(cfg, &obj); exists && !ok {
		e.warnf("%s is not valid JSON — leaving it untouched (fix or remove it, then re-run)", cfg)
		return
	}
	env, _ := obj["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
		obj["env"] = env
	}
	ours := claudeBaseURL(e.APIBase)
	if base, ok := env["ANTHROPIC_BASE_URL"].(string); ok {
		// Normalize both sides: a hand-written config may carry the
		// OpenAI-style /v1 suffix.
		if claudeBaseURL(base) == ours {
			e.notef("Claude Code — already configured")
			return
		}
		e.warnf("Claude Code already points ANTHROPIC_BASE_URL at %s — leaving %s untouched", base, cfg)
		return
	}
	if e.DryRun {
		e.logf("would add 2ba provider to %s", cfg)
		return
	}
	env["ANTHROPIC_BASE_URL"] = ours
	env["ANTHROPIC_AUTH_TOKEN"] = e.APIKey
	env["ANTHROPIC_MODEL"] = e.Model
	env["ANTHROPIC_SMALL_FAST_MODEL"] = e.Model
	// claude may be installed but never run; create the config dir (0700,
	// since the settings carry a key) without touching an existing one.
	if err := os.MkdirAll(home, 0o700); err != nil {
		e.warnf("could not create %s: %v", home, err)
		return
	}
	e.backup(cfg)
	if err := writeIndentedJSON(cfg, obj); err != nil {
		e.warnf("could not write %s: %v", cfg, err)
		return
	}
	e.logf("Claude Code: 2ba configured, model %s (%s)", e.Model, cfg)
}

// claudeConfigManaged reports whether path's env block points at base — the
// predicate removeClaudeConfig removes on. Used to back the file up only
// when the uninstall actually removes the installer's keys.
func claudeConfigManaged(path, base string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return false
	}
	env, _ := obj["env"].(map[string]any)
	if env == nil {
		return false
	}
	baseURL, _ := env["ANTHROPIC_BASE_URL"].(string)
	return claudeBaseURL(baseURL) == base
}

// removeClaudeConfig strips the ANTHROPIC_* vars the installer wrote, but
// only when the settings point at base (the predicate ConfigureClaude used
// to write them). Unrelated settings files are left untouched, and a user's
// own ANTHROPIC_API_KEY always survives.
func removeClaudeConfig(path, base string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return false, err
	}
	env, _ := obj["env"].(map[string]any)
	if env == nil {
		return false, nil
	}
	baseURL, _ := env["ANTHROPIC_BASE_URL"].(string)
	if claudeBaseURL(baseURL) != base {
		return false, nil
	}
	for _, key := range claudeManagedEnvVars {
		delete(env, key)
	}
	if len(env) == 0 {
		delete(obj, "env")
	}
	if err := writeIndentedJSON(path, obj); err != nil {
		return false, err
	}
	return true, nil
}
