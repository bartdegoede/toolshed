#!/usr/bin/env bash
# End-to-end tests: the real toolshed binary and the real ping-mcp example
# server, against mock `claude` / `codex` CLIs (test/mocks) in a throwaway
# HOME. Run via `just test-e2e`.
set -uo pipefail

TS="${TOOLSHED_BIN:?set TOOLSHED_BIN}"
pass=0; fail=0
ok()   { pass=$((pass+1)); printf '  \033[32m✓\033[0m %s\n' "$1"; }
bad()  { fail=$((fail+1)); printf '  \033[31m✗\033[0m %s\n' "$1"; [[ -n "${2:-}" ]] && printf '    %s\n' "$2"; }
section() { printf '\n\033[1m%s\033[0m\n' "$1"; }
expect() { # expect <desc> <exit-code> <cmd...>
    local desc="$1" want="$2"; shift 2
    out="$("$@" 2>&1)"; local got=$?
    [[ $got -eq $want ]] && ok "$desc" || bad "$desc (exit $got, want $want)" "$out"
}
has() { grep -q -- "$2" <<<"$1" && ok "$3" || bad "$3" "$1"; }
claude_cfg() { jq -c --arg n "$1" '.mcpServers[$n] // empty' "$HOME/.claude.json" 2>/dev/null; }
codex_has() { grep -q "^\[mcp_servers.$1\]" "$HOME/.codex/config.toml" 2>/dev/null; }

section "basics"
expect "--version" 0 "$TS" --version
expect "--help" 0 "$TS" --help
expect "unknown command exits 2" 2 "$TS" frobnicate
expect "list on a fresh machine" 0 "$TS" list
has "$out" "Examples" "built-in examples are listed"

section "add"
expect "add a command tool (no inputs → live check)" 0 "$TS" add hi -- ping-mcp
has "$out" "offers 1 tool" "add starts the server and finds its tools"
expect "add a tool with a secret" 0 "$TS" add greet --name Greeter --secret NTFY_TOPIC --env NTFY_SERVER=https://ntfy.example.com -- ping-mcp
expect "add a remote tool" 0 "$TS" add docs --url https://docs.example.com/mcp --no-check
expect "add rejects a bad id" 1 "$TS" add Bad_Id -- ping-mcp
expect "add rejects two kinds at once" 2 "$TS" add both --npx a --url https://x
expect "registry file validates" 0 "$TS" validate
reg="$(cat "$TOOLSHED_HOME/registry.yaml")"
has "$reg" "NTFY_TOPIC" "secret input written to the registry"
has "$reg" "Add tools with" "registry file has its explanatory header"

section "tools (live MCP handshake)"
expect "tools hi" 0 "$TS" tools hi
has "$out" "notify" "lists the server's tools"

section "install"
expect "install without a required secret fails under -y" 1 "$TS" install greet -y
expect "install greet" 0 "$TS" install greet -y --set NTFY_TOPIC=abc123
has "$out" "claude, codex" "installed into both clients"
cfg="$(claude_cfg greet)"
has "$cfg" '"command":"ping-mcp"' "claude: runs ping-mcp"
has "$cfg" '"NTFY_TOPIC":"abc123"' "claude: secret in env when the keyring is disabled"
has "$cfg" '"NTFY_SERVER":"https://ntfy.example.com"' "claude: fixed env"
codex_has greet && ok "codex: registered" || bad "codex: registered"
expect "install --set for an unknown input fails" 1 "$TS" install hi -y --set NOPE=1
expect "install a remote tool" 0 "$TS" install docs -y
has "$(claude_cfg docs)" '"type":"http"' "claude: remote server registered as http"
grep -q 'url = "https://docs.example.com/mcp"' "$HOME/.codex/config.toml" && ok "codex: remote url" || bad "codex: remote url"
expect "headers_helper tools are Claude-only" 0 "$TS" install github -y
codex_has github && bad "github should not be in codex" || ok "github skipped for codex"
expect "--client codex on a Claude-only tool fails" 1 "$TS" install github -y --client codex
expect "install an unknown tool fails" 1 "$TS" install nope -y

section "list"
expect "list" 0 "$TS" list
has "$out" "Your tools" "user tools section"
expect "list --json" 0 "$TS" list --json
[[ "$(jq -r '.tools[] | select(.id=="greet") | .clients | join(",")' <<<"$out")" == "claude,codex" ]] && ok "json shows greet in both clients" || bad "json clients" "$out"

section "project scope"
repo="$(mktemp -d)"; git -C "$repo" init -q
( cd "$repo" && "$TS" install hi -y --project >/dev/null 2>&1 )
jq -e '.mcpServers.hi' "$repo/.mcp.json" >/dev/null 2>&1 && ok "--project writes .mcp.json" || bad "--project writes .mcp.json"
out="$(cd "$repo" && "$TS" list)"; has "$out" "(project)" "list shows the project scope"
norepo="$(mktemp -d)"; pushd "$norepo" >/dev/null; expect "--project outside a repo fails" 1 "$TS" install hi -y --project; popd >/dev/null

section "update keeps earlier values"
expect "update greet" 0 "$TS" update greet
has "$(claude_cfg greet)" '"NTFY_TOPIC":"abc123"' "value survived update"

section "exec"
resp="$( (printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}'; sleep 1) | "$TS" exec greet -- ping-mcp 2>&1)"
has "$resp" '"name":"ping-mcp"' "exec hands off to the real server"
expect "exec without a command is a usage error" 2 "$TS" exec docs

section "toolshed mcp"
init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}'
resp="$( (printf '%s\n' "$init" '{"jsonrpc":"2.0","method":"notifications/initialized"}' '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'; sleep 1) | "$TS" mcp 2>&1)"
has "$resp" '"add_server"' "toolshed mcp lists its tools"
has "$resp" '"install_server"' "including install_server"

section "uninstall / remove"
expect "uninstall greet" 0 "$TS" uninstall greet
[[ -z "$(claude_cfg greet)" ]] && ok "gone from claude" || bad "gone from claude"
codex_has greet && bad "gone from codex" || ok "gone from codex"
expect "remove a built-in example fails" 1 "$TS" remove ping
expect "remove docs (installed → uninstalls first)" 0 "$TS" remove docs
[[ -z "$(claude_cfg docs)" ]] && ok "removed tool was uninstalled" || bad "removed tool was uninstalled"
grep -q "id: docs" "$TOOLSHED_HOME/registry.yaml" && bad "docs still in registry" || ok "docs deleted from registry"

section "broken registry"
echo "tools: [{id: x, comand: y}]" > "$TOOLSHED_HOME/registry.yaml"
expect "commands fail loudly on a broken registry" 1 "$TS" list
has "$out" "comand" "error names the bad field"

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[[ $fail -eq 0 ]]
