#!/usr/bin/env bash
# Prepares a clean sandbox for recording the demo (sourced by demo.tape):
# fresh binaries, a throwaway git repo, and project-scoped installs only, so
# your global Claude Code / Codex config is never touched.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sandbox=/tmp/toolshed-demo
export DEMO_TOPIC=toolshed-demo-x8k2p4q7   # public ntfy topic used by the recording
rm -rf "$sandbox" && mkdir -p "$sandbox/bin" "$sandbox/acme-api/.claude" "$sandbox/acme-api/src"
(cd "$root" && go build -o "$sandbox/bin/toolshed" ./cmd/toolshed)
(cd "$root/examples/ping-mcp" && go build -o "$sandbox/bin/ping-mcp" .)
security delete-generic-password -s toolshed -a ping/NTFY_TOPIC >/dev/null 2>&1 || true

cd "$sandbox/acme-api"
git init -q
cat > src/billing.go <<'GO'
package billing

// TODO: retry failed charges with exponential backoff
func Charge(cents int) error { return nil }

// TODO: send the receipt email once the template is approved
func Receipt(id string) {}
GO
cat > src/auth.go <<'GO'
package auth

// TODO: rotate session keys every 24h
func Login(user, pass string) bool { return false }
GO
# Let `claude -p` use project servers without the interactive approval prompt.
echo '{"enableAllProjectMcpServers": true}' > .claude/settings.local.json

export PATH="$sandbox/bin:$PATH"
export TOOLSHED_HOME="$sandbox/home"
toolshed install toolshed --project -y >/dev/null   # let Claude manage its own tools
export PS1='\[\e[38;5;214m\]acme-api\[\e[0m\] \[\e[38;5;245m\]❯\[\e[0m\] '
# Only the sandbox's servers, not the recorder's personal ones.
alias claude='claude --strict-mcp-config --mcp-config .mcp.json'
