# Plan: more clients (Gemini CLI, Cursor, VS Code, opencode)

Status: plan, not started. Written 2026-11-09 so the work can be picked up
later. Nothing here is implemented.

toolshed supports Claude Code and Codex. This plan adds Gemini CLI, Cursor,
VS Code (GitHub Copilot) and opencode as the first wave, and leaves room for
Windsurf, Zed and Claude Desktop later.

The rule from AGENTS.md applies throughout: a client isn't supported until it
works as well as Claude Code and Codex, verified against the real client, not
against our idea of it. The Codex work showed why: the CLI we trusted reset a
server's settings on every update, and our mock didn't.

## What each client needs (from research, all to be verified)

From the clients' docs and third-party guides on 2026-11-09. Several sources
disagreed, so Phase 0 below checks every line against the client itself
(source code, its own CLI, or a real install) before any code is written.

| | Gemini CLI | Cursor | VS Code / Copilot | opencode |
| --- | --- | --- | --- | --- |
| User config | `~/.gemini/settings.json` | `~/.cursor/mcp.json` | `mcp.json` in the user profile (`~/Library/Application Support/Code/User/` on macOS, `~/.config/Code/User/` on Linux) | `~/.config/opencode/opencode.json` (verify) |
| Project config | `.gemini/settings.json` | `.cursor/mcp.json` | `.vscode/mcp.json` | `opencode.json` in the repo |
| Servers key | `mcpServers` | `mcpServers` | `servers` | `mcp` (v2 docs: `mcp.servers`) |
| Local server | `command`, `args`, `env` | `command`, `args`, `env` | `type: stdio`, `command`, `args`, `env` | `type: local`, `command` as one array, `environment` |
| Remote server | `httpUrl` (streamable) or `url` (SSE), `headers` | `url`, `headers` | `type: http`, `url`, `headers` | `type: remote`, `url`, `headers`, `oauth` |
| Headers helper | not found | not found | not found | not found |
| Variables | `$VAR` in values (verify) | `${env:NAME}` | `${input:…}`, `${env:…}` | `{env:NAME}` (verify) |
| JSON with comments | yes (verify) | no (verify) | yes | yes (JSONC) |
| CLI to edit | `gemini mcp add/list/remove` | none (`cursor-agent mcp list`, verify) | `code --add-mcp` (user scope only) | `opencode mcp …` (verify) |
| Headless, so CI can test the real client | yes (npm) | `cursor-agent` maybe | no (desktop app) | yes (npm) |
| How it's detected | `gemini` on PATH | `~/.cursor` exists, or the app | `code` on PATH, or the app | `opencode` on PATH |

