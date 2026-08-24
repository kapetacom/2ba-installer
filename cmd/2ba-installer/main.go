// Command 2ba-installer pairs a browser session, mints a 2ba API key, and
// configures the local coding tools to use 2ba.ai as their model provider.
//
// It is the Go replacement for the historical static/install.sh and is what
// the thin `curl … | sh` bootstrap downloads and execs. The interactive menu
// opens /dev/tty so it keeps working when the binary is run through a pipe.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/kapetacom/2ba-installer/internal/configure"
	"github.com/kapetacom/2ba-installer/internal/detect"
	"github.com/kapetacom/2ba-installer/internal/menu"
	"github.com/kapetacom/2ba-installer/internal/pairing"
	"github.com/kapetacom/2ba-installer/internal/termx"
)

// version is stamped at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

const (
	siteURL      = "https://2ba.ai"
	defaultModel = "amber"
	defaultBase  = "https://api.2ba.ai/v1"
)

// safeChars is the allowed charset for model names and base URLs: characters
// that cannot break out of the shell rc block or the JSON/TOML they're placed in.
var safeChars = regexp.MustCompile(`^[A-Za-z0-9._~:/-]*$`)

type options struct {
	dryRun    bool
	uninstall bool
	yes       bool
	services  string
	model     string
	apiBase   string
	apiOrigin string
	keyFile   string
	reauth    bool
}

