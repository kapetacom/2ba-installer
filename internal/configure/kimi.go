package configure

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// kimiContextSize mirrors install.sh: 2ba does not advertise a per-model
// context window, so a conservative value only affects when Kimi compacts.
const kimiContextSize = 262144

// kimiModelKey sanitises the model into a bare TOML key (A-Za-z0-9_-),
// replacing every other character with '-' (equivalent to
// `tr -c 'A-Za-z0-9_-' '-'`).
func kimiModelKey(model string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, model)
}

// managedKimiBlock is two self-contained TOML tables. [table] headers at
// column 0 always start a new top-level table, so the block may be appended to
// the end of a file regardless of what precedes it. It never sets
// default_model (a top-level key=value would silently belong to the last
// opened table); the model is picked with `kimi -m` or /model.
func managedKimiBlock(e *Env, providerType string) string {
	var b strings.Builder
	fmt.Fprintln(&b, BlockBegin)
	fmt.Fprintln(&b, "[providers.2ba]")
	fmt.Fprintf(&b, "type = %q\n", providerType)
	fmt.Fprintf(&b, "base_url = %q\n", e.APIBase)
	fmt.Fprintf(&b, "api_key = %q\n", e.APIKey)
	b.WriteString("\n")
	fmt.Fprintf(&b, "[models.2ba-%s]\n", kimiModelKey(e.Model))
	b.WriteString("provider = \"2ba\"\n")
	fmt.Fprintf(&b, "model = %q\n", e.Model)
	fmt.Fprintf(&b, "max_context_size = %d\n", kimiContextSize)
	fmt.Fprintln(&b, BlockEnd)
	return b.String()
}

func (e *Env) configureKimiOne(dir, providerType string) {
	if !dirExists(dir) {
		return
	}
	cfg := filepath.Join(dir, "config.toml")
	legacyJSON := filepath.Join(dir, "config.json")
	if !fileExists(cfg) && fileExists(legacyJSON) {
		// Kimi migrates JSON -> TOML on its next run; appending to a TOML the
		// CLI would then discard is pointless. Let the user run it first.
		e.warnf("Kimi: %s found — run `kimi` once so it migrates to config.toml, then re-run this installer", legacyJSON)
		return
	}
	e.backup(cfg)
	if e.DryRun {
		if fileExists(cfg) && hasExactLine(cfg, BlockBegin) {
			e.logf("would update the 2ba block in %s", cfg)
		} else {
			e.logf("would add 2ba provider to %s", cfg)
		}
		return
	}
	if fileExists(cfg) && hasExactLine(cfg, BlockBegin) {
		if ok, _ := stripManagedBlock(cfg); !ok {
			return
		}
	}
	mk := kimiModelKey(e.Model)
	if fileExists(cfg) && (hasExactLine(cfg, "[providers.2ba]") || hasExactLine(cfg, "[models.2ba-"+mk+"]")) {
		e.logf("existing \"2ba\" entries in %s — leaving it as-is", cfg)
		return
	}
	if err := appendBlock(cfg, managedKimiBlock(e, providerType)); err != nil {
		e.warnf("could not write %s: %v", cfg, err)
		return
	}
	_ = os.Chmod(cfg, 0o600)
	e.logf("Kimi: provider \"2ba\" + model 2ba-%s added to %s", mk, cfg)
	e.hintf("use it with: kimi -m 2ba-%s   (or switch with /model inside Kimi)", mk)
}

// ConfigureKimi configures both Kimi locations: ~/.kimi (original kimi-cli,
// "openai_legacy") and the KIMI_CODE_HOME-aware Kimi Code CLI dir ("openai").
func ConfigureKimi(e *Env) {
	legacy := filepath.Join(home(), ".kimi")
	code := kimiCodeHome()

	targets := 0
	if dirExists(legacy) {
		targets = 1
	}
	if dirExists(code) {
		targets = 1
	}

	e.configureKimiOne(legacy, "openai_legacy")
	e.configureKimiOne(code, "openai")

	if targets == 0 {
		if onPath("kimi") {
			e.warnf("Kimi CLI found but no config dir yet (~/.kimi or ~/.kimi-code) — run `kimi` once so it creates its config, then re-run this installer")
		} else {
			e.warnf("Kimi CLI not detected (no ~/.kimi or ~/.kimi-code) — install it, run it once, then re-run this installer")
		}
	}
}
