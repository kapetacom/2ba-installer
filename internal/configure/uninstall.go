package configure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// removeJSONEntry deletes the 2ba-owned "2ba"/"2BA" provider and model entries
// from an agent JSON config, rewriting it. A corrupt file is left untouched and
// an error returned. It returns nil when the file is missing.
func removeJSONEntry(path, key string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	if providers, ok := obj["provider"].(map[string]any); ok {
		delete(providers, key)
	}
	if m, ok := obj["model"].(string); ok && strings.HasPrefix(m, key+"/") {
		delete(obj, "model")
	}
	if models, ok := obj["models"].(map[string]any); ok {
		delete(models, key)
	}
	return writeIndentedJSON(path, obj)
}

// Uninstall removes everything the installer manages: the shell rc block, the
// agent JSON entries, the Kimi TOML blocks, and the key file. Backups are kept
// as *.bak.2ba next to the modified files.
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

	// agent JSON configs (opencode + windsurf + zcode)
	for _, cfg := range []string{
		filepath.Join(xdgConfig(), "opencode", "opencode.json"),
		filepath.Join(home(), ".codeium", "windsurf", "model_config.json"),
		filepath.Join(zcodeHome(), "v2", "config.json"),
	} {
		if !fileExists(cfg) {
			continue
		}
		if e.DryRun {
			e.logf("would remove the 2ba entry from %s", cfg)
			continue
		}
		e.backup(cfg)
		key := "2ba"
		if strings.Contains(cfg, "windsurf") {
			key = "2BA"
		}
		if err := removeJSONEntry(cfg, key); err != nil {
			e.warnf("%s is not valid JSON — leaving it untouched", cfg)
		} else {
			e.logf("removed 2ba entry from %s", cfg)
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
