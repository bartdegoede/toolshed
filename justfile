# toolshed

bin := "toolshed"

# Show available commands
default:
    @just --list --unsorted

# Build ./toolshed
[group('dev')]
build version="dev":
    go build -ldflags "-X main.Version={{version}}" -o {{bin}} ./cmd/toolshed

# Build and run toolshed (pass args after the recipe name)
[group('dev')]
run *ARGS: build
    ./{{bin}} {{ARGS}}

# Install toolshed and the ping-mcp example into $GOBIN
[group('dev')]
install:
    go install ./cmd/toolshed
    cd examples/ping-mcp && go install .

# Unit tests (race detector on) for toolshed and the example
[group('test')]
test:
    go test -race ./...
    cd examples/ping-mcp && go test -race ./...

# Lint (golangci-lint v2)
[group('test')]
lint:
    golangci-lint run ./...

# E2E tests: real binary + real ping-mcp against mock claude/codex in a temp HOME
[group('test')]
test-e2e: build
    #!/usr/bin/env bash
    set -euo pipefail
    root="{{justfile_directory()}}"
    tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
    mkdir -p "$tmp/home" "$tmp/bin"
    (cd "$root/examples/ping-mcp" && go build -o "$tmp/bin/ping-mcp" .)
    export HOME="$tmp/home" TOOLSHED_HOME="$tmp/toolshed" TOOLSHED_NO_KEYRING=1 CODEX_HOME=""
    export TOOLSHED_BIN="$root/{{bin}}" PATH="$root/test/mocks:$tmp/bin:$PATH"
    git config --global user.email e2e@example.com 2>/dev/null || true
    "$root/test/e2e.sh"
    # The TUI, driven with keys in a pty, on a fresh HOME.
    rm -rf "$tmp/home" "$tmp/toolshed" && mkdir -p "$tmp/home" "$tmp/toolshed"
    (cd "$tmp/home" && python3 "$root/test/tui_e2e.py")

# Remove build artifacts
clean:
    rm -rf {{bin}} dist

# Re-record docs/demo.gif (needs vhs, uvx, npx and a logged-in Claude Code)
[group('docs')]
demo:
    vhs demo/demo.tape
    security delete-generic-password -s toolshed -a ping/NTFY_TOPIC >/dev/null 2>&1 || true

# Re-take docs/import.png and docs/import-secrets.png (needs vhs; mock clients, throwaway HOME)
[group('docs')]
screenshots:
    vhs demo/screenshots.tape
    rm -rf /tmp/toolshed-screenshots /tmp/toolshed-screenshots.gif

# Contract tests against the real claude and codex CLIs, in a temp HOME
[group('test')]
test-contract: build
    #!/usr/bin/env bash
    set -euo pipefail
    root="{{justfile_directory()}}"
    tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
    mkdir -p "$tmp/home" "$tmp/codex"
    export HOME="$tmp/home" TOOLSHED_HOME="$tmp/toolshed" TOOLSHED_NO_KEYRING=1 CODEX_HOME="$tmp/codex"
    export TOOLSHED_BIN="$root/{{bin}}"
    cd "$tmp/home"
    "$root/test/contract.sh"
