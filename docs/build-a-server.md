# Build your own server

<!-- summary: A complete MCP server in about 110 lines of Go -->

Some tools you want your agent to use don't have an MCP server, internal
tools in particular. Writing one is less work than it sounds:
[`examples/ping-mcp`](../examples/ping-mcp) is a complete MCP server in about
110 lines of Go that sends push notifications to your phone through
[ntfy](https://ntfy.sh). "Run the migration and ping me when it's done."

```sh
go install github.com/bartdegoede/toolshed/examples/ping-mcp@latest
toolshed install ping     # it's one of the built-in examples
```

Copy it, swap in your own tools, and add it:

```sh
toolshed add mine -- my-server
```
