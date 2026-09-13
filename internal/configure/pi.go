package configure

import (
	"os"
	"path/filepath"
)

// Pi (pi.dev) declares custom providers in models.json inside its agent
// directory ($PI_CODING_AGENT_DIR, default ~/.pi/agent). 2ba speaks the
// OpenAI-compatible API, so the entry uses api "openai-completions". Unlike
// opencode/ZCode, Pi's models list is an array keyed by each entry's "id".
// The key is embedded in the entry (like ZCode's) so no env var is needed.

// PiAgentDir returns $PI_CODING_AGENT_DIR or ~/.pi/agent. It is the single
// source of truth for this path; the detect package reuses it instead of
// keeping a second copy.
func PiAgentDir() string {
	if v := os.Getenv("PI_CODING_AGENT_DIR"); v != "" {
		return v
	}
	return filepath.Join(home(), ".pi", "agent")
}

// piModelsFile is the custom-provider catalog Pi reads.
func piModelsFile() string {
	return filepath.Join(PiAgentDir(), "models.json")
}

// piModelEntry returns the model entry the installer writes: a friendly
// name, the thinking-model declaration (the amber backend accepts the
// low/medium/high reasoning_effort values Pi sends for OpenAI-compatible
// providers), image input, and the same conservative context window the
// other declarative targets use.
func piModelEntry(model string) map[string]any {
	return map[string]any{
		"id":            model,
		"name":          pyCapitalize(model) + " (2ba.ai)",
		"reasoning":     true,
		"input":         []string{"text", "image"},
		"contextWindow": zcodeContextSize,
	}
}

// patchPiModel upgrades the model entry whose id is model in an existing
// "2ba" provider to the current declaration — the upgrade path for configs
// written by an older installer. Missing fields are added; existing values
// are never overwritten, and an entry without our model (user-managed) is
// left alone. It reports whether the config changed.
func patchPiModel(provider any, model string) bool {
	p, ok := provider.(map[string]any)
	if !ok {
		return false
	}
	models, ok := p["models"].([]any)
	if !ok {
		return false
	}
	changed := false
	for _, entry := range models {
		m, ok := entry.(map[string]any)
		if !ok || m["id"] != model {
			continue
		}
		for k, v := range piModelEntry(model) {
			if _, exists := m[k]; !exists {
				m[k] = v
				changed = true
			}
		}
	}
	return changed
}

// ConfigurePi adds a "2ba" OpenAI-compatible provider to Pi's models.json.
// An existing "2ba" entry from an older installer is upgraded in place with
// the current model declaration instead of being skipped.
func ConfigurePi(e *Env) {
	agentDir := PiAgentDir()
	if !dirExists(agentDir) && !onPath("pi") {
		e.warnf("Pi not detected (no %s) — install it, run it once, then re-run this installer", agentDir)
		return
	}
	if e.Model == "" || e.APIKey == "" {
		e.warnf("pi: refusing to write with an empty model or API key")
		return
	}
	cfg := piModelsFile()
	obj := map[string]any{}
	if exists, ok := loadJSONObject(cfg, &obj); exists && !ok {
		e.warnf("%s is not valid JSON — leaving it untouched (fix or remove it, then re-run)", cfg)
		return
	}
	providers, _ := obj["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
		obj["providers"] = providers
	}
	if existing, exists := providers["2ba"]; exists {
		if !patchPiModel(existing, e.Model) {
			e.notef("Pi — already configured")
			return
		}
		if e.DryRun {
			e.logf("would add model capabilities to the existing \"2ba\" provider in %s", cfg)
			return
		}
		e.backup(cfg)
		if err := writeIndentedJSON(cfg, obj); err != nil {
			e.warnf("could not write %s: %v", cfg, err)
			return
		}
		e.logf("Pi: model capabilities added to the existing \"2ba\" provider (%s)", cfg)
		return
	}
	if e.DryRun {
		e.logf("would add 2ba provider to %s", cfg)
		return
	}
	// pi may be installed but never run; create the agent dir (0700, since
	// the catalog carries a key) without touching an existing one.
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		e.warnf("could not create %s: %v", agentDir, err)
		return
	}
	e.backup(cfg)
	providers["2ba"] = map[string]any{
		"baseUrl": e.APIBase,
		"api":     "openai-completions",
		"apiKey":  e.APIKey,
		"models":  []any{piModelEntry(e.Model)},
	}
	if err := writeIndentedJSON(cfg, obj); err != nil {
		e.warnf("could not write %s: %v", cfg, err)
		return
	}
	e.logf("Pi: provider \"2ba\" added, model %s (%s)", e.Model, cfg)
}
