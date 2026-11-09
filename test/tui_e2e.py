#!/usr/bin/env python3
"""End-to-end tests for the TUI: the real toolshed binary in a pseudo-terminal,
driven with key presses, against the mock claude/codex CLIs in a throwaway
HOME (set up by `just test-e2e`).

Every step checks what the keys *did* (the registry, Claude Code's and Codex's
config), not just what's on screen: the TUI's confirm forms once ignored every
answer while drawing perfectly. Standard library only.
"""
import json
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import time
import termios
import fcntl

TS = os.environ["TOOLSHED_BIN"]
HOME = os.environ["HOME"]
REGISTRY = os.path.join(os.environ["TOOLSHED_HOME"], "registry.yaml")
CODEX = os.path.join(HOME, ".codex", "config.toml")

passed = failed = 0


def ok(desc):
    global passed
    passed += 1
    print(f"  \033[32m✓\033[0m {desc}")


def bad(desc, detail=""):
    global failed
    failed += 1
    print(f"  \033[31m✗\033[0m {desc}")
    if detail:
        print(f"    {detail}")


def check(desc, cond, detail=""):
    if cond:
        ok(desc)
    else:
        bad(desc, detail)


def claude_servers():
    try:
        with open(os.path.join(HOME, ".claude.json")) as f:
            return json.load(f).get("mcpServers", {})
    except FileNotFoundError:
        return {}


def codex_has(name):
    try:
        with open(CODEX) as f:
            return f"[mcp_servers.{name}]" in f.read()
    except FileNotFoundError:
        return False


def registry():
    with open(REGISTRY) as f:
        return f.read()


def wait_for(desc, check, timeout=15):
    """Pass when check() becomes true within timeout seconds."""
    end = time.time() + timeout
    while time.time() < end:
        if check():
            ok(desc)
            return True
        tui.pump(0.1)
    bad(desc, f"not true after {timeout}s; screen tail: {tui.text()[-300:]!r}")
    return False


class TUI:
    KEYS = {"enter": b"\r", "left": b"\x1b[D", "down": b"\x1b[B", "space": b" ", "esc": b"\x1b"}

    def __init__(self):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.execvpe(TS, [TS], dict(os.environ, TERM="xterm-256color", NO_COLOR=""))
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 140, 0, 0))
        self.out = b""

    def pump(self, seconds):
        end = time.time() + seconds
        while time.time() < end:
            r, _, _ = select.select([self.fd], [], [], 0.05)
            if r:
                try:
                    self.out += os.read(self.fd, 65536)
                except OSError:
                    return

    def text(self):
        return re.sub(r"\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07|\x1b[=>()][0-9A-B]?", "", self.out.decode(errors="replace"))

    def screen_has(self, s, timeout=10):
        end = time.time() + timeout
        while time.time() < end:
            if s in self.text():
                return True
            self.pump(0.1)
        return False

    def press(self, *keys):
        for k in keys:
            os.write(self.fd, self.KEYS.get(k) or k.encode())
            self.pump(0.4)

    def quit(self):
        self.press("q")
        for _ in range(30):
            pid, _ = os.waitpid(self.pid, os.WNOHANG)
            if pid:
                return True
            time.sleep(0.1)
        os.kill(self.pid, signal.SIGKILL)
        return False


def sh(*args):
    subprocess.run(args, check=True, capture_output=True)


# A registry with a tool an agent added (pending) and one to delete, the
# latter installed; and a server set up by hand in Claude Code, to import.
with open(REGISTRY, "w") as f:
    f.write("tools:\n"
            "  - id: agentic\n    command: ping-mcp\n    pending: true\n"
            "  - id: bye\n    command: ping-mcp\n")
sh(TS, "install", "bye", "-y")
sh("claude", "mcp", "add-json", "byhand",
   json.dumps({"type": "stdio", "command": "ping-mcp", "env": {"API_TOKEN": "sk-live-abc123", "LEVEL": "debug"}}),
   "-s", "user")

print("\n\033[1mtui (real binary in a pty)\033[0m")
tui = TUI()
if not tui.screen_has("YOUR TOOLS"):
    bad("the TUI starts", tui.text()[-300:])
    sys.exit(1)
ok("the TUI starts")

# Approve the agent's tool: the dialog defaults to No, so enter alone must not.
tui.press("i")
tui.screen_has("Approve")
tui.press("enter")
tui.pump(1)
check("approve: enter on the default No leaves it pending", "pending: true" in registry())
tui.press("i", "left", "enter")
wait_for("approve: Yes clears pending in the registry", lambda: "pending: true" not in registry())

# Install it (nothing to ask: no inputs, not a git repo).
tui.press("i")
wait_for("install: registered with Claude Code", lambda: "agentic" in claude_servers())
wait_for("install: registered with Codex", lambda: codex_has("agentic"))

# Uninstall it.
tui.press("u", "left", "enter")
wait_for("uninstall: gone from Claude Code", lambda: "agentic" not in claude_servers())
wait_for("uninstall: gone from Codex", lambda: not codex_has("agentic"))
check("uninstall: still in the registry", "id: agentic" in registry())

# Delete the other one (installed): it's uninstalled and removed.
tui.press("down", "d", "left", "enter")
wait_for("delete: gone from the registry", lambda: "id: bye" not in registry())
wait_for("delete: uninstalled from Claude Code", lambda: "bye" not in claude_servers())

# Import the hand-made server, unticking API_TOKEN (ticked by its name).
tui.press("m")
tui.screen_has("NOT IN YOUR TOOLSHED")
tui.press("enter")
tui.screen_has("Which of these are secrets")
tui.press("space", "enter")
wait_for("import: added to the registry", lambda: "id: byhand" in registry())
reg = registry()
block = reg[reg.index("id: byhand"):]
check("import: the unticked value isn't a secret", "secret: true" not in block, block)

check("q quits", tui.quit())

print(f"\n{passed} passed, {failed} failed")
sys.exit(1 if failed else 0)
