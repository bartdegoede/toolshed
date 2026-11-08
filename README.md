<h1 align="center">⚒ toolshed</h1>

<p align="center">
  <b>Add any MCP server to Claude Code and Codex in seconds.</b><br>
  One TUI, one command, secrets in your keychain instead of your dotfiles.
</p>

<p align="center">
  <img src="docs/demo.gif" alt="toolshed demo" width="800">
</p>

---

Connecting an agent to a new tool usually means finding the right JSON
snippet, pasting an API token into a config file, doing it again for the
other client, and hoping the server actually starts.

toolshed does that part for you:

- **Add anything.** An npm or Python package, any CLI that speaks MCP, or a
  remote URL. `toolshed add` starts the server right away and shows you the
  tools it offers, so you know it works before your agent tries it.
- **Install everywhere.** One keypress registers the tool with Claude Code
  *and* Codex, for every project or just the current repo.
- **Keep secrets secret.** API tokens go to the macOS Keychain or Linux Secret
  Service. Your agent's config only says `toolshed exec jira`, and the token
  is injected when the server starts.
- **Let your agent do it.** `toolshed mcp` is itself an MCP server: ask Claude
  to "add the Playwright server for this repo" and it finds and adds it. You
  approve what it will run with one keypress, and it checks and installs it.
  Secrets are never passed through the chat; it sends you to the TUI for
  those.
- **Share a toolbox.** Your tools live in one small YAML file. Commit it, and
  your team gets the same setup with `toolshed install`.

## Install

```sh
brew install bartdegoede/tap/toolshed
```

or with Go (1.25 or newer):

```sh
go install github.com/bartdegoede/toolshed/cmd/toolshed@latest
```

Prebuilt binaries for macOS and Linux are on the
[releases page](https://github.com/bartdegoede/toolshed/releases).

You need [Claude Code](https://claude.com/claude-code) and/or
[Codex](https://github.com/openai/codex) on your PATH. macOS and Linux.

## Quickstart

```sh
toolshed
```

That's the whole interface: browse the examples, press `a` to add your own,
`i` to install, `t` to see a server's tools live.

Prefer the command line?

```sh
toolshed add memory --npx @modelcontextprotocol/server-memory
#  ✓ Added memory
#  ✓ memory offers 9 tools
#      create_entities   Create multiple new entities in the knowledge graph
#      …

toolshed install memory
#  ✓ memory → claude, codex
```

More ways to add tools:

```sh
toolshed add fetch  --uvx mcp-server-fetch                     # Python package
toolshed add docs   --url https://mcp.context7.com/mcp         # remote server
toolshed add jira   --secret JIRA_TOKEN -- jira-mcp --site acme  # any command, with a secret
```

## Let your agent manage its tools

```sh
toolshed install toolshed     # registers `toolshed mcp` with Claude Code / Codex
```

Then just ask:

> Add the Playwright MCP server, check it starts, and install it for this repo.

The agent gets `list_servers`, `inspect_server`, `add_server`,
`install_server` and `uninstall_server`. New servers load the next time the
client reconnects (`/mcp` in Claude Code).

A server the agent adds is **pending** until you approve it: toolshed won't
start or install it before you've seen the command it runs. Approve it with
`toolshed approve <id>`, or select it in `toolshed` and press `i`. The agent
also can't replace a tool you already have, so it can never point an existing
tool (and its secrets) at a different command.

## Commands

| | |
| --- | --- |
| `toolshed` | Interactive TUI |
| `toolshed add <id> …` | Add a tool (`--npx`, `--uvx`, `--url`, or `-- <command>`) |
| `toolshed install <id>…` | Install into Claude Code and Codex (`--project` for this repo only) |
| `toolshed uninstall <id>…` | Remove from the clients (`--purge` also deletes secrets) |
| `toolshed tools <id>` | Start a server and list its tools |
| `toolshed list` | Everything, and where it's installed (`--json`) |
| `toolshed update` | Re-register installed tools after editing the registry |
| `toolshed approve <id>` | Review and allow a tool your agent added |
| `toolshed remove <id>` | Uninstall and delete from your toolshed |
| `toolshed edit` | Open your registry in `$EDITOR` |
| `toolshed mcp` | Serve toolshed to your agent over MCP |

## Your registry

`toolshed add` writes to `~/.toolshed/registry.yaml`:

```yaml
tools:
  - id: jira
    command: jira-mcp
    args: [--site, acme]
    inputs:
      - key: JIRA_TOKEN
        secret: true
```

See [docs/registry.md](docs/registry.md) for every option, team sharing, and
exactly how secrets are handled.

## Build your own server

[`examples/ping-mcp`](examples/ping-mcp) is a complete, genuinely useful MCP
server in about 110 lines of Go: it sends push notifications to your phone
("run the migration and ping me when it's done"). Copy it, add your tools,
then `toolshed add mine -- my-server`.

## Development

```sh
just test       # unit tests
just test-e2e   # end to end: real binary + real server, mock claude/codex
just lint
```

## License

[MIT](LICENSE)
