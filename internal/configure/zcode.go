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

// ConfigureZcode adds a "2ba" OpenAI-compatible provider to ZCode's
// v2/config.json. ZCode keys its provider map by provider id (custom ids are
// arbitrary strings; the UI just happens to mint UUIDs), so the stable "2ba"
// key gives us idempotency and a clean uninstall. The entry shape matches what
// ZCode itself writes for custom providers.
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
	if e.DryRun {
		if fileExists(cfg) && hasProvider2ba(cfg) {
			e.notef("ZCode — already configured")
		} else {
			e.logf("would add 2ba provider to %s", cfg)
		}
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
		e.notef("ZCode — already configured")
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
			e.Model: map[string]any{
				"limit":      map[string]any{"context": zcodeContextSize},
				"modalities": map[string]any{"input": []string{"text"}, "output": []string{"text"}},
			},
		},
	}
	if err := writeIndentedJSON(cfg, obj); err != nil {
		e.warnf("could not write %s: %v", cfg, err)
		return
	}
	e.logf("ZCode: provider \"2ba\" added, model %s (%s)", e.Model, cfg)
}

// hasProvider2ba reports whether the ZCode config already carries a "2ba"
// provider entry (used for the dry-run wording).
func hasProvider2ba(cfg string) bool {
	obj := map[string]any{}
	if _, ok := loadJSONObject(cfg, &obj); !ok {
		return false
	}
	providers, _ := obj["provider"].(map[string]any)
	_, ok := providers["2ba"]
	return ok
}
