package configure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Goose (block/goose) is Block's agent harness. A custom provider is a JSON
// file in <config>/custom_providers/<id>.json shaped like goose's bundled
// declarative definitions (crates/goose-providers/src/declarative), and the
// default provider/model live as YAML keys in <config>/config.yaml:
// active_provider plus a providers.<id> entry — exactly what goose's own
// set_active_provider writes. The config.yaml edit uses yaml.Node so user
// comments and key order survive a re-run.
//
// The key is not embedded: goose resolves api_key_env through get_secret,
// which checks the (uppercased) environment variable first — the shell
// service already exports OPENAI_API_KEY from the 0600 key file, so the
// provider only has to name it.

// gooseProviderID is both the "name" field and the JSON filename; goose
// accepts a leading digit (validate_provider_id allows ascii digits).
const gooseProviderID = "2ba"

// gooseContextSize mirrors the other declarative targets: 2ba does not
// advertise a per-model context window, so a conservative value only affects
// when Goose compacts.
const gooseContextSize = 262144

// GooseConfigDir returns goose's config directory, mirroring goose's own
// paths.rs (etcetera): Apple strategy on macOS, XDG elsewhere. Verified
// against goose 1.55.0 on Linux. (goose's GOOSE_PATH_ROOT debugging override
// is deliberately not honoured.) Single source of truth — detect reuses this.
func GooseConfigDir() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home(), "Library", "Application Support", "Block", "goose")
	}
	return filepath.Join(xdgConfig(), "goose")
}

func gooseProviderFile() string {
	return filepath.Join(GooseConfigDir(), "custom_providers", gooseProviderID+".json")
}

func gooseConfigFile() string {
	return filepath.Join(GooseConfigDir(), "config.yaml")
}

// gooseProviderObject is the declaration the installer writes for the 2ba
// provider. Fields follow goose's bundled OpenAI-compatible definitions
// (e.g. zai_coding_plan.json): context_limit is per model, reasoning marks a
// thinking model, preserves_thinking keeps reasoning_content out of the
// visible reply.
func gooseProviderObject(e *Env) map[string]any {
	return map[string]any{
		"name":         gooseProviderID,
		"engine":       "openai",
		"display_name": "2ba.ai",
		"description":  "2ba.ai models (EU-hosted, OpenAI-compatible)",
		"api_key_env":  "OPENAI_API_KEY",
		"base_url":     e.APIBase,
		"models": []any{map[string]any{
			"name":          e.Model,
			"context_limit": gooseContextSize,
			"reasoning":     true,
		}},
		"supports_streaming": true,
		"preserves_thinking": true,
	}
}

// gooseProviderIsOurs reports whether an existing provider file was written
// by the installer: it must carry our provider id and our api_key_env. Both
// are stable across --model and --api-base changes, so a rerun with a
// different model still recognizes (and upgrades) our own file. Goose's own
// UI flow never produces this combination — it names custom providers
// custom_<slug> and generates its own key names — so anything else is
// user-owned and left alone.
func gooseProviderIsOurs(obj map[string]any) bool {
	if name, _ := obj["name"].(string); name != gooseProviderID {
		return false
	}
	key, _ := obj["api_key_env"].(string)
	return key == "OPENAI_API_KEY"
}

// patchGooseProvider brings an installer-written provider file up to the
// current declaration: installer-owned fields are updated in place (so
// --api-base and --model changes take effect), unknown top-level keys the
// user added are preserved, nothing is removed. The models array is merged
// per entry (mergeGooseModels) so user-added models and per-model keys
// survive too. It reports whether the file changed. Fields are compared in
// their marshaled form: json.Unmarshal yields float64 for numbers, and a
// reloaded 262144 must equal a fresh int one.
func patchGooseProvider(obj map[string]any, e *Env) bool {
	changed := false
	for k, v := range gooseProviderObject(e) {
		if k == "models" {
			if merged, modelsChanged := mergeGooseModels(obj[k], v); modelsChanged {
				obj[k] = merged
				changed = true
			}
			continue
		}
		got, _ := json.Marshal(obj[k])
		want, _ := json.Marshal(v)
		if string(got) != string(want) {
			obj[k] = v
			changed = true
		}
	}
	return changed
}