func main() {
	var opts options

	// Pull out -h/--help and --version up front, like the old script did.
	args := os.Args[1:]
	rest := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "-h", "--help":
			usage()
			return
		case "--version":
			fmt.Println("2ba-installer", version)
			return
		default:
			rest = append(rest, a)
		}
	}

	fs := flag.NewFlagSet("2ba-installer", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = usage
	fs.BoolVar(&opts.dryRun, "dry-run", false, "print the plan without touching anything")
	fs.BoolVar(&opts.uninstall, "uninstall", false, "remove everything the installer manages")
	fs.BoolVar(&opts.yes, "yes", false, "non-interactive: accept all defaults")
	fs.StringVar(&opts.services, "services", "", "services to configure: shell,opencode,windsurf,kimi,continue,cursor")
	fs.StringVar(&opts.model, "model", defaultModel, "model to configure")
	fs.StringVar(&opts.apiBase, "api-base", defaultBase, "API base URL override")
	fs.StringVar(&opts.apiOrigin, "api-origin", "", "pairing/website origin override, e.g. http://localhost:8080")
	fs.StringVar(&opts.keyFile, "key-file", "", "read the API key from a file instead of browser pairing")
	fs.BoolVar(&opts.reauth, "reauth", false, "ignore a saved key and pair again via the browser")
	if err := fs.Parse(rest); err != nil {
		os.Exit(1)
	}

	banner()

	// Refuse to run as root: we write into the user's home, and a root-owned
	// key file would be wrong.
	if os.Geteuid() == 0 {
		die("run as a normal user, not root")
	}

	apiOrigin := opts.apiOrigin
	if apiOrigin == "" {
		apiOrigin = siteURL
	}
	keyPath := pairing.DefaultKeyPath()

	if opts.uninstall {
		configure.Uninstall(configure.NewEnv(opts.model, opts.apiBase, apiOrigin, "", keyPath, opts.dryRun))
		return
	}

	// model/base end up in config files and shell rc's — keep them safe.
	if !safeChars.MatchString(opts.model) {
		die("model must only contain letters, digits, . _ ~ : / -")
	}
	if !safeChars.MatchString(opts.apiBase) {
		die("api base must only contain letters, digits, . _ ~ : / -")
	}

	// Interactive only when we can actually read answers: a terminal available
	// (stdin, or /dev/tty when piped), no --yes, and not a dry run.
	in := os.Stdin
	canInteract := !opts.yes && !opts.dryRun
	if canInteract {
		tty, ok := termx.Interactive()
		if !ok {
			canInteract = false
		} else if tty != nil {
			in = tty
			defer tty.Close()
		}
	}

	// Which services to configure.
	var sel menu.Selections
	switch {
	case opts.services != "":
		list, err := parseServices(opts.services)
		if err != nil {
			die("%v", err)
		}
		sel = fromServiceList(list)
		if !sel.Any() {
			warnf("--services selected nothing — no configuration will be written")
		}
	case canInteract:
		res, err := menu.Run(in, os.Stdout)
		if err != nil {
			die("menu error: %v", err)
		}
		if !res.Confirmed {
			die("aborted — nothing was changed")
		}
		sel = res.Selections
	default:
		sel = fromDetect()
	}
	if what := summary(sel); what != "" {
		logf("configuring: %s", what)
	} else {
		warnf("no services selected — only the API key will be stored")
	}

	fmt.Printf("  base URL: %s    model: %s\n\n", opts.apiBase, opts.model)

	// Acquire the key.
	var apiKey string
	switch {
	case opts.dryRun:
		apiKey = "sk-dry-run-placeholder"
		logf("dry run — nothing will be written")
	case opts.keyFile != "":
		data, err := os.ReadFile(opts.keyFile)
		if err != nil {
			die("key file not found: %s", opts.keyFile)
		}
		apiKey = strings.TrimSpace(string(data))
		logf("API key read from %s", opts.keyFile)
	default:
		reuse := !opts.reauth && pairing.KeyExists(keyPath)
		if reuse && canInteract {
			if wantReauth := promptReauth(in, keyPath); wantReauth {
				opts.reauth = true
				reuse = false
			}
		}
		if reuse {
			apiKey, _ = pairing.LoadKey(keyPath)
			logf("reusing API key from %s", keyPath)
		} else {
			if opts.reauth {
				logf("forcing re-authentication via the browser (ignoring %s)", keyPath)
			}
			client := pairing.New()
			if opts.apiOrigin != "" {
				client.Origin = opts.apiOrigin
			}
			tr, err := client.Pair(context.Background(), os.Stdout)
			if err != nil {
				die("%v", err)
			}
			// Server defaults fill in only what wasn't set via flag.
			if tr.Model != "" && !flagSet(fs, "model") {
				opts.model = tr.Model
			}
			if tr.BaseURL != "" && !flagSet(fs, "api-base") {
				opts.apiBase = tr.BaseURL
			}
			apiKey = tr.APIKey
		}
	}

	// The key always lands in its 0600-perm file, however it was sourced.
	if !opts.dryRun {
		if err := pairing.SaveKey(keyPath, apiKey); err != nil {
			die("could not store the API key: %v", err)
		}
		logf("API key stored at %s (chmod 600)", keyPath)
	}

	// Configure each selected service.
	env := configure.NewEnv(opts.model, opts.apiBase, apiOrigin, apiKey, keyPath, opts.dryRun)
	if sel.Shell {
		configure.ConfigureShellEnv(env)
	}
	if sel.Opencode {
		configure.ConfigureOpencode(env)
	}
	if sel.Windsurf {
		configure.ConfigureWindsurf(env)
	}
	if sel.Kimi {
		configure.ConfigureKimi(env)
	}
	if sel.Continue {
		configure.InstructContinue(env)
	}
	if sel.Cursor {
		configure.InstructCursor(env)
	}

	fmt.Println()
	if opts.dryRun {
		logf("dry run complete — re-run without --dry-run to apply")
	} else {
		logf("done. key: %s   model: %s", fingerprint(apiKey), opts.model)
		logf("manage keys at %s/api-keys", apiOrigin)
	}
	fmt.Println()
}

func usage() {
	fmt.Fprintln(os.Stderr, "2ba installer — pairs your browser, creates an API key, and configures")
	fmt.Fprintln(os.Stderr, "your local coding agents to use 2ba.ai as the model provider.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage: 2ba-installer [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --dry-run        print the plan without touching anything")
	fmt.Fprintln(os.Stderr, "  --uninstall      remove everything this binary manages")
	fmt.Fprintln(os.Stderr, "  --services LIST  services to configure: shell,opencode,windsurf,kimi,")
	fmt.Fprintln(os.Stderr, "                   continue,cursor (default: all detected)")
	fmt.Fprintf(os.Stderr, "  --model NAME     model to configure (default: %s)\n", defaultModel)
	fmt.Fprintf(os.Stderr, "  --api-base URL   API base URL override (default: %s)\n", defaultBase)
	fmt.Fprintln(os.Stderr, "  --api-origin URL pairing/website origin override, e.g. http://localhost:8080")
	fmt.Fprintln(os.Stderr, "  --key-file PATH  read the API key from a file instead of browser pairing")
	fmt.Fprintln(os.Stderr, "  --reauth         ignore a saved key and pair again via the browser")
	fmt.Fprintln(os.Stderr, "  --yes            non-interactive: accept all defaults")
	fmt.Fprintln(os.Stderr, "  --version        print the version and exit")
}

