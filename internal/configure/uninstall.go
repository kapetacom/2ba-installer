package configure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// jsonEntryExists reports whether an agent JSON config holds a 2ba entry under
// key — the predicate that decides whether the uninstall rewrites (and backs
// up) the file. A missing, corrupt, or non-object catalog reports false.
func jsonEntryExists(path, key string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return false
	}
	if providers, ok := obj["provider"].(map[string]any); ok {
		if _, has := providers[key]; has {
			return true
		}
	}
	// Pi's models.json uses the plural "providers" map.
	if providers, ok := obj["providers"].(map[string]any); ok {
		if _, has := providers[key]; has {
			return true
		}
	}
	if m, ok := obj["model"].(string); ok && strings.HasPrefix(m, key+"/") {
		return true
	}
	if models, ok := obj["models"].(map[string]any); ok {
		if _, has := models[key]; has {
			return true
		}
	}
	return false
}

// removeJSONEntry deletes the 2ba-owned "2ba"/"2BA" provider and model entries
// from an agent JSON config, rewriting it. It returns removed=false without
// touching the file when no entry matches. A corrupt file is left untouched
// and an error returned.
func removeJSONEntry(path, key string) (bool, error) {
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
	removed := false
	if providers, ok := obj["provider"].(map[string]any); ok {
		if _, has := providers[key]; has {
			delete(providers, key)
			removed = true
		}
	}
	// Pi's models.json uses the plural "providers" map.
	if providers, ok := obj["providers"].(map[string]any); ok {
		if _, has := providers[key]; has {
			delete(providers, key)
			removed = true
		}
	}
	if m, ok := obj["model"].(string); ok && strings.HasPrefix(m, key+"/") {
		delete(obj, "model")
		removed = true
	}
	if models, ok := obj["models"].(map[string]any); ok {
		if _, has := models[key]; has {
			delete(models, key)
			removed = true
		}
	}
	if !removed {
		return false, nil
	}
	return true, writeIndentedJSON(path, obj)
}

// Uninstall removes everything the installer manages: the shell rc block, the
// agent JSON entries, the 2ba-code custom-provider entry, the Claude Code
// env block, the Kimi TOML blocks, and the key file. Backups are kept as
// *.bak.2ba next to the modified files.
func Uninstall(e *Env) {
	e.logf("removing 2ba.ai managed configuration…")

	// shell rc
	if rc := firstShellRC(); rc != "" {
		if e.DryRun {
			e.logf("would strip the managed block from %s", rc)
		} else if containsSubstring(rc, BlockBegin) {
			e.backup(rc)
			if ok, _ := stripManagedBlock(rc); ok {
				e.logf("stripped env block from %s", rc)
			} else {
				e.warnf("%s has the managed begin marker but no end marker; leaving it untouched", rc)
			}
		}
	}

	// agent JSON configs (opencode + windsurf + zcode + pi)
	for _, cfg := range []string{
		filepath.Join(xdgConfig(), "opencode", "opencode.json"),
		filepath.Join(home(), ".codeium", "windsurf", "model_config.json"),
		filepath.Join(zcodeHome(), "v2", "config.json"),
		piModelsFile(),
	} {
		if !fileExists(cfg) {
			continue
		}
		key := "2ba"
		if strings.Contains(cfg, "windsurf") {
			key = "2BA"
		}
		// A catalog that holds no 2ba entry is left completely untouched:
		// no rewrite, no backup (same rule as the 2ba-code and Claude paths).
		if !jsonEntryExists(cfg, key) {
			continue
		}
		if e.DryRun {
			e.logf("would remove the 2ba entry from %s", cfg)
			continue
		}
		e.backup(cfg)
		if _, err := removeJSONEntry(cfg, key); err != nil {
			e.warnf("%s is not valid JSON — leaving it untouched", cfg)
		} else {
			e.logf("removed 2ba entry from %s", cfg)
		}
	}

	// 2ba-code custom-provider store (owner-only secrets file). The backup
	// happens only when a matching entry exists, so an unrelated store is
	// left completely untouched.
	if tp := twocodeProvidersFile(); fileExists(tp) {
		base := trimAPIBase(e.APIBase)
		if e.DryRun {
			e.logf("would remove the 2ba provider from %s", tp)
		} else if twocodeStoreHasBase(tp, base) {
			e.backup(tp)
			if removed, err := removeTwocodeProvider(tp, base); err != nil {
				e.warnf("%s is not valid JSON — leaving it untouched", tp)
			} else if removed {
				e.logf("removed 2ba entry from %s", tp)
			}
		}
	}

	// Claude Code settings (env block), same backup-only-when-matching rule.
	if cs := claudeSettingsFile(); fileExists(cs) {
		base := claudeBaseURL(e.APIBase)
		if e.DryRun {
			e.logf("would remove the 2ba configuration from %s", cs)
		} else if claudeConfigManaged(cs, base) {
			e.backup(cs)
			if removed, err := removeClaudeConfig(cs, base); err != nil {
				e.warnf("could not remove the 2ba configuration from %s: %v", cs, err)
			} else if removed {
				e.logf("removed 2ba configuration from %s", cs)
			}
		}
	}

	// Kimi TOML configs (both locations)
	for _, cfg := range []string{
		filepath.Join(home(), ".kimi", "config.toml"),
		filepath.Join(kimiCodeHome(), "config.toml"),
	} {
		if !fileExists(cfg) {
			continue
		}
		if e.DryRun {
			e.logf("would remove the 2ba block from %s (if present)", cfg)
			continue
		}
		if hasExactLine(cfg, BlockBegin) {
			e.backup(cfg)
			if ok, _ := stripManagedBlock(cfg); ok {
				e.logf("removed 2ba block from %s", cfg)
			}
		}
	}

	if e.DryRun {
		e.logf("would delete %s", e.KeyFile)
	} else {
		_ = os.Remove(e.KeyFile)
		e.logf("deleted %s", e.KeyFile)
	}
	e.logf("done. backups kept as *.bak.2ba next to the modified files.")
}
