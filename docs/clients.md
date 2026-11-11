# Claude Code and Codex

<!-- summary: Where tools are installed, and how -->

toolshed installs every tool into both Claude Code and Codex, unless the
tool's `clients` list says otherwise or you pass `--client`. Each client is
only used when it's on your `PATH`.

## Scope

`toolshed install <id>` installs a new tool for your user, so it's available
in every project. `--project` (or "This repo only" in the TUI) writes it into
the repository instead, so it can be committed and shared:

| | For your user | For this repository |
| --- | --- | --- |
| Claude Code | `~/.claude.json` | `.mcp.json` |
| Codex | `~/.codex/config.toml` | `.codex/config.toml` |

Codex only reads a project's config once you've trusted the project (start
`codex` in it and accept); toolshed tells you when it isn't trusted yet.
Tools with secrets are registered there as plain `toolshed exec …`, found on
`PATH`, rather than your own toolshed's path, so they work for teammates who
installed toolshed somewhere else.

Installing a tool again keeps it where it is in each client, including Claude
Code's per-project "local" scope (in the TUI, that's the form's default).
Choosing the other scope (`--user` or `--project`, or the other option in the
TUI) moves it: toolshed registers it at the new scope, with the settings it
doesn't manage (a Claude Code `timeout`, Codex's `startup_timeout_sec` and
tool approvals), then removes the old registration, so a tool is never in
two places at once. Moving a tool out of the repository removes it from the
repository's `.mcp.json` and `.codex/config.toml`, which teammates may be
using; toolshed tells you when it moves something.

## How the config is written

Claude Code registrations go through `claude mcp add-json`. Settings toolshed
doesn't manage (a `timeout`, say) are carried over when it installs a tool
again.

Codex registrations are written to its `config.toml` directly, because
`codex mcp add` resets a server's whole table, so every update would lose
your settings, and it can't set a headers helper. toolshed only touches the
keys it manages in `[mcp_servers.<id>]` (`command`, `args`, `env`, or `url`,
`http_headers_helper`, `http_headers`), and leaves everything else as it
was: comments, `startup_timeout_sec`, enabled and disabled tools, per-tool
approvals, other servers. It checks the result parses before replacing the
file. Older toolshed versions registered `my-tool` as `my_tool` in Codex; the
next update renames it and keeps its settings.

## After installing

Claude Code picks up new servers when it reconnects: run `/mcp`, or restart
it. Codex loads them in a new session.
