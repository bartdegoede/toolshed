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
