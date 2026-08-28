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

// ConfigureOpencode adds a "2ba" OpenAI-compatible provider to opencode.json
// and sets it as the default model if none is set.
func ConfigureOpencode(e *Env) {
	cfg := filepath.Join(xdgConfig(), "opencode", "opencode.json")
	if !dirExists(filepath.Dir(cfg)) {
		return
	}
	e.backup(cfg)
	if e.DryRun {
		e.logf("would add provider \"2ba\" to %s", cfg)
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
	if _, exists := providers["2ba"]; exists {
		e.notef("OpenCode — already configured")
		return
	}
	providers["2ba"] = map[string]any{
		"npm":  "@ai-sdk/openai-compatible",
		"name": "2ba.ai",
		"options": map[string]any{
			"baseURL": e.APIBase,
			"apiKey":  e.APIKey,
		},
		"models": map[string]any{
			e.Model: map[string]any{
				"name": pyCapitalize(e.Model) + " (2ba.ai)",
				// Declare amber as a thinking model so OpenCode renders the
				// reasoning stream; 2ba emits thinking as reasoning_content
				// (no middleware mirror, by decision).
				"reasoning":   true,
				"interleaved": "reasoning_content",
			},
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
