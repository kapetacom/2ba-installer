package configure

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Hermes Agent (NousResearch/hermes-agent) is a Python CLI that reads its
// config from ~/.hermes/config.yaml. The installer edits the YAML in
// place using yaml.Node so user comments and key order survive a re-run,
// and uses a stable top-level marker (`__2ba`) to identify the entries
// it owns (so --reauth rotation and --uninstall without a fresh key
// still work).

// HermesHome returns the Hermes home directory, respecting $HERMES_HOME.
// Single source of truth — the detect package reuses this.
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

// hermesMarkerKey is the top-level marker key that signals installer
// ownership of the file. Its value is a mapping carrying a `version`
// field; presence (any value) means "this file has been touched by the
// 2ba installer". Stable across api_key rotation: --reauth rewrites
// the api_key but the marker persists.
const hermesMarkerKey = "__2ba"

// hermesMarkerVersion is the current marker version. Bumped when the
// installer's on-disk shape changes in a way that invalidates existing
// installer-written entries (none so far).
const hermesMarkerVersion = 1

// hermesOwnedFields lists the keys the installer manages inside
// `providers.2ba`. On a re-run, fields the user has hand-edited (i.e.
// keys not in this list) are left alone; fields in this list are
// updated so a --model or --api-base change takes effect on the next
// run, and a --reauth rotates the api_key.
var hermesOwnedFields = []string{"api", "api_key", "transport", "default_model"}

// ConfigureHermes edits ~/.hermes/config.yaml in place: it adds a 2ba
// provider (or updates installer-owned fields on a re-run), sets the
// default model slot, and stamps a top-level __2ba marker that future
// runs use to recognise installer ownership. yaml.Node round-trips
// comments and key order; we never rewrite a file that holds no
// installer entry.
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

	doc, err := loadHermesYAML(cfg)
	if err != nil {
		e.warnf("%s is not valid YAML — leaving it untouched (fix or remove it, then re-run)", cfg)
		return
	}

	root := hermesRoot(doc)
	owned := hermesFileIsOwned(root)

	// If the file already carries a "providers.2ba" entry but no
	// installer marker, the entry is user-owned — leave it alone. Same
	// rule the JSON-config services follow: "no 2ba entry under our
	// key" means "the user has a value here, do not clobber it".
	if !owned && hermesUserOwnsTwoBAProvider(root) {
		e.notef("Hermes — already configured")
		return
	}

	providerChanged := mergeHermesProvider(root, e)
	defaultChanged := mergeHermesDefault(root, e.Model)

	// Stable marker: stamp it when we wrote or updated an
	// installer-owned entry. On a re-run with a stale field, we want
	// the marker to stay — it identifies the file as ours across
	// rotations. When the file is already owned and complete (no
	// merges changed anything), we treat it as already-configured and
	// skip the write entirely.
	markerChanged := mergeHermesMarker(root)
	stampMarker := providerChanged || defaultChanged || markerChanged || !owned
	if !stampMarker {
		e.notef("Hermes — already configured")
		return
	}

	if e.DryRun {
		e.logf("would add or update provider \"2ba\" in %s", cfg)
		return
	}
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		e.warnf("could not create %s: %v", homeDir, err)
		return
	}
	e.backup(cfg)
	if err := writeHermesYAML(cfg, doc); err != nil {
		e.warnf("could not write %s: %v", cfg, err)
		return
	}
	e.logf("Hermes: provider \"2ba\" updated, default model 2ba/%s (%s)", e.Model, cfg)
}

