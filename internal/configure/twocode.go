package configure

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// 2ba-code (the desktop app) keeps custom LLM providers in an owner-only
// secrets file, not in a plain JSON config:
//
//	<data dir>/agent-home/.config/2ba-code/secrets/custom-model-providers.json
//
// The on-disk shape, validation rules, and 0700-dir/0600-file permissions
// mirror desktop/src/{portable_credential_store,provider_service}.rs in the
// 2ba-agent repo: schemaVersion must be 2, providerId is "custom-<uuidv4>",
// apiFormat is one of anthropic-messages/openai-chat-completions/
// openai-responses, at most 25 providers, and every field is re-validated on
// each read — a malformed file makes the app treat the list as broken, so we
// never rewrite a file we cannot fully parse.

// twocodeSchemaVersion is the only storage version the desktop accepts.
const twocodeSchemaVersion = 2

// twocodeMaxProviders mirrors the desktop's MAXIMUM_PROVIDER_COUNT.
const twocodeMaxProviders = 25

// twocodeDataDir returns the 2ba-code desktop profile directory, mirroring
// the app's own lookup: $TWOBA_DATA_DIR, then the platform application-data
// directory.
func twocodeDataDir() string {
	if v := os.Getenv("TWOBA_DATA_DIR"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home(), "Library", "Application Support", "2ba-code")
	case "windows":
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, "2ba-code")
		}
	}
	return filepath.Join(xdgConfig(), "2ba-code")
}

// twocodeProvidersFile is the custom-provider store the desktop reads.
func twocodeProvidersFile() string {
	return filepath.Join(twocodeDataDir(), "agent-home", ".config", "2ba-code", "secrets", "custom-model-providers.json")
}

// twocodeModel mirrors the desktop's StoredModel (camelCase on disk).
type twocodeModel struct {
	ModelID       string `json:"modelId"`
	ContextWindow int    `json:"contextWindow,omitempty"`
}

// twocodeProvider mirrors the desktop's StoredProvider.
type twocodeProvider struct {
	ProviderID string         `json:"providerId"`
	Label      string         `json:"label"`
	APIFormat  string         `json:"apiFormat"`
	BaseURL    string         `json:"baseURL"`
	APIKey     string         `json:"apiKey"`
	Models     []twocodeModel `json:"models"`
	CreatedAt  int64          `json:"createdAt"`
	UpdatedAt  int64          `json:"updatedAt"`
}

type twocodeRoot struct {
	SchemaVersion int               `json:"schemaVersion"`
	Providers     []twocodeProvider `json:"providers"`
}

// trimAPIBase normalizes an API base for comparisons (trimmed, no trailing
// slash).
func trimAPIBase(base string) string {
	return strings.TrimSuffix(strings.TrimSpace(base), "/")
}

func (r *twocodeRoot) hasBase(base string) bool {
	for _, p := range r.Providers {
		if trimAPIBase(p.BaseURL) == base {
			return true
		}
	}
	return false
}

// newTwocodeProviderID returns "custom-<uuidv4>", the id shape the desktop
// mints and re-validates (36 chars of [0-9a-f-]) on every read.
func newTwocodeProviderID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	const d = "0123456789abcdef"
	hex := func(h []byte) string {
		s := make([]byte, len(h)*2)
		for i, c := range h {
			s[i*2] = d[c>>4]
			s[i*2+1] = d[c&0x0f]
		}
		return string(s)
	}
	return "custom-" + hex(b[0:4]) + "-" + hex(b[4:6]) + "-" + hex(b[6:8]) + "-" + hex(b[8:10]) + "-" + hex(b[10:16]), nil
}

