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

or `go install github.com/bartdegoede/toolshed/cmd/toolshed@latest` (Go
1.26 or newer). Prebuilt binaries are on the
[releases page](https://github.com/bartdegoede/toolshed/releases). You need
[Claude Code](https://claude.com/claude-code) and/or
[Codex](https://github.com/openai/codex) on your PATH. macOS and Linux.

## Quickstart

```sh
toolshed
```

That's the whole interface: browse the examples, press `a` to add your own,
`i` to install, `t` to see a server's tools live, `m` to import the servers
you already have.

Prefer the command line?

```sh
toolshed add memory --npx @modelcontextprotocol/server-memory
#  ✓ Added memory
#    ✓ pinned to 2.1.0
#  ✓ memory offers 9 tools
#      create_entities   Create multiple new entities in the knowledge graph
#      …

toolshed install memory
#  ✓ memory → claude, codex
```

## Documentation

The full docs are at **[toolshed.run](https://toolshed.run)**, and in
[`docs/`](docs):

- [Install](docs/install.md) and [Getting started](docs/getting-started.md)
- [Commands](docs/commands.md): every command and its flags
- [The registry](docs/registry.md): your tools in one YAML file, and sharing it with a team
- [Secrets](docs/secrets.md): tokens in your keychain, for local and remote servers
- [Import](docs/import.md): adopt the servers you already set up by hand
- [Your agent](docs/agents.md): let Claude Code or Codex add servers themselves
- [Updates and versions](docs/updates.md): pinned packages, upgrades, registry changes
- [Doctor](docs/doctor.md): check that every installed tool works
- [Claude Code and Codex](docs/clients.md): where tools are installed, and how
- [Build your own server](docs/build-a-server.md)

## Development

```sh
just test       # unit tests
just test-e2e   # end to end: real binary + real server, mock claude/codex
just lint
```

## License

[MIT](LICENSE)
