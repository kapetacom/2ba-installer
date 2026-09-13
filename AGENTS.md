# 2ba-installer

Go installer for [2ba.ai](https://2ba.ai): pairs a browser session, mints a 2ba
API key, and configures local coding tools to use 2ba as their model provider.

## Development

- Verify before committing: `go build ./...`, `go vet ./...`, `go test ./...`,
  and `gofmt -l .` returning nothing (CI enforces all four).
- Tests pin every config-dir env var (`HOME`, `XDG_CONFIG_HOME`,
  `KIMI_CODE_HOME`, `ZCODE_HOME`, `TWOBA_DATA_DIR`, `CLAUDE_CONFIG_DIR`,
  `PI_CODING_AGENT_DIR`) inside `t.TempDir()` — keep new services isolated the
  same way.
- Conventional commits: `feat:` / `fix:` / `chore:`.

## Conventions

- A new service is one file in `internal/configure` plus wiring into
  `internal/detect`, `internal/menu` (fixed row order — append at the end),
  `cmd/2ba-installer/main.go` (`--services` list, summary, dispatch), and
  `uninstall.go`. JSON-config services follow the opencode/zcode pattern:
  load-or-empty object, never clobber user values, upgrade the installer's own
  entries in place, leave user-owned entries and corrupt files untouched.
- Never rewrite a user file that holds no 2ba entry, and only back up
  (`*.bak.2ba`) immediately before a real write.
- The API key is stored at `~/.config/2ba/2BA_API_KEY` (0600); files that
  embed the key must be written 0600.

## Releases

1. Bump the version: new service or capability → minor (`vX.Y.0`), fixes →
   patch.
2. Push a semver tag on `main` (`git tag vX.Y.Z && git push origin vX.Y.Z`).
   GoReleaser (`.goreleaser.yaml`) builds darwin/linux × amd64/arm64 and
   publishes the GitHub release; release notes are grouped by
   conventional-commit type.
3. **Always create a Discord announcement for the release** and post it to the
   2ba Discord channel once the release workflow has finished. Only
   user-visible changes belong in the post (no CI/internal work), and it
   should read for users, not developers: focus on the features and bug
   fixes in plain language — what users can now do — not on how the
   installer works (no config paths, env vars, or implementation details
   unless a user actually needs them). Use
   Discord-compatible Markdown — `-` bullets, `**bold**`, `` `inline code` ``
   and fenced code blocks; no tables, no HTML. Follow the established format:

   ````
   2ba installer vX.Y.Z is out 🎉
   {one-line hook: what this release is about}

   - **{Tool}** — what changed for the user
   - **{Tool}** — what changed for the user

   New here? Install:
   ```
   curl -fsSL https://2ba.ai/install.sh | sh
   ```

   Already installed? Re-run the same command — it upgrades your existing configs in place, nothing to clean up.
   ````
