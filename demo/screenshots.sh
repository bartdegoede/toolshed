#!/usr/bin/env bash
# Prepares a sandbox for docs/import.png and docs/import-secrets.png (sourced
# by screenshots.tape): a throwaway HOME whose clients have servers set up by
# hand, and the mock claude/codex/agy CLIs, so nothing real is read or written.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sandbox=/tmp/toolshed-screenshots
rm -rf "$sandbox" && mkdir -p "$sandbox/bin" "$sandbox/home/.codex" "$sandbox/home/.gemini/config"
version="$(cd "$root" && git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')"
(cd "$root" && go build -ldflags "-X main.Version=${version:-dev}" -o "$sandbox/bin/toolshed" ./cmd/toolshed)

REAL_HOME="$HOME"
export HOME="$sandbox/home" CODEX_HOME="" TOOLSHED_HOME="$sandbox/home/.toolshed"
export PATH="$sandbox/bin:$root/test/mocks:$PATH"
# The keyring probe looks for your login keychain under HOME. The screenshots
# stop before importing, so nothing is read from it or written to it.
mkdir -p "$HOME/Library" && ln -s "$REAL_HOME/Library/Keychains" "$HOME/Library/Keychains"
cat > "$HOME/.claude.json" <<'JSON'
{
  "mcpServers": {
    "github-local": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_d3mo0nly7xQ2rT9kLm4vB8nC1", "GITHUB_TOOLSETS": "repos,issues"}
    },
    "sentry": {
      "type": "http",
      "url": "https://mcp.sentry.dev/mcp",
      "headers": {"Authorization": "Bearer sntrys_demo0nly4f8a2c9e1b7d"}
    }
  }
}
JSON
cat > "$HOME/.codex/config.toml" <<'TOML'
[mcp_servers.postgres]
command = "uvx"
args = ["postgres-mcp", "--access-mode=restricted"]
env = { DATABASE_URL = "postgresql://app:hunter2@db.internal/app", PGSSLMODE = "require" }
TOML
cat > "$HOME/.gemini/config/mcp_config.json" <<'JSON'
{
  "mcpServers": {
    "brave-search": {
      "command": "npx",
      "args": ["-y", "@brave/brave-search-mcp-server"],
      "env": {"BRAVE_API_KEY": "BSAdemo0nly9c2f7e4a1d8b3"}
    }
  }
}
JSON
cd "$HOME"
export PS1='❯ '
