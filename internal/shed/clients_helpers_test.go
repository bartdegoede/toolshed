package shed

import "github.com/bartdegoede/toolshed/internal/clients"

// inClaude is a server registered in Claude Code at user scope.
func inClaude(cfg clients.Config) *clients.Installation {
	return &clients.Installation{Regs: map[string]clients.Registration{clients.Claude: {Scope: "user", Config: cfg}}}
}

// inBoth is a server registered in Claude Code and Codex at user scope.
func inBoth(claude, codex clients.Config) *clients.Installation {
	return &clients.Installation{Regs: map[string]clients.Registration{
		clients.Claude: {Scope: "user", Config: claude},
		clients.Codex:  {Scope: "user", Config: codex},
	}}
}
