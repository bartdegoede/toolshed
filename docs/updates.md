# Updates and versions

<!-- summary: Pinned packages, upgrades, and registry changes -->

## Package versions

`npx -y some-server` runs whatever was published last, every time your agent
starts it, with the tool's secrets in its environment. So toolshed pins npm
and PyPI packages to their newest release when you add them:
`@acme/jira-mcp@1.4.2`, `mcp-server-fetch==2026.11.4`. Pass `--no-pin` to
keep a package unpinned.

```sh
toolshed upgrade --dry-run    # what's out of date
toolshed upgrade              # move every pin to the newest release
toolshed upgrade jira         # just one
```

`upgrade` moves the pin in your registry and installs the tool again where
it's installed. The built-in examples are pinned too, and move with toolshed
releases.

npm lookups use `npm_config_registry` when it's set, as npm does. For PyPI
mirrors, set `TOOLSHED_PYPI_URL` to one that serves PyPI's JSON API.

## When the registry changes

A registry can change after you installed a tool from it: a teammate edits a
shared registry, you edit your own, or a toolshed release updates a built-in
example. Installing again would give that tool's keyring secrets to a
command you haven't looked at.

So `toolshed install`, `toolshed update` and the TUI compare the registry
with what Claude Code and Codex actually run: the command and its arguments,
the fixed `env`, which keyring entries the secrets come from, and a remote
tool's URL and auth headers. If any of those changed, they show you the
change and ask before installing again. Without a terminal they refuse
unless you pass `-y`, and your agent can't accept a change for you.

```sh
toolshed update        # every installed tool
toolshed update jira   # one
```
