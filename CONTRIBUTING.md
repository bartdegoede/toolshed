# Contributing

Thanks for helping! Bug reports, registry ideas and PRs are all welcome.

## Setup

Go (version in `go.mod`), [just](https://github.com/casey/just), and `jq` for
the e2e suite.

```sh
just test       # unit tests (race detector on)
just test-e2e   # real binary + real example server against mock claude/codex
just lint       # golangci-lint v2
```

The e2e suite never touches your real Claude Code / Codex config or keychain:
it runs in a throwaway `$HOME` with mock client CLIs from `test/mocks`.

## Layout

```
cmd/toolshed/        entry point
internal/registry/   registry schema, loading, merging, validation, editing
internal/clients/    Claude Code + Codex: register/unregister, read configs
internal/secrets/    OS keyring
internal/shed/       the operations: install, uninstall, exec, probe
internal/cli/        subcommands (thin wrappers over shed)
internal/tui/        Bubble Tea UI (thin wrapper over shed)
examples/ping-mcp/  example server (separate Go module)
```

## Guidelines

- Keep features in `shed` so the CLI and TUI stay in sync.
- New registry fields need validation in `registry.ValidateEntry`, a test
  case, and a line in `docs/registry.md`.
- Don't reference IDs from the shipped `registry.yaml` in tests; build
  entries inline instead, so the examples can change freely.
