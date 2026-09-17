package configure

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	testBase   = "https://api.2ba.ai/v1"
	testOrigin = "https://2ba.ai"
)

// mustWrite/mustMkdir are small seeding helpers.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// newEnv pins HOME/XDG_CONFIG_HOME/KIMI_CODE_HOME inside the fake home and
// returns an Env that captures its own log output.
func newEnv(t *testing.T, home, model, key string, dryRun bool) (*Env, *bytes.Buffer) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("KIMI_CODE_HOME", filepath.Join(home, ".kimi-code"))
	t.Setenv("ZCODE_HOME", filepath.Join(home, ".zcode"))
	t.Setenv("TWOBA_DATA_DIR", filepath.Join(home, ".config", "2ba-code"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent"))
	t.Setenv("OPENCLAW_STATE_DIR", filepath.Join(home, ".openclaw"))
	t.Setenv("HERMES_HOME", filepath.Join(home, ".hermes"))
	var buf bytes.Buffer
	env := NewEnv(model, testBase, testOrigin, key, filepath.Join(home, ".config", "2ba", "2BA_API_KEY"), dryRun)
	env.Out = &buf
	return env, &buf
}

// -------------------------------------------------------------------------- shell

func TestShellBlockReadsKeyFile(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".zshrc"), "# rc\n")
	env, _ := newEnv(t, home, "amber", "tuba-sk-secret-key", false)

	ConfigureShellEnv(env)

	rc, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if strings.Contains(string(rc), "tuba-sk-secret-key") {
		t.Errorf("raw key embedded in shell rc:\n%s", rc)
	}
	keyFile := filepath.Join(home, ".config", "2ba", "2BA_API_KEY")
	if !strings.Contains(string(rc), `$(cat "`+keyFile+`" 2>/dev/null)`) {
		t.Errorf("rc block does not read the key file:\n%s", rc)
	}
	for _, want := range []string{
		`export OPENAI_API_BASE="` + testBase + `"`,
		"aider --model openai/amber",
	} {
		if !strings.Contains(string(rc), want) {
			t.Errorf("rc missing %q:\n%s", want, rc)
		}
	}
}

func TestShellIdempotent(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".zshrc"), "# rc\n")
	env, _ := newEnv(t, home, "amber", "k", false)
	ConfigureShellEnv(env)
	ConfigureShellEnv(env)

	rc, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if n := strings.Count(string(rc), BlockBegin); n != 1 {
		t.Errorf("want exactly one managed block, found %d:\n%s", n, rc)
	}
	if !strings.Contains(string(rc), "# rc") {
		t.Errorf("original rc content lost:\n%s", rc)
	}
}

func TestShellNoRC(t *testing.T) {
	home := t.TempDir()
	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigureShellEnv(env)
	if !strings.Contains(buf.String(), "no shell rc found") {
		t.Errorf("expected no-rc warning:\n%s", buf.String())
	}
}

// ------------------------------------------------------------- opencode / windsurf