// logf/warnf/die mirror the old install.sh's coloured output.
func logf(format string, a ...any)  { fmt.Printf("  \033[32m*\033[0m "+format+"\n", a...) }
func warnf(format string, a ...any) { fmt.Printf("  \033[33m!\033[0m "+format+"\n", a...) }
func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "\033[31merror:\033[0m "+format+"\n", a...)
	os.Exit(1)
}

func banner() {
	const art = ` ____   ____    ____        ____   _|
    | | |  _ \  |  _ \      |  _ \  | |
  __| | | | | |  | | | |      | | | |  | |
 /  | | | |__ |  | |__ |      | |__ |  | |
|__/   |____|   |____|   .   |____|  |_|`
	const (
		red   = "\033[0m"
		dim   = "\033[2m"
		green = "\033[1;32m"
	)
	fmt.Printf("\n%s%s%s\n", green, art, red)
	fmt.Printf("  %shigh-throughput, OpenAI-compatible, 100%% EU-hosted%s\n\n", dim, red)
}

// promptReauth asks whether to keep a found key or force re-authentication.
// It returns true when the user wants to re-auth.
func promptReauth(in interface{ Read([]byte) (int, error) }, keyPath string) bool {
	fmt.Printf("\n  A saved API key was found at %s.\n", keyPath)
	fmt.Print("  Keep it (Enter) or force re-authentication via the browser (r)? ")
	br := bufio.NewReader(in)
	for {
		line, err := br.ReadString('\n')
		if err != nil { // EOF/ctrl-d: keep the key
			return false
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "r", "reauth":
			return true
		case "", "y", "yes":
			return false
		default:
			warnf("press Enter to keep the saved key, or r to force re-authentication")
		}
	}
}

// parseServices splits a --services value (comma/space separated) into the
// canonical service names, erroring on anything unknown.
func parseServices(s string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' '
	}) {
		name := tok
		if name == "sh" {
			name = "shell"
		}
		switch name {
		case "shell", "opencode", "windsurf", "kimi", "continue", "cursor":
		default:
			return nil, fmt.Errorf("unknown service '%s' — valid: shell,opencode,windsurf,kimi,continue,cursor", tok)
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

func fromServiceList(names []string) menu.Selections {
	var s menu.Selections
	for _, n := range names {
		switch n {
		case "shell":
			s.Shell = true
		case "opencode":
			s.Opencode = true
		case "windsurf":
			s.Windsurf = true
		case "kimi":
			s.Kimi = true
		case "continue":
			s.Continue = true
		case "cursor":
			s.Cursor = true
		}
	}
	return s
}

func fromDetect() menu.Selections {
	d := detect.Detect()
	return menu.Selections{
		Shell: d.Shell, Opencode: d.Opencode, Windsurf: d.Windsurf,
		Kimi: d.Kimi, Continue: d.Continue, Cursor: d.Cursor,
	}
}

// summary is a human-readable list of the selected services ("", if none).
func summary(s menu.Selections) string {
	var parts []string
	if s.Shell {
		parts = append(parts, "shell env")
	}
	if s.Opencode {
		parts = append(parts, "opencode")
	}
	if s.Windsurf {
		parts = append(parts, "windsurf")
	}
	if s.Kimi {
		parts = append(parts, "kimi")
	}
	if s.Continue {
		parts = append(parts, "continue")
	}
	if s.Cursor {
		parts = append(parts, "cursor")
	}
	return strings.Join(parts, ", ")
}

// flagSet reports whether the named flag was explicitly set on the command line.
func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// fingerprint shows a key as its first 6 and last 4 characters, never the whole thing.
func fingerprint(k string) string {
	if len(k) <= 10 {
		return "…"
	}
	return k[:6] + "…" + k[len(k)-4:]
}
