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
codex_cfg() { # codex_cfg <name> [file]: the server's table as JSON (via python's tomllib)
    python3 -c 'import sys,tomllib,json; print(json.dumps(tomllib.load(open(sys.argv[2],"rb")).get("mcp_servers",{}).get(sys.argv[1],{}),sort_keys=True,separators=(",",":")))' "$1" "${2:-$HOME/.codex/config.toml}" 2>/dev/null; }

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
expect "install a remote tool with a headers helper" 0 "$TS" install github -y
has "$(claude_cfg github)" '"headersHelper":"echo.*gh auth token' "claude: headersHelper"
has "$(codex_cfg github)" '"http_headers_helper":"echo.*gh auth token' "codex: http_headers_helper"
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
has "$(codex_cfg hi "$repo/.codex/config.toml")" '"command":"ping-mcp"' "--project writes .codex/config.toml"
out="$(cd "$repo" && "$TS" install hi -y --project 2>&1)"; has "$out" "trust the project" "and says Codex needs the project trusted"
out="$(cd "$repo" && "$TS" list)"; has "$out" "(project)" "list shows the project scope"
norepo="$(mktemp -d)"; pushd "$norepo" >/dev/null; expect "--project outside a repo fails" 1 "$TS" install hi -y --project; popd >/dev/null

section "update keeps earlier values"
# The user's own settings for greet in both clients.
python3 - "$HOME/.codex/config.toml" <<'PY'
import sys; p=sys.argv[1]; s=open(p).read()
s=s.replace("[mcp_servers.greet]\n", "[mcp_servers.greet]\nstartup_timeout_sec = 45 # mine\n", 1)
s+="\n[mcp_servers.greet.tools.notify]\napproval_mode = \"approve\"\n"
open(p,"w").write(s)
PY
jq '.mcpServers.greet.timeout = 9000' "$HOME/.claude.json" > "$HOME/.claude.tmp" && mv "$HOME/.claude.tmp" "$HOME/.claude.json"
expect "update greet" 0 "$TS" update greet
has "$(claude_cfg greet)" '"NTFY_TOPIC":"abc123"' "value survived update"
has "$(claude_cfg greet)" '"timeout":9000' "claude: the user's timeout survived update"
gcfg="$(codex_cfg greet)"
has "$gcfg" '"startup_timeout_sec":45' "codex: the user's timeout survived update"
has "$gcfg" '"approval_mode":"approve"' "codex: per-tool approval survived update"
grep -q "# mine" "$HOME/.codex/config.toml" && ok "codex: comments survived update" || bad "codex: comments survived update"

section "registry changes under an installed tool"
# Like a teammate editing a shared registry: what greet runs changes.
sed -i.bak 's|https://ntfy.example.com|https://evil.example.com|' "$TOOLSHED_HOME/registry.yaml" && rm -f "$TOOLSHED_HOME/registry.yaml.bak"
expect "update refuses an unreviewed change without a terminal" 1 "$TS" update greet </dev/null
has "$out" "NTFY_SERVER=https://evil.example.com" "and shows what changed"
has "$(claude_cfg greet)" 'https://ntfy.example.com' "the installed tool was left alone"
expect "update -y accepts it" 0 "$TS" update greet -y
has "$(claude_cfg greet)" 'https://evil.example.com' "and re-registers it"
expect "update with nothing changed" 0 "$TS" update greet </dev/null

section "doctor"
expect "doctor --no-start on a healthy setup" 0 "$TS" doctor --no-start
has "$out" "greet" "lists installed tools"
expect "doctor starts the servers (the example docs URL doesn't exist, so it fails)" 1 "$TS" doctor
has "$out" "greet .*1 tool" "greet starts and offers its tool"
has "$out" "doesn't start" "docs is reported as not starting"
sed -i.bak 's|https://evil.example.com|https://ntfy.example.com|' "$TOOLSHED_HOME/registry.yaml" && rm -f "$TOOLSHED_HOME/registry.yaml.bak"
expect "doctor flags a registry change" 1 "$TS" doctor --no-start
has "$out" "toolshed update greet" "and says how to fix it"
json="$("$TS" doctor --no-start --json 2>/dev/null)"
[[ "$(jq -r '.tools[] | select(.id=="greet") | .findings[0].level' <<<"$json")" == "error" ]] && ok "doctor --json has the finding" || bad "doctor --json has the finding" "$json"
expect "fix it" 0 "$TS" update greet -y