func TestOpencodeAddKeepsUserProvider(t *testing.T) {
	home := t.TempDir()
	ocPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	mustWrite(t, ocPath, `{"provider": {"mine": {"name": "keep me"}}}`)
	env, _ := newEnv(t, home, "amber", "k", false)

	ConfigureOpencode(env)

	oc, _ := os.ReadFile(ocPath)
	if !strings.Contains(string(oc), `"mine"`) || !strings.Contains(string(oc), `"2ba"`) {
		t.Errorf("opencode lost user provider or missed 2ba:\n%s", oc)
	}
	var cfg map[string]any
	if err := json.Unmarshal(oc, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if cfg["model"] != "2ba/amber" {
		t.Errorf("default model = %v, want 2ba/amber", cfg["model"])
	}
	m := cfg["provider"].(map[string]any)["2ba"].(map[string]any)["models"].(map[string]any)["amber"].(map[string]any)
	mods, _ := m["modalities"].(map[string]any)
	if input, _ := mods["input"].([]any); len(input) != 2 || input[0] != "text" || input[1] != "image" {
		t.Errorf("model must declare image input:\n%s", oc)
	}
}

func TestOpencodeExisting2baUntouched(t *testing.T) {
	home := t.TempDir()
	ocPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	existing := `{"provider": {"2ba": {"name": "USER-OWNED", "options": {"apiKey": "user-secret"}}}}`
	mustWrite(t, ocPath, existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureOpencode(env)

	if got, _ := os.ReadFile(ocPath); string(got) != existing {
		t.Errorf("existing 2ba provider was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("expected leave-as-is notice:\n%s", buf.String())
	}
}

func TestOpencodeMalformedJSON(t *testing.T) {
	home := t.TempDir()
	ocPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	broken := "{not json"
	mustWrite(t, ocPath, broken)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureOpencode(env)

	if got, _ := os.ReadFile(ocPath); string(got) != broken {
		t.Errorf("malformed config was rewritten:\n%s", got)
	}
	if !strings.Contains(buf.String(), "not valid JSON") {
		t.Errorf("expected malformed-JSON warning:\n%s", buf.String())
	}
}

func TestOpencodeUpgradesOldEntryWithModelConfig(t *testing.T) {
	home := t.TempDir()
	ocPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	// exactly what a pre-thinking, pre-vision installer wrote
	mustWrite(t, ocPath, `{"model": "2ba/amber", "provider": {"2ba": {
		"npm": "@ai-sdk/openai-compatible", "name": "2ba.ai",
		"options": {"baseURL": "https://api.2ba.ai/v1", "apiKey": "user-rotated-key"},
		"models": {"amber": {"name": "Amber (2ba.ai)"}}}}}`)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureOpencode(env)

	oc, _ := os.ReadFile(ocPath)
	var cfg map[string]any
	if err := json.Unmarshal(oc, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	two := cfg["provider"].(map[string]any)["2ba"].(map[string]any)
	m := two["models"].(map[string]any)["amber"].(map[string]any)
	if m["reasoning"] != true || m["interleaved"] != "reasoning_content" {
		t.Errorf("thinking fields not added:\n%s", oc)
	}
	mods, _ := m["modalities"].(map[string]any)
	if input, _ := mods["input"].([]any); len(input) != 2 || input[0] != "text" || input[1] != "image" {
		t.Errorf("modalities not added:\n%s", oc)
	}
	if m["name"] != "Amber (2ba.ai)" {
		t.Errorf("model name changed:\n%s", oc)
	}
	if opts, _ := two["options"].(map[string]any); opts["apiKey"] != "user-rotated-key" {
		t.Errorf("user options modified:\n%s", oc)
	}
	if cfg["model"] != "2ba/amber" {
		t.Errorf("default model changed:\n%s", oc)
	}
	if !strings.Contains(buf.String(), "model capabilities added") {
		t.Errorf("expected upgrade notice:\n%s", buf.String())
	}
}

func TestOpencodeCompleteEntryUntouched(t *testing.T) {
	home := t.TempDir()
	ocPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	// user-customized values must survive a re-run
	existing := `{"provider": {"2ba": {"models": {"amber": {
		"name": "Amber (2ba.ai)", "reasoning": false, "interleaved": "reasoning",
		"modalities": {"input": ["text"], "output": ["text"]}}}}}}`
	mustWrite(t, ocPath, existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureOpencode(env)

	if got, _ := os.ReadFile(ocPath); string(got) != existing {
		t.Errorf("complete entry was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("expected leave-as-is notice:\n%s", buf.String())
	}
}

func TestWindsurfDoesNotStealDefault(t *testing.T) {
	home := t.TempDir()
	wsPath := filepath.Join(home, ".codeium", "windsurf", "model_config.json")
	mustWrite(t, wsPath, `{"models": {"gpt": {"name": "GPT", "default": true}}}`)
	env, _ := newEnv(t, home, "amber", "k", false)

	ConfigureWindsurf(env)

	var cfg map[string]any
	ws, _ := os.ReadFile(wsPath)
	if err := json.Unmarshal(ws, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	models := cfg["models"].(map[string]any)
	if d, _ := models["2BA"].(map[string]any)["default"].(bool); d {
		t.Errorf("2BA must not steal default from an existing default:\n%s", ws)
	}
}

// -------------------------------------------------------------------------- kimi

func TestKimiAppendBlock(t *testing.T) {
	home := t.TempDir()
	kimiDir := filepath.Join(home, ".kimi-code")
	cfgPath := filepath.Join(kimiDir, "config.toml")
	userCfg := "default_model = \"kimi-code/k3\"\n\n[providers.kimi]\ntype = \"kimi\"\n"
	mustWrite(t, cfgPath, userCfg)
	env, _ := newEnv(t, home, "amber", "tuba-sk-kimi-key", false)

	ConfigureKimi(env)

	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), userCfg) {
		t.Errorf("user config content modified or reordered:\n%s", got)
	}
	for _, want := range []string{
		"[providers.2ba]",
		`type = "openai"`,
		`base_url = "` + testBase + `"`,
		`api_key = "tuba-sk-kimi-key"`,
		"[models.2ba-amber]",
		`provider = "2ba"`,
		`model = "amber"`,
		"max_context_size = 262144",
		`capabilities = ["thinking", "image_in", "tool_use"]`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("kimi config missing %q:\n%s", want, got)
		}
	}
	if st, _ := os.Stat(cfgPath); st.Mode().Perm() != 0o600 {
		t.Errorf("kimi config perms = %v, want 0600", st.Mode().Perm())
	}
}

func TestKimiLegacyProviderType(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".kimi")) // legacy dir
	env, _ := newEnv(t, home, "amber", "k", false)
	ConfigureKimi(env)
	got, _ := os.ReadFile(filepath.Join(home, ".kimi", "config.toml"))
	if !strings.Contains(string(got), `type = "openai_legacy"`) {
		t.Errorf("legacy kimi dir should use openai_legacy:\n%s", got)
	}
}

func TestKimiIdempotent(t *testing.T) {
	home := t.TempDir()
	cfgPath := filepath.Join(home, ".kimi-code", "config.toml")
	mustMkdir(t, filepath.Dir(cfgPath))
	for _, key := range []string{"tuba-sk-first", "tuba-sk-second"} {
		env, _ := newEnv(t, home, "amber", key, false)
		ConfigureKimi(env)
	}
	got, _ := os.ReadFile(cfgPath)
	if n := strings.Count(string(got), "2ba.ai (managed)"); n != 2 {
		t.Errorf("want exactly one block (2 markers), found %d:\n%s", n, got)
	}
	if strings.Contains(string(got), "tuba-sk-first") {
		t.Errorf("stale key not replaced:\n%s", got)
	}
	if !strings.Contains(string(got), "tuba-sk-second") {
		t.Errorf("new key missing:\n%s", got)
	}
}

func TestKimiUserOwnedEntries(t *testing.T) {
	home := t.TempDir()
	cfgPath := filepath.Join(home, ".kimi-code", "config.toml")
	owned := "[providers.2ba]\ntype = \"kimi\"\nbase_url = \"https://api.kimi.com/coding/v1\"\napi_key = \"user-secret\"\n"
	mustWrite(t, cfgPath, owned)
	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigureKimi(env)
	if got, _ := os.ReadFile(cfgPath); string(got) != owned {
		t.Errorf("user-owned 2ba provider was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("expected leave-as-is notice:\n%s", buf.String())
	}
}

func TestKimiDryRun(t *testing.T) {
	home := t.TempDir()
	cfgPath := filepath.Join(home, ".kimi", "config.toml")
	mustWrite(t, cfgPath, "default_model = \"kimi-for-coding\"\n")
	env, buf := newEnv(t, home, "amber", "k", true)
	ConfigureKimi(env)
	if !strings.Contains(buf.String(), "would add 2ba provider to") {
		t.Errorf("dry-run plan missing kimi entry:\n%s", buf.String())
	}
	if got, _ := os.ReadFile(cfgPath); strings.Contains(string(got), "2ba.ai (managed)") {
		t.Errorf("dry run modified the kimi config:\n%s", got)
	}
}

func TestKimiJSONMigrationWarning(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".kimi-code", "config.json"), "{}")
	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigureKimi(env)
	if !strings.Contains(buf.String(), "run `kimi` once") {
		t.Errorf("expected JSON-migration warning:\n%s", buf.String())
	}
}

// -------------------------------------------------------------------------- zcode

func TestZcodeAddKeepsUserProvider(t *testing.T) {
	home := t.TempDir()
	zcfg := filepath.Join(home, ".zcode", "v2", "config.json")
	mustWrite(t, zcfg, `{"provider": {"builtin:zai": {"name": "keep me"}}}`)
	env, _ := newEnv(t, home, "amber", "tuba-sk-zcode-key", false)

	ConfigureZcode(env)

	var cfg map[string]any
	zc, _ := os.ReadFile(zcfg)
	if err := json.Unmarshal(zc, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	providers := cfg["provider"].(map[string]any)
	if _, present := providers["builtin:zai"]; !present {
		t.Errorf("user provider lost:\n%s", zc)
	}
	p, ok := providers["2ba"].(map[string]any)
	if !ok {
		t.Fatalf("2ba provider missing:\n%s", zc)
	}
	if p["kind"] != "openai-compatible" || p["enabled"] != true || p["source"] != "custom" {
		t.Errorf("2ba provider shape wrong:\n%s", zc)
	}
	opts, _ := p["options"].(map[string]any)
	if opts["apiKey"] != "tuba-sk-zcode-key" || opts["baseURL"] != testBase {
		t.Errorf("2ba options wrong:\n%s", zc)
	}
	models, _ := p["models"].(map[string]any)
	if _, present := models["amber"]; !present {
		t.Errorf("model amber missing:\n%s", zc)
	}
	m, _ := models["amber"].(map[string]any)
	mods, _ := m["modalities"].(map[string]any)
	if input, _ := mods["input"].([]any); len(input) != 2 || input[0] != "text" || input[1] != "image" {
		t.Errorf("model must declare image input:\n%s", zc)
	}
	if st, _ := os.Stat(zcfg); st.Mode().Perm() != 0o600 {
		t.Errorf("zcode config perms = %v, want 0600", st.Mode().Perm())
	}
}

func TestZcodeExisting2baUntouched(t *testing.T) {
	home := t.TempDir()
	zcfg := filepath.Join(home, ".zcode", "v2", "config.json")
	existing := `{"provider": {"2ba": {"name": "USER-OWNED", "options": {"apiKey": "user-secret"}}}}`
	mustWrite(t, zcfg, existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureZcode(env)

	if got, _ := os.ReadFile(zcfg); string(got) != existing {
		t.Errorf("existing 2ba provider was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("expected leave-as-is notice:\n%s", buf.String())
	}
}

func TestZcodeUpgradesOldEntryWithModelConfig(t *testing.T) {
	home := t.TempDir()
	zcfg := filepath.Join(home, ".zcode", "v2", "config.json")
	// exactly what a pre-reasoning, pre-vision installer wrote
	mustWrite(t, zcfg, `{"provider": {"2ba": {
		"name": "2ba", "kind": "openai-compatible",
		"options": {"apiKey": "user-rotated-key", "baseURL": "https://api.2ba.ai/v1", "apiKeyRequired": true},
		"enabled": true, "source": "custom",
		"models": {"amber": {
			"limit": {"context": 131072},
			"modalities": {"input": ["text"], "output": ["text"]}}}}}}`)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureZcode(env)

	zc, _ := os.ReadFile(zcfg)
	var cfg map[string]any
	if err := json.Unmarshal(zc, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	two := cfg["provider"].(map[string]any)["2ba"].(map[string]any)
	m := two["models"].(map[string]any)["amber"].(map[string]any)
	r, ok := m["reasoning"].(map[string]any)
	if !ok || r["enabled"] != true || r["defaultVariant"] != "medium" {
		t.Errorf("reasoning selector not added:\n%s", zc)
	}
	mods, _ := m["modalities"].(map[string]any)
	if input, _ := mods["input"].([]any); len(input) != 2 || input[0] != "text" || input[1] != "image" {
		t.Errorf("text-only input modalities not widened:\n%s", zc)
	}
	if out, _ := mods["output"].([]any); len(out) != 1 || out[0] != "text" {
		t.Errorf("output modalities changed:\n%s", zc)
	}
	if lim, _ := m["limit"].(map[string]any); lim["context"] != float64(131072) {
		t.Errorf("user limit modified:\n%s", zc)
	}
	if opts, _ := two["options"].(map[string]any); opts["apiKey"] != "user-rotated-key" {
		t.Errorf("user options modified:\n%s", zc)
	}
	if !strings.Contains(buf.String(), "model capabilities added") {
		t.Errorf("expected upgrade notice:\n%s", buf.String())
	}
}

func TestZcodeCompleteEntryUntouched(t *testing.T) {
	home := t.TempDir()
	zcfg := filepath.Join(home, ".zcode", "v2", "config.json")
	// user-customized reasoning and modalities must survive a re-run; the
	// input list already declares the current shape, so nothing is widened
	existing := `{"provider": {"2ba": {"models": {"amber": {
		"reasoning": {"enabled": false, "variants": ["low"], "defaultVariant": "low"},
		"modalities": {"input": ["text", "image"], "output": ["text", "image"]}}}}}}`
	mustWrite(t, zcfg, existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureZcode(env)

	if got, _ := os.ReadFile(zcfg); string(got) != existing {
		t.Errorf("complete entry was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("expected leave-as-is notice:\n%s", buf.String())
	}
}

func TestZcodeUserModalitiesValuesUntouched(t *testing.T) {
	// an explicit null or a non-object is a user value too: the upgrade must
	// not replace it with our declaration, only widen our own old shape
	for name, val := range map[string]string{
		"explicit null": `null`,
		"non-object":    `"text"`,
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			zcfg := filepath.Join(home, ".zcode", "v2", "config.json")
			existing := `{"provider": {"2ba": {"models": {"amber": {"reasoning": {"enabled": true}, "modalities": ` + val + `}}}}}`
			mustWrite(t, zcfg, existing)
			env, buf := newEnv(t, home, "amber", "k", false)

			ConfigureZcode(env)

			if got, _ := os.ReadFile(zcfg); string(got) != existing {
				t.Errorf("user modalities value replaced:\n%s", got)
			}
			if !strings.Contains(buf.String(), "already configured") {
				t.Errorf("expected leave-as-is notice:\n%s", buf.String())
			}
		})
	}
}

func TestZcodeNoHomeDir(t *testing.T) {
	home := t.TempDir()
	// Keep zcode off PATH so the "not detected" branch is taken even on a
	// machine that has zcode installed.
	t.Setenv("PATH", t.TempDir())
	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigureZcode(env)
	if !strings.Contains(buf.String(), "ZCode not detected") || !strings.Contains(buf.String(), "install it, run it once") {
		t.Errorf("expected not-detected warning:\n%s", buf.String())
	}
}

func TestZcodeNoV2Dir(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".zcode"))
	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigureZcode(env)
	if !strings.Contains(buf.String(), "no v2 config yet") {
		t.Errorf("expected v2-missing warning:\n%s", buf.String())
	}
}

func TestZcodeMalformedJSON(t *testing.T) {
	home := t.TempDir()
	zcfg := filepath.Join(home, ".zcode", "v2", "config.json")
	broken := "{not json"
	mustWrite(t, zcfg, broken)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureZcode(env)

	if got, _ := os.ReadFile(zcfg); string(got) != broken {
		t.Errorf("malformed config was rewritten:\n%s", got)
	}
	if !strings.Contains(buf.String(), "not valid JSON") {
		t.Errorf("expected malformed-JSON warning:\n%s", buf.String())
	}
}

func TestZcodeDryRun(t *testing.T) {
	home := t.TempDir()
	zcfg := filepath.Join(home, ".zcode", "v2", "config.json")
	mustWrite(t, zcfg, `{}`)
	env, buf := newEnv(t, home, "amber", "k", true)
	ConfigureZcode(env)
	if !strings.Contains(buf.String(), "would add 2ba provider to") {
		t.Errorf("dry-run plan missing zcode entry:\n%s", buf.String())
	}
	if got, _ := os.ReadFile(zcfg); string(got) != `{}` {
		t.Errorf("dry run modified the zcode config:\n%s", got)
	}
}

func TestZcodeDryRunExisting(t *testing.T) {
	home := t.TempDir()
	zcfg := filepath.Join(home, ".zcode", "v2", "config.json")
	existing := `{"provider": {"2ba": {"name": "2ba"}}}`
	mustWrite(t, zcfg, existing)
	env, buf := newEnv(t, home, "amber", "k", true)
	ConfigureZcode(env)
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("dry run must match the real path (leave existing provider as-is):\n%s", buf.String())
	}
	if got, _ := os.ReadFile(zcfg); string(got) != existing {
		t.Errorf("dry run modified the zcode config:\n%s", got)
	}
}

// -------------------------------------------------------------------- 2ba-code

// twocodeFile is the custom-provider store under the pinned TWOBA_DATA_DIR.
func twocodeFile(home string) string {
	return filepath.Join(home, ".config", "2ba-code", "agent-home", ".config", "2ba-code", "secrets", "custom-model-providers.json")
}

// twocodeSeed is a valid store with one user-owned provider, as the desktop
// would have written it.
const twocodeUserProvider = `{"providerId":"custom-12345678-1234-4abc-8def-123456789abc","label":"OpenRouter","apiFormat":"openai-chat-completions","baseURL":"https://openrouter.ai/api/v1","apiKey":"user-secret","models":[{"modelId":"test-model"}],"createdAt":1,"updatedAt":1}`

func TestTwocodeAddCreatesFile(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".config", "2ba-code"))
	env, buf := newEnv(t, home, "amber", "tuba-sk-twocode-key", false)

	ConfigureTwocode(env)

	data, err := os.ReadFile(twocodeFile(home))
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		SchemaVersion int `json:"schemaVersion"`
		Providers     []struct {
			ProviderID string `json:"providerId"`
			Label      string `json:"label"`
			APIFormat  string `json:"apiFormat"`
			BaseURL    string `json:"baseURL"`
			APIKey     string `json:"apiKey"`
			Models     []struct {
				ModelID              string   `json:"modelId"`
				ContextWindow        int      `json:"contextWindow"`
				ThinkingLevels       []string `json:"thinkingLevels"`
				DefaultThinkingLevel string   `json:"defaultThinkingLevel"`
			} `json:"models"`
			CreatedAt int64 `json:"createdAt"`
			UpdatedAt int64 `json:"updatedAt"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	if root.SchemaVersion != 2 || len(root.Providers) != 1 {
		t.Fatalf("unexpected store:\n%s", data)
	}
	p := root.Providers[0]
	if !regexp.MustCompile(`^custom-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(p.ProviderID) {
		t.Errorf("providerId %q is not custom-<uuidv4>", p.ProviderID)
	}
	if p.Label != "2ba" || p.APIFormat != "openai-chat-completions" || p.BaseURL != testBase || p.APIKey != "tuba-sk-twocode-key" {
		t.Errorf("provider fields wrong:\n%s", data)
	}
	if len(p.Models) != 1 || p.Models[0].ModelID != "amber" || p.Models[0].ContextWindow != 262144 {
		t.Errorf("model entry wrong:\n%s", data)
	}
	// "off" is mandatory — the desktop rejects the whole store without it
	if got := p.Models[0].ThinkingLevels; len(got) != 4 || got[0] != "off" || got[3] != "high" {
		t.Errorf("thinking levels = %v, want [off low medium high]:\n%s", got, data)
	}
	if p.Models[0].DefaultThinkingLevel != "medium" {
		t.Errorf("default thinking level = %q, want medium:\n%s", p.Models[0].DefaultThinkingLevel, data)
	}
	if p.CreatedAt <= 0 || p.UpdatedAt <= 0 {
		t.Errorf("timestamps missing:\n%s", data)
	}
	if st, _ := os.Stat(twocodeFile(home)); st.Mode().Perm() != 0o600 {
		t.Errorf("providers file perms = %v, want 0600", st.Mode().Perm())
	}
	secrets := filepath.Join(home, ".config", "2ba-code", "agent-home", ".config", "2ba-code", "secrets")
	if st, _ := os.Stat(secrets); st.Mode().Perm() != 0o700 {
		t.Errorf("secrets dir perms = %v, want 0700", st.Mode().Perm())
	}
	if !strings.Contains(buf.String(), "2ba-code: provider") {
		t.Errorf("expected add notice:\n%s", buf.String())
	}
}

func TestTwocodeKeepsUserProvider(t *testing.T) {
	home := t.TempDir()
	seed := `{"schemaVersion":2,"providers":[` + twocodeUserProvider + `]}`
	mustWrite(t, twocodeFile(home), seed)
	env, _ := newEnv(t, home, "amber", "k", false)

	ConfigureTwocode(env)

	data, _ := os.ReadFile(twocodeFile(home))
	var root struct {
		Providers []struct {
			Label  string `json:"label"`
			APIKey string `json:"apiKey"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	if len(root.Providers) != 2 {
		t.Fatalf("want user + 2ba provider, got %d:\n%s", len(root.Providers), data)
	}
	byLabel := map[string]string{}
	for _, p := range root.Providers {
		byLabel[p.Label] = p.APIKey
	}
	if byLabel["OpenRouter"] != "user-secret" {
		t.Errorf("user provider key modified:\n%s", data)
	}
	if byLabel["2ba"] != "k" {
		t.Errorf("2ba provider missing or wrong:\n%s", data)
	}
}

func TestTwocodePreservesUnknownFields(t *testing.T) {
	home := t.TempDir()
	// Fields the desktop may have added and this installer knows nothing
	// about must survive the rewrite.
	seed := `{"schemaVersion":2,"topLevelExtra":"keep","providers":[{"providerId":"custom-12345678-1234-4abc-8def-123456789abc","label":"OpenRouter","apiFormat":"openai-chat-completions","baseURL":"https://openrouter.ai/api/v1","apiKey":"user-secret","models":[{"modelId":"test-model","maxTokens":8192}],"createdAt":1,"updatedAt":1,"icon":"openrouter"}]}`
	mustWrite(t, twocodeFile(home), seed)
	env, _ := newEnv(t, home, "amber", "k", false)

	ConfigureTwocode(env)

	data, _ := os.ReadFile(twocodeFile(home))
	for _, want := range []string{`"topLevelExtra": "keep"`, `"icon": "openrouter"`, `"maxTokens": 8192`, `"user-secret"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("field %s lost on rewrite:\n%s", want, data)
		}
	}
}

func TestTwocodeExisting2baUntouched(t *testing.T) {
	home := t.TempDir()
	ours := `{"providerId":"custom-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","label":"2ba","apiFormat":"openai-chat-completions","baseURL":"` + testBase + `","apiKey":"stale-key","models":[{"modelId":"old-model"}],"createdAt":1,"updatedAt":1}`
	seed := `{"schemaVersion":2,"providers":[` + ours + `]}`
	mustWrite(t, twocodeFile(home), seed)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureTwocode(env)

	if got, _ := os.ReadFile(twocodeFile(home)); string(got) != seed {
		t.Errorf("existing 2ba provider was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("expected leave-as-is notice:\n%s", buf.String())
	}
}

func TestTwocodeUpgradesOldEntryWithThinkingLevels(t *testing.T) {
	home := t.TempDir()
	// exactly what a pre-thinking installer wrote: our base, our model, no levels
	ours := `{"providerId":"custom-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","label":"2ba","apiFormat":"openai-chat-completions","baseURL":"` + testBase + `","apiKey":"user-rotated-key","models":[{"modelId":"amber","contextWindow":131072}],"createdAt":1,"updatedAt":1}`
	seed := `{"schemaVersion":2,"providers":[` + twocodeUserProvider + `,` + ours + `]}`
	mustWrite(t, twocodeFile(home), seed)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureTwocode(env)

	data, err := os.ReadFile(twocodeFile(home))
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Providers []struct {
			Label  string `json:"label"`
			APIKey string `json:"apiKey"`
			Models []struct {
				ModelID              string   `json:"modelId"`
				ThinkingLevels       []string `json:"thinkingLevels"`
				DefaultThinkingLevel string   `json:"defaultThinkingLevel"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	if len(root.Providers) != 2 {
		t.Fatalf("provider count changed:\n%s", data)
	}
	for _, p := range root.Providers {
		switch p.Label {
		case "OpenRouter":
			if p.Models[0].ThinkingLevels != nil {
				t.Errorf("user provider modified:\n%s", data)
			}
		case "2ba":
			if p.APIKey != "user-rotated-key" {
				t.Errorf("user options modified:\n%s", data)
			}
			if got := p.Models[0].ThinkingLevels; len(got) != 4 || got[0] != "off" {
				t.Errorf("thinking levels not added: %v\n%s", got, data)
			}
			if p.Models[0].DefaultThinkingLevel != "medium" {
				t.Errorf("default thinking level not added:\n%s", data)
			}
		}
	}
	if !strings.Contains(buf.String(), "thinking levels added") {
		t.Errorf("expected upgrade notice:\n%s", buf.String())
	}
}

func TestTwocodePartiallyDeclaredEntryUntouched(t *testing.T) {
	// a value on either thinking key marks the entry user-managed: backfilling
	// the other could produce a default that is not among the levels, which
	// the desktop rejects the whole store for
	for name, models := range map[string]string{
		"levels without default": `[{"modelId":"amber","thinkingLevels":["off","low"]}]`,
		"default without levels": `[{"modelId":"amber","defaultThinkingLevel":"low"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			ours := `{"providerId":"custom-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","label":"2ba","apiFormat":"openai-chat-completions","baseURL":"` + testBase + `","apiKey":"k","models":` + models + `,"createdAt":1,"updatedAt":1}`
			seed := `{"schemaVersion":2,"providers":[` + ours + `]}`
			mustWrite(t, twocodeFile(home), seed)
			env, buf := newEnv(t, home, "amber", "k", false)

			ConfigureTwocode(env)

			if got, _ := os.ReadFile(twocodeFile(home)); string(got) != seed {
				t.Errorf("partially declared entry modified:\n%s", got)
			}
			if !strings.Contains(buf.String(), "already configured") {
				t.Errorf("expected leave-as-is notice:\n%s", buf.String())
			}
		})
	}
}

func TestTwocodeNoProfileDir(t *testing.T) {
	home := t.TempDir()
	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigureTwocode(env)
	if !strings.Contains(buf.String(), "2ba-code not detected") || !strings.Contains(buf.String(), "install it, run it once") {
		t.Errorf("expected not-detected warning:\n%s", buf.String())
	}
}

func TestTwocodeMalformedJSON(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".config", "2ba-code"))
	broken := "{not json"
	mustWrite(t, twocodeFile(home), broken)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureTwocode(env)

	if got, _ := os.ReadFile(twocodeFile(home)); string(got) != broken {
		t.Errorf("malformed store was rewritten:\n%s", got)
	}
	if !strings.Contains(buf.String(), "not valid JSON") {
		t.Errorf("expected malformed-JSON warning:\n%s", buf.String())
	}
}

func TestTwocodeUnsupportedSchema(t *testing.T) {
	home := t.TempDir()
	legacy := `{"schemaVersion":1,"providers":[]}`
	mustWrite(t, twocodeFile(home), legacy)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureTwocode(env)

	if got, _ := os.ReadFile(twocodeFile(home)); string(got) != legacy {
		t.Errorf("legacy store was rewritten:\n%s", got)
	}
	if !strings.Contains(buf.String(), "unsupported schemaVersion") {
		t.Errorf("expected schema warning:\n%s", buf.String())
	}
}

func TestTwocodeDryRun(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".config", "2ba-code"))
	env, buf := newEnv(t, home, "amber", "k", true)
	ConfigureTwocode(env)
	if !strings.Contains(buf.String(), "would add 2ba provider to") {
		t.Errorf("dry-run plan missing 2ba-code entry:\n%s", buf.String())
	}
	if _, err := os.Stat(twocodeFile(home)); !os.IsNotExist(err) {
		t.Errorf("dry run created the providers file")
	}
}

