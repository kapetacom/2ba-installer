package configure

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// OpenClaw stores its main config at <state>/openclaw.json. The state dir is
// overridable with $OPENCLAW_STATE_DIR (the docs quote "~/.openclaw" as the
// default and do NOT honor $XDG_CONFIG_HOME). Custom providers live under
// models.providers; the default model is set through agents.defaults.model.

// OpenclawStateDir returns the OpenClaw state directory, respecting
// $OPENCLAW_STATE_DIR. It is the single source of truth for this path; the
// detect package reuses it instead of keeping a second copy.
func OpenclawStateDir() string {
	if v := os.Getenv("OPENCLAW_STATE_DIR"); v != "" {
		return v
	}
	return filepath.Join(home(), ".openclaw")
}

// openclawConfigFile is the user-scope config OpenClaw reads.
func openclawConfigFile() string {
	return filepath.Join(OpenclawStateDir(), "openclaw.json")
}

// openclawContextSize mirrors the other declarative targets: 2ba does not
// advertise a per-model context window, so a conservative value only affects
// when OpenClaw compacts.
const openclawContextSize = 262144

// openclawMaxTokens is the max output tokens the installer declares; OpenClaw
// stops generating at this limit, so we pick a generous value to avoid
// truncating long agent runs.
const openclawMaxTokens = 8192

// openclawModalities is the modalities declaration amber warrants: it takes
// images on input and emits text only. OpenClaw treats a model without
// image in its `input` list as text-only and refuses image attachments.
func openclawModalities() map[string]any {
	return map[string]any{"input": []string{"text", "image"}, "output": []string{"text"}}
}

// openclawCost is the zero-cost block the installer writes; 2ba is a flat
// subscription and the cost object is required to be an object on every model.
func openclawCost() map[string]any {
	return map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0}
}

// openclawModel returns the model entry the installer writes for model: a
// friendly name, the thinking-model declaration, image input, a conservative
// context window, and the zero-cost block.
func openclawModel(model string) map[string]any {
	return map[string]any{
		"id":            model,
		"name":          pyCapitalize(model) + " (2ba.ai)",
		"reasoning":     true,
		"input":         []string{"text", "image"},
		"contextWindow": openclawContextSize,
		"maxTokens":     openclawMaxTokens,
		"cost":          openclawCost(),
	}
}

// patchOpenclawModel brings the model entry of an existing "2ba" provider up
// to the current declaration — the upgrade path for configs written by an
// older installer. Missing fields are backfilled; existing values are never
// overwritten, and an entry without our model id (user-managed) is left alone.
// It reports whether the config changed.
func patchOpenclawModel(provider any, model string) bool {
	p, ok := provider.(map[string]any)
	if !ok {
		return false
	}
	models, ok := p["models"].([]any)
	if !ok {
		return false
	}
	found := false
	changed := false
	for _, entry := range models {
		m, ok := entry.(map[string]any)
		if !ok || m["id"] != model {
			continue
		}
		found = true
		if _, ok := m["reasoning"]; !ok {
			m["reasoning"] = true
			changed = true
		}
		if _, ok := m["input"]; !ok {
			m["input"] = []string{"text", "image"}
			changed = true
		}
		if _, ok := m["contextWindow"]; !ok {
			m["contextWindow"] = openclawContextSize
			changed = true
		}
		if _, ok := m["maxTokens"]; !ok {
			m["maxTokens"] = openclawMaxTokens
			changed = true
		}
		if _, ok := m["cost"]; !ok {
			m["cost"] = openclawCost()
			changed = true
		}
		if _, ok := m["name"]; !ok {
			m["name"] = pyCapitalize(model) + " (2ba.ai)"
			changed = true
		}
	}
	return found && changed
}

// openclawHasModel reports whether any provider in providers has a model with
// id == model — the predicate that decides whether a "2ba" provider is
// installer-managed (and therefore eligible for upgrade) or user-managed.
func openclawHasModel(providers map[string]any, model string) bool {
	for _, raw := range providers {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		models, _ := p["models"].([]any)
		for _, entry := range models {
			m, ok := entry.(map[string]any)
			if ok && m["id"] == model {
				return true
			}
		}
	}
	return false
}

