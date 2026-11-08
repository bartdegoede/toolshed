// Package clients registers MCP servers with Claude Code and Codex, and
// reads back what each client currently has configured.
//
// Registration always goes through the clients' own CLIs (`claude mcp
// add-json`, `codex mcp add`) so toolshed never writes their config formats
// itself. Detection reads the config files directly because it's ~1000x
// faster than `claude mcp list`, which health-checks every server.
package clients

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

const (
	Claude = "claude"
	Codex  = "codex"
)

// All lists supported clients in display order.
var All = []string{Claude, Codex}

// Config is a server registration, in Claude Code's add-json shape.
type Config struct {
	Type          string            `json:"type"` // "stdio" or "http"
	Command       string            `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	URL           string            `json:"url,omitempty"`
	HeadersHelper string            `json:"headersHelper,omitempty"`
}

// lookPath is swapped in tests.
var lookPath = exec.LookPath

// Available reports whether a client's CLI is on PATH.
func Available(client string) bool {
	_, err := lookPath(client)
	return err == nil
}

// AvailableClients returns the installed clients, in display order.
func AvailableClients() []string {
	var out []string
	for _, c := range All {
		if Available(c) {
			out = append(out, c)
		}
	}
	return out
}

// codexName converts a server name to Codex's form; Codex config keys are
// TOML bare keys, so toolshed registers hyphens as underscores.
func codexName(name string) string { return strings.ReplaceAll(name, "-", "_") }

// Register adds (or replaces) a server in one client. scope applies to
// Claude only ("user", "local" or "project"); Codex registrations are always
// global.
//
// Replacing means remove-then-add, since neither CLI has an "update". If the
// add fails, the previous registration is put back, so a failed update never
// leaves the tool uninstalled.
func Register(client, name string, cfg Config, scope string) error {
	prev := previous(client, name, scope)
	err := register(client, name, cfg, scope)
	if err == nil || prev == nil {
		return err
	}
	if rerr := register(client, name, *prev, scope); rerr != nil {
		return fmt.Errorf("%w (restoring the previous registration also failed: %v)", err, rerr)
	}
	return fmt.Errorf("%w (kept the previous registration)", err)
}

// previous returns the current registration of name in client/scope, if any.
func previous(client, name, scope string) *Config {
	inst := Detect()[name]
	switch {
	case inst == nil:
		return nil
	case client == Claude && inst.ClaudeScope == scope:
		return &inst.Config
	case client == Codex && inst.Codex:
		return &inst.CodexConfig
	}
	return nil
}

func register(client, name string, cfg Config, scope string) error {
	switch client {
	case Claude:
		data, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		_ = run("claude", "mcp", "remove", name, "-s", scope) // replace, not duplicate
		return run("claude", "mcp", "add-json", name, string(data), "-s", scope)
	case Codex:
		_ = run("codex", "mcp", "remove", codexName(name))
		args := []string{"mcp", "add", codexName(name)}
		if cfg.Type == "http" {
			args = append(args, "--url", cfg.URL)
		} else {
			keys := make([]string, 0, len(cfg.Env))
			for k := range cfg.Env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				args = append(args, "--env", k+"="+cfg.Env[k])
			}
			args = append(append(args, "--", cfg.Command), cfg.Args...)
		}
		return run("codex", args...)
	}
	return fmt.Errorf("unknown client %q", client)
}

// Unregister removes a server from one client.
func Unregister(client, name, scope string) error {
	switch client {
	case Claude:
		return run("claude", "mcp", "remove", name, "-s", scope)
	case Codex:
		return run("codex", "mcp", "remove", codexName(name))
	}
	return fmt.Errorf("unknown client %q", client)
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("%s %s: %w", name, args[0], err)
		}
		return errors.New(msg)
	}
	return nil
}