func TestTwocodeDryRunExisting(t *testing.T) {
	home := t.TempDir()
	// a complete entry: dry-run must treat it as configured, not upgradable
	ours := `{"providerId":"custom-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","label":"2ba","apiFormat":"openai-chat-completions","baseURL":"` + testBase + `","apiKey":"k","models":[{"modelId":"amber","thinkingLevels":["off","low","medium","high"],"defaultThinkingLevel":"medium"}],"createdAt":1,"updatedAt":1}`
	seed := `{"schemaVersion":2,"providers":[` + ours + `]}`
	mustWrite(t, twocodeFile(home), seed)
	env, buf := newEnv(t, home, "amber", "k", true)
	ConfigureTwocode(env)
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("dry run must match the real path (leave existing provider as-is):\n%s", buf.String())
	}
	if got, _ := os.ReadFile(twocodeFile(home)); string(got) != seed {
		t.Errorf("dry run modified the store:\n%s", got)
	}
}

// ----------------------------------------------------------------------- claude

func claudeSettings(home string) string {
	return filepath.Join(home, ".claude", "settings.json")
}

func TestClaudeAddMergesIntoSettings(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, claudeSettings(home), `{"theme":"dark","env":{"SOME_VAR":"keep"}}`)
	env, buf := newEnv(t, home, "amber", "tuba-sk-claude-key", false)

	ConfigureClaude(env)

	data, _ := os.ReadFile(claudeSettings(home))
	var cfg struct {
		Theme string            `json:"theme"`
		Env   map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	if cfg.Theme != "dark" || cfg.Env["SOME_VAR"] != "keep" {
		t.Errorf("user settings lost:\n%s", data)
	}
	want := map[string]string{
		"ANTHROPIC_BASE_URL":         "https://api.2ba.ai",
		"ANTHROPIC_AUTH_TOKEN":       "tuba-sk-claude-key",
		"ANTHROPIC_MODEL":            "amber",
		"ANTHROPIC_SMALL_FAST_MODEL": "amber",
	}
	for k, v := range want {
		if cfg.Env[k] != v {
			t.Errorf("env %s = %q, want %q:\n%s", k, cfg.Env[k], v, data)
		}
	}
	if st, _ := os.Stat(claudeSettings(home)); st.Mode().Perm() != 0o600 {
		t.Errorf("settings perms = %v, want 0600", st.Mode().Perm())
	}
	if !strings.Contains(buf.String(), "Claude Code: 2ba configured") {
		t.Errorf("expected configure notice:\n%s", buf.String())
	}
}

func TestClaudeExisting2baUntouched(t *testing.T) {
	home := t.TempDir()
	existing := `{"env":{"ANTHROPIC_BASE_URL":"https://api.2ba.ai/v1","ANTHROPIC_AUTH_TOKEN":"stale-key","ANTHROPIC_MODEL":"amber"}}`
	mustWrite(t, claudeSettings(home), existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureClaude(env)

	if got, _ := os.ReadFile(claudeSettings(home)); string(got) != existing {
		t.Errorf("existing 2ba configuration was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("expected leave-as-is notice:\n%s", buf.String())
	}
}

func TestClaudeOtherGatewayUntouched(t *testing.T) {
	home := t.TempDir()
	existing := `{"env":{"ANTHROPIC_BASE_URL":"https://proxy.example.com","ANTHROPIC_API_KEY":"real-anthropic-key"}}`
	mustWrite(t, claudeSettings(home), existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureClaude(env)

	if got, _ := os.ReadFile(claudeSettings(home)); string(got) != existing {
		t.Errorf("user's gateway configuration was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already points ANTHROPIC_BASE_URL") {
		t.Errorf("expected other-gateway warning:\n%s", buf.String())
	}
}

func TestClaudeNoHomeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir()) // keep claude off PATH too
	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigureClaude(env)
	if !strings.Contains(buf.String(), "Claude Code not detected") || !strings.Contains(buf.String(), "install it, run it once") {
		t.Errorf("expected not-detected warning:\n%s", buf.String())
	}
}

func TestClaudeMalformedJSON(t *testing.T) {
	home := t.TempDir()
	broken := "{not json"
	mustWrite(t, claudeSettings(home), broken)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureClaude(env)

	if got, _ := os.ReadFile(claudeSettings(home)); string(got) != broken {
		t.Errorf("malformed settings were rewritten:\n%s", got)
	}
	if !strings.Contains(buf.String(), "not valid JSON") {
		t.Errorf("expected malformed-JSON warning:\n%s", buf.String())
	}
}

func TestClaudeDryRun(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".claude"))
	env, buf := newEnv(t, home, "amber", "k", true)
	ConfigureClaude(env)
	if !strings.Contains(buf.String(), "would add 2ba provider to") {
		t.Errorf("dry-run plan missing claude entry:\n%s", buf.String())
	}
	if _, err := os.Stat(claudeSettings(home)); !os.IsNotExist(err) {
		t.Errorf("dry run created the settings file")
	}
}