section "secret set"
out="$(printf 'rotated\n' | "$TS" secret set greet 2>&1)" && ok "secret set reads the value from stdin" || bad "secret set reads the value from stdin" "$out"
has "$(claude_cfg greet)" '"NTFY_TOPIC":"rotated"' "without a keyring it re-registers with the new value"
expect "secret set on a tool without secrets" 1 "$TS" secret set hi
expect "completion zsh" 0 "$TS" completion zsh
has "$out" "compdef _toolshed toolshed" "prints a zsh script"
ids="$("$TS" __ids)"; has "$ids" "greet" "__ids lists tool ids for completion"

section "exec"
resp="$( (printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}'; sleep 1) | "$TS" exec greet -- ping-mcp 2>&1)"
has "$resp" '"name":"ping-mcp"' "exec hands off to the real server"
expect "exec without a command is a usage error" 2 "$TS" exec docs

section "toolshed mcp"
init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}'
resp="$( (printf '%s\n' "$init" '{"jsonrpc":"2.0","method":"notifications/initialized"}' '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'; sleep 1) | "$TS" mcp 2>&1)"
has "$resp" '"add_server"' "toolshed mcp lists its tools"
has "$resp" '"install_server"' "including install_server"

section "agent-added tools need approval"
# What add_server writes: a user entry marked pending.
cat >> "$TOOLSHED_HOME/registry.yaml" <<'EOF'
  - id: agentic
    command: ping-mcp
    pending: true
EOF
expect "tools refuses a pending tool" 1 "$TS" tools agentic
has "$out" "toolshed approve agentic" "and says how to approve it"
expect "install refuses a pending tool" 1 "$TS" install agentic -y
[[ -z "$(claude_cfg agentic)" ]] && ok "nothing was registered" || bad "nothing was registered"
expect "approve without a terminal needs -y" 1 "$TS" approve agentic </dev/null
expect "approve -y" 0 "$TS" approve agentic -y
has "$out" "ping-mcp" "approve shows what will run"
expect "install after approval" 0 "$TS" install agentic -y
[[ -n "$(claude_cfg agentic)" ]] && ok "registered after approval" || bad "registered after approval"
expect "add refuses to replace a tool without --force" 1 "$TS" add agentic --no-check -- sh
expect "remove agentic" 0 "$TS" remove agentic

section "import"
# A server set up by hand in both clients, with its token in plain text.
claude mcp add-json byhand '{"type":"stdio","command":"ping-mcp","env":{"NTFY_TOPIC":"t1","NTFY_SERVER":"https://n.example"}}' -s user >/dev/null
printf '\n[mcp_servers.byhand]\ncommand = "ping-mcp"\nenv = { NTFY_TOPIC = "t1", NTFY_SERVER = "https://n.example" }\n' >> "$HOME/.codex/config.toml"
expect "doctor points at import" 0 "$TS" doctor --no-start
has "$out" "toolshed import" "for servers toolshed doesn't manage"
expect "import --dry-run" 0 "$TS" import --dry-run
has "$out" "byhand" "lists the server"
grep -q "id: byhand" "$TOOLSHED_HOME/registry.yaml" && bad "--dry-run changed the registry" || ok "--dry-run changed nothing"
expect "import without a terminal needs -y" 1 "$TS" import </dev/null
expect "import -y" 0 "$TS" import byhand -y
reg="$(cat "$TOOLSHED_HOME/registry.yaml")"
has "$reg" "id: byhand" "added to the registry"
grep -q "n.example" "$TOOLSHED_HOME/registry.yaml" && bad "no values in the registry" "$reg" || ok "no values in the registry"
has "$(claude_cfg byhand)" '"NTFY_SERVER":"https://n.example"' "values stay in the client config"
expect "imported tool is healthy" 0 "$TS" doctor --no-start
expect "import again: nothing left" 0 "$TS" import -y
has "$out" "Nothing to import" "says so"
expect "remove byhand" 0 "$TS" remove byhand

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
