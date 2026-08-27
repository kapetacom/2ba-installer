package configure

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
				ModelID       string `json:"modelId"`
				ContextWindow int    `json:"contextWindow"`
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
	ours := `{"providerId":"custom-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","label":"2ba","apiFormat":"openai-chat-completions","baseURL":"` + testBase + `","apiKey":"k","models":[{"modelId":"amber"}],"createdAt":1,"updatedAt":1}`
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
