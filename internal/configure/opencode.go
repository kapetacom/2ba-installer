package configure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"unicode"
)

// pyCapitalize mirrors Python str.capitalize: first rune upper, the rest lower
// (used to build the friendly "Amber (2ba.ai)" model name).
func pyCapitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	for i := 1; i < len(r); i++ {
		r[i] = unicode.ToLower(r[i])
	}
	return string(r)
}

// loadJSONObject reads and parses the JSON object at path into obj. It returns
// false if the file does not exist (obj left as an empty map) or is corrupt
// (obj untouched); in the corrupt case it returns ok=false and the caller
// should leave the file alone.
func loadJSONObject(path string, obj *map[string]any) (exists, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, true // missing → start from an empty object
	}
	if err := json.Unmarshal(data, obj); err != nil {
		return true, false // corrupt → do not overwrite the user's settings
	}
	return true, true
}

// opencodeModel returns the model entry the installer writes for model: a
// friendly name plus the thinking-model declaration. 2ba streams thinking
// as reasoning_content (no middleware mirror, by decision), which is what
// "interleaved" tells OpenCode to read.
func opencodeModel(model string) map[string]any {
	return map[string]any{
		"name":        pyCapitalize(model) + " (2ba.ai)",
		"reasoning":   true,
		"interleaved": "reasoning_content",
	}
}

// patchOpencodeThinking adds the thinking-model fields to the model entry
// of an existing "2ba" provider when they are missing — the upgrade path
// for configs written by an older installer. Existing values are never
// overwritten, and an entry without our model (user-managed) is left
// alone. It reports whether the config changed.
func patchOpencodeThinking(provider any, model string) bool {
	p, ok := provider.(map[string]any)
	if !ok {
		return false
	}
	models, ok := p["models"].(map[string]any)
	if !ok {
		return false
	}
	m, ok := models[model].(map[string]any)
	if !ok {
		return false
	}
	changed := false
	if _, ok := m["reasoning"]; !ok {
		m["reasoning"] = true
		changed = true
	}
	if _, ok := m["interleaved"]; !ok {
		m["interleaved"] = "reasoning_content"
		changed = true
	}
	return changed
}

// ConfigureOpencode adds a "2ba" OpenAI-compatible provider to opencode.json
// and sets it as the default model if none is set. An existing "2ba" entry
// from an older installer is upgraded in place with the thinking-model
// fields instead of being skipped.
func ConfigureOpencode(e *Env) {
	cfg := filepath.Join(xdgConfig(), "opencode", "opencode.json")
	if !dirExists(filepath.Dir(cfg)) {
		return
	}
	obj := map[string]any{}
	if exists, ok := loadJSONObject(cfg, &obj); exists && !ok {
		e.warnf("%s is not valid JSON — leaving it untouched (fix or remove it, then re-run)", cfg)
		return
	}
	providers, _ := obj["provider"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
		obj["provider"] = providers
	}
	if existing, exists := providers["2ba"]; exists {
		if !patchOpencodeThinking(existing, e.Model) {
			e.notef("OpenCode — already configured")
			return
		}
		if e.DryRun {
			e.logf("would add thinking config to the existing \"2ba\" provider in %s", cfg)
			return
		}
		e.backup(cfg)
		if err := writeIndentedJSON(cfg, obj); err != nil {
			e.warnf("could not write %s: %v", cfg, err)
			return
		}
		e.logf("OpenCode: thinking config added to the existing \"2ba\" provider (%s)", cfg)
		return
	}
	if e.DryRun {
		e.logf("would add provider \"2ba\" to %s", cfg)
		return
	}
	e.backup(cfg)
	providers["2ba"] = map[string]any{
		"npm":  "@ai-sdk/openai-compatible",
		"name": "2ba.ai",
		"options": map[string]any{
			"baseURL": e.APIBase,
			"apiKey":  e.APIKey,
		},
		"models": map[string]any{
			e.Model: opencodeModel(e.Model),
		},
	}
	if _, hasModel := obj["model"]; !hasModel {
		obj["model"] = "2ba/" + e.Model
	}
	if err := writeIndentedJSON(cfg, obj); err != nil {
		e.warnf("could not write %s: %v", cfg, err)
		return
	}
	e.logf("OpenCode: provider \"2ba\" added, default model 2ba/%s (%s)", e.Model, cfg)
}
