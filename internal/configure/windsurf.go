package configure

import (
	"path/filepath"
)

// ConfigureWindsurf adds a "2BA" model to Windsurf's model_config.json, marking
// it the default only when no other model already is.
func ConfigureWindsurf(e *Env) {
	cfg := filepath.Join(home(), ".codeium", "windsurf", "model_config.json")
	if !dirExists(filepath.Dir(cfg)) {
		return
	}
	e.backup(cfg)
	if e.DryRun {
		e.logf("would add model \"2BA\" to %s", cfg)
		return
	}
	obj := map[string]any{}
	if exists, ok := loadJSONObject(cfg, &obj); exists && !ok {
		e.warnf("%s is not valid JSON — leaving it untouched (fix or remove it, then re-run)", cfg)
		return
	}
	models, _ := obj["models"].(map[string]any)
	if models == nil {
		models = map[string]any{}
		obj["models"] = models
	}
	if _, exists := models["2BA"]; exists {
		e.logf("existing \"2BA\" model found in %s — leaving it as-is", cfg)
		return
	}
	anyDefault := false
	for _, m := range models {
		if am, ok := m.(map[string]any); ok {
			if d, ok := am["default"].(bool); ok && d {
				anyDefault = true
				break
			}
		}
	}
	models["2BA"] = map[string]any{
		"name":    "2ba.ai",
		"default": !anyDefault,
		"apiKey":  e.APIKey,
		"apiBase": e.APIBase,
		"model":   e.Model,
	}
	if err := writeIndentedJSON(cfg, obj); err != nil {
		e.warnf("could not write %s: %v", cfg, err)
		return
	}
	e.logf("Windsurf: model \"2BA\" added (%s)", cfg)
}
