# Clients

<!-- summary: Claude Code, Codex and Antigravity: where tools are installed, and how -->

toolshed installs every tool into Claude Code, Codex and Antigravity
(Antigravity CLI, `agy`), unless the tool's `clients` list says otherwise or
you pass `--client`. Each client is only used when it's on your `PATH`
(`claude`, `codex`, `agy`).

## Scope

`toolshed install <id>` installs a new tool for your user, so it's available
in every project. `--project` (or "This repo only" in the TUI) writes it into
the repository instead, so it can be committed and shared:

| | For your user | For this repository |
| --- | --- | --- |
| Claude Code | `~/.claude.json` | `.mcp.json` |
| Codex | `~/.codex/config.toml` | `.codex/config.toml` |
| Antigravity | `~/.gemini/config/mcp_config.json` | `.agents/mcp_config.json` |

Codex only reads a project's config once you've trusted the project (start
`codex` in it and accept); toolshed tells you when it isn't trusted yet.
Tools with secrets are registered there as plain `toolshed exec …`, found on
`PATH`, rather than your own toolshed's path, so they work for teammates who
installed toolshed somewhere else.

Claude Code's per-project "local" scope (in `~/.claude.json`, just for you)
has no counterpart in Antigravity: a tool installed there is left out of
Antigravity, with a note, rather than installed for every project. Codex
gets it for your user, as before.

Installing a tool again keeps it where it is in each client, including Claude
Code's per-project "local" scope (in the TUI, that's the form's default).
Choosing the other scope (`--user` or `--project`, or the other option in the
TUI) moves it: toolshed registers it at the new scope, with the settings it
doesn't manage (a Claude Code `timeout`, Codex's `startup_timeout_sec` and
tool approvals, Antigravity's `timeoutSeconds` and `enabledTools`), then
removes the old registration, so a tool is never in two places at once.
Moving a tool out of the repository removes it from the repository's
`.mcp.json`, `.codex/config.toml` and `.agents/mcp_config.json`, which
teammates may be using; toolshed tells you when it moves something.

If a client has the same name at two scopes anyway, it uses one of them:
Claude Code takes "local" over user over project (`.mcp.json`), Codex
takes the project's once you've trusted the project, otherwise your user's,
and Antigravity takes your user's over the project's.
toolshed shows the one the client uses, and `toolshed doctor` points out the
other.

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

Antigravity registrations are written to its `mcp_config.json` directly too,
because `agy mcp add` replaces a server's whole entry. toolshed only touches
`command`, `args`, `env`, or `serverUrl` (or `url`, if the entry uses it)
and `headers`; `disabled`, `timeoutSeconds`, `enabledTools`, other servers
and comments stay as they were, and the result is checked before the file
is replaced.

Antigravity can't run a command to get a server's headers, so a remote tool
whose headers come from a command (`header_cmd`, or secrets in its headers,
which toolshed reads from your keyring that way) isn't installed there; toolshed says so and installs it into the others.

## After installing

Claude Code picks up new servers when it reconnects: run `/mcp`, or restart
it. Codex and Antigravity load them in a new session.
