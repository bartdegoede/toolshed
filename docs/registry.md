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
    args: [-y, "@acme/jira-mcp@1.4.2"]   # pinned: see "Package versions" below
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
| `headers` | Remote only. HTTP headers to send; `${KEY}` is replaced by the value of input `KEY` (see "Remote servers with a token" below). |
| `headers_helper` | Remote only. A shell command that prints a JSON object of HTTP headers. Claude Code (`headersHelper`) and Codex (`http_headers_helper`) run it when they connect, so tokens are never stored. Set `headers` or `headers_helper`, not both. |
| `env` | Command servers only. Environment variables always set for the server. |
| `inputs` | Values prompted for at install. Command servers get them as environment variables; remote servers use them in `headers`. `secret` ones are stored in the OS keyring; others go into the client config. `default` pre-fills a value; `optional` allows skipping it. |
| `inputs[].share` | Keyring group to share a secret between tools (e.g. two Atlassian tools sharing one token). Defaults to the tool's ID. |
| `clients` | Install into only `claude` and/or `codex`. Default is both. |
| `pending` | Set by `toolshed mcp` on tools your agent adds. toolshed won't start or install the tool until you approve it (`toolshed approve <id>`, or `i` in the TUI), which removes the key. |

Unknown keys are errors, so a typo like `comand:` fails loudly instead of
being ignored. Check a file with `toolshed validate path/to/registry.yaml`.

## Remote servers with a token

A remote server that wants an API token gets it in a header:

```sh
toolshed add acme --url https://mcp.acme.com/mcp \
  --header 'Authorization=Bearer ${ACME_TOKEN}' --secret ACME_TOKEN
```

(In the TUI, answer "yes" to "Does it need an API token?".) At install you
enter the token, and it goes to your keyring. Claude Code and Codex are both
registered with a headers helper, `toolshed headers acme …`, which they run
when they connect: it reads the token from the keyring and prints the
headers. The token is never in their config files. Without a keyring the
headers are written into the config as they are (Claude Code `headers`, Codex
`http_headers`).

A literal token in a header (`--header 'Authorization=Bearer ghp_…'`) is
refused, because it would be stored in your registry in plain text; the same
goes for your agent's `add_server`.

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

To replace a secret later (a rotated API token, say), run
`toolshed secret set <id>`. Installed tools read the keyring each time they
start, so there's nothing to reinstall. The value is typed at a hidden
prompt, or piped in (`pbpaste | toolshed secret set jira`); it's never an
argument, where it would end up in your shell history.

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

## When the registry changes under an installed tool

A registry can change after you installed a tool from it: a teammate edits a
shared registry (`TOOLSHED_REGISTRY`), you edit your own, or a toolshed
release updates a built-in example. Re-installing would then give that
tool's keyring secrets to a command you haven't looked at.

So `toolshed install`, `toolshed update` and the TUI compare the registry
with what Claude Code and Codex actually run: the command and its arguments,
the fixed `env`, which keyring entries the secrets come from, and a remote
tool's URL and `headers_helper`. If any of those changed, they show you the
change and ask before re-registering. Without a terminal they refuse unless
you pass `-y`. Your agent can't accept a change for you: `toolshed mcp`
won't start or re-install a changed tool, and tells the agent to send you to
`toolshed update <id>`.

A shared registry is still trusted for tools you haven't installed yet:
whoever can change it decides what a first `toolshed install` runs.

## Importing servers you already have

