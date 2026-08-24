# 2ba-installer

The Go installer for [2ba.ai](https://2ba.ai) — a high-throughput, OpenAI-compatible,
100% EU-hosted model API. It pairs your browser, mints a 2ba API key, and configures
your local coding tools to use 2ba.ai as their model provider.

This is the binary that the one-liner installs:

```sh
curl -fsSL https://2ba.ai/install.sh | sh
```

`install.sh` (served by the 2ba backend) is a thin bootstrap that downloads the
matching binary from [this repo's latest release](https://github.com/kapetacom/2ba-installer/releases/latest)
and runs it. The binary opens `/dev/tty` for the interactive menu, so it keeps
working even when run through a pipe.

## What it configures

| Service  | What it does |
| -------- | ------------ |
| shell env | exports `OPENAI_API_KEY`/`OPENAI_API_BASE` in your shell rc (key read from a 0600 file) |
| opencode | adds a `2ba` OpenAI-compatible provider to `~/.config/opencode/opencode.json` |
| windsurf | adds a `2BA` model to `~/.codeium/windsurf/model_config.json` |
| kimi     | adds a `2ba` provider + model to Kimi's `config.toml` (both `~/.kimi` and `~/.kimi-code`) |
| continue | prints manual steps for `~/.continue` |
| cursor   | prints manual steps for the Cursor UI |

The API key is always stored at `~/.config/2ba/2BA_API_KEY` (mode `0600`).

## Options

```
--dry-run        print the plan without touching anything
--uninstall      remove everything this binary manages
--services LIST  services to configure: shell,opencode,windsurf,kimi,continue,cursor
--model NAME     model to configure (default: amber)
--api-base URL   API base URL override (default: https://api.2ba.ai/v1)
--api-origin URL pairing/website origin override, e.g. http://localhost:8080
--key-file PATH  read the API key from a file instead of browser pairing
--reauth         ignore a saved key and pair again via the browser
--yes            non-interactive: accept all defaults
--version        print the version
```

## Development

```sh
go build ./...
go test ./...
```

Releases are cut by pushing a semver tag (`vX.Y.Z`); GoReleaser builds the
darwin/linux × amd64/arm64 binaries and a `checksums.txt`.