func TestClaudeDryRunExisting(t *testing.T) {
	home := t.TempDir()
	existing := `{"env":{"ANTHROPIC_BASE_URL":"https://api.2ba.ai","ANTHROPIC_AUTH_TOKEN":"k"}}`
	mustWrite(t, claudeSettings(home), existing)
	env, buf := newEnv(t, home, "amber", "k", true)
	ConfigureClaude(env)
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("dry run must match the real path (leave existing configuration as-is):\n%s", buf.String())
	}
	if got, _ := os.ReadFile(claudeSettings(home)); string(got) != existing {
		t.Errorf("dry run modified the settings:\n%s", got)
	}
}

func TestRevertClaudeRemovesManaged(t *testing.T) {
	home := t.TempDir()
	seed := `{"theme":"dark","env":{"ANTHROPIC_BASE_URL":"https://api.2ba.ai/v1","ANTHROPIC_AUTH_TOKEN":"tuba-sk-old","ANTHROPIC_MODEL":"amber","ANTHROPIC_SMALL_FAST_MODEL":"amber","ANTHROPIC_API_KEY":"user-real-key"}}`
	mustWrite(t, claudeSettings(home), seed)
	env, buf := newEnv(t, home, "amber", "k", false)

	RevertClaude(env)

	data, _ := os.ReadFile(claudeSettings(home))
	var cfg struct {
		Theme string            `json:"theme"`
		Env   map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	if cfg.Theme != "dark" {
		t.Errorf("user settings lost:\n%s", data)
	}
	for _, k := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL", "ANTHROPIC_SMALL_FAST_MODEL"} {
		if _, has := cfg.Env[k]; has {
			t.Errorf("%s not removed:\n%s", k, data)
		}
	}
	if cfg.Env["ANTHROPIC_API_KEY"] != "user-real-key" {
		t.Errorf("user's ANTHROPIC_API_KEY was removed:\n%s", data)
	}
	if _, err := os.Stat(claudeSettings(home) + ".bak.2ba"); err != nil {
		t.Errorf("no backup created:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "removed 2ba configuration") {
		t.Errorf("expected removal notice:\n%s", buf.String())
	}
}

func TestRevertClaudeOtherGatewayUntouched(t *testing.T) {
	home := t.TempDir()
	seed := `{"env":{"ANTHROPIC_BASE_URL":"https://proxy.example.com","ANTHROPIC_API_KEY":"k"}}`
	mustWrite(t, claudeSettings(home), seed)
	env, buf := newEnv(t, home, "amber", "k", false)

	RevertClaude(env)

	if got, _ := os.ReadFile(claudeSettings(home)); string(got) != seed {
		t.Errorf("unrelated settings were rewritten:\n%s", got)
	}
	if _, err := os.Stat(claudeSettings(home) + ".bak.2ba"); !os.IsNotExist(err) {
		t.Errorf("no-op run created a backup")
	}
	if buf.String() != "" {
		t.Errorf("no output expected on a no-op:\n%s", buf.String())
	}
}

func TestRevertClaudeNoSettings(t *testing.T) {
	home := t.TempDir()
	env, buf := newEnv(t, home, "amber", "k", false)
	RevertClaude(env)
	if buf.String() != "" {
		t.Errorf("no output expected without a settings file:\n%s", buf.String())
	}
}

// An empty --api-base normalizes to "", and so does a settings file whose
// env has no ANTHROPIC_BASE_URL. That must not count as a match: the file
// is not ours and its ANTHROPIC_* keys belong to the user.
func TestRevertClaudeEmptyBaseUntouched(t *testing.T) {
	home := t.TempDir()
	seed := `{"env":{"ANTHROPIC_MODEL":"claude-opus-5","ANTHROPIC_API_KEY":"k"}}`
	mustWrite(t, claudeSettings(home), seed)
	env, buf := newEnv(t, home, "amber", "k", false)
	env.APIBase = ""

	RevertClaude(env)

	if got, _ := os.ReadFile(claudeSettings(home)); string(got) != seed {
		t.Errorf("settings rewritten on an empty base:\n%s", got)
	}
	if _, err := os.Stat(claudeSettings(home) + ".bak.2ba"); !os.IsNotExist(err) {
		t.Errorf("no-op run created a backup")
	}
	if buf.String() != "" {
		t.Errorf("no output expected on a no-op:\n%s", buf.String())
	}
}

func TestRevertClaudeDryRun(t *testing.T) {
	home := t.TempDir()
	seed := `{"env":{"ANTHROPIC_BASE_URL":"https://api.2ba.ai","ANTHROPIC_AUTH_TOKEN":"old"}}`
	mustWrite(t, claudeSettings(home), seed)
	env, buf := newEnv(t, home, "amber", "k", true)

	RevertClaude(env)

	if !strings.Contains(buf.String(), "would remove the 2ba configuration") {
		t.Errorf("dry-run plan missing revert entry:\n%s", buf.String())
	}
	if got, _ := os.ReadFile(claudeSettings(home)); string(got) != seed {
		t.Errorf("dry run modified the settings:\n%s", got)
	}
	if _, err := os.Stat(claudeSettings(home) + ".bak.2ba"); !os.IsNotExist(err) {
		t.Errorf("dry run created a backup")
	}
}

// A backup is only taken right before a real write, so no-op runs must not
// leave *.bak.2ba files behind.
func TestNoBackupOnNoOp(t *testing.T) {
	home := t.TempDir()
	zcfg := filepath.Join(home, ".zcode", "v2", "config.json")
	mustWrite(t, zcfg, `{"provider": {"2ba": {"name": "2ba"}}}`)
	twc := twocodeFile(home)
	mustWrite(t, twc, `{"schemaVersion":2,"providers":[{"providerId":"custom-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","label":"2ba","apiFormat":"openai-chat-completions","baseURL":"`+testBase+`","apiKey":"old","models":[{"modelId":"amber","thinkingLevels":["off","low","medium","high"],"defaultThinkingLevel":"medium"}],"createdAt":1,"updatedAt":1}]}`)
	mustWrite(t, claudeSettings(home), `{"env":{"ANTHROPIC_BASE_URL":"https://api.2ba.ai","ANTHROPIC_AUTH_TOKEN":"old"}}`)
	mustWrite(t, piModels(home), `{"providers":{"2ba":{"baseUrl":"`+testBase+`","api":"openai-completions","apiKey":"old","compat":{"supportsDeveloperRole":false},"models":[{"id":"amber","name":"Amber (2ba.ai)","reasoning":true,"input":["text","image"],"contextWindow":262144}]}}}`)

	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigureZcode(env)
	ConfigureTwocode(env)
	ConfigureClaude(env)
	ConfigurePi(env)

	if n := strings.Count(buf.String(), "already configured"); n != 4 {
		t.Errorf("want four already-configured notes, got %d:\n%s", n, buf.String())
	}
	for _, f := range []string{zcfg, twc, claudeSettings(home), piModels(home)} {
		if _, err := os.Stat(f + ".bak.2ba"); err == nil {
			t.Errorf("no-op run left a backup: %s.bak.2ba", f)
		}
	}
}

// The same rule on uninstall: a file that holds no 2ba entry is not backed up.
func TestUninstallNoBackupWhenNoMatch(t *testing.T) {
	home := t.TempDir()
	twc := twocodeFile(home)
	mustWrite(t, twc, `{"schemaVersion":2,"providers":[`+twocodeUserProvider+`]}`)
	mustWrite(t, claudeSettings(home), `{"env":{"ANTHROPIC_BASE_URL":"https://proxy.example.com","ANTHROPIC_API_KEY":"k"}}`)

	env, _ := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	for _, f := range []string{twc, claudeSettings(home)} {
		if _, err := os.Stat(f + ".bak.2ba"); err == nil {
			t.Errorf("uninstall without a 2ba entry left a backup: %s.bak.2ba", f)
		}
	}
}

// -------------------------------------------------------------------------- pi

func piModels(home string) string {
	return filepath.Join(home, ".pi", "agent", "models.json")
}

func TestPiAddCreatesFile(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".pi", "agent"))
	env, buf := newEnv(t, home, "amber", "tuba-sk-pi-key", false)

	ConfigurePi(env)

	data, err := os.ReadFile(piModels(home))
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			API     string `json:"api"`
			APIKey  string `json:"apiKey"`
			Compat  struct {
				SupportsDeveloperRole bool `json:"supportsDeveloperRole"`
			} `json:"compat"`
			Models []struct {
				ID            string   `json:"id"`
				Name          string   `json:"name"`
				Reasoning     bool     `json:"reasoning"`
				Input         []string `json:"input"`
				ContextWindow int      `json:"contextWindow"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	p, ok := root.Providers["2ba"]
	if !ok {
		t.Fatalf("2ba provider missing:\n%s", data)
	}
	if p.BaseURL != testBase || p.API != "openai-completions" || p.APIKey != "tuba-sk-pi-key" {
		t.Errorf("provider fields wrong:\n%s", data)
	}
	// the gateway rejects the "developer" role, so Pi must be told to send
	// its system prompt as a plain "system" message
	if p.Compat.SupportsDeveloperRole {
		t.Errorf("compat must set supportsDeveloperRole to false:\n%s", data)
	}
	if len(p.Models) != 1 || p.Models[0].ID != "amber" || p.Models[0].Name != "Amber (2ba.ai)" {
		t.Errorf("model entry wrong:\n%s", data)
	}
	if !p.Models[0].Reasoning {
		t.Errorf("model must be declared a thinking model:\n%s", data)
	}
	in := p.Models[0].Input
	if len(in) != 2 || in[0] != "text" || in[1] != "image" {
		t.Errorf("model must declare image input:\n%s", data)
	}
	if p.Models[0].ContextWindow != 262144 {
		t.Errorf("context window = %d, want 262144:\n%s", p.Models[0].ContextWindow, data)
	}
	if st, _ := os.Stat(piModels(home)); st.Mode().Perm() != 0o600 {
		t.Errorf("models.json perms = %v, want 0600", st.Mode().Perm())
	}
	if !strings.Contains(buf.String(), "Pi: provider") {
		t.Errorf("expected add notice:\n%s", buf.String())
	}
}

func TestPiAddKeepsUserProvider(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, piModels(home), `{"providers": {"ollama": {"baseUrl": "http://localhost:11434/v1", "api": "openai-completions"}}}`)
	env, _ := newEnv(t, home, "amber", "k", false)

	ConfigurePi(env)

	var cfg map[string]any
	data, _ := os.ReadFile(piModels(home))
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	providers := cfg["providers"].(map[string]any)
	if _, present := providers["ollama"]; !present {
		t.Errorf("user provider lost:\n%s", data)
	}
	if _, present := providers["2ba"]; !present {
		t.Errorf("2ba provider missing:\n%s", data)
	}
}

func TestPiExisting2baUntouched(t *testing.T) {
	home := t.TempDir()
	// a "2ba" provider without our model is user-managed (or a different
	// service that happens to use the same id): leave it completely alone
	existing := `{"providers": {"2ba": {"name": "USER-OWNED", "apiKey": "user-secret"}}}`
	mustWrite(t, piModels(home), existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigurePi(env)

	if got, _ := os.ReadFile(piModels(home)); string(got) != existing {
		t.Errorf("existing 2ba provider was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("expected leave-as-is notice:\n%s", buf.String())
	}
}

func TestPiUpgradesOldEntry(t *testing.T) {
	home := t.TempDir()
	// an entry with our model but no capability fields, as a stripped-down
	// (or user-trimmed) catalog would look
	existing := `{"providers": {"2ba": {"baseUrl": "` + testBase + `", "api": "openai-completions", "apiKey": "user-rotated-key", "models": [{"id": "amber"}]}}}`
	mustWrite(t, piModels(home), existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigurePi(env)

	data, _ := os.ReadFile(piModels(home))
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	p := cfg["providers"].(map[string]any)["2ba"].(map[string]any)
	if p["apiKey"] != "user-rotated-key" {
		t.Errorf("user apiKey modified:\n%s", data)
	}
	m := p["models"].([]any)[0].(map[string]any)
	if m["name"] != "Amber (2ba.ai)" {
		t.Errorf("name not backfilled:\n%s", data)
	}
	if m["reasoning"] != true {
		t.Errorf("reasoning not backfilled:\n%s", data)
	}
	if m["contextWindow"] != float64(262144) {
		t.Errorf("contextWindow not backfilled:\n%s", data)
	}
	if input, _ := m["input"].([]any); len(input) != 2 || input[0] != "text" || input[1] != "image" {
		t.Errorf("input modalities not backfilled:\n%s", data)
	}
	compat, _ := p["compat"].(map[string]any)
	if compat == nil || compat["supportsDeveloperRole"] != false {
		t.Errorf("compat not backfilled:\n%s", data)
	}
	if !strings.Contains(buf.String(), "model capabilities added") {
		t.Errorf("expected upgrade notice:\n%s", buf.String())
	}
}

func TestPiCompleteEntryUntouched(t *testing.T) {
	home := t.TempDir()
	// user-customized values must survive a re-run
	existing := `{"providers": {"2ba": {"baseUrl": "` + testBase + `", "api": "openai-completions", "apiKey": "k", "compat": {"supportsDeveloperRole": false}, "models": [{"id": "amber", "name": "Amber (2ba.ai)", "reasoning": false, "input": ["text"], "contextWindow": 128000}]}}}`
	mustWrite(t, piModels(home), existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigurePi(env)

	if got, _ := os.ReadFile(piModels(home)); string(got) != existing {
		t.Errorf("complete entry was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("expected leave-as-is notice:\n%s", buf.String())
	}
}

func TestPiNoAgentDir(t *testing.T) {
	home := t.TempDir()
	// Keep pi off PATH so the "not detected" branch is taken even on a
	// machine that has pi installed.
	t.Setenv("PATH", t.TempDir())
	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigurePi(env)
	if !strings.Contains(buf.String(), "Pi not detected") || !strings.Contains(buf.String(), "install it, run it once") {
		t.Errorf("expected not-detected warning:\n%s", buf.String())
	}
}

func TestPiMalformedJSON(t *testing.T) {
	// A bare "null" is valid JSON that unmarshals into a nil map — it must
	// be rejected like corrupt JSON, not rewritten into an object.
	for _, broken := range []string{"{not json", "null"} {
		t.Run(broken, func(t *testing.T) {
			home := t.TempDir()
			mustWrite(t, piModels(home), broken)
			env, buf := newEnv(t, home, "amber", "k", false)

			ConfigurePi(env)

			if got, _ := os.ReadFile(piModels(home)); string(got) != broken {
				t.Errorf("malformed catalog was rewritten:\n%s", got)
			}
			if !strings.Contains(buf.String(), "not valid JSON") {
				t.Errorf("expected malformed-JSON warning:\n%s", buf.String())
			}
		})
	}
}

func TestPiProvidersNotAnObject(t *testing.T) {
	// a user value on "providers" with the wrong shape must survive, not be
	// replaced by the installer's map
	existing := `{"providers": []}`
	home := t.TempDir()
	mustWrite(t, piModels(home), existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigurePi(env)

	if got, _ := os.ReadFile(piModels(home)); string(got) != existing {
		t.Errorf("non-object providers field was replaced:\n%s", got)
	}
	if !strings.Contains(buf.String(), "not an object") {
		t.Errorf("expected shape warning:\n%s", buf.String())
	}
}

func TestPiDryRun(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".pi", "agent"))
	env, buf := newEnv(t, home, "amber", "k", true)
	ConfigurePi(env)
	if !strings.Contains(buf.String(), "would add 2ba provider to") {
		t.Errorf("dry-run plan missing pi entry:\n%s", buf.String())
	}
	if _, err := os.Stat(piModels(home)); !os.IsNotExist(err) {
		t.Errorf("dry run created the models file")
	}
}

func TestPiDryRunExisting(t *testing.T) {
	home := t.TempDir()
	existing := `{"providers": {"2ba": {"baseUrl": "` + testBase + `", "api": "openai-completions", "apiKey": "k", "compat": {"supportsDeveloperRole": false}, "models": [{"id": "amber", "name": "Amber (2ba.ai)", "reasoning": true, "input": ["text", "image"], "contextWindow": 262144}]}}}`
	mustWrite(t, piModels(home), existing)
	env, buf := newEnv(t, home, "amber", "k", true)
	ConfigurePi(env)
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("dry run must match the real path (leave existing provider as-is):\n%s", buf.String())
	}
	if got, _ := os.ReadFile(piModels(home)); string(got) != existing {
		t.Errorf("dry run modified the catalog:\n%s", got)
	}
}

// ---------------------------------------------------------------------- uninstall

func TestUninstallShellBlock(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	mustWrite(t, rc, "# rc\n"+BlockBegin+"\nexport OPENAI_API_BASE=\"x\"\n"+BlockEnd+"\n")
	env, buf := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	got, _ := os.ReadFile(rc)
	if strings.Contains(string(got), "2ba.ai (managed)") {
		t.Errorf("managed block not stripped:\n%s", got)
	}
	if strings.TrimSpace(string(got)) != "# rc" {
		t.Errorf("unexpected rc content after uninstall:\n%s", got)
	}
	if !strings.Contains(buf.String(), "stripped env block") {
		t.Errorf("expected strip notice:\n%s", buf.String())
	}
}

func TestUninstallHalfMarkerPreserved(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	mustWrite(t, rc, "# rc\n"+BlockBegin+"\nexport OPENAI_API_KEY=\"leak\"\n# user stuff below\n")
	env, buf := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	if got, _ := os.ReadFile(rc); !strings.Contains(string(got), "# user stuff below") {
		t.Errorf("rc was truncated on half-marked file:\n%s", got)
	}
	if !strings.Contains(buf.String(), "no end marker") {
		t.Errorf("expected half-marker warning:\n%s", buf.String())
	}
}

func TestUninstallOpencode(t *testing.T) {
	home := t.TempDir()
	ocPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	mustWrite(t, ocPath, `{"provider": {"2ba": {"name": "2ba.ai"}, "mine": {"name": "keep"}}, "model": "2ba/amber"}`)
	env, buf := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	var cfg map[string]any
	oc, _ := os.ReadFile(ocPath)
	if err := json.Unmarshal(oc, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	providers := cfg["provider"].(map[string]any)
	if _, present := providers["2ba"]; present {
		t.Errorf("2ba provider not removed:\n%s", oc)
	}
	if _, present := providers["mine"]; !present {
		t.Errorf("user provider was removed:\n%s", oc)
	}
	if _, present := cfg["model"]; present {
		t.Errorf("default model not removed:\n%s", oc)
	}
	if !strings.Contains(buf.String(), "removed 2ba entry") {
		t.Errorf("expected removal notice:\n%s", buf.String())
	}
}

func TestUninstallZcode(t *testing.T) {
	home := t.TempDir()
	zcfg := filepath.Join(home, ".zcode", "v2", "config.json")
	mustWrite(t, zcfg, `{"provider": {"2ba": {"name": "2ba.ai"}, "builtin:zai": {"name": "keep"}}}`)
	env, buf := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	var cfg map[string]any
	zc, _ := os.ReadFile(zcfg)
	if err := json.Unmarshal(zc, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	providers := cfg["provider"].(map[string]any)
	if _, present := providers["2ba"]; present {
		t.Errorf("2ba provider not removed:\n%s", zc)
	}
	if _, present := providers["builtin:zai"]; !present {
		t.Errorf("user provider was removed:\n%s", zc)
	}
	if !strings.Contains(buf.String(), "removed 2ba entry") {
		t.Errorf("expected removal notice:\n%s", buf.String())
	}
}

func TestUninstallPi(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, piModels(home), `{"providers": {"2ba": {"baseUrl": "`+testBase+`", "api": "openai-completions", "apiKey": "tuba-sk-old"}, "ollama": {"baseUrl": "http://localhost:11434/v1"}}}`)
	env, buf := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	var cfg map[string]any
	data, _ := os.ReadFile(piModels(home))
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	providers := cfg["providers"].(map[string]any)
	if _, present := providers["2ba"]; present {
		t.Errorf("2ba provider not removed:\n%s", data)
	}
	if _, present := providers["ollama"]; !present {
		t.Errorf("user provider was removed:\n%s", data)
	}
	if !strings.Contains(buf.String(), "removed 2ba entry") {
		t.Errorf("expected removal notice:\n%s", buf.String())
	}
}

// Uninstall must leave agent JSON catalogs that hold no 2ba entry
// byte-identical and without a backup.
func TestUninstallNoRewriteWithout2ba(t *testing.T) {
	home := t.TempDir()
	piSeed := `{"providers": {"ollama": {"baseUrl": "http://localhost:11434/v1"}}}`
	ocSeed := `{"provider": {"mine": {"name": "keep"}}}`
	mustWrite(t, piModels(home), piSeed)
	mustWrite(t, filepath.Join(home, ".config", "opencode", "opencode.json"), ocSeed)

	env, _ := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	if got, _ := os.ReadFile(piModels(home)); string(got) != piSeed {
		t.Errorf("unrelated pi catalog was rewritten:\n%s", got)
	}
	if got, _ := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json")); string(got) != ocSeed {
		t.Errorf("unrelated opencode config was rewritten:\n%s", got)
	}
	for _, f := range []string{piModels(home), filepath.Join(home, ".config", "opencode", "opencode.json")} {
		if _, err := os.Stat(f + ".bak.2ba"); err == nil {
			t.Errorf("uninstall without a 2ba entry left a backup: %s.bak.2ba", f)
		}
	}
}

func TestUninstallTwocodeKeepsOther(t *testing.T) {
	home := t.TempDir()
	ours := `{"providerId":"custom-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","label":"2ba","apiFormat":"openai-chat-completions","baseURL":"` + testBase + `","apiKey":"tuba-sk-old","models":[{"modelId":"amber"}],"createdAt":1,"updatedAt":1}`
	seed := `{"schemaVersion":2,"providers":[` + twocodeUserProvider + `,` + ours + `]}`
	mustWrite(t, twocodeFile(home), seed)
	env, buf := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	data, err := os.ReadFile(twocodeFile(home))
	if err != nil {
		t.Fatalf("store should survive with the user provider: %v", err)
	}
	if strings.Contains(string(data), "custom-aaaaaaaa") || strings.Contains(string(data), "tuba-sk-old") {
		t.Errorf("2ba provider not removed:\n%s", data)
	}
	if !strings.Contains(string(data), "user-secret") {
		t.Errorf("user provider was removed:\n%s", data)
	}
	if !strings.Contains(buf.String(), "removed 2ba entry") {
		t.Errorf("expected removal notice:\n%s", buf.String())
	}
}

func TestUninstallTwocodeDeletesEmptyStore(t *testing.T) {
	home := t.TempDir()
	ours := `{"providerId":"custom-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","label":"2ba","apiFormat":"openai-chat-completions","baseURL":"` + testBase + `","apiKey":"tuba-sk-old","models":[{"modelId":"amber"}],"createdAt":1,"updatedAt":1}`
	mustWrite(t, twocodeFile(home), `{"schemaVersion":2,"providers":[`+ours+`]}`)
	env, _ := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	if _, err := os.Stat(twocodeFile(home)); !os.IsNotExist(err) {
		t.Errorf("store with only the 2ba provider should be deleted")
	}
}

func TestUninstallTwocodeNoMatch(t *testing.T) {
	home := t.TempDir()
	seed := `{"schemaVersion":2,"providers":[` + twocodeUserProvider + `]}`
	mustWrite(t, twocodeFile(home), seed)
	env, _ := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	if got, _ := os.ReadFile(twocodeFile(home)); string(got) != seed {
		t.Errorf("unrelated store was rewritten:\n%s", got)
	}
}

func TestUninstallClaude(t *testing.T) {
	home := t.TempDir()
	seed := `{"theme":"dark","env":{"ANTHROPIC_BASE_URL":"https://api.2ba.ai","ANTHROPIC_AUTH_TOKEN":"tuba-sk-old","ANTHROPIC_MODEL":"amber","ANTHROPIC_SMALL_FAST_MODEL":"amber"}}`
	mustWrite(t, claudeSettings(home), seed)
	env, buf := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	data, _ := os.ReadFile(claudeSettings(home))
	var cfg struct {
		Theme string            `json:"theme"`
		Env   map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	if cfg.Theme != "dark" {
		t.Errorf("user settings lost:\n%s", data)
	}
	if cfg.Env != nil {
		t.Errorf("empty env block should be dropped:\n%s", data)
	}
	for _, k := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL", "ANTHROPIC_SMALL_FAST_MODEL"} {
		if strings.Contains(string(data), k) {
			t.Errorf("%s not removed:\n%s", k, data)
		}
	}
	if !strings.Contains(buf.String(), "removed 2ba configuration") {
		t.Errorf("expected removal notice:\n%s", buf.String())
	}
}

func TestUninstallClaudeKeepsUserKey(t *testing.T) {
	home := t.TempDir()
	seed := `{"env":{"ANTHROPIC_BASE_URL":"https://api.2ba.ai","ANTHROPIC_AUTH_TOKEN":"tuba-sk-old","ANTHROPIC_API_KEY":"user-real-key"}}`
	mustWrite(t, claudeSettings(home), seed)
	env, _ := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	if got, _ := os.ReadFile(claudeSettings(home)); !strings.Contains(string(got), `"ANTHROPIC_API_KEY": "user-real-key"`) {
		t.Errorf("user's ANTHROPIC_API_KEY was removed:\n%s", got)
	}
}

func TestUninstallClaudeNoMatch(t *testing.T) {
	home := t.TempDir()
	seed := `{"env":{"ANTHROPIC_BASE_URL":"https://proxy.example.com","ANTHROPIC_API_KEY":"k"}}`
	mustWrite(t, claudeSettings(home), seed)
	env, _ := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	if got, _ := os.ReadFile(claudeSettings(home)); string(got) != seed {
		t.Errorf("unrelated settings were rewritten:\n%s", got)
	}
}

func TestUninstallKimiBlock(t *testing.T) {
	home := t.TempDir()
	cfgPath := filepath.Join(home, ".kimi-code", "config.toml")
	mustWrite(t, cfgPath, "default_model = \"kimi-code/k3\"\n"+BlockBegin+"\n[providers.2ba]\ntype = \"openai\"\n"+BlockEnd+"\n")
	env, buf := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	got, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(got), "2ba.ai (managed)") {
		t.Errorf("managed block not stripped:\n%s", got)
	}
	if !strings.Contains(string(got), `default_model = "kimi-code/k3"`) {
		t.Errorf("user content lost on uninstall:\n%s", got)
	}
	if !strings.Contains(buf.String(), "removed 2ba block") {
		t.Errorf("expected removal notice:\n%s", buf.String())
	}
}

func TestUninstallDeletesKeyFile(t *testing.T) {
	home := t.TempDir()
	keyFile := filepath.Join(home, ".config", "2ba", "2BA_API_KEY")
	mustWrite(t, keyFile, "tuba-sk-x")
	env, _ := newEnv(t, home, "amber", "k", false)
	Uninstall(env)
	if _, err := os.Stat(keyFile); !os.IsNotExist(err) {
		t.Errorf("key file not deleted")
	}
}

// ---------------------------------------------------------------------- openclaw

func openclawConfig(home string) string {
	return filepath.Join(home, ".openclaw", "openclaw.json")
}

func TestOpenclawAddCreatesFile(t *testing.T) {
	home := t.TempDir()
	stateDir := filepath.Join(home, ".openclaw")
	mustMkdir(t, stateDir)
	env, buf := newEnv(t, home, "amber", "tuba-sk-openclaw-key", false)

	ConfigureOpenclaw(env)

	data, err := os.ReadFile(openclawConfig(home))
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Models struct {
			Providers map[string]struct {
				BaseURL        string `json:"baseUrl"`
				APIKey         string `json:"apiKey"`
				API            string `json:"api"`
				TimeoutSeconds int    `json:"timeoutSeconds"`
				Models         []struct {
					ID            string   `json:"id"`
					Name          string   `json:"name"`
					Reasoning     bool     `json:"reasoning"`
					Input         []string `json:"input"`
					ContextWindow int      `json:"contextWindow"`
					MaxTokens     int      `json:"maxTokens"`
				} `json:"models"`
			} `json:"providers"`
		} `json:"models"`
		Agents struct {
			Defaults struct {
				Model any `json:"model"`
			} `json:"defaults"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	p, ok := root.Models.Providers["2ba"]
	if !ok {
		t.Fatalf("2ba provider missing:\n%s", data)
	}
	if p.BaseURL != testBase || p.APIKey != "tuba-sk-openclaw-key" || p.API != "openai-completions" || p.TimeoutSeconds != 300 {
		t.Errorf("provider fields wrong:\n%s", data)
	}
	if len(p.Models) != 1 {
		t.Fatalf("want exactly one model, got %d:\n%s", len(p.Models), data)
	}
	m := p.Models[0]
	if m.ID != "amber" || m.Name != "Amber (2ba.ai)" {
		t.Errorf("model identity/name wrong: %+v\n%s", m, data)
	}
	if !m.Reasoning {
		t.Errorf("model must be declared a thinking model:\n%s", data)
	}
	if len(m.Input) != 2 || m.Input[0] != "text" || m.Input[1] != "image" {
		t.Errorf("model must declare image input:\n%s", data)
	}
	if m.ContextWindow != 262144 || m.MaxTokens != 8192 {
		t.Errorf("model limits wrong: %+v\n%s", m, data)
	}
	if got := root.Agents.Defaults.Model; got == nil {
		t.Errorf("agents.defaults.model missing:\n%s", data)
	} else if m, ok := got.(map[string]any); !ok {
		t.Errorf("agents.defaults.model = %v, want object form:\n%s", got, data)
	} else if m["primary"] != "2ba/amber" {
		t.Errorf("agents.defaults.model.primary = %v, want \"2ba/amber\":\n%s", m["primary"], data)
	}
	if st, _ := os.Stat(openclawConfig(home)); st.Mode().Perm() != 0o600 {
		t.Errorf("config perms = %v, want 0600", st.Mode().Perm())
	}
	if !strings.Contains(buf.String(), "OpenClaw: provider") {
		t.Errorf("expected add notice:\n%s", buf.String())
	}
}

