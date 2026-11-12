# Commands

<!-- summary: Every command and its flags -->

`toolshed` with no arguments opens the TUI. Every command below also takes
`--help`.

## Adding and installing

`toolshed add <id> [flags] [-- command args...]` adds a tool to your
registry, starts it and lists its tools. Pick one kind:

| Flag | |
| --- | --- |
| `--npx <package>` | an npm package, run with `npx -y` |
| `--uvx <package>` | a Python package, run with `uvx` |
| `--url <url>` | a remote server (streamable HTTP) |
| `-- <command> [args]` | any command that speaks MCP over stdio |

| Flag | |
| --- | --- |
| `--secret KEY` | a secret the server needs, asked for at install and kept in the keychain (repeatable) |
| `--input KEY` | a non-secret value asked for at install (repeatable) |
| `--env KEY=VALUE` | a fixed environment variable (repeatable) |
| `--header 'Name=value'` | remote only: an HTTP header; `${KEY}` uses input `KEY` (repeatable) |
| `--headers-helper <cmd>` | remote only: a command that prints JSON auth headers |
| `--name`, `--description` | how it shows up in the TUI |
| `--no-pin` | keep an npm or Python package unpinned |
| `--no-check` | don't start the server to list its tools |
| `--install` | install it right away |
| `--force` | replace a tool with the same id |

`toolshed install <id>...` installs into the clients you chose (asked the
first time; see [Choosing clients](clients.md#choosing-clients)): for every
project, or, for a tool that's installed already, again where it is. See
[Scope](clients.md#scope).

| Flag | |
| --- | --- |
| `--project` | this repository only: `.mcp.json`, `.codex/config.toml` and `.agents/mcp_config.json` |
| `--user` | every project (moves a tool that's in the repository) |
| `--client claude\|codex\|antigravity` | only this client, whatever you chose |
| `--set KEY=VALUE` | a value, instead of being asked (repeatable) |
| `-y` | never ask; values come from `--set` and the environment |

`toolshed clients [<name>...]` shows which clients tools go into, or
chooses them: `toolshed clients claude codex`. Tools already installed stay
where they are.

`toolshed import [<name>...]` adopts the servers your clients
already have. `-y` uses the guessed secrets, `--dry-run` only shows what it
would do, `--no-install` only adds them to your registry. See
[Import](import.md).

## Keeping tools up to date

| Command | |
| --- | --- |
| `toolshed update [<id>...] [-y]` | install again where they're installed, after the registry changed. Asks first if what a tool runs changed; `-y` accepts. |
| `toolshed upgrade [<id>...] [--dry-run]` | move npm and Python packages to their newest release |
| `toolshed secret set <id> [KEY]` | replace a secret, such as a rotated token. Typed at a hidden prompt, or piped in. |
| `toolshed approve <id> [-y]` | review and allow a tool your agent added |

## Looking around

| Command | |
| --- | --- |
| `toolshed list [--json]` | every tool, and where it's installed |
| `toolshed tools <id>` | start a server and list the tools it offers |
| `toolshed doctor [--no-start] [--json]` | check every installed tool (see [Doctor](doctor.md)) |
| `toolshed validate [file]` | check a registry file |

## Removing

| Command | |
| --- | --- |
| `toolshed uninstall <id>... [--purge]` | remove from your clients; `--purge` also deletes its secrets |
| `toolshed uninstall <id> --client <client> --scope <scope>` | remove one copy: from one client, at one scope, also one the client ignores here (see [Doctor](doctor.md)) |
| `toolshed remove <id>` | uninstall and delete from your registry |

## Everything else

| Command | |
| --- | --- |
| `toolshed edit` | open your registry in `$EDITOR` |
| `toolshed mcp` | serve toolshed to your agent over MCP (see [Your agent](agents.md)) |
| `toolshed completion bash\|zsh\|fish` | print a shell completion script |
| `toolshed version` | print the version |

Your clients run two more, `toolshed exec` and `toolshed headers`,
to start servers with their secrets; you don't need them yourself (see
[Secrets](secrets.md)).

Exit codes: 0 when it worked, 1 when it didn't, 2 for a usage mistake.

## Environment

| Variable | |
| --- | --- |
| `TOOLSHED_REGISTRY` | use this registry file instead of `~/.toolshed/registry.yaml` |
| `TOOLSHED_HOME` | toolshed's directory (default `~/.toolshed`) |
| `TOOLSHED_NO_KEYRING` | don't use the OS keyring; secrets go into the client config |
| `CODEX_HOME` | Codex's directory, as Codex uses it (default `~/.codex`) |
| `npm_config_registry`, `TOOLSHED_PYPI_URL` | where to look up package versions |
| `NO_COLOR` | plain output |