// loadHermesYAML parses cfg into a yaml.DocumentNode, returning an empty
// document for a missing file and an error for a corrupt one. The
// returned node is the document root that the caller mutates and
// passes to writeHermesYAML.
func loadHermesYAML(cfg string) (*yaml.Node, error) {
	data, err := os.ReadFile(cfg)
	if err != nil {
		if os.IsNotExist(err) {
			// start from an empty document; the writer will add the
			// 2ba provider block and marker.
			return emptyHermesDocument(), nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return emptyHermesDocument(), nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// emptyHermesDocument returns a fresh yaml.DocumentNode whose root is
// an empty MappingNode — the seed for a brand-new config.yaml.
func emptyHermesDocument() *yaml.Node {
	root := &yaml.Node{Kind: yaml.MappingNode}
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
}

// hermesRoot returns the root MappingNode of doc, creating it if the
// file was an empty document. hermesRoot assumes doc is a
// DocumentNode with at least one Content entry (or zero — in which
// case we append a fresh root MappingNode).
func hermesRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind != yaml.DocumentNode {
		// loadHermesYAML always returns a DocumentNode; this is a
		// defensive guard for future refactors.
		root := &yaml.Node{Kind: yaml.MappingNode}
		doc.Kind = yaml.DocumentNode
		doc.Content = append(doc.Content, root)
		return root
	}
	if len(doc.Content) == 0 {
		root := &yaml.Node{Kind: yaml.MappingNode}
		doc.Content = append(doc.Content, root)
		return root
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		root = &yaml.Node{Kind: yaml.MappingNode}
		doc.Content = append(doc.Content, root)
	}
	return root
}

// mapGetOrCreateValue returns the value node associated with key in
// parent (a MappingNode). If the key is absent, a new key/value pair
// is appended (preserving order — new keys go at the end) and the new
// value node (a fresh empty MappingNode) is returned.
func mapGetOrCreateValue(parent *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1]
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"}
	valueNode := &yaml.Node{Kind: yaml.MappingNode}
	parent.Content = append(parent.Content, keyNode, valueNode)
	return valueNode
}

// mapGet looks up key in parent and returns its value node, or nil if
// absent. Unlike mapGetOrCreateValue it does not mutate parent.
func mapGet(parent *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1]
		}
	}
	return nil
}

// mapSetScalar sets the scalar value at key in parent, creating the
// key if absent. Returns true when the document changed.
func mapSetScalar(parent *yaml.Node, key, value string) bool {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			if parent.Content[i+1].Value == value {
				return false
			}
			parent.Content[i+1] = &yaml.Node{
				Kind:  yaml.ScalarNode,
				Value: value,
				Tag:   "!!str",
			}
			return true
		}
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: "!!str"},
	)
	return true
}

// mapDelete removes the key from parent (a MappingNode). Returns true
// when the key was present and removed.
func mapDelete(parent *yaml.Node, key string) bool {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
			return true
		}
	}
	return false
}

// hermesFileIsOwned reports whether root carries the installer's
// stable marker — i.e. this file is one we wrote (or updated), and the
// entries inside are ours.
func hermesFileIsOwned(root *yaml.Node) bool {
	return mapGet(root, hermesMarkerKey) != nil
}

// hermesUserOwnsTwoBAProvider reports whether root has a `providers.2ba`
// entry that is not under the installer's marker — i.e. the user
// owns the 2ba provider entry. We use this to leave the entry alone on
// a fresh install that has no marker yet.
func hermesUserOwnsTwoBAProvider(root *yaml.Node) bool {
	providers := mapGet(root, "providers")
	if providers == nil {
		return false
	}
	return mapGet(providers, "2ba") != nil
}

// mergeHermesMarker ensures the top-level marker is present and
// carries the current version. Returns true when the document changed.
func mergeHermesMarker(root *yaml.Node) bool {
	m := mapGetOrCreateValue(root, hermesMarkerKey)
	if m.Kind != yaml.MappingNode {
		// user wrote a non-object here — overwrite with a fresh object
		// since the marker is our own and reserved.
		m.Kind = yaml.MappingNode
		m.Tag = "!!map"
		m.Content = nil
		return true
	}
	return mapSetScalar(m, "version", "1")
}

