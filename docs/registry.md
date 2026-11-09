# The registry

<!-- summary: Your tools in one YAML file, and every key -->

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

## Sharing with a team

Sharing a registry with a team is just sharing a file: commit it somewhere
and point `TOOLSHED_REGISTRY` at it. Everyone runs `toolshed install` and
enters their own tokens. If someone later changes what a tool in that file
runs, toolshed shows you the change before it re-installs the tool with your
token (see [Updates and versions](updates.md#when-the-registry-changes)).

A shared registry is trusted for tools you haven't installed yet: whoever
can change it decides what a first `toolshed install` runs.

## Entry reference

```yaml
tools:
  - id: jira                      # required: lowercase, digits, dashes
    name: Jira                    # shown in the TUI (defaults to id)
    description: |                # first line is the summary
      Search and update Jira issues.

    # A local server (stdio): a command on PATH, or an absolute path…
    command: npx
    args: [-y, "@acme/jira-mcp@1.4.2"]   # pinned: see updates.md
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

…or a remote server, with a token from your keyring:

```yaml
  - id: acme
    url: https://mcp.acme.com/mcp
    headers:                      # ${KEY} is replaced by input KEY
      Authorization: Bearer ${ACME_TOKEN}
    inputs:
      - key: ACME_TOKEN
        secret: true
```

…or with headers from a command you already have:

```yaml
  - id: github
    url: https://api.githubcopilot.com/mcp/
    headers_helper: >-
      echo "{\"Authorization\": \"Bearer $(gh auth token)\"}"
```

| Key | Meaning |
| --- | --- |
| `command`, `args` | A local MCP server speaking stdio. toolshed checks `command` is on PATH before installing. |
| `url` | A remote MCP server (streamable HTTP). Set either `command` or `url`, not both. |
| `headers` | Remote only. HTTP headers to send; `${KEY}` is replaced by the value of input `KEY` (see [Secrets](secrets.md#remote-servers-with-a-token)). |
| `headers_helper` | Remote only. A shell command that prints a JSON object of HTTP headers. Claude Code (`headersHelper`) and Codex (`http_headers_helper`) run it when they connect, so tokens are never stored. Set `headers` or `headers_helper`, not both. |
| `env` | Command servers only. Environment variables always set for the server. |
| `inputs` | Values prompted for at install. Command servers get them as environment variables; remote servers use them in `headers`. `secret` ones are stored in the OS keyring; others go into the client config. `default` pre-fills a value; `optional` allows skipping it. |
| `inputs[].share` | Keyring group to share a secret between tools (e.g. two Atlassian tools sharing one token). Defaults to the tool's ID. |
| `clients` | Install into only `claude` and/or `codex`. Default is both. |
| `pending` | Set by `toolshed mcp` on tools your agent adds. toolshed won't start or install the tool until you approve it (`toolshed approve <id>`, or `i` in the TUI), which removes the key. |

Unknown keys are errors, so a typo like `comand:` fails loudly instead of
being ignored. Check a file with `toolshed validate path/to/registry.yaml`.
