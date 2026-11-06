# toolshed

Go CLI + TUI that adds MCP servers to Claude Code and Codex from a YAML
registry, with secrets in the OS keyring. User docs: README.md,
docs/registry.md. Layout and rules: CONTRIBUTING.md.

```bash
just build      # ./toolshed
just test       # go test -race ./... (+ examples/ping-mcp)
just test-e2e   # test/e2e.sh with test/mocks/{claude,codex}
just lint
```

- Operations live in internal/shed; internal/cli and internal/tui are thin.
- TUI: Bubble Tea v2 / Lipgloss v2 / huh v2 (charm.land/... import paths).
- Never let tests touch the real ~/.claude.json, ~/.codex or keychain: use
  secrets.UseMockForTest, t.Setenv("HOME", t.TempDir()), TOOLSHED_HOME.
- macOS + Linux only (exec uses syscall.Exec).

## Releasing

Only when asked. Semver tags on `main`; CI must be green on the commit first.

```bash
gh run list -w CI -L1                        # green on HEAD?
git tag vX.Y.Z && git push origin vX.Y.Z     # triggers .github/workflows/release.yml
gh run watch "$(gh run list -w Release -L1 --json databaseId -q '.[0].databaseId')" --exit-status
gh api repos/bartdegoede/homebrew-tap/contents/Casks/toolshed.rb -q .path   # tap updated?
```

- GoReleaser builds macOS/Linux binaries, the GitHub release, and the cask in
  bartdegoede/homebrew-tap (needs the `HOMEBREW_TAP_TOKEN` repo secret).
- Validate `.goreleaser.yaml` changes before tagging: `goreleaser check` and
  `goreleaser release --snapshot --clean --skip=publish` (then `rm -rf dist`).
  Secret templates must be plain `{{ .Env.NAME }}`; anything else fails the cask step.
- Releases build with Go `stable`; `go.mod` (1.25) is the `go install` minimum.
  Don't raise it, or `examples/ping-mcp/go.mod`, without a reason.
- A failed release: fix on `main`, then tag the next patch version.
- Re-record `docs/demo.gif` (`just demo`) when the TUI changes visibly.
