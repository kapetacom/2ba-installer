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

// TwocodeDataDir returns the 2ba-code desktop profile directory, mirroring
// the app's own lookup: $TWOBA_DATA_DIR, then the platform application-data
// directory. It is the single source of truth for this path; the detect
// package reuses it instead of keeping a second copy.
func TwocodeDataDir() string {
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
	return filepath.Join(TwocodeDataDir(), "agent-home", ".config", "2ba-code", "secrets", "custom-model-providers.json")
}

// trimAPIBase normalizes an API base for comparisons (trimmed, no trailing
// slash).
func trimAPIBase(base string) string {
	return strings.TrimSuffix(strings.TrimSpace(base), "/")
}

// twocodeProviderBase returns the trimmed base URL of one provider entry of
// the store, or "" when the entry is not an object with a usable baseURL.
func twocodeProviderBase(p any) string {
	obj, _ := p.(map[string]any)
	if obj == nil {
		return ""
	}
	url, _ := obj["baseURL"].(string)
	return trimAPIBase(url)
}

// twocodeProvidersHaveBase reports whether any provider in providers points
// at base. The desktop keys providers by a random UUID, so the base URL is
// the only stable handle for the 2ba entry; ConfigureTwocode and
// removeTwocodeProvider share this predicate.
func twocodeProvidersHaveBase(providers []any, base string) bool {
	for _, p := range providers {
		if twocodeProviderBase(p) == base {
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
// custom-model-providers.json. An existing provider pointing at the same
// base is left untouched, and the store is handled as generic JSON so every
// field of the other entries — including ones this installer does not know
// about — survives the rewrite.
func ConfigureTwocode(e *Env) {
	dataDir := TwocodeDataDir()
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

	root := map[string]any{"schemaVersion": twocodeSchemaVersion}
	providers := []any{}
	if fileExists(file) {
		data, err := os.ReadFile(file)
		if err != nil {
			e.warnf("could not read %s: %v — leaving it untouched", file, err)
			return
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			e.warnf("%s is not valid JSON — leaving it untouched (fix or remove it, then re-run)", file)
			return
		}
		if ver, _ := parsed["schemaVersion"].(float64); int(ver) != twocodeSchemaVersion {
			e.warnf("%s has unsupported schemaVersion %v — leaving it untouched", file, parsed["schemaVersion"])
			return
		}
		switch p := parsed["providers"].(type) {
		case []any:
			providers = p
		case nil:
		default:
			e.warnf("%s has a providers field that is not a list — leaving it untouched", file)
			return
		}
		root = parsed
	}

	if twocodeProvidersHaveBase(providers, trimAPIBase(e.APIBase)) {
		e.notef("2ba-code — already configured")
		return
	}
	if len(providers) >= twocodeMaxProviders {
		e.warnf("%s already holds %d providers (the 2ba-code maximum) — add 2ba in the app instead", file, len(providers))
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
	root["providers"] = append(providers, map[string]any{
		"providerId": id,
		"label":      "2ba",
		"apiFormat":  "openai-chat-completions",
		"baseURL":    trimAPIBase(e.APIBase),
		"apiKey":     e.APIKey,
		"models":     []any{map[string]any{"modelId": e.Model, "contextWindow": zcodeContextSize}},
		"createdAt":  now,
		"updatedAt":  now,
	})
	e.backup(file)
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

// twocodeStoreHasBase reports whether file holds at least one provider whose
// base URL matches base — the predicate removeTwocodeProvider removes on.
// Used to back the file up only when the uninstall actually removes an entry.
func twocodeStoreHasBase(file, base string) bool {
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return false
	}
	providers, _ := root["providers"].([]any)
	return twocodeProvidersHaveBase(providers, base)
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
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return false, err
	}
	providers, _ := root["providers"].([]any)
	kept := []any{}
	removed := 0
	for _, p := range providers {
		if twocodeProviderBase(p) == base {
			removed++
			continue
		}
		kept = append(kept, p)
	}
	if removed == 0 {
		return false, nil
	}
	if len(kept) == 0 {
		_ = os.Remove(file)
		return true, nil
	}
	root["providers"] = kept
	if err := writeIndentedJSON(file, root); err != nil {
		return false, err
	}
	return true, nil
}