// When the state dir does not exist yet, ConfigureOpenclaw creates it 0700
// to match what 2ba-code does for its secrets tree. The test puts a fake
// `openclaw` binary on PATH so the not-detected bail-out is skipped and the
// installer is responsible for creating the directory itself.
func TestOpenclawCreatesStateDir(t *testing.T) {
	home := t.TempDir()
	stateDir := filepath.Join(home, ".openclaw")
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("state dir should not exist yet")
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "openclaw"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	env, _ := newEnv(t, home, "amber", "k", false)
	ConfigureOpenclaw(env)

	if st, err := os.Stat(stateDir); err != nil {
		t.Errorf("state dir not created: %v", err)
	} else if !st.IsDir() {
		t.Errorf("state dir is not a directory")
	} else if st.Mode().Perm() != 0o700 {
		t.Errorf("state dir perms = %v, want 0700", st.Mode().Perm())
	}
}

func TestOpenclawAddKeepsUserProvider(t *testing.T) {
	home := t.TempDir()
	ocPath := openclawConfig(home)
	mustMkdir(t, filepath.Dir(ocPath))
	mustWrite(t, ocPath, `{"models": {"providers": {"mine": {"baseUrl": "http://example", "apiKey": "k", "api": "openai-completions", "models": [{"id": "x"}]}}}}`)
	env, _ := newEnv(t, home, "amber", "k", false)

	ConfigureOpenclaw(env)

	var cfg map[string]any
	data, _ := os.ReadFile(ocPath)
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	providers := cfg["models"].(map[string]any)["providers"].(map[string]any)
	if _, present := providers["mine"]; !present {
		t.Errorf("user provider lost:\n%s", data)
	}
	if _, present := providers["2ba"]; !present {
		t.Errorf("2ba provider missing:\n%s", data)
	}
}

