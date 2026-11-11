# Doctor

<!-- summary: Check that every installed tool works -->

```sh
toolshed doctor
```

checks every installed tool and says how to fix what it finds:

| Finding | Fix |
| --- | --- |
| the registry entry changed since it was installed | `toolshed update <id>` |
| it's registered with a toolshed binary that's gone, e.g. after switching from `go install` to Homebrew | `toolshed update <id>` |
| its command isn't on `PATH` | install the command |
| a secret is missing from the keyring | `toolshed install <id>` |
| an npm or PyPI package isn't pinned (a warning) | `toolshed upgrade <id>` |
| a client has it at two scopes, and ignores one of them here (a warning) | `toolshed uninstall <id> --client <client> --scope <scope>` |
| it doesn't start, or doesn't list its tools | `toolshed tools <id>` to see why |

A tool at two scopes in one client is either a leftover (older versions of
toolshed added a second copy when you installed a tool again at another
scope) or deliberate: a repository's shared `.mcp.json` next to your own copy for
every project. The client uses only one of them here (see
[Scope](clients.md#scope)), and doctor says which, and whether the other runs
something different. The fix it suggests removes the copy that's ignored
here; to keep that one instead, remove the other with `--scope`.

It also lists servers registered through toolshed that are no longer in your
registry (`toolshed import <name>` brings them back), and how many other
servers Claude Code and Codex have that toolshed doesn't manage (see
[Import](import.md)).

`--no-start` skips starting the servers, which is much faster with npm and
Python packages. It exits with 1 if anything is broken (warnings don't
count), so it works in scripts, and `--json` prints the report.