// ConfigureTwocode adds a "2ba" custom provider to the 2ba-code desktop's
// custom-model-providers.json. The desktop keys providers by a random UUID,
// so we match on the API base instead: an existing provider pointing at the
// same base is left untouched, and every other entry in the file is
// preserved verbatim.
func ConfigureTwocode(e *Env) {
	dataDir := twocodeDataDir()
	if !dirExists(dataDir) {
		e.warnf("2ba-code not detected (no %s) — install it, run it once, then re-run this installer", dataDir)
		return
	}
	// The desktop re-validates every field on each read and drops the whole
	// store on failure, so never write values it would reject.
	if len(e.Model) == 0 || len(e.Model) > 160 || len(e.APIKey) == 0 || len(e.APIKey) > 16384 {
		e.warnf("2ba-code: refusing to write a provider with an out-of-range model or API key")
		return
	}
	file := twocodeProvidersFile()
	e.backup(file)

	root := &twocodeRoot{SchemaVersion: twocodeSchemaVersion}
	if fileExists(file) {
		data, err := os.ReadFile(file)
		if err != nil {
			e.warnf("could not read %s: %v — leaving it untouched", file, err)
			return
		}
		var parsed twocodeRoot
		if err := json.Unmarshal(data, &parsed); err != nil {
			e.warnf("%s is not valid JSON — leaving it untouched (fix or remove it, then re-run)", file)
			return
		}
		if parsed.SchemaVersion != twocodeSchemaVersion {
			e.warnf("%s has unsupported schemaVersion %d — leaving it untouched", file, parsed.SchemaVersion)
			return
		}
		root = &parsed
	}

	if root.hasBase(trimAPIBase(e.APIBase)) {
		if e.DryRun {
			e.notef("2ba-code — already configured")
		} else {
			e.notef("2ba-code — already configured")
		}
		return
	}
	if len(root.Providers) >= twocodeMaxProviders {
		e.warnf("%s already holds %d providers (the 2ba-code maximum) — add 2ba in the app instead", file, len(root.Providers))
		return
	}
	if e.DryRun {
		e.logf("would add 2ba provider to %s", file)
		return
	}

	id, err := newTwocodeProviderID()
	if err != nil {
		e.warnf("could not generate a provider id for %s: %v", file, err)
		return
	}
	now := time.Now().UnixMilli()
	root.Providers = append(root.Providers, twocodeProvider{
		ProviderID: id,
		Label:      "2ba",
		APIFormat:  "openai-chat-completions",
		BaseURL:    trimAPIBase(e.APIBase),
		APIKey:     e.APIKey,
		Models:     []twocodeModel{{ModelID: e.Model, ContextWindow: zcodeContextSize}},
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		e.warnf("could not create the 2ba-code secrets directory: %v", err)
		return
	}
	// The desktop keeps its credential tree owner-only; tighten the two
	// private dirs to match what its own prepare() would set.
	_ = os.Chmod(filepath.Join(dataDir, "agent-home", ".config", "2ba-code"), 0o700)
	_ = os.Chmod(filepath.Dir(file), 0o700)
	if err := writeIndentedJSON(file, root); err != nil {
		e.warnf("could not write %s: %v", file, err)
		return
	}
	e.logf("2ba-code: provider \"2ba\" added, model %s (%s)", e.Model, file)
}

// removeTwocodeProvider drops every provider whose base URL matches base —
// the same predicate ConfigureTwocode uses to find the entry. It returns
// removed=true when at least one provider was dropped. A store left with no
// providers is deleted outright; the desktop treats a missing file as an
// empty list.
func removeTwocodeProvider(file, base string) (bool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return false, err
	}
	var root twocodeRoot
	if err := json.Unmarshal(data, &root); err != nil {
		return false, err
	}
	kept := []twocodeProvider{}
	removed := 0
	for _, p := range root.Providers {
		if trimAPIBase(p.BaseURL) == base {
			removed++
		} else {
			kept = append(kept, p)
		}
	}
	if removed == 0 {
		return false, nil
	}
	if len(kept) == 0 {
		_ = os.Remove(file)
		return true, nil
	}
	root.Providers = kept
	if err := writeIndentedJSON(file, root); err != nil {
		return false, err
	}
	return true, nil
}