// mergeGooseModels merges the installer's single-model declaration into an
// existing models array without clobbering user data: our model entry is
// created or its installer-owned subfields (context_limit, reasoning) are
// refreshed in place, while other model entries the user added, extra
// per-model keys, and entries left over from earlier --model values are
// kept.
func mergeGooseModels(existing, want any) (any, bool) {
	wantEntries, _ := want.([]any)
	if len(wantEntries) == 0 {
		return existing, false
	}
	ours, _ := wantEntries[0].(map[string]any)
	entries, ok := existing.([]any)
	if !ok {
		return want, true // absent or unexpected shape → our declaration
	}
	out := make([]any, 0, len(entries)+1)
	changed := false
	found := false
	for _, ent := range entries {
		em, ok := ent.(map[string]any)
		if !ok {
			out = append(out, ent) // opaque entry — keep verbatim
			continue
		}
		if n, _ := em["name"].(string); n == ours["name"] {
			found = true
			for k, v := range ours {
				got, _ := json.Marshal(em[k])
				w, _ := json.Marshal(v)
				if string(got) != string(w) {
					em[k] = v
					changed = true
				}
			}
			out = append(out, em)
			continue
		}
		out = append(out, ent)
	}
	if !found {
		out = append(out, ours)
		changed = true
	}
	if !changed {
		return existing, false
	}
	return out, true
}

// mergeGooseProviderEntry adds the providers.2ba entry goose uses to resolve
// the active provider's model. An existing entry (ours from an older run, or
// the user's own) is never overwritten.
func mergeGooseProviderEntry(root *yaml.Node, model string) bool {
	providers := mapGet(root, "providers")
	if providers == nil {
		providers = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "providers", Tag: "!!str"},
			providers)
	}
	if mapGet(providers, gooseProviderID) != nil {
		return false
	}
	// enabled/configured must be real YAML booleans, not quoted strings —
	// goose deserializes ProviderEntry with serde and rejects "true" as bool.
	providers.Content = append(providers.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: gooseProviderID, Tag: "!!str"},
		&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "enabled", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "true", Tag: "!!bool"},
			{Kind: yaml.ScalarNode, Value: "model", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: model, Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "configured", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "true", Tag: "!!bool"},
		}})
	return true
}

// mergeGooseDefault sets active_provider to 2ba only when no default is
// expressed — neither the modern active_provider key nor the legacy
// GOOSE_PROVIDER. An existing default is never stolen.
func mergeGooseDefault(root *yaml.Node) bool {
	if mapGet(root, "active_provider") != nil || mapGet(root, "GOOSE_PROVIDER") != nil {
		return false
	}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "active_provider", Tag: "!!str"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: gooseProviderID, Tag: "!!str"})
	return true
}

// writeGooseYAML serializes doc and writes it, preserving an existing file's
// permissions (0600 for a file we create — no secret inside, but goose's own
// config writes are private too).
func writeGooseYAML(path string, doc *yaml.Node) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	perm := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		perm = st.Mode().Perm()
	}
	if err := os.WriteFile(path, out, perm); err != nil {
		return err
	}
	return os.Chmod(path, perm)
}

