#!/usr/bin/env bash
# Contract tests: toolshed against the REAL `claude`, `codex` and `agy`
# (Antigravity) CLIs (not the mocks), in a throwaway HOME and CODEX_HOME. They
# check that each client reads back what toolshed meant to write, using the
# client's own commands (`codex mcp get --json`, `agy mcp list`). Run via
# `just test-contract`; needs claude, codex, agy, jq and python3 on PATH.
#
# `agy mcp list` only reads the user's config; that Antigravity's agent also
# loads <repo>/.agents/mcp_config.json, and that the user's server wins over
# the repo's with the same name, was checked by hand in agy 1.5.5's /mcp.
set -uo pipefail

TS="${TOOLSHED_BIN:?set TOOLSHED_BIN}"
for c in claude codex agy jq python3; do command -v "$c" >/dev/null || { echo "contract tests need $c on PATH" >&2; exit 1; }; done
pass=0; fail=0
ok()   { pass=$((pass+1)); printf '  \033[32m✓\033[0m %s\n' "$1"; }
bad()  { fail=$((fail+1)); printf '  \033[31m✗\033[0m %s\n' "$1"; [[ -n "${2:-}" ]] && printf '    %s\n' "$2"; }
check() { # check <desc> <jq filter over $json> (must print true)
    [[ "$(jq -r "$2" <<<"$json" 2>/dev/null)" == "true" ]] && ok "$1" || bad "$1" "$json"
}
section() { printf '\n\033[1m%s\033[0m\n' "$1"; }
cget() { json="$(codex mcp get "$1" --json 2>/dev/null)"; } # stderr: warnings, not JSON
# aget: `agy mcp list` (a table) as {"<name>": {"type", "status", "target"}}.
aget() { json="$(cd "${1:-.}" && agy mcp list 2>&1 | python3 -c '
import sys, json, re
rows = {}
for line in sys.stdin.read().splitlines()[1:]:
    parts = re.split(r"\s{2,}", line.strip(), maxsplit=3)
    if len(parts) == 4:
        rows[parts[0]] = {"type": parts[1], "status": parts[2], "target": parts[3]}
print(json.dumps(rows))')"; }
printf 'claude %s, %s, agy %s\n' "$(claude --version)" "$(codex --version)" "$(agy --version)"

section "stdio tool"
"$TS" add notes --no-check --env LEVEL=debug -- sh -c 'exec cat' >/dev/null
"$TS" install notes -y >/dev/null 2>&1 || bad "install notes"
cget notes
check "codex reads command and args" '.transport.command == "sh" and .transport.args == ["-c","exec cat"]'
check "codex reads env" '.transport.env.LEVEL == "debug"'
json="$(jq -c '.mcpServers.notes' "$HOME/.claude.json")"
check "claude has it" '.command == "sh" and .env.LEVEL == "debug"'
aget
check "antigravity reads it" '.notes == {"type": "stdio", "status": "enabled", "target": "sh -c exec cat"}'

section "update keeps the user's Codex settings"
python3 - "$CODEX_HOME/config.toml" <<'PY'
import sys; p=sys.argv[1]; s=open(p).read()
s=s.replace("[mcp_servers.notes]\n", "[mcp_servers.notes]\nstartup_timeout_sec = 45\ndisabled_tools = [\"danger\"]\n", 1)
open(p,"w").write(s)
PY
agy_file="$HOME/.gemini/config/mcp_config.json"
jq '.mcpServers.notes.timeoutSeconds = 30 | .mcpServers.notes.enabledTools = ["read"]' "$agy_file" > "$agy_file.tmp" && mv "$agy_file.tmp" "$agy_file"
sed -i.bak 's/LEVEL: debug/LEVEL: info/' "$TOOLSHED_HOME/registry.yaml" && rm -f "$TOOLSHED_HOME/registry.yaml.bak"
"$TS" update notes -y >/dev/null 2>&1 || bad "update notes"
cget notes
check "the change reached codex" '.transport.env.LEVEL == "info"'
check "startup_timeout_sec kept" '.startup_timeout_sec == 45'
check "disabled_tools kept" '.disabled_tools == ["danger"]'
json="$(jq -c '.mcpServers.notes' "$agy_file")"
check "antigravity: the change, and the user's settings kept" '.env.LEVEL == "info" and .timeoutSeconds == 30 and .enabledTools == ["read"]'
aget
check "antigravity still reads it" '.notes.status == "enabled"'

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
aget
check "antigravity: skipped (it has no headers helper)" 'has("hosted") | not'

section "remote tool with a token header (no keyring: static headers)"
"$TS" add hdr --url https://hdr.example.com/mcp --header 'Authorization=Bearer ${HDR_TOKEN}' --secret HDR_TOKEN >/dev/null
"$TS" install hdr -y --set HDR_TOKEN=t0k >/dev/null 2>&1 || bad "install hdr"
cget hdr
check "codex reads http_headers" '.transport.http_headers != null'
json="$(jq -c '.mcpServers.hdr' "$HOME/.claude.json")"
check "claude has the header" '.headers.Authorization == "Bearer t0k"'
aget
check "antigravity reads it as http" '.hdr == {"type": "http", "status": "enabled", "target": "https://hdr.example.com/mcp"}'
agy mcp add extra -- true >/dev/null 2>&1 && agy mcp remove extra >/dev/null 2>&1 && ok "agy can still edit the file toolshed wrote" || bad "agy can still edit the file toolshed wrote"
"$TS" remove hdr >/dev/null 2>&1 || bad "remove hdr"

section "project scope"
repo="$(mktemp -d)"; git -C "$repo" init -q
(cd "$repo" && "$TS" install notes -y --project >/dev/null 2>&1) || bad "install --project"
printf '\n[projects."%s"]\ntrust_level = "trusted"\n' "$(cd "$repo" && pwd -P)" >> "$CODEX_HOME/config.toml"
json="$(cd "$repo" && codex mcp list --json 2>/dev/null)"
check "codex sees the project's server once trusted" '[.[] | select(.name == "notes")] | length == 1'
json="$(jq -c '.mcpServers.notes' "$repo/.mcp.json")"
check "claude's .mcp.json has it" '.command == "sh"'

section "changing scope moves it"
# notes was at user scope (with Codex settings from above); --project moved it.
json="$(codex mcp list --json 2>/dev/null)"
check "codex: gone from user scope" '[.[] | select(.name == "notes")] | length == 0'
json="$(cd "$repo" && codex mcp get notes --json 2>/dev/null)"
check "codex: its settings moved with it" '.startup_timeout_sec == 45 and .disabled_tools == ["danger"]'
json="$(jq -c '.mcpServers // {}' "$HOME/.claude.json")"
check "claude: gone from user scope" 'has("notes") | not'
aget
check "antigravity: gone from user scope" 'has("notes") | not'
json="$(jq -c '.mcpServers.notes' "$repo/.agents/mcp_config.json")"
check "antigravity: in the repo's .agents/mcp_config.json, settings and all" '.command == "sh" and .timeoutSeconds == 30'
(cd "$repo" && "$TS" install notes -y --user >/dev/null 2>&1) || bad "install --user"
cget notes
check "codex: back at user scope, settings and all" '.startup_timeout_sec == 45 and .disabled_tools == ["danger"]'
json="$(python3 -c 'import sys,tomllib,json; print(json.dumps(tomllib.load(open(sys.argv[1],"rb")).get("mcp_servers",{})))' "$repo/.codex/config.toml")"
check "codex: gone from the repo" 'has("notes") | not'
json="$(jq -c '.mcpServers // {}' "$repo/.mcp.json")"
check "claude: gone from .mcp.json" 'has("notes") | not'
json="$(jq -c '.mcpServers.notes' "$HOME/.claude.json")"
check "claude: back at user scope" '.command == "sh"'
aget
check "antigravity: back at user scope" '.notes.status == "enabled"'

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

section "a second copy: doctor agrees with each client on which one it uses"
"$TS" add twin --no-check -- sh -c true >/dev/null
for s in project user; do
    (cd "$repo" && claude mcp add-json twin '{"type":"stdio","command":"sh","args":["-c","true"]}' -s "$s" >/dev/null 2>&1) || bad "claude mcp add-json -s $s"
done
printf '\n[mcp_servers.twin]\ncommand = "sh"\nargs = ["-c", "true"]\nstartup_timeout_sec = 11\n' >> "$CODEX_HOME/config.toml"
printf '\n[mcp_servers.twin]\ncommand = "sh"\nargs = ["-c", "true"]\nstartup_timeout_sec = 22\n' >> "$repo/.codex/config.toml"
claude_uses="$(cd "$repo" && claude mcp get twin 2>/dev/null | sed -n 's/^ *Scope: \([A-Za-z]*\) config.*/\1/p' | tr A-Z a-z)"
codex_uses="$(cd "$repo" && codex mcp get twin --json 2>/dev/null | jq -r 'if .startup_timeout_sec == 22 then "project" else "user" end')"
json="$(cd "$repo" && "$TS" doctor --no-start --json 2>/dev/null)"
check "claude uses the $claude_uses one, and doctor says so" "[.tools[] | select(.id == \"twin\") | .findings[].text | select(startswith(\"Claude Code also has it\") and endswith(\"uses the $claude_uses one here\"))] | length == 1"
check "codex uses the $codex_uses one, and doctor says so" "[.tools[] | select(.id == \"twin\") | .findings[].text | select(startswith(\"Codex also has it\") and endswith(\"uses the $codex_uses one here\"))] | length == 1"
fixes="$(jq -r '.tools[] | select(.id == "twin") | .findings[].fix | select(startswith("toolshed uninstall"))' <<<"$json")"
while read -r fix; do
    (cd "$repo" && "$TS" ${fix#toolshed } >/dev/null 2>&1) || bad "$fix"
done <<<"$fixes"
json="$(cd "$repo" && claude mcp list 2>&1 | jq -Rs .)"
check "claude no longer warns about it" 'contains("\"twin\" is defined in multiple scopes") | not'
json="$(python3 -c 'import sys,tomllib,json; print(json.dumps([("twin" in tomllib.load(open(p,"rb")).get("mcp_servers",{})) for p in sys.argv[1:]]))' "$CODEX_HOME/config.toml" "$repo/.codex/config.toml")"
check "codex has one copy left" 'map(select(.)) | length == 1'
json="$(cd "$repo" && "$TS" doctor --no-start --json 2>/dev/null)"
check "doctor is quiet about it" '[.tools[] | select(.id == "twin") | (.findings // [])[].text | select(contains("also has it"))] | length == 0'
"$TS" remove twin >/dev/null 2>&1 || true

section "uninstall"
"$TS" uninstall notes hosted >/dev/null 2>&1 || bad "uninstall"
json="$(codex mcp list --json)"
check "gone from codex" '[.[] | select(.name == "notes" or .name == "hosted")] | length == 0'
json="$(jq -c '.mcpServers // {}' "$HOME/.claude.json")"
check "gone from claude" 'has("notes") or has("hosted") | not'
aget
check "gone from antigravity" 'has("notes") | not'
python3 -c 'import sys,tomllib; tomllib.load(open(sys.argv[1],"rb"))' "$CODEX_HOME/config.toml" && ok "config.toml still parses" || bad "config.toml still parses"

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[[ $fail -eq 0 ]]
