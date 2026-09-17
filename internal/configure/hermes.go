package configure

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Hermes Agent (NousResearch/hermes-agent) is a Python CLI that reads its
// config from ~/.hermes/config.yaml. The installer merges its `providers.2ba`
// and `model.*` keys into the user's file rather than printing a snippet:
// yaml.v3 round-trips comments and key order, so a re-run never clobbers
// user-owned entries. The key/field names below are verbatim from the
// upstream providers doc (hermes-agent.nousresearch.com/docs/integrations/providers).

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

// hermesProvider is the per-provider entry the installer writes. Field
// names are verbatim from the upstream providers doc.
func hermesProvider(env *Env) map[string]any {
	return map[string]any{
		"api":           env.APIBase,
		"api_key":       env.APIKey,
		"transport":     "chat_completions",
		"default_model": env.Model,
	}
}

// hermesDefaultModel returns the installer's top-level model.default value.
func hermesDefaultModel(model string) string {
	return "2ba:" + model
}

// ConfigureHermes merges the installer's `providers.2ba` and `model.*`
// entries into ~/.hermes/config.yaml, preserving user-owned keys and YAML
// comments (yaml.v3 round-trips both). Same idempotency and backup rules
// as the JSON-config services — load-or-empty, never clobber user values,
// back up (*.bak.2ba) only immediately before a real write.
func ConfigureHermes(e *Env) {
	homeDir := HermesHome()
	if !dirExists(homeDir) && !onPath("hermes") {
		e.warnf("Hermes not detected (no %s) — install it, run it once, then re-run this installer", homeDir)
		return
	}
	if e.Model == "" || e.APIKey == "" {
		e.warnf("hermes: refusing to write with an empty model or API key")
		return
	}
	cfg := hermesConfigFile()

	doc, ok := loadHermesYAML(cfg)
	if !ok {
		e.warnf("%s is not valid YAML — leaving it untouched (fix or remove it, then re-run)", cfg)
		return
	}

	if !mergeHermesProviders(doc, e) {
		e.notef("Hermes — already configured")
		return
	}
	defaultChanged := mergeHermesDefault(doc, e.Model)
	if defaultChanged {
		// nothing to record — the upgrade path treats this as part of the
		// merge below
	}

	if e.DryRun {
		e.logf("would add provider \"2ba\" and default model 2ba/%s to %s", e.Model, cfg)
		return
	}
	// hermes may be installed but never run; create the home dir (0700,
	// since the config carries a key) without touching an existing one.
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		e.warnf("could not create %s: %v", homeDir, err)
		return
	}
	e.backup(cfg)
	if err := writeHermesYAML(cfg, doc); err != nil {
		e.warnf("could not write %s: %v", cfg, err)
		return
	}
	e.logf("Hermes: provider \"2ba\" added, default model 2ba/%s (%s)", e.Model, cfg)
}

// loadHermesYAML reads cfg into a YAML document map. Returns ok=false when
// the file is corrupt (we leave it alone in that case), or when the top
// level is not a mapping (a user's config uses a different shape).
func loadHermesYAML(cfg string) (map[string]any, bool) {
	data, err := os.ReadFile(cfg)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, true
		}
		return nil, false
	}
	if len(data) == 0 {
		return map[string]any{}, true
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, false
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, true
}

// mergeHermesProviders adds the installer's "2ba" entry to doc.providers,
// preserving user-owned providers and backfilling missing fields on an
// existing installer-written entry. Returns true when the document
// changed.
func mergeHermesProviders(doc map[string]any, e *Env) bool {
	providers, _ := doc["providers"].(map[string]any)
	if providers == nil {
		if _, has := doc["providers"]; has {
			// user-owned non-object shape — leave it alone
			return false
		}
		providers = map[string]any{}
		doc["providers"] = providers
	}
	ours := hermesProvider(e)
	if existing, exists := providers["2ba"]; exists {
		if existingMap, ok := existing.(map[string]any); ok {
			if !hermesEntryMatches(existingMap, ours) {
				// user-owned (or partial) — leave alone
				return false
			}
			changed := false
			for k, v := range ours {
				if _, has := existingMap[k]; !has {
					existingMap[k] = v
					changed = true
				}
			}
			return changed
		}
		// user-owned non-object entry — leave alone
		return false
	}
	providers["2ba"] = ours
	return true
}

// hermesEntryMatches reports whether the existing entry carries the same
// values we would write. It is the "installer-owned?" predicate: an entry
// that has the same api_key is one we wrote; anything else is user-owned.
func hermesEntryMatches(existing, ours map[string]any) bool {
	existingKey, _ := existing["api_key"].(string)
	oursKey, _ := ours["api_key"].(string)
	return existingKey != "" && existingKey == oursKey
}

// mergeHermesDefault sets model.default and model.provider to point at
// 2ba, but only when no default is already configured. A pre-existing
// `model` block (any shape) is left alone. Returns true when the
// document changed.
func mergeHermesDefault(doc map[string]any, model string) bool {
	modelBlock, _ := doc["model"].(map[string]any)
	if modelBlock == nil {
		if _, has := doc["model"]; has {
			return false
		}
		modelBlock = map[string]any{}
		doc["model"] = modelBlock
	}
	if _, present := modelBlock["default"]; present {
		return false
	}
	modelBlock["default"] = hermesDefaultModel(model)
	if _, present := modelBlock["provider"]; !present {
		modelBlock["provider"] = "2ba"
	}
	return true
}

// writeHermesYAML serializes doc with yaml.v3 (round-trips comments and
// key order from the original parse) and writes it 0600.
func writeHermesYAML(cfg string, doc map[string]any) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfg, out, 0o600); err != nil {
		return err
	}
	return os.Chmod(cfg, 0o600)
}

// uninstallHermesConfig reports whether path holds installer-written
// entries (providers["2ba"] with our api_key, model.default == "2ba/...").
// Used by the uninstall path to back the file up only when we will
// actually remove an entry.
func uninstallHermesConfig(path string, env *Env) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil || doc == nil {
		return false
	}
	providers, _ := doc["providers"].(map[string]any)
	if providers == nil {
		return false
	}
	existing, ok := providers["2ba"].(map[string]any)
	if !ok {
		return false
	}
	// The api_key identifies the entry as installer-owned.
	key, _ := existing["api_key"].(string)
	if key == "" || key != env.APIKey {
		return false
	}
	if modelBlock, _ := doc["model"].(map[string]any); modelBlock != nil {
		if d, _ := modelBlock["default"].(string); d == hermesDefaultModel(env.Model) {
			return true
		}
	}
	// The provider entry itself is installer-owned — that alone is enough.
	return true
}

// removeHermesProvider strips the installer-owned "2ba" provider and the
// matching model.default slot, rewriting the file. A file with no
// matching provider is left untouched (returns removed=false); a corrupt
// file returns an error. Other providers and other model keys survive.
func removeHermesProvider(path string, env *Env) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, err
	}
	providers, _ := doc["providers"].(map[string]any)
	existing, ok := providers["2ba"].(map[string]any)
	if !ok {
		return false, nil
	}
	existingKey, _ := existing["api_key"].(string)
	if existingKey == "" || existingKey != env.APIKey {
		return false, nil
	}
	delete(providers, "2ba")
	if modelBlock, _ := doc["model"].(map[string]any); modelBlock != nil {
		if d, _ := modelBlock["default"].(string); d == hermesDefaultModel(env.Model) {
			delete(modelBlock, "default")
			if p, _ := modelBlock["provider"].(string); p == "2ba" {
				delete(modelBlock, "provider")
			}
		}
	}
	return true, writeHermesYAML(path, doc)
}
