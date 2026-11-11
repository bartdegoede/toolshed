# Getting started

<!-- summary: Add a server, check it works, install it -->

Run `toolshed` and you get a list of your tools, plus a few built-in
examples. The keys:

| Key | |
| --- | --- |
| `↑` `↓` | move |
| `a` | add a tool |
| `t` | start the server and list the tools it offers |
| `i` / `enter` | install (or approve, for a tool your agent added) |
| `u` | uninstall |
| `d` | delete one of your tools |
| `m` | import the servers your clients already have |
| `c` | choose the clients tools go into |
| `q` | quit |

Adding a tool asks what kind it is (an npm or Python package, a command, or a
remote URL) and what to call it. toolshed then starts it and lists its tools,
so you know it works before your agent tries it. The first install asks
which of your coding agents should get tools (every one on your `PATH` is
ticked) and remembers it; press `c` to change it. Installing asks for any
values the tool needs and where to install it: for every project, or just
this repository. Installing a tool again starts from where it is now, in
each client, so accepting the form updates it in place.

## From the command line

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

More kinds of tools:

```sh
toolshed add fetch --uvx mcp-server-fetch                        # Python package
toolshed add docs  --url https://mcp.context7.com/mcp            # remote server
toolshed add jira  --secret JIRA_TOKEN -- jira-mcp --site acme   # any command, with a secret
toolshed add acme  --url https://mcp.acme.com/mcp \
  --header 'Authorization=Bearer ${ACME_TOKEN}' --secret ACME_TOKEN   # remote server with a token
```

Secrets are asked for at install, at a hidden prompt, and go to your keychain
(see [Secrets](secrets.md)). npm and Python packages are pinned to their
current release (see [Updates and versions](updates.md)).

## Next

- Already have servers set up by hand? [Import](import.md) them.
- Let your agent add servers itself: [Your agent](agents.md).
- Share a set of tools with your team: [The registry](registry.md).
