package configure

import (
	"os"
	"path/filepath"
)

// zcodeContextSize mirrors kimiContextSize: 2ba does not advertise a per-model
// context window, so a conservative value only affects when ZCode compacts.
const zcodeContextSize = 262144

// zcodeHome returns $ZCODE_HOME or ~/.zcode.
func zcodeHome() string {
	if v := os.Getenv("ZCODE_HOME"); v != "" {
		return v
	}
	return filepath.Join(home(), ".zcode")
}

// zcodeReasoning is the reasoning-effort selector surfaced in ZCode's UI.
// The variant strings are sent as reasoning_effort, which the amber
// backend accepts (high aliases to its top tier).
func zcodeReasoning() map[string]any {
	return map[string]any{
		"enabled":        true,
		"variants":       []string{"low", "medium", "high"},
		"defaultVariant": "medium",
	}
}

// zcodeModel returns the model entry the installer writes: context limit,
// modalities, and the reasoning selector.
func zcodeModel() map[string]any {
	return map[string]any{
		"limit":      map[string]any{"context": zcodeContextSize},
		"modalities": map[string]any{"input": []string{"text"}, "output": []string{"text"}},
		"reasoning":  zcodeReasoning(),
	}
}

// patchZcodeReasoning adds the reasoning selector to the model entry of an
// existing "2ba" provider when it is missing — the upgrade path for
// configs written by an older installer. Existing values are never
// overwritten, and an entry without our model (user-managed) is left
// alone. It reports whether the config changed.
func patchZcodeReasoning(provider any, model string) bool {
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
	if _, ok := m["reasoning"]; ok {
		return false
	}
	m["reasoning"] = zcodeReasoning()
	return true
}

// ConfigureZcode adds a "2ba" OpenAI-compatible provider to ZCode's
// v2/config.json. ZCode keys its provider map by provider id (custom ids are
// arbitrary strings; the UI just happens to mint UUIDs), so the stable "2ba"
// key gives us idempotency and a clean uninstall. The entry shape matches what
// ZCode itself writes for custom providers. An existing "2ba" entry from an
// older installer is upgraded in place with the reasoning selector instead of
// being skipped.
func ConfigureZcode(e *Env) {
	zhome := zcodeHome()
	if !dirExists(zhome) {
		if onPath("zcode") {
			e.warnf("ZCode found but no %s yet — run `zcode` once so it creates its config, then re-run this installer", zhome)
		} else {
			e.warnf("ZCode not detected (no %s) — install it, run it once, then re-run this installer", zhome)
		}
		return
	}
	cfg := filepath.Join(zhome, "v2", "config.json")
	if !dirExists(filepath.Dir(cfg)) {
		e.warnf("ZCode found but no v2 config yet (%s) — run ZCode once so it creates its config, then re-run this installer", cfg)
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
		if !patchZcodeReasoning(existing, e.Model) {
			e.notef("ZCode — already configured")
			return
		}
		if e.DryRun {
			e.logf("would add reasoning config to the existing \"2ba\" provider in %s", cfg)
			return
		}
		e.backup(cfg)
		if err := writeIndentedJSON(cfg, obj); err != nil {
			e.warnf("could not write %s: %v", cfg, err)
			return
		}
		e.logf("ZCode: reasoning config added to the existing \"2ba\" provider (%s)", cfg)
		return
	}
	if e.DryRun {
		e.logf("would add 2ba provider to %s", cfg)
		return
	}
	e.backup(cfg)
	providers["2ba"] = map[string]any{
		"name": "2ba",
		"kind": "openai-compatible",
		"options": map[string]any{
			"apiKey":         e.APIKey,
			"baseURL":        e.APIBase,
			"apiKeyRequired": true,
		},
		"enabled": true,
		"source":  "custom",
		"models": map[string]any{
			e.Model: zcodeModel(),
		},
	}
	if err := writeIndentedJSON(cfg, obj); err != nil {
		e.warnf("could not write %s: %v", cfg, err)
		return
	}
	e.logf("ZCode: provider \"2ba\" added, model %s (%s)", e.Model, cfg)
}