func TestOpenclawExisting2baUntouched(t *testing.T) {
	home := t.TempDir()
	ocPath := openclawConfig(home)
	mustMkdir(t, filepath.Dir(ocPath))
	// a "2ba" provider without our model id is user-managed
	existing := `{"models": {"providers": {"2ba": {"baseUrl": "http://example", "apiKey": "user-secret", "api": "openai-completions", "models": [{"id": "other"}]}}}}`
	mustWrite(t, ocPath, existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureOpenclaw(env)

	if got, _ := os.ReadFile(ocPath); string(got) != existing {
		t.Errorf("existing user-managed 2ba provider was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("expected leave-as-is notice:\n%s", buf.String())
	}
}

func TestOpenclawUpgradesOldEntry(t *testing.T) {
	home := t.TempDir()
	ocPath := openclawConfig(home)
	mustMkdir(t, filepath.Dir(ocPath))
	// exactly what a pre-vision installer would write: our model, no input/cost/etc.
	existing := `{"models":{"providers":{"2ba":{"baseUrl":"` + testBase + `","apiKey":"user-rotated-key","api":"openai-completions","timeoutSeconds":300,"models":[{"id":"amber","name":"Amber (2ba.ai)"}]}}}}`
	mustWrite(t, ocPath, existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureOpenclaw(env)

	data, _ := os.ReadFile(ocPath)
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	p := cfg["models"].(map[string]any)["providers"].(map[string]any)["2ba"].(map[string]any)
	if p["apiKey"] != "user-rotated-key" {
		t.Errorf("user apiKey modified:\n%s", data)
	}
	m := p["models"].([]any)[0].(map[string]any)
	if m["reasoning"] != true {
		t.Errorf("reasoning not backfilled:\n%s", data)
	}
	if input, _ := m["input"].([]any); len(input) != 2 || input[0] != "text" || input[1] != "image" {
		t.Errorf("input modalities not backfilled:\n%s", data)
	}
	if m["contextWindow"] != float64(262144) || m["maxTokens"] != float64(8192) {
		t.Errorf("limits not backfilled: %+v\n%s", m, data)
	}
	if _, has := m["cost"]; !has {
		t.Errorf("cost block not backfilled:\n%s", data)
	}
	// The installer also fills a missing default-model slot.
	dm, _ := cfg["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)
	if dm == nil || dm["primary"] != "2ba/amber" {
		t.Errorf("agents.defaults.model not backfilled to object form:\n%s", data)
	}
	if !strings.Contains(buf.String(), "model capabilities") {
		t.Errorf("expected upgrade notice:\n%s", buf.String())
	}
}

func TestOpenclawCompleteEntryUntouched(t *testing.T) {
	home := t.TempDir()
	ocPath := openclawConfig(home)
	mustMkdir(t, filepath.Dir(ocPath))
	// user-customized values must survive a re-run: false reasoning and
	// single-element input are the user's choice
	seedSeed := map[string]any{
		"models": map[string]any{
			"providers": map[string]any{
				"2ba": map[string]any{
					"baseUrl": testBase, "apiKey": "k", "api": "openai-completions",
					"timeoutSeconds": 300,
					"models": []any{
						map[string]any{
							"id": "amber", "name": "Amber (2ba.ai)",
							"reasoning": false, "input": []string{"text"},
							"contextWindow": 128000, "maxTokens": 4096,
							"cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
						},
					},
				},
			},
		},
		"agents": map[string]any{
			"defaults": map[string]any{"model": map[string]any{"primary": "2ba/amber"}},
		},
	}
	existingBytes, _ := json.MarshalIndent(seedSeed, "", "  ")
	existing := string(existingBytes)
	mustWrite(t, ocPath, existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureOpenclaw(env)

	if got, _ := os.ReadFile(ocPath); string(got) != existing {
		t.Errorf("complete entry was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("expected leave-as-is notice:\n%s", buf.String())
	}
}

func TestOpenclawNoStateDir(t *testing.T) {
	home := t.TempDir()
	// Keep openclaw off PATH so the "not detected" branch is taken even on a
	// machine that has openclaw installed.
	t.Setenv("PATH", t.TempDir())
	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigureOpenclaw(env)
	if !strings.Contains(buf.String(), "OpenClaw not detected") || !strings.Contains(buf.String(), "install it, run it once") {
		t.Errorf("expected not-detected warning:\n%s", buf.String())
	}
}

func TestOpenclawMalformedJSON(t *testing.T) {
	home := t.TempDir()
	ocPath := openclawConfig(home)
	mustMkdir(t, filepath.Dir(ocPath))
	broken := "{not json"
	mustWrite(t, ocPath, broken)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureOpenclaw(env)

	if got, _ := os.ReadFile(ocPath); string(got) != broken {
		t.Errorf("malformed config was rewritten:\n%s", got)
	}
	if !strings.Contains(buf.String(), "not valid JSON") {
		t.Errorf("expected malformed-JSON warning:\n%s", buf.String())
	}
}

func TestOpenclawDryRun(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".openclaw"))
	env, buf := newEnv(t, home, "amber", "k", true)
	ConfigureOpenclaw(env)
	if !strings.Contains(buf.String(), "would add provider \"2ba\" to") {
		t.Errorf("dry-run plan missing openclaw entry:\n%s", buf.String())
	}
	if _, err := os.Stat(openclawConfig(home)); !os.IsNotExist(err) {
		t.Errorf("dry run created the openclaw config")
	}
}

func TestOpenclawDryRunExisting(t *testing.T) {
	home := t.TempDir()
	ocPath := openclawConfig(home)
	mustMkdir(t, filepath.Dir(ocPath))
	seedSeed := map[string]any{
		"models": map[string]any{
			"providers": map[string]any{
				"2ba": map[string]any{
					"baseUrl": testBase, "apiKey": "k", "api": "openai-completions",
					"timeoutSeconds": 300,
					"models": []any{
						map[string]any{
							"id": "amber", "name": "Amber (2ba.ai)", "reasoning": true,
							"input":         []string{"text", "image"},
							"contextWindow": 262144, "maxTokens": 8192,
							"cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
						},
					},
				},
			},
		},
		"agents": map[string]any{
			"defaults": map[string]any{"model": map[string]any{"primary": "2ba/amber"}},
		},
	}
	existingBytes, _ := json.MarshalIndent(seedSeed, "", "  ")
	existing := string(existingBytes)
	mustWrite(t, ocPath, existing)
	env, buf := newEnv(t, home, "amber", "k", true)
	ConfigureOpenclaw(env)
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("dry run must match the real path (leave existing provider as-is):\n%s", buf.String())
	}
	if got, _ := os.ReadFile(ocPath); string(got) != existing {
		t.Errorf("dry run modified the openclaw config:\n%s", got)
	}
}

func TestUninstallOpenclaw(t *testing.T) {
	home := t.TempDir()
	ocPath := openclawConfig(home)
	mustMkdir(t, filepath.Dir(ocPath))
	// Build the seed from a Go value so the JSON is always balanced.
	seedSeed := map[string]any{
		"models": map[string]any{
			"providers": map[string]any{
				"2ba": map[string]any{
					"baseUrl":        testBase,
					"apiKey":         "tuba-sk-old",
					"api":            "openai-completions",
					"timeoutSeconds": 300,
					"models": []any{
						map[string]any{
							"id": "amber", "name": "Amber (2ba.ai)",
							"reasoning": true, "input": []string{"text", "image"},
							"contextWindow": 262144, "maxTokens": 8192,
							"cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
						},
					},
				},
				"mine": map[string]any{
					"baseUrl": "http://example", "apiKey": "k", "api": "openai-completions",
					"models": []any{map[string]any{"id": "x"}},
				},
			},
		},
		"agents": map[string]any{
			"defaults": map[string]any{"model": map[string]any{"primary": "2ba/amber"}},
		},
	}
	seedBytes, _ := json.MarshalIndent(seedSeed, "", "  ")
	mustWrite(t, ocPath, string(seedBytes))
	env, buf := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	data, _ := os.ReadFile(ocPath)
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	providers := cfg["models"].(map[string]any)["providers"].(map[string]any)
	if _, present := providers["2ba"]; present {
		t.Errorf("2ba provider not removed:\n%s", data)
	}
	if _, present := providers["mine"]; !present {
		t.Errorf("user provider was removed:\n%s", data)
	}
	if defaults, _ := cfg["agents"].(map[string]any)["defaults"].(map[string]any); defaults != nil {
		if m, _ := defaults["model"].(string); m != "" {
			t.Errorf("agents.defaults.model not removed (still %q):\n%s", m, data)
		}
	}
	if !strings.Contains(buf.String(), "removed 2ba entry") {
		t.Errorf("expected removal notice:\n%s", buf.String())
	}
}

func TestUninstallOpenclawNoMatch(t *testing.T) {
	home := t.TempDir()
	ocPath := openclawConfig(home)
	mustMkdir(t, filepath.Dir(ocPath))
	seed := `{"models": {"providers": {"mine": {"baseUrl": "http://example", "apiKey": "k", "api": "openai-completions", "models": [{"id": "x"}]}}}}`
	mustWrite(t, ocPath, seed)
	env, _ := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	if got, _ := os.ReadFile(ocPath); string(got) != seed {
		t.Errorf("unrelated openclaw config was rewritten:\n%s", got)
	}
	if _, err := os.Stat(ocPath + ".bak.2ba"); err == nil {
		t.Errorf("uninstall without a 2ba entry left a backup")
	}
}

// A "2ba" provider that the installer wrote can also carry models the user
// added under the same provider key. Uninstall must remove only the
// installer-owned model and leave the provider (and the user's sibling
// model) in place — the provider is only dropped when nothing remains.
func TestUninstallOpenclawKeepsSiblingModel(t *testing.T) {
	home := t.TempDir()
	ocPath := openclawConfig(home)
	mustMkdir(t, filepath.Dir(ocPath))
	seed := `{"models":{"providers":{"2ba":{"baseUrl":"` + testBase + `","apiKey":"k","api":"openai-completions","timeoutSeconds":300,"models":[{"id":"amber","name":"Amber (2ba.ai)"},{"id":"user-extra","name":"User Extra"}]}}}}`
	mustWrite(t, ocPath, seed)
	env, _ := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	data, _ := os.ReadFile(ocPath)
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	p, ok := cfg["models"].(map[string]any)["providers"].(map[string]any)["2ba"].(map[string]any)
	if !ok {
		t.Fatalf("2ba provider dropped despite a surviving model:\n%s", data)
	}
	entries, _ := p["models"].([]any)
	if len(entries) != 1 {
		t.Fatalf("want 1 model remaining, got %d:\n%s", len(entries), data)
	}
	if entries[0].(map[string]any)["id"] != "user-extra" {
		t.Errorf("wrong model kept; want user-extra, got %v:\n%s", entries[0], data)
	}
}

// A user's own default-model value is left alone — uninstall only removes
// the object form the installer writes ({primary: "2ba/<model>"}). The 2ba
// provider is removed because its only model was installer-owned.
func TestUninstallOpenclawLeavesUserDefault(t *testing.T) {
	home := t.TempDir()
	ocPath := openclawConfig(home)
	mustMkdir(t, filepath.Dir(ocPath))
	seedSeed := map[string]any{
		"models": map[string]any{
			"providers": map[string]any{
				"2ba": map[string]any{
					"baseUrl": testBase, "apiKey": "k", "api": "openai-completions",
					"timeoutSeconds": 300,
					"models": []any{
						map[string]any{"id": "amber", "name": "Amber (2ba.ai)"},
					},
				},
			},
		},
		"agents": map[string]any{
			"defaults": map[string]any{"model": map[string]any{"primary": "some-other-model"}},
		},
	}
	seedBytes, _ := json.MarshalIndent(seedSeed, "", "  ")
	mustWrite(t, ocPath, string(seedBytes))
	env, _ := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	data, _ := os.ReadFile(ocPath)
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	// The installer-owned 2ba provider is dropped: it had no other models.
	if _, present := cfg["models"].(map[string]any)["providers"].(map[string]any)["2ba"]; present {
		t.Errorf("2ba provider should have been dropped:\n%s", data)
	}
	defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
	if defaults["model"].(map[string]any)["primary"] != "some-other-model" {
		t.Errorf("user's default model was changed: %v:\n%s", defaults["model"], data)
	}
}

// A legacy installer's string-form default gets upgraded to the documented
// object form so the uninstall path can recognise it. The shape is the
// installer's own previous default ("2ba/<model>"), not a user value.
func TestOpenclawUpgradesLegacyStringDefault(t *testing.T) {
	home := t.TempDir()
	ocPath := openclawConfig(home)
	mustMkdir(t, filepath.Dir(ocPath))
	seedSeed := map[string]any{
		"models": map[string]any{
			"providers": map[string]any{
				"2ba": map[string]any{
					"baseUrl": testBase, "apiKey": "k", "api": "openai-completions",
					"timeoutSeconds": 300,
					"models": []any{
						map[string]any{
							"id": "amber", "name": "Amber (2ba.ai)", "reasoning": true,
							"input":         []string{"text", "image"},
							"contextWindow": 262144, "maxTokens": 8192,
							"cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
						},
					},
				},
			},
		},
		"agents": map[string]any{
			"defaults": map[string]any{"model": "2ba/amber"},
		},
	}
	existingBytes, _ := json.MarshalIndent(seedSeed, "", "  ")
	mustWrite(t, ocPath, string(existingBytes))
	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigureOpenclaw(env)

	data, _ := os.ReadFile(ocPath)
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
	m, ok := defaults["model"].(map[string]any)
	if !ok {
		t.Fatalf("agents.defaults.model not upgraded to object form: %v\n%s", defaults["model"], data)
	}
	if m["primary"] != "2ba/amber" {
		t.Errorf("agents.defaults.model.primary = %v, want \"2ba/amber\":\n%s", m["primary"], data)
	}
	if !strings.Contains(buf.String(), "default model added") {
		t.Errorf("expected default-model-added notice:\n%s", buf.String())
	}
}

// A "2ba" provider that is user-managed (no installer-owned model) must not
// be removed just because a sibling provider happens to carry a model with
// the same id — openclawConfigManaged must only consult the "2ba" provider.
func TestUninstallOpenclawIgnoresSiblingModelMatch(t *testing.T) {
	home := t.TempDir()
	ocPath := openclawConfig(home)
	mustMkdir(t, filepath.Dir(ocPath))
	seed := `{"models":{"providers":{"2ba":{"baseUrl":"http://example","apiKey":"user-secret","api":"openai-completions","models":[{"id":"other"}]},"mine":{"baseUrl":"http://example","apiKey":"k","api":"openai-completions","models":[{"id":"amber"}]}}}}`
	mustWrite(t, ocPath, seed)
	env, _ := newEnv(t, home, "amber", "k", false)
	Uninstall(env)

	data, _ := os.ReadFile(ocPath)
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	providers := cfg["models"].(map[string]any)["providers"].(map[string]any)
	if _, present := providers["2ba"]; !present {
		t.Errorf("user-managed 2ba provider was removed:\n%s", data)
	}
	if _, present := providers["mine"]; !present {
		t.Errorf("sibling provider was removed:\n%s", data)
	}
	if _, err := os.Stat(ocPath + ".bak.2ba"); err == nil {
		t.Errorf("uninstall without an installer-owned entry left a backup")
	}
}

// ---------------------------------------------------------------------- hermes

// hermesConfig returns the installer's path to ~/.hermes/config.yaml in
// the test's fake home.
func hermesConfig(home string) string {
	return filepath.Join(home, ".hermes", "config.yaml")
}

// readHermes reads ~/.hermes/config.yaml into a string map (only one level
// deep — sufficient for the assertions below).
func readHermes(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

// On a fresh install with no ~/.hermes/config.yaml the installer creates
// the home dir (0700) and writes the config file (0600) with the
// providers.2ba and model.* seeds.
func TestHermesAddCreatesFile(t *testing.T) {
	home := t.TempDir()
	env, buf := newEnv(t, home, "amber", "tuba-sk-hermes-key", false)

	ConfigureHermes(env)

	cfg := hermesConfig(home)
	if st, _ := os.Stat(cfg); st == nil || st.Mode().Perm() != 0o600 {
		t.Errorf("config perms wrong: %v", st)
	}
	if st, _ := os.Stat(filepath.Join(home, ".hermes")); st == nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Errorf("home perms wrong: %v", st)
	}
	doc := readHermes(t, cfg)
	providers, _ := doc["providers"].(map[string]any)
	p, ok := providers["2ba"].(map[string]any)
	if !ok {
		t.Fatalf("providers.2ba missing:\n%s", buf.String())
	}
	if p["api"] != testBase || p["api_key"] != "tuba-sk-hermes-key" ||
		p["transport"] != "chat_completions" || p["default_model"] != "amber" {
		t.Errorf("providers.2ba fields wrong: %v", p)
	}
	model, _ := doc["model"].(map[string]any)
	if model["default"] != "2ba:amber" || model["provider"] != "2ba" {
		t.Errorf("model block wrong: %v", model)
	}
	if !strings.Contains(buf.String(), "Hermes: provider") {
		t.Errorf("expected add notice:\n%s", buf.String())
	}
}

// A user-owned config.yaml with a sibling provider and a custom model
// default must survive the installer's merge — only the installer's own
// keys are written.
func TestHermesAddPreservesUserKeys(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	userCfg := "providers:\n  openrouter:\n    api: https://openrouter.ai/api/v1\n    api_key: user-secret\n    transport: chat_completions\nmodel:\n  provider: openrouter\n  default: openrouter:llama3\n"
	mustWrite(t, hermesConfig(home), userCfg)
	env, _ := newEnv(t, home, "amber", "k", false)

	ConfigureHermes(env)

	doc := readHermes(t, hermesConfig(home))
	providers, _ := doc["providers"].(map[string]any)
	if _, present := providers["openrouter"]; !present {
		t.Errorf("user provider lost:\n%v", providers)
	}
	if _, present := providers["2ba"]; !present {
		t.Errorf("2ba provider missing:\n%v", providers)
	}
	model, _ := doc["model"].(map[string]any)
	if model["default"] != "openrouter:llama3" {
		t.Errorf("user model.default was overwritten: %v", model)
	}
	if model["provider"] != "openrouter" {
		t.Errorf("user model.provider was overwritten: %v", model)
	}
}

// A user-owned "2ba" provider (no installer-written marker) must be
// left alone on a fresh run.
func TestHermesUserOwned2baUntouched(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	existing := "providers:\n  2ba:\n    api: http://example\n    api_key: user-secret\n    transport: chat_completions\n    default_model: other\n"
	mustWrite(t, hermesConfig(home), existing)
	env, _ := newEnv(t, home, "amber", "k", false)

	ConfigureHermes(env)

	if got, _ := os.ReadFile(hermesConfig(home)); string(got) != existing {
		t.Errorf("user-managed 2ba provider was modified:\n%s", got)
	}
}

// An earlier-version installer wrote a 2ba provider (and stamped the
// marker), but the entry is missing `default_model`. On a re-run the
// installer backfills the missing field and leaves the user's
// api_key rotation intact.
func TestHermesUpgradesOldEntry(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	existing := "providers:\n  2ba:\n    api: " + testBase + "\n    api_key: user-rotated-key\n    transport: chat_completions\n__2ba:\n  version: \"1\"\n"
	mustWrite(t, hermesConfig(home), existing)
	env, buf := newEnv(t, home, "amber", "user-rotated-key", false)

	ConfigureHermes(env)

	doc := readHermes(t, hermesConfig(home))
	p := doc["providers"].(map[string]any)["2ba"].(map[string]any)
	if p["api_key"] != "user-rotated-key" {
		t.Errorf("user api_key overwritten: %v", p)
	}
	if p["default_model"] != "amber" {
		t.Errorf("default_model not backfilled: %v", p)
	}
	if !strings.Contains(buf.String(), "Hermes: provider") {
		t.Errorf("expected add notice:\n%s", buf.String())
	}
}

// --reauth rotates the api_key: a re-run with a new key updates the
// stored api_key on the installer-managed entry (matched by the
// __2ba marker, not by api_key comparison).
func TestHermesReauthRotatesKey(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	existing := "providers:\n  2ba:\n    api: " + testBase + "\n    api_key: stale-key\n    transport: chat_completions\n    default_model: amber\n__2ba:\n  version: \"1\"\nmodel:\n  default: 2ba:amber\n  provider: 2ba\n"
	mustWrite(t, hermesConfig(home), existing)
	env, _ := newEnv(t, home, "amber", "fresh-key", false)
	ConfigureHermes(env)

	doc := readHermes(t, hermesConfig(home))
	p := doc["providers"].(map[string]any)["2ba"].(map[string]any)
	if p["api_key"] != "fresh-key" {
		t.Errorf("api_key was not rotated: %v", p)
	}
	if p["default_model"] != "amber" {
		t.Errorf("default_model changed: %v", p)
	}
}

// --model change rotates default_model in place on a re-run; the
// installer-managed entry is matched by the marker, so api_key is
// updated too (in this test the api_key happens to be unchanged).
func TestHermesModelChangeUpdatesEntry(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	existing := "providers:\n  2ba:\n    api: " + testBase + "\n    api_key: k\n    transport: chat_completions\n    default_model: old-model\n__2ba:\n  version: \"1\"\nmodel:\n  default: 2ba:old-model\n  provider: 2ba\n"
	mustWrite(t, hermesConfig(home), existing)
	env, _ := newEnv(t, home, "amber", "k", false)
	ConfigureHermes(env)

	doc := readHermes(t, hermesConfig(home))
	p := doc["providers"].(map[string]any)["2ba"].(map[string]any)
	if p["default_model"] != "amber" {
		t.Errorf("default_model was not updated: %v", p)
	}
	model := doc["model"].(map[string]any)
	if model["default"] != "2ba:amber" {
		t.Errorf("model.default was not updated: %v", model)
	}
}

// A complete installer-owned entry (api_key, default_model, marker)
// with a model.default that already matches the installer — nothing
// to change. Reports "already configured" and writes no backup.
func TestHermesCompleteEntryUntouchedWithMarker(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	existing := "providers:\n  2ba:\n    api: " + testBase + "\n    api_key: k\n    transport: chat_completions\n    default_model: amber\n__2ba:\n  version: \"1\"\nmodel:\n  default: 2ba:amber\n  provider: 2ba\n"
	mustWrite(t, hermesConfig(home), existing)
	env, buf := newEnv(t, home, "amber", "k", false)

	ConfigureHermes(env)

	if got, _ := os.ReadFile(hermesConfig(home)); string(got) != existing {
		t.Errorf("complete entry was modified:\n%s", got)
	}
	if !strings.Contains(buf.String(), "already configured") {
		t.Errorf("expected leave-as-is notice:\n%s", buf.String())
	}
	if _, err := os.Stat(hermesConfig(home) + ".bak.2ba"); err == nil {
		t.Errorf("no-op run left a backup")
	}
}

// A complete installer-owned entry but model.default is missing —
// the provider merge is a no-op, but the model block is filled in
// (Copilot review pointed out this case short-circuited in an
// earlier draft).
func TestHermesCompleteProviderFillsMissingDefault(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	existing := "providers:\n  2ba:\n    api: " + testBase + "\n    api_key: k\n    transport: chat_completions\n    default_model: amber\n__2ba:\n  version: \"1\"\nmodel:\n  provider: 2ba\n"
	mustWrite(t, hermesConfig(home), existing)
	env, _ := newEnv(t, home, "amber", "k", false)

	ConfigureHermes(env)

	doc := readHermes(t, hermesConfig(home))
	model := doc["model"].(map[string]any)
	if model["default"] != "2ba:amber" {
		t.Errorf("model.default not backfilled: %v", model)
	}
}

// Dry-run does not touch the file (and leaves no backup).
func TestHermesDryRun(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	env, buf := newEnv(t, home, "amber", "k", true)
	ConfigureHermes(env)
	if !strings.Contains(buf.String(), "would add or update provider") {
		t.Errorf("dry-run plan missing hermes entry:\n%s", buf.String())
	}
	if _, err := os.Stat(hermesConfig(home)); !os.IsNotExist(err) {
		t.Errorf("dry run created the hermes config")
	}
}

// A corrupt YAML file is left untouched.
func TestHermesMalformedYAML(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	mustWrite(t, hermesConfig(home), "providers:\n  2ba:\n    api: : :\n  this is : not valid: yaml")
	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigureHermes(env)
	if !strings.Contains(buf.String(), "not valid YAML") {
		t.Errorf("expected malformed-YAML warning:\n%s", buf.String())
	}
}

// When neither the home dir nor `hermes` is on PATH, ConfigureHermes is
// a no-op (same detection rule as ConfigurePi / ConfigureClaude).
func TestHermesNotDetected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	env, buf := newEnv(t, home, "amber", "k", false)
	ConfigureHermes(env)
	if !strings.Contains(buf.String(), "Hermes not detected") {
		t.Errorf("expected not-detected warning:\n%s", buf.String())
	}
}

// Refuses to write with an empty model or API key.
func TestHermesRefusesEmpty(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	t.Run("empty model", func(t *testing.T) {
		env, buf := newEnv(t, home, "", "k", false)
		ConfigureHermes(env)
		if !strings.Contains(buf.String(), "empty model") {
			t.Errorf("expected empty-model warning:\n%s", buf.String())
		}
	})
	t.Run("empty key", func(t *testing.T) {
		env, buf := newEnv(t, home, "amber", "", false)
		ConfigureHermes(env)
		if !strings.Contains(buf.String(), "empty") {
			t.Errorf("expected empty-key warning:\n%s", buf.String())
		}
	})
}

// Uninstall removes only our entries; sibling providers and user-set
// model.default survive.
func TestUninstallHermes(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	seed := "providers:\n  2ba:\n    api: " + testBase + "\n    api_key: tuba-sk-old\n    transport: chat_completions\n    default_model: amber\n  openrouter:\n    api: https://openrouter.ai/api/v1\n    api_key: user-secret\n    transport: chat_completions\n__2ba:\n  version: \"1\"\nmodel:\n  provider: openrouter\n  default: openrouter:llama3\n"
	mustWrite(t, hermesConfig(home), seed)
	env, _ := newEnv(t, home, "amber", "tuba-sk-old", false)
	Uninstall(env)

	doc := readHermes(t, hermesConfig(home))
	providers, _ := doc["providers"].(map[string]any)
	if _, present := providers["2ba"]; present {
		t.Errorf("2ba provider not removed:\n%v", providers)
	}
	if _, present := providers["openrouter"]; !present {
		t.Errorf("user provider removed:\n%v", providers)
	}
	model, _ := doc["model"].(map[string]any)
	if model["default"] != "openrouter:llama3" {
		t.Errorf("user model.default changed: %v", model)
	}
	if model["provider"] != "openrouter" {
		t.Errorf("user model.provider changed: %v", model)
	}
}

// Uninstall of a config that has no installer-owned entry leaves the
// file byte-identical and without a backup.
func TestUninstallHermesNoMatch(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	seed := "providers:\n  openrouter:\n    api: https://openrouter.ai/api/v1\n    api_key: user-secret\n    transport: chat_completions\nmodel:\n  provider: openrouter\n  default: openrouter:llama3\n"
	mustWrite(t, hermesConfig(home), seed)
	env, _ := newEnv(t, home, "amber", "k", false)
	Uninstall(env)
	if got, _ := os.ReadFile(hermesConfig(home)); string(got) != seed {
		t.Errorf("unrelated hermes config was rewritten:\n%s", got)
	}
	if _, err := os.Stat(hermesConfig(home) + ".bak.2ba"); err == nil {
		t.Errorf("uninstall without a 2ba entry left a backup")
	}
}

// The CLI uninstall path constructs Env with an empty APIKey
// (cmd/2ba-installer/main.go calls Uninstall before the key is
// loaded). The marker-based ownership predicate means we still find
// and remove the installer's entries.
func TestUninstallHermesWithEmptyAPIKey(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".hermes"))
	seed := "providers:\n  2ba:\n    api: " + testBase + "\n    api_key: stored-key\n    transport: chat_completions\n    default_model: amber\n__2ba:\n  version: \"1\"\nmodel:\n  default: 2ba:amber\n  provider: 2ba\n"
	mustWrite(t, hermesConfig(home), seed)
	env, _ := newEnv(t, home, "amber", "", false)
	Uninstall(env)

	doc := readHermes(t, hermesConfig(home))
	providers, _ := doc["providers"].(map[string]any)
	if _, present := providers["2ba"]; present {
		t.Errorf("2ba provider not removed with empty APIKey:\n%v", providers)
	}
	if _, present := doc["__2ba"]; present {
		t.Errorf("__2ba marker not removed:\n%v", doc["__2ba"])
	}
}
