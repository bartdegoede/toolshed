#!/usr/bin/env bash
# Contract tests: toolshed against the REAL `claude` and `codex` CLIs (not the
# mocks), in a throwaway HOME and CODEX_HOME. They check that each client
# reads back what toolshed meant to write, using the client's own commands
# (`codex mcp get --json`). Run via `just test-contract`; needs claude, codex,
# jq and python3 on PATH.
set -uo pipefail

TS="${TOOLSHED_BIN:?set TOOLSHED_BIN}"
for c in claude codex jq python3; do command -v "$c" >/dev/null || { echo "contract tests need $c on PATH" >&2; exit 1; }; done
pass=0; fail=0
ok()   { pass=$((pass+1)); printf '  \033[32m✓\033[0m %s\n' "$1"; }
bad()  { fail=$((fail+1)); printf '  \033[31m✗\033[0m %s\n' "$1"; [[ -n "${2:-}" ]] && printf '    %s\n' "$2"; }
check() { # check <desc> <jq filter over $json> (must print true)
    [[ "$(jq -r "$2" <<<"$json" 2>/dev/null)" == "true" ]] && ok "$1" || bad "$1" "$json"
}
section() { printf '\n\033[1m%s\033[0m\n' "$1"; }
cget() { json="$(codex mcp get "$1" --json 2>&1)"; }
printf 'claude %s, %s\n' "$(claude --version)" "$(codex --version)"

section "stdio tool"
"$TS" add notes --no-check --env LEVEL=debug -- sh -c 'exec cat' >/dev/null
"$TS" install notes -y >/dev/null 2>&1 || bad "install notes"
cget notes
check "codex reads command and args" '.transport.command == "sh" and .transport.args == ["-c","exec cat"]'
check "codex reads env" '.transport.env.LEVEL == "debug"'
json="$(jq -c '.mcpServers.notes' "$HOME/.claude.json")"
check "claude has it" '.command == "sh" and .env.LEVEL == "debug"'

section "update keeps the user's Codex settings"
python3 - "$CODEX_HOME/config.toml" <<'PY'
import sys; p=sys.argv[1]; s=open(p).read()
s=s.replace("[mcp_servers.notes]\n", "[mcp_servers.notes]\nstartup_timeout_sec = 45\ndisabled_tools = [\"danger\"]\n", 1)
open(p,"w").write(s)
PY
sed -i.bak 's/LEVEL: debug/LEVEL: info/' "$TOOLSHED_HOME/registry.yaml" && rm -f "$TOOLSHED_HOME/registry.yaml.bak"
"$TS" update notes -y >/dev/null 2>&1 || bad "update notes"
cget notes
check "the change reached codex" '.transport.env.LEVEL == "info"'
check "startup_timeout_sec kept" '.startup_timeout_sec == 45'
check "disabled_tools kept" '.disabled_tools == ["danger"]'

section "remote tool with a headers helper"
"$TS" add hosted --url https://mcp.example.com/mcp --headers-helper 'echo {}' --no-check >/dev/null
"$TS" install hosted -y >/dev/null 2>&1 || bad "install hosted"
cget hosted
check "codex reads url" '.transport.url == "https://mcp.example.com/mcp"'
check "codex reads http_headers_helper (it redacts the value)" '.transport.http_headers_helper != null'
json="$(python3 -c 'import sys,tomllib,json; print(json.dumps(tomllib.load(open(sys.argv[1],"rb"))["mcp_servers"]["hosted"]))' "$CODEX_HOME/config.toml")"
check "with the right command" '.http_headers_helper == "echo {}"'
json="$(jq -c '.mcpServers.hosted' "$HOME/.claude.json")"
check "claude has headersHelper" '.headersHelper == "echo {}"'

section "remote tool with a token header (no keyring: static headers)"
"$TS" add hdr --url https://hdr.example.com/mcp --header 'Authorization=Bearer ${HDR_TOKEN}' --secret HDR_TOKEN >/dev/null
"$TS" install hdr -y --set HDR_TOKEN=t0k >/dev/null 2>&1 || bad "install hdr"
cget hdr
check "codex reads http_headers" '.transport.http_headers != null'
json="$(jq -c '.mcpServers.hdr' "$HOME/.claude.json")"
check "claude has the header" '.headers.Authorization == "Bearer t0k"'
"$TS" remove hdr >/dev/null 2>&1 || bad "remove hdr"

section "project scope"
repo="$(mktemp -d)"; git -C "$repo" init -q
(cd "$repo" && "$TS" install notes -y --project >/dev/null 2>&1) || bad "install --project"
printf '\n[projects."%s"]\ntrust_level = "trusted"\n' "$(cd "$repo" && pwd -P)" >> "$CODEX_HOME/config.toml"
json="$(cd "$repo" && codex mcp list --json 2>&1)"
check "codex sees the project's server once trusted" '[.[] | select(.name == "notes")] | length == 1'
json="$(jq -c '.mcpServers.notes' "$repo/.mcp.json")"
check "claude's .mcp.json has it" '.command == "sh"'

section "server names toolshed used to write"
cat >> "$CODEX_HOME/config.toml" <<'EOF'

[mcp_servers.old_name]
command = "sh"
startup_timeout_sec = 12
EOF
"$TS" add old-name --no-check -- sh -c true >/dev/null
"$TS" update old-name -y >/dev/null 2>&1 || bad "update old-name"
json="$(codex mcp list --json)"
check "renamed to old-name" '[.[] | .name] | (index("old-name") != null) and (index("old_name") == null)'
cget old-name
check "and kept its settings" '.startup_timeout_sec == 12'

section "import keeps Codex-only auth"
cat >> "$CODEX_HOME/config.toml" <<'EOF'

[mcp_servers.byhand]
url = "https://byhand.example.com/mcp"
bearer_token_env_var = "BYHAND_TOKEN"
EOF
"$TS" import byhand -y >/dev/null 2>&1 || bad "import byhand"
grep -q "id: byhand" "$TOOLSHED_HOME/registry.yaml" && ok "imported" || bad "imported"
cget byhand
check "bearer_token_env_var kept" '.transport.bearer_token_env_var == "BYHAND_TOKEN"'

section "uninstall"
"$TS" uninstall notes hosted >/dev/null 2>&1 || bad "uninstall"
json="$(codex mcp list --json)"
check "gone from codex" '[.[] | select(.name == "notes" or .name == "hosted")] | length == 0'
json="$(jq -c '.mcpServers // {}' "$HOME/.claude.json")"
check "gone from claude" 'has("notes") or has("hosted") | not'
python3 -c 'import sys,tomllib; tomllib.load(open(sys.argv[1],"rb"))' "$CODEX_HOME/config.toml" && ok "config.toml still parses" || bad "config.toml still parses"

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[[ $fail -eq 0 ]]