// ConfigureOpenclaw adds a "2ba" OpenAI-compatible provider to
// <state>/openclaw.json and sets 2ba/<model> as the default agent model if
// none is set. An existing "2ba" entry from an older installer is upgraded in
// place with the current model declaration instead of being skipped; a "2ba"
// provider that does not carry our model is left alone.
func ConfigureOpenclaw(e *Env) {
	stateDir := OpenclawStateDir()
	if !dirExists(stateDir) && !onPath("openclaw") {
		e.warnf("OpenClaw not detected (no %s) — install it, run it once, then re-run this installer", stateDir)
		return
	}
	if e.Model == "" || e.APIKey == "" {
		e.warnf("openclaw: refusing to write with an empty model or API key")
		return
	}
	cfg := openclawConfigFile()

	obj := map[string]any{}
	if exists, ok := loadJSONObject(cfg, &obj); exists && !ok {
		e.warnf("%s is not valid JSON — leaving it untouched (fix or remove it, then re-run)", cfg)
		return
	}
	// OpenClaw stores the provider catalog under models.providers. A missing
	// top-level "models" key (or a "models" that is not an object) means the
	// user's file uses a different shape — leave it untouched.
	models, _ := obj["models"].(map[string]any)
	if models == nil {
		if _, has := obj["models"]; has {
			e.warnf("%s has a models field that is not an object — leaving it untouched", cfg)
			return
		}
		models = map[string]any{}
		obj["models"] = models
	}
	providers, _ := models["providers"].(map[string]any)
	if providers == nil {
		if _, has := models["providers"]; has {
			e.warnf("%s has a models.providers field that is not an object — leaving it untouched", cfg)
			return
		}
		providers = map[string]any{}
		models["providers"] = providers
	}

	if existing, exists := providers["2ba"]; exists {
		// A "2ba" provider without our model id is user-managed; the
		// installer never touches it, even on upgrade runs.
		if !openclawHasModel(map[string]any{"2ba": existing}, e.Model) {
			e.notef("OpenClaw — already configured")
			return
		}
		if !patchOpenclawModel(existing, e.Model) {
			e.notef("OpenClaw — already configured")
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
		e.logf("OpenClaw: model capabilities added to the existing \"2ba\" provider (%s)", cfg)
		return
	}
	if e.DryRun {
		e.logf("would add provider \"2ba\" to %s", cfg)
		return
	}
	// openclaw may be installed but never run; create the state dir (0700,
	// since the config carries a key) without touching an existing one.
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		e.warnf("could not create %s: %v", stateDir, err)
		return
	}
	e.backup(cfg)
	providers["2ba"] = map[string]any{
		"baseUrl":        e.APIBase,
		"apiKey":         e.APIKey,
		"api":            "openai-completions",
		"timeoutSeconds": 300,
		"models":         []any{openclawModel(e.Model)},
	}
	// Set agents.defaults.model.primary to "2ba/<model>" only when no
	// default is configured. OpenClaw accepts either a plain string or an
	// object with primary/fallbacks, so we leave the latter alone.
	openclawSetDefault(obj, e.Model)
	if err := writeIndentedJSON(cfg, obj); err != nil {
		e.warnf("could not write %s: %v", cfg, err)
		return
	}
	e.logf("OpenClaw: provider \"2ba\" added, default model 2ba/%s (%s)", e.Model, cfg)
}

// openclawSetDefault sets agents.defaults.model.primary = "2ba/<model>" only
// when no default is already configured. A pre-existing object form
// (primary/fallbacks) is left alone, as is any non-map shape.
func openclawSetDefault(obj map[string]any, model string) {
	agents, _ := obj["agents"].(map[string]any)
	if agents == nil {
		agents = map[string]any{}
		obj["agents"] = agents
	}
	defaults, _ := agents["defaults"].(map[string]any)
	if defaults == nil {
		defaults = map[string]any{}
		agents["defaults"] = defaults
	}
	if _, present := defaults["model"]; present {
		return
	}
	defaults["model"] = "2ba/" + model
}

// openclawConfigManaged reports whether path holds a "2ba" provider whose
// models include one with id == model — the predicate the uninstall uses to
// decide whether the file is installer-managed. Missing, corrupt, or
// user-managed files report false.
func openclawConfigManaged(path, model string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return false
	}
	models, _ := obj["models"].(map[string]any)
	if models == nil {
		return false
	}
	providers, _ := models["providers"].(map[string]any)
	if providers == nil {
		return false
	}
	return openclawHasModel(providers, model)
}

// removeOpenclawProvider drops the "2ba" provider from models.providers and
// the matching default-model slot, rewriting it. A file that holds no
// matching provider is left untouched (returns removed=false); a corrupt file
// returns an error so the caller can surface a warning. The provider's other
// models, sibling providers, and the installer's models.mode / sibling
// agents.defaults entries all survive the rewrite.
func removeOpenclawProvider(path, model string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return false, err
	}
	models, _ := obj["models"].(map[string]any)
	providers, _ := models["providers"].(map[string]any)
	if _, ok := providers["2ba"]; !ok {
		return false, nil
	}
	delete(providers, "2ba")
	defaultModel := "2ba/" + model
	if agents, _ := obj["agents"].(map[string]any); agents != nil {
		if defaults, _ := agents["defaults"].(map[string]any); defaults != nil {
			if m, ok := defaults["model"].(string); ok && m == defaultModel {
				delete(defaults, "model")
			}
		}
	}
	return true, writeIndentedJSON(path, obj)
}