Sources: [Gemini CLI](https://geminicli.com/docs/tools/mcp-server),
[Cursor](https://cursor.com/docs/context/mcp),
[VS Code](https://code.visualstudio.com/docs/copilot/reference/mcp-configuration),
[opencode](https://opencode.ai/docs/mcp-servers). Later waves:
[Windsurf](https://docs.windsurf.com/windsurf/cascade/mcp) (`mcpServers`,
`serverUrl` for remote, user scope only), [Zed](https://zed.dev/docs/context-servers)
(`context_servers` in settings.json; the docs show two command shapes),
Claude Desktop (`claude_desktop_config.json`, `mcpServers`, local servers only).

## Decisions so far

- First wave: Gemini CLI, Cursor, VS Code / Copilot, opencode.
- Do the adapter refactor first, with no behaviour change, then add the
  clients one at a time.
- Write config files directly with format-preserving edits, like Codex,
  rather than through the clients' CLIs. A CLI is only used where it's the
  sole supported way in, and only if the contract tests show it's lossless.

## Open questions

1. **Which clients a tool installs into by default.** Today it's every client
   found. With six that's too much (VS Code installed for other reasons would
   get every server). Options:
   - remember the clients you chose: ask once, store `clients: [...]` in
     `~/.toolshed/config.yaml`, `--client` overrides (recommended);
   - every client found, as today;
   - ask on every install.
2. **Remote servers with a token, on clients without a headers helper.**
   Options:
   - (a) a stdio-to-HTTP relay, `toolshed proxy <id> …`, registered like
     `toolshed exec`, so the token stays in the keychain;
   - (b) write the token into the client's `headers` (plain text, like the
     no-keyring fallback today) after a warning;
   - (c) the client's own env interpolation (`${env:NAME}`), which only works
     if the variable is set where the client starts, so it's mostly useless
     for GUI apps;
   - (d) skip those servers on those clients, with a clear note.

   Recommendation: (d) first, (a) as its own phase.
3. **VS Code's `${input:…}` prompts.** VS Code can ask for a secret itself
   and store it. toolshed could map secret inputs to that instead of the
   wrapper, or ignore it. Leaning towards ignoring it, so it works the same
   as everywhere else.
4. **What counts as "installed" for desktop apps.** For Cursor and VS Code a
   config directory can exist without the app (synced dotfiles). Decide
   whether to look for the app too.

## Architecture

### Client adapters (`internal/clients`)

Replace the Claude/Codex switches (about 50 places across clients, shed,
cli and tui) with one interface, and one adapter per client:

```go
type Client interface {
    Name() string                    // "claude", "gemini", …
    Label() string                   // "Claude Code", "Gemini CLI"
    Available() bool                 // installed on this machine
    Scopes() []Scope                 // user, project (and Claude's local)
    Supports(f Feature) bool         // HeadersHelper, RemoteHTTP, …
    Detect(root string) []Registration
    Register(name string, cfg Config, scope Scope) error
    Unregister(name string, scope Scope) error
}
```

- `Installation` becomes a map from client name to `Registration` (scope,
  key in that client's config, the config as stored), instead of
  `ClaudeScope`, `Codex`, `CodexScope`, `CodexName`, `CodexConfig`.
- `Config` stays the neutral description: command, args, env, url, headers,
  headers helper. Each adapter translates it into its own shape, and keeps
  keys toolshed doesn't manage, as the Claude and Codex adapters do now.
- `clients.All` becomes a registry of adapters, in display order.

### Editing config files without damage

- TOML: the existing go-toml editor (Codex).
- JSON and JSONC: `github.com/tailscale/hujson`, which parses JSON with
  comments and trailing commas, lets you patch values, and writes the rest
  back unchanged. One generic "set or remove object member at path" editor,
  used by Gemini, Cursor, VS Code, opencode (and later Windsurf, Zed, Claude
  Desktop).
- Same safety as Codex: only the keys toolshed manages, an atomic write that
  keeps the file mode, and parse the result and compare it with what was
  intended before replacing the file.
- Claude Code keeps using `claude mcp add-json`.

### Secrets

- Local servers: `toolshed exec` already works for every client that can
  start a command, which is all of them.
- Remote servers with a token: through the headers helper where a client
  supports it, otherwise per open question 2.
- Project scope without a keyring is still refused, for every client.

### Front ends

- CLI: `--client` accepts the new names; `list` and `doctor` show every
  client.
- TUI: two dots per tool don't scale to six. Show the clients a tool is in
  as short labels (`CC CX GM CU VS OC`) in the detail pane, and a count or
  compact marks in the list. Mock it up before building.
- The MCP server (`install_server`) takes a list of clients too.
- `import` reads every client's config, and keeps a server's other
  registrations when importing it from one client.

## Phases

Each phase is its own PR (or a few), green CI, docs updated.

0. **Verify formats.** For each first-wave client, check every row of the
   table against the source or a real install. Record a sample config of
   each kind (user and project, local and remote, with comments) as test
   fixtures. Settle open questions 2 and 4.
1. **Adapter refactor.** Claude Code and Codex behind the interface, no
   behaviour change. The existing unit, e2e, pty and contract tests must
   pass unchanged; that's the proof.
2. **The JSON/JSONC editor**, with the same kind of tests as the TOML one:
   keeps comments, other servers, unmanaged keys; changes transport cleanly;
   refuses layouts it doesn't understand.
3. **Default targets** (open question 1), with the TUI change. Before the
   new clients, so nobody gets six registrations on upgrade.
4. **Gemini CLI.** First, because it's headless: contract tests against the
   real `gemini` in CI (npm, pinned), checked with `gemini mcp list`.
5. **opencode.** Also headless, with contract tests against the real
   `opencode`.
6. **Cursor.** Fixture tests. Contract tests through `cursor-agent` if it
   can list servers headlessly; otherwise a manual check, written down in
   the PR.
7. **VS Code / Copilot.** Fixture tests, the user-profile path per OS, and a
   manual check in VS Code. Find out first which location VS Code currently
   reads, because its docs disagree.
8. **Remote tokens without a headers helper** (`toolshed proxy`), if open
   question 2 lands there.
9. Docs: `docs/clients.md` becomes a page per client, or a table, and the
   README and the site say "Claude Code, Codex, Gemini CLI, Cursor, VS Code
   and opencode".

Later waves, the same way: Windsurf, Zed, Claude Desktop.

## Testing (the rules from AGENTS.md, per client)

- Fixtures of real configs, including comments and settings toolshed doesn't
  manage, which must survive every write.
- Every flow tested by its outcome: the file contents after install, update,
  uninstall, import.
- Contract tests against the real client where it runs headless (Gemini CLI,
  opencode, maybe Cursor), pinned in CI and bumped on purpose.
- The pty TUI test covers choosing clients.
- Check each new test fails when the code is broken on purpose.

## Risks

- Formats keep moving (VS Code's locations, opencode v2's `mcp.servers`).
  Detect both where we can; pinned contract tests catch it for the headless
  clients only.
- Desktop apps can't be tested in CI, so their support rests on fixtures and
  manual checks. Say that in the docs.
- Writing to editor config files people care about. The format-preserving
  editors and the "parse and compare before replacing" check are what make
  this safe; don't shortcut them.
