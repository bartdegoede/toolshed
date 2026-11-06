# Render with: just demo   (needs vhs, uvx, npx and a logged-in Claude Code)
Output docs/demo.gif

Set Shell bash
Set FontSize 17
Set Width 1280
Set Height 760
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 35ms
Set CursorBlink false

Hide
Type "bash --norc --noprofile" Enter
Type "shopt -s expand_aliases; source demo/setup.sh && clear" Enter
Sleep 5s
Show

# 1. Install an example: the secret is typed masked and kept in the Keychain
Type "toolshed" Sleep 300ms Enter
Sleep 2s
Down Sleep 1s
Type "i" Sleep 1s
Type "toolshed-demo-x8k2p4q7" Sleep 500ms Enter
Sleep 600ms Down Sleep 600ms Enter
Sleep 2.5s

# 2. Add your own tool in a few keystrokes: toolshed starts it and lists its tools
Type "a" Sleep 1s
Down Sleep 500ms Enter Sleep 400ms
Type "mcp-server-time" Sleep 500ms Enter
Sleep 400ms Enter
Sleep 300ms Enter
Sleep 300ms Enter
Sleep 300ms Enter
Sleep 5s
Type "i" Sleep 1s Down Sleep 500ms Enter
Sleep 3s
Type "q" Sleep 500ms

# 3. Claude Code's config has no secret in it
Type "jq -c .mcpServers.ping .mcp.json" Sleep 300ms Enter
Sleep 3s

# 4. The agent uses the new tool
Type `claude -p "Find the TODOs in this repo and ping my phone with a one-line summary." --allowedTools mcp__ping Grep Glob Read` Sleep 400ms Enter
Hide
Sleep 45s
Show
Sleep 5s

# 5. ...and adds tools to itself, through toolshed
Type `claude -p "Add the Playwright MCP server with toolshed, check that it starts, and install it for this repo. Two sentences." --allowedTools mcp__toolshed` Sleep 400ms Enter
Hide
Sleep 45s
Show
Sleep 5s
# 6. Back in toolshed: the agent's additions, with their tools live from the server
Type "toolshed" Sleep 300ms Enter
Sleep 2s
Down Sleep 800ms
Type "t"
Sleep 7s
