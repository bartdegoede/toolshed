# Secrets

<!-- summary: Tokens in your keychain, not in config files -->

When a tool needs a secret, such as an API token, toolshed asks for it when
you install the tool, at a hidden prompt, and keeps it in your OS keyring:
the macOS Keychain, or the Secret Service on Linux. It's stored under service
`toolshed`, account `<id>/<KEY>`.

## Command servers

A server that gets its token from an environment variable is registered with
Claude Code and Codex as a wrapper, not as the server itself:

```sh
toolshed exec jira --secret JIRA_TOKEN=jira -- jira-mcp --site acme
```

That line is all the client's config contains. When the client starts the
server, `toolshed exec` reads the secrets from the keyring, adds them to the
environment, and then replaces itself with the real server (the `exec`
system call). The client talks to the server directly, with nothing in
between. `toolshed exec` doesn't read your registry: everything it needs is
in its arguments, so an installed tool keeps working if you move the
registry.

## Remote servers with a token

A remote server that wants an API token gets it in a header:

```sh
toolshed add acme --url https://mcp.acme.com/mcp \
  --header 'Authorization=Bearer ${ACME_TOKEN}' --secret ACME_TOKEN
```

In the TUI, answer "yes" to "Does it need an API token?". At install you
enter the token, and it goes to your keyring. Claude Code (`headersHelper`)
and Codex (`http_headers_helper`) are both registered with a headers helper,
`toolshed headers acme …`, which they run when they connect: it reads the
token from the keyring and prints the headers. The token is never in their
config files.

A literal token in a header (`--header 'Authorization=Bearer ghp_…'`) is
refused, because it would be stored in your registry in plain text. The
same goes for your agent's `add_server`.

## Replacing a secret

```sh
toolshed secret set jira
```

Installed tools read the keyring each time they start, so a rotated token
needs nothing else. The value is typed at a hidden prompt, or piped in
(`pbpaste | toolshed secret set jira`); it's never an argument, where it
would end up in your shell history. Tools that share a secret (`share:` in
the registry) all get the new value.

## Without a keyring

On CI, SSH-only machines, or with `TOOLSHED_NO_KEYRING=1`, there's no
keyring, and secrets go into the client's config as plain values (Claude
Code `env` / `headers`, Codex `env` / `http_headers`). toolshed refuses to do
that for a `--project` install, because a project's config is meant to be
committed.

## What it protects against

The keyring keeps tokens out of dotfiles, backups, screen shares and
accidental commits. It doesn't isolate them from other programs running as
you: on macOS, toolshed stores them through the `security` tool, so any
process of yours can read them back.