// ConfigureGoose registers 2ba as a custom OpenAI-compatible provider and,
// when no default is set, makes it the active provider. Existing user values
// (another active provider, a hand-edited providers.2ba, a user-authored
// 2ba.json) are left untouched.
func ConfigureGoose(e *Env) {
	dir := GooseConfigDir()
	if !dirExists(dir) && !onPath("goose") {
		e.warnf("Goose not detected (no %s) — install it, run it once, then re-run this installer", dir)
		return
	}
	if e.Model == "" || e.APIKey == "" {
		e.warnf("goose: refusing to write with an empty model or API key")
		return
	}
	provPath, cfgPath := gooseProviderFile(), gooseConfigFile()

	prov := map[string]any{}
	if exists, ok := loadJSONObject(provPath, &prov); exists && !ok {
		e.warnf("%s is not valid JSON — leaving it untouched (fix or remove it, then re-run)", provPath)
		return
	}
	providerIsNew := !fileExists(provPath)
	if !providerIsNew && !gooseProviderIsOurs(prov) {
		e.notef("Goose — already configured")
		return
	}

	doc, err := loadHermesYAML(cfgPath)
	if err != nil {
		e.warnf("%s is not valid YAML — leaving it untouched (fix or remove it, then re-run)", cfgPath)
		return
	}
	root := hermesRoot(doc)
	if v := mapGet(root, "providers"); v != nil && v.Kind != yaml.MappingNode {
		e.warnf("%s has a top-level `providers` that is not an object — leaving it untouched", cfgPath)
		return
	}
	for _, key := range []string{"active_provider", "GOOSE_PROVIDER"} {
		if v := mapGet(root, key); v != nil && v.Kind != yaml.ScalarNode {
			e.warnf("%s has a top-level `%s` that is not a plain value — leaving it untouched", cfgPath, key)
			return
		}
	}

	providerChanged := patchGooseProvider(prov, e) || providerIsNew
	entryChanged := mergeGooseProviderEntry(root, e.Model)
	defaultChanged := mergeGooseDefault(root)
	if !providerChanged && !entryChanged && !defaultChanged {
		e.notef("Goose — already configured")
		return
	}
	if e.DryRun {
		e.logf("would add or update the \"2ba\" provider in %s and %s", provPath, cfgPath)
		return
	}
	if err := os.MkdirAll(filepath.Dir(provPath), 0o700); err != nil {
		e.warnf("could not create %s: %v", filepath.Dir(provPath), err)
		return
	}
	e.backup(provPath)
	if err := writeIndentedJSON(provPath, prov); err != nil {
		e.warnf("could not write %s: %v", provPath, err)
		return
	}
	e.backup(cfgPath)
	if err := writeGooseYAML(cfgPath, doc); err != nil {
		e.warnf("could not write %s: %v", cfgPath, err)
		return
	}
	e.logf("Goose: provider \"2ba\" added, default model %s (%s)", e.Model, provPath)
}

// gooseConfigManaged reports whether config.yaml holds installer-written
// entries: a providers.2ba entry, or a default pointing at 2ba.
func gooseConfigManaged(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false
	}
	root := hermesRoot(&doc)
	if providers := mapGet(root, "providers"); providers != nil {
		if mapGet(providers, gooseProviderID) != nil {
			return true
		}
	}
	for _, key := range []string{"active_provider", "GOOSE_PROVIDER"} {
		if v := mapGet(root, key); v != nil && v.Value == gooseProviderID {
			return true
		}
	}
	return false
}

// gooseProviderFileIsOurs reports whether the provider file at path is
// installer-owned (same predicate as the configure path). A corrupt file
// returns an error so the caller can warn and leave it untouched — the
// uninstall never deletes a file it cannot classify.
func gooseProviderFileIsOurs(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return false, err
	}
	if obj == nil { // a bare "null" parses cleanly but is no provider config
		return false, os.ErrInvalid
	}
	return gooseProviderIsOurs(obj), nil
}

// deleteKeyPreservingHeadComment removes key from parent like mapDelete,
// but when the deleted key was the first entry and carried a head comment
// (a file-leading `# comment` parses onto the first key), the comment is
// transferred to the new first entry instead of vanishing with its key.
func deleteKeyPreservingHeadComment(parent *yaml.Node, key string) bool {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value != key {
			continue
		}
		orphan := ""
		if i == 0 {
			orphan = parent.Content[i].HeadComment
		}
		parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
		if orphan != "" && len(parent.Content) > 0 && parent.Content[0].HeadComment == "" {
			parent.Content[0].HeadComment = orphan
		}
		return true
	}
	return false
}

// removeGooseConfig strips the installer's entries from config.yaml: the
// providers.2ba entry and any default (active_provider or legacy
// GOOSE_PROVIDER) that points at 2ba. User-set defaults for other providers
// survive. It reports whether the file changed.
func removeGooseConfig(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, err
	}
	root := hermesRoot(&doc)
	removed := false
	if providers := mapGet(root, "providers"); providers != nil {
		if mapDelete(providers, gooseProviderID) {
			removed = true
		}
	}
	for _, key := range []string{"active_provider", "GOOSE_PROVIDER"} {
		if v := mapGet(root, key); v != nil && v.Value == gooseProviderID {
			if deleteKeyPreservingHeadComment(root, key) {
				removed = true
			}
		}
	}
	if !removed {
		return false, nil
	}
	return true, writeGooseYAML(path, &doc)
}