// mergeHermesProvider creates or updates the `providers.2ba` entry.
// Returns true when the document changed. On a file the installer
// already owns (carries the marker), the installer-managed keys are
// updated in place even when they differ from the file's existing
// values — this is how --reauth rotates the api_key and --model /
// --api-base changes take effect. The marker itself is what
// establishes ownership; the api_key is just data.
func mergeHermesProvider(root *yaml.Node, e *Env) bool {
	providers := mapGetOrCreateValue(root, "providers")
	if providers.Kind != yaml.MappingNode {
		// user wrote something else at "providers" — leave alone
		return false
	}
	entry := mapGetOrCreateValue(providers, "2ba")
	if entry.Kind != yaml.MappingNode {
		entry.Kind = yaml.MappingNode
		entry.Tag = "!!map"
		entry.Content = nil
	}
	changed := false
	// Always update installer-managed fields so a re-run with new
	// --model / --api-base / --reauth takes effect.
	changed = mapSetScalar(entry, "api", e.APIBase) || changed
	changed = mapSetScalar(entry, "api_key", e.APIKey) || changed
	changed = mapSetScalar(entry, "transport", "chat_completions") || changed
	changed = mapSetScalar(entry, "default_model", e.Model) || changed
	return changed
}

// mergeHermesDefault sets model.default and model.provider. The slots
// are considered installer-managed iff they carry a value the
// installer wrote (`2ba:<anything>` for default, `2ba` for provider)
// — that covers both a fresh install (slots absent → we add them)
// and a re-run with a changed --model (slots match the installer
// pattern → we update them in place). User-set values (anything
// else) are left alone.
func mergeHermesDefault(root *yaml.Node, model string) bool {
	modelBlock := mapGetOrCreateValue(root, "model")
	if modelBlock.Kind != yaml.MappingNode {
		return false
	}
	changed := false
	if d := mapGet(modelBlock, "default"); d == nil || isInstallerTwoBADefault(d.Value) {
		changed = mapSetScalar(modelBlock, "default", "2ba:"+model) || changed
	}
	if p := mapGet(modelBlock, "provider"); p == nil || p.Value == "2ba" {
		changed = mapSetScalar(modelBlock, "provider", "2ba") || changed
	}
	return changed
}

// isInstallerTwoBADefault reports whether model.default looks like a
// value the installer wrote, i.e. matches the `2ba:<model>` pattern.
// Used to distinguish installer-managed slots from user-set ones in
// the `model:` block.
func isInstallerTwoBADefault(v string) bool {
	return strings.HasPrefix(v, "2ba:")
}

// writeHermesYAML serializes doc and writes it 0600. yaml.Marshal on a
// DocumentNode preserves comments and key order from the input.
func writeHermesYAML(cfg string, doc *yaml.Node) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfg, out, 0o600); err != nil {
		return err
	}
	return os.Chmod(cfg, 0o600)
}

// uninstallHermesConfig reports whether path is a Hermes config the
// installer owns. The marker is the source of truth — we don't need
// (or want) the current api_key to identify the entry, because the
// CLI uninstall path constructs Env with an empty APIKey.
func uninstallHermesConfig(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false
	}
	return hermesFileIsOwned(hermesRoot(&doc))
}

// removeHermesProvider strips the installer's `providers.2ba` entry,
// the matching model.default / model.provider slots, and the
// __2ba marker. A file without the marker is left untouched (returns
// removed=false). A corrupt file returns an error.
func removeHermesProvider(path string, model string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if len(data) == 0 {
		return false, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, err
	}
	root := hermesRoot(&doc)
	if !hermesFileIsOwned(root) {
		return false, nil
	}
	removed := false
	if providers := mapGet(root, "providers"); providers != nil {
		if entry := mapGet(providers, "2ba"); entry != nil {
			// replace the 2ba entry with an empty mapping of the same
			// shape — actually, since we may have other content in
			// the file, just delete the key.
			if mapDelete(providers, "2ba") {
				removed = true
			}
		}
	}
	if modelBlock := mapGet(root, "model"); modelBlock != nil {
		// Only clear slots the installer wrote. A user's own default
		// (`openrouter:llama3`) survives a 2ba uninstall.
		if d := mapGet(modelBlock, "default"); d != nil && d.Value == "2ba:"+model {
			if mapDelete(modelBlock, "default") {
				removed = true
			}
		}
		if p := mapGet(modelBlock, "provider"); p != nil && p.Value == "2ba" {
			if mapDelete(modelBlock, "provider") {
				removed = true
			}
		}
	}
	if mapDelete(root, hermesMarkerKey) {
		removed = true
	}
	if !removed {
		return false, nil
	}
	return true, writeHermesYAML(path, &doc)
}