`toolshed import` looks for servers in `~/.claude.json` (user scope, plus
the current repository's local and `.mcp.json` servers) and in Codex's
`config.toml` that aren't in your registry yet. For each one it:

1. shows the command (or URL) and asks whether to import it;
2. lists its environment variables and asks which hold secrets, starting
   from a guess based on the name (`*TOKEN*`, `*SECRET*`, `*PASSWORD*`,
   `*API_KEY*`, `*_KEY`, …);
3. adds it to your registry, with every variable as an input. The registry
   never gets the values, so it stays safe to share;
4. installs it again where it was: secrets go to the keyring and the server
   is registered through `toolshed exec`, so the plain-text token leaves the
   client's config. Other values stay in the client's config.

`-y` imports everything with the guessed secrets, `--dry-run` only shows what
it would do, and `--no-install` only adds to the registry. `toolshed import
<name>` imports one server. In the TUI, `m` opens the same list: pick a
server, tick which values are secrets (the guesses are ticked already), and
press enter.

Names that aren't valid ids (`My_Server`) are imported as one (`my-server`),
and the old name is removed. A server that Claude Code and Codex run
differently under the same name is imported from Claude Code, and Codex's is
left alone. Remote servers that send fixed headers (`Authorization: Bearer …`, `X-Api-Key`)
get those headers as inputs, and the ones that look like credentials move to
the keyring, like env vars do. Servers that use the old SSE transport are
skipped. A server
registered through toolshed whose entry you deleted is imported with its
secrets where they already are in the keyring.

Imported packages aren't pinned (toolshed keeps exactly what was running);
`toolshed doctor` points them out, and `toolshed upgrade` pins them.

## Checking your tools

`toolshed doctor` checks every installed tool and says how to fix what it
finds:

- the registry entry changed since it was installed (fix: `toolshed update <id>`);
- it's registered with a toolshed binary that's gone, e.g. after switching
  from `go install` to Homebrew (`toolshed update <id>`);
- its command isn't on `PATH`, or a secret is missing from the keyring
  (`toolshed install <id>`);
- an npm or PyPI package isn't pinned (a warning: `toolshed upgrade <id>`);
- it doesn't start or list its tools.

It also lists servers registered through toolshed that are no longer in your
registry, and how many other servers Claude Code and Codex have that toolshed
doesn't manage. `--no-start` skips starting the servers, which is much faster
with npx and uvx tools. It exits with 1 if anything is broken (warnings don't
count), so it works in scripts, and `--json` prints the report.

## Package versions

`npx -y some-server` runs whatever was published last, every time your agent
starts it, with the tool's secrets in its environment. So `toolshed add`
(and the TUI, and your agent through `toolshed mcp`) pins npm and PyPI
packages to their newest release: `@acme/jira-mcp@1.4.2`,
`mcp-server-fetch==2026.11.4`. Pass `--no-pin` to keep a package unpinned.

`toolshed upgrade` looks up the newest release of every npx and uvx tool
in your registry, moves its pin, and re-installs it where it's installed.
`toolshed upgrade <id>` does one tool, and `--dry-run` only lists what's
out of date. The built-in examples are pinned too, and move with toolshed
releases.

npm lookups use `npm_config_registry` when it's set, as npm does. For PyPI
mirrors, set `TOOLSHED_PYPI_URL` to one that serves PyPI's JSON API.

## Install scope

`toolshed install <id>` installs for your user, so the tool is available
in every project. `--project` (or choosing "this repo" in the TUI) writes it
into the repository instead, so it can be committed and shared: `.mcp.json`
for Claude Code and `.codex/config.toml` for Codex. Codex only reads a
project's config once you've trusted the project (start `codex` in it and
accept); toolshed tells you when it isn't trusted yet. Tools with secrets are
registered there as plain `toolshed exec …` (found on `PATH`) rather than
your own toolshed path, so they work for teammates who installed toolshed
somewhere else. `toolshed update` keeps each tool's scope in each client.

## How toolshed writes each client's config

Claude Code registrations go through `claude mcp add-json`. Settings
toolshed doesn't manage (a `timeout`, say) are carried over when it
re-registers a tool.

Codex registrations are written to its `config.toml` directly, because
`codex mcp add` resets a server's whole table, so every update would lose
your settings for it, and it can't set a headers helper. toolshed only
touches the keys it manages in `[mcp_servers.<id>]` (`command`, `args`,
`env`, or `url`, `http_headers_helper`, `http_headers`), and leaves
everything else as it was: comments, `startup_timeout_sec`,
`enabled_tools` / `disabled_tools`, per-tool approvals, other servers. It
checks the result parses before replacing the file. Older toolshed versions
registered `my-tool` as `my_tool` in Codex; the next update renames it and
keeps its settings.
