# Your agent

<!-- summary: Let your coding agent add servers itself -->

toolshed is an MCP server itself. Install it like any other tool:

```sh
toolshed install toolshed
```

and your agent gets `list_servers`, `inspect_server`, `add_server`,
`install_server` and `uninstall_server`. Then you can ask:

> Add the Playwright MCP server, check it starts, and install it for this repo.

New servers load the next time the client reconnects: `/mcp` in Claude Code,
a new session in Codex.

## What it can't do

A server is a command toolshed runs on your machine, and some come with your
API tokens. So the agent's reach is limited on purpose:

- **Anything it adds is pending.** toolshed won't start it, not even to list
  its tools, or install it until you've approved it: `toolshed approve <id>`
  shows you exactly what it will run, or select it in the TUI and press `i`.
- **It can't handle secrets.** If a server needs a token, `install_server`
  refuses and tells the agent to send you to `toolshed install <id>` in your
  terminal. Secrets passed to it anyway are rejected, and so are headers with
  a token written in.
- **It can't replace a tool you have.** A replacement would inherit that
  tool's secrets from the keyring.
- **It can't move a tool.** `install_server` reinstalls an installed tool
  where it is; asking for another scope is refused, because moving a tool
  out of the repository changes config your teammates may share. You move
  it with `toolshed install <id> --user` or `--project`.
- **It can't choose clients.** `install_server` installs into the clients
  you chose with `toolshed clients` (or every one on your `PATH`, until you
  do).
- **It can't accept registry changes.** If a tool's registry entry now runs
  something different from what's installed, the agent can't start or
  re-install it; you review the change with `toolshed update <id>`.

Packages the agent adds are pinned to their current release, like the ones
you add.
