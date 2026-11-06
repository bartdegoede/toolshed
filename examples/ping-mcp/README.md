# ping-mcp

Push notifications from your agent to your phone, via [ntfy](https://ntfy.sh).

> "Run the full test suite and ping me when it's done."

1. Install the ntfy app ([iOS](https://apps.apple.com/app/ntfy/id1625396347),
   [Android](https://play.google.com/store/apps/details?id=io.heckel.ntfy))
   and subscribe to a long, random topic name, e.g. `bart-7f3k2q9x`.
2. Install the server and give toolshed that topic. It's stored in your OS
   keyring, because anyone who knows it can message you.

```sh
go install github.com/bartdegoede/toolshed/examples/ping-mcp@latest
toolshed install ping
```

| Variable | |
| --- | --- |
| `NTFY_TOPIC` | Topic to publish to (secret) |
| `NTFY_SERVER` | Self-hosted ntfy URL (default `https://ntfy.sh`) |
| `NTFY_TOKEN` | Access token for protected topics |

## As a template

This is about 120 lines of Go on the
[official MCP SDK](https://github.com/modelcontextprotocol/go-sdk): one tool,
config from environment variables, errors returned to the agent as tool
errors. Copy the directory to start your own server. It never touches the
keyring itself, because toolshed injects secrets into the environment when
it starts the server.
