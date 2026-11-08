package shed

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

// Changes describes how an installed tool's registry entry differs from what
// Claude Code and Codex currently run for it: a different command or URL,
// different fixed environment, or secrets from a different keyring entry.
//
// It exists because a registry can change underneath an installed tool (a
// teammate edits a shared registry, a toolshed release updates an example),
// and re-installing would then hand the tool's keyring secrets to a command
// the user never looked at. Callers show these changes and ask first.
//
// Values the user typed (non-secret inputs) and toolshed's own path aren't
// compared: those change for harmless reasons.
func Changes(e *registry.Entry, inst *clients.Installation) []string {
	if inst == nil {
		return nil
	}
	var out []string
	for _, cfg := range registrations(inst) {
		for _, c := range changes(e, cfg) {
			if !slices.Contains(out, c) {
				out = append(out, c)
			}
		}
	}
	return out
}

func changes(e *registry.Entry, cfg clients.Config) []string {
	was := describe(cfg)
	if e.IsRemote() {
		var out []string
		if cfg.URL != e.URL {
			out = append(out, fmt.Sprintf("connects to %s (was: %s)", e.URL, was))
		}
		if cfg.URL != "" && cfg.HeadersHelper != e.HeaderCmd && e.SupportsClient(clients.Claude) {
			out = append(out, fmt.Sprintf("gets its auth headers from %s (was: %s)", orNothing(e.HeaderCmd), orNothing(cfg.HeadersHelper)))
		}
		return out
	}
	if cfg.URL != "" {
		return []string{fmt.Sprintf("runs %s (was: %s)", e.CommandLine(), was)}
	}
	var out []string
	cmd, refs, wrapped := Unwrap(cfg)
	if !slices.Equal(cmd, append([]string{e.Command}, e.Args...)) {
		out = append(out, fmt.Sprintf("runs %s (was: %s)", e.CommandLine(), was))
	}
	for _, k := range sortedKeys(e.Env) {
		if old, ok := cfg.Env[k]; !ok {
			out = append(out, fmt.Sprintf("sets %s=%s (wasn't set)", k, e.Env[k]))
		} else if old != e.Env[k] {
			out = append(out, fmt.Sprintf("sets %s=%s (was: %s)", k, e.Env[k], old))
		}
	}
	if wrapped && secrets.Available() {
		for _, in := range e.Inputs {
			if !in.Secret {
				continue
			}
			group := e.SecretGroup(in)
			switch old, ok := refs[in.Key]; {
			case !ok:
				out = append(out, fmt.Sprintf("gets the secret %s from your keyring (%s)", in.Key, group))
			case old != group:
				out = append(out, fmt.Sprintf("gets %s from %s's keyring entry (was: %s's)", in.Key, group, old))
			}
		}
	}
	return out
}

// Unwrap returns the command a registration really runs. Tools with secrets
// are registered as `toolshed exec <id> --secret KEY=group ... -- <command>`;
// for those it returns the command after "--", the secrets it gets (key →
// keyring group) and wrapped=true.
func Unwrap(cfg clients.Config) (cmd []string, refs map[string]string, wrapped bool) {
	if filepath.Base(cfg.Command) != "toolshed" || len(cfg.Args) < 2 || cfg.Args[0] != "exec" {
		return append([]string{cfg.Command}, cfg.Args...), nil, false
	}
	refs = map[string]string{}
	args := cfg.Args[2:] // after "exec <id>"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--":
			return args[i+1:], refs, true
		case "--secret":
			if i+1 < len(args) {
				k, g, _ := strings.Cut(args[i+1], "=")
				refs[k] = g
				i++
			}
		}
	}
	return nil, refs, true
}

// describe is a one-line version of a registration, for "was: …".
func describe(cfg clients.Config) string {
	if cfg.URL != "" {
		return cfg.URL
	}
	cmd, _, _ := Unwrap(cfg)
	if len(cmd) == 0 {
		return "nothing"
	}
	e := registry.Entry{Command: cmd[0], Args: cmd[1:]}
	return e.CommandLine()
}

func orNothing(s string) string {
	if s == "" {
		return "nothing"
	}
	return s
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
