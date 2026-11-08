# The registry

Every tool in your toolshed is an entry in a YAML file. Most of the time you
never touch it, because `toolshed add` and the TUI write it for you. This page
is for when you want to edit it by hand (`toolshed edit`) or share a
registry with your team.

## Where tools come from

| | Path | Notes |
| --- | --- | --- |
| **Your tools** | `~/.toolshed/registry.yaml` | Written by `toolshed add`. Override the path with `$TOOLSHED_REGISTRY`. |
| **Examples** | [`registry.yaml`](../registry.yaml) in this repo | Compiled into the binary. |

Your tools are listed first and win if an ID matches an example. To hide the
examples entirely, put `examples: false` at the top of your file.

Sharing a registry with a team is just sharing a file: commit it somewhere
and point `TOOLSHED_REGISTRY` at it.

## Entry reference

```yaml
tools:
  - id: jira                      # required: lowercase, digits, dashes
    name: Jira                    # shown in the TUI (defaults to id)
    description: |                # first line is the summary
      Search and update Jira issues.

    # A local server (stdio): a command on PATH, or an absolute path…
    command: npx
    args: [-y, "@acme/jira-mcp"]
    env:                          # fixed environment variables
      JIRA_SITE: acme.atlassian.net
    inputs:                       # values asked for at install time
      - key: JIRA_TOKEN
        label: API token
        help: Create one at https://id.atlassian.com/manage-profile/security/api-tokens
        secret: true              # → OS keyring, never a config file
      - key: JIRA_PROJECT
        default: OPS
        optional: true

    clients: [claude, codex]      # optional: restrict where it installs
```

…or a remote server:

```yaml
  - id: github
    url: https://api.githubcopilot.com/mcp/
    headers_helper: >-            # optional, Claude Code only
      echo "{\"Authorization\": \"Bearer $(gh auth token)\"}"
```

| Key | Meaning |
| --- | --- |
| `command`, `args` | A local MCP server speaking stdio. toolshed checks `command` is on PATH before installing. |
| `url` | A remote MCP server (streamable HTTP). Set either `command` or `url`, not both. |
| `headers_helper` | Remote only. A shell command that prints a JSON object of HTTP headers. Claude Code runs it on every connect, so tokens are never stored. Codex has no equivalent, so these tools install into Claude Code only. |
| `env` | Environment variables always set for the server. |
| `inputs` | Environment variables prompted for at install. `secret` ones are stored in the OS keyring; others go into the client config. `default` pre-fills a value; `optional` allows skipping it. |
| `inputs[].share` | Keyring group to share a secret between tools (e.g. two Atlassian tools sharing one token). Defaults to the tool's ID. |
| `clients` | Install into only `claude` and/or `codex`. Default is both. |
| `pending` | Set by `toolshed mcp` on tools your agent adds. toolshed won't start or install the tool until you approve it (`toolshed approve <id>`, or `i` in the TUI), which removes the key. |

Unknown keys are errors, so a typo like `comand:` fails loudly instead of
being ignored. Check a file with `toolshed validate path/to/registry.yaml`.

## How secrets work

When a tool has `secret` inputs, toolshed:

1. stores each value in the OS keyring (macOS Keychain or Linux Secret
   Service) under service `toolshed`, account `<id>/<KEY>`;
2. registers the server with Claude Code or Codex as `toolshed exec <id>`,
   with no secret in the config;
3. when the client starts the server, `toolshed exec` reads the keyring, adds
   the values to the environment, and replaces itself with the real server
   process.

Without a usable keyring (CI, SSH-only boxes, or `TOOLSHED_NO_KEYRING=1`),
secrets fall back to plain env vars in the client config. toolshed refuses to
do that for a `--project` install, because `.mcp.json` is meant to be
committed.

What the keyring protects against: tokens sitting in plain text in dotfiles,
backups and screen shares, and being committed by accident. It doesn't
isolate secrets from other programs running as you: on macOS, toolshed
stores them through the `security` tool, so any process of yours can read
them back.

## Tools your agent adds

`toolshed mcp` lets your agent add tools, and a tool is a command toolshed
will run. So anything the agent adds is saved with `pending: true`, and
toolshed won't start it (not even to list its tools) or install it until you
approve it. `toolshed approve <id>` shows you exactly what it will run first.
The agent also can't replace a tool that's already in your registry, because
the replacement would inherit that tool's secrets.

A shared registry (`TOOLSHED_REGISTRY`) is trusted the same way as your own
file: whoever can change it decides what `toolshed install` and
`toolshed update` run.

## Install scope

`toolshed install <id>` installs for your user, so the tool is available
in every project. `--project` (or choosing "this repo" in the TUI) writes it
to the repository's `.mcp.json` instead, so it can be committed and shared.
This is Claude Code only; Codex has no project scope. Tools with secrets are
registered there as plain `toolshed exec …` (found on `PATH`) rather than
your own toolshed path, so they work for teammates who installed toolshed
somewhere else. `toolshed update` keeps each tool's scope.
