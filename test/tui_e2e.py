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


def agy_has(name):
    try:
        with open(os.path.join(HOME, ".gemini", "config", "mcp_config.json")) as f:
            return name in json.load(f).get("mcpServers", {})
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
    KEYS = {"enter": b"\r", "left": b"\x1b[D", "down": b"\x1b[B", "up": b"\x1b[A", "space": b" ", "esc": b"\x1b"}

    def __init__(self, cwd=None):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            if cwd:
                os.chdir(cwd)
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

# Install it. The first install asks which clients to use (every one on
# PATH ticked): untick Antigravity. Then nothing else to ask (no inputs,
# not a git repo).
SETTINGS = os.path.join(os.environ["TOOLSHED_HOME"], "config.yaml")


def settings():
    try:
        with open(SETTINGS) as f:
            return f.read()
    except FileNotFoundError:
        return ""


tui.press("i")
if tui.screen_has("Install tools into"):
    ok("first install: asks which clients to use")
    tui.press("down", "down", "space", "enter")
else:
    bad("first install: asks which clients to use", tui.text()[-300:])
wait_for("install: registered with Claude Code", lambda: "agentic" in claude_servers())
wait_for("install: registered with Codex", lambda: codex_has("agentic"))
check("install: not in Antigravity, which was unticked", not agy_has("agentic"))
check("first install: the choice is saved", "- claude" in settings() and "antigravity" not in settings(), settings())

# Uninstall it.
tui.press("u", "left", "enter")
wait_for("uninstall: gone from Claude Code", lambda: "agentic" not in claude_servers())
wait_for("uninstall: gone from Codex", lambda: not codex_has("agentic"))
check("uninstall: still in the registry", "id: agentic" in registry())

# c changes the choice: tick Antigravity again.
tui.press("c")
if tui.screen_has("Install tools into"):
    tui.press("down", "down", "space", "enter")
    wait_for("clients: Antigravity chosen again", lambda: "- antigravity" in settings())
else:
    bad("clients: c opens the choice", tui.text()[-300:])

# Delete the other one (installed): it's uninstalled and removed.
tui.press("down", "d", "left", "enter")
wait_for("delete: gone from the registry", lambda: "id: bye" not in registry())
wait_for("delete: uninstalled from Claude Code", lambda: "bye" not in claude_servers())
wait_for("delete: uninstalled from Antigravity", lambda: not agy_has("bye"))

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

# In a git repo, "This repo only" works for a tool only Codex can run, too.
# The repo is below HOME so its .codex/config.toml isn't ~/.codex's.
repo = os.path.join(HOME, "repo")
sh("git", "init", "-q", repo)
with open(REGISTRY, "a") as f:
    f.write("  - id: cxonly\n    command: ping-mcp\n    clients: [codex]\n")
tui = TUI(cwd=repo)
tui.screen_has("YOUR TOOLS")
tui.press("down")  # agentic, byhand, cxonly: the new tool is last
tui.press("down")
tui.screen_has("Codex only")
tui.press("i")
if tui.screen_has("Install for"):
    ok("this repo: the form asks where, for a Codex-only tool")
    tui.press("down", "enter")
else:
    bad("this repo: the form asks where, for a Codex-only tool", tui.text()[-300:])


def repo_codex_has(name):
    try:
        with open(os.path.join(repo, ".codex", "config.toml")) as f:
            return f"[mcp_servers.{name}]" in f.read()
    except FileNotFoundError:
        return False


wait_for("this repo: in the repo's .codex/config.toml", lambda: repo_codex_has("cxonly"))
check("this repo: not in ~/.codex/config.toml", not codex_has("cxonly"))

# Reinstalling it and accepting the default keeps it in the repo, without
# adding a copy to ~/.codex/config.toml.
tui.out = b""
tui.press("i")
if tui.screen_has("Install for"):
    tui.press("enter")
    wait_for("reinstall: done", lambda: "Installed cxonly" in tui.text())
else:
    bad("reinstall: the form asks where", tui.text()[-300:])
check("reinstall: still in the repo's .codex/config.toml", repo_codex_has("cxonly"))
check("reinstall: no copy in ~/.codex/config.toml", not codex_has("cxonly"))

# Choosing "All projects" instead moves it out of the repo.
tui.out = b""
tui.press("i")
if tui.screen_has("Install for"):
    tui.press("up", "enter")
    wait_for("move: in ~/.codex/config.toml", lambda: codex_has("cxonly"))
    check("move: gone from the repo's .codex/config.toml", not repo_codex_has("cxonly"))
else:
    bad("move: the form asks where", tui.text()[-300:])
check("q quits", tui.quit())

print(f"\n{passed} passed, {failed} failed")
sys.exit(1 if failed else 0)
