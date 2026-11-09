# Security

toolshed runs commands on your machine and keeps API tokens in your keychain,
so security reports are very welcome.

## Reporting a vulnerability

Please report it privately through
[GitHub's private vulnerability reporting](https://github.com/bartdegoede/toolshed/security/advisories/new),
not in a public issue. Include what you did, what happened, and the toolshed
version (`toolshed --version`).

This is a personal project, so there's no formal response time, but security
reports come first: expect a reply within a week. Fixes go out as a patch
release, and versions with a serious problem are retracted in `go.mod` (as
v0.1.0 and v0.1.1 were) so `go install …@latest` skips them.

## Supported versions

Only the latest release gets fixes. Upgrade with `brew upgrade toolshed` or
`go install github.com/bartdegoede/toolshed/cmd/toolshed@latest`.

## What's in scope

The things toolshed promises, for example:

- secrets ending up somewhere other than the OS keyring (a config file, the
  registry, a log, the agent's chat) while a keyring is available;
- a tool getting another tool's secrets, or a command running with a tool's
  secrets that you didn't install or approve;
- an agent using `toolshed mcp` to run, install or change something without
  your approval;
- toolshed damaging or leaking the contents of Claude Code's or Codex's
  config.

Out of scope: what an MCP server does once you've approved and installed it,
and other programs running as your user reading the keychain (see
[Secrets](docs/secrets.md#what-it-protects-against)).
