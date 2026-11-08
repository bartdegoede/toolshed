package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
	"github.com/bartdegoede/toolshed/internal/shed"
)

// runSecret manages a tool's secrets. `secret set <id> [KEY]` replaces one
// (a rotated API token, say) without reinstalling: installed tools read the
// keyring each time they start. The value is typed at a hidden prompt, or
// read from stdin when it isn't a terminal (`pbpaste | toolshed secret set
// jira`), never taken as an argument where it would land in shell history.
func runSecret(args []string) error {
	if len(args) == 0 || args[0] != "set" {
		return usagef("secret needs a subcommand: set")
	}
	fs := flag.NewFlagSet("secret set", flag.ContinueOnError)
	pos, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	if len(pos) < 1 || len(pos) > 2 {
		return usagef("secret set needs a tool id and, if it has several secrets, which one")
	}
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	e, err := lookup(reg, pos[0])
	if err != nil {
		return err
	}
	in, err := pickSecret(e, pos[1:])
	if err != nil {
		return err
	}
	value, err := readSecret(e, in)
	if err != nil {
		return err
	}
	if value == "" {
		return fmt.Errorf("no value given; %s is unchanged", in.Key)
	}

	inst := clients.Detect()[e.ID]
	if !secrets.Available() {
		// No keyring: the value lives in the clients' config, so re-register.
		if inst == nil {
			return fmt.Errorf("there's no OS keyring, so secrets are stored when you install: run `toolshed install %s`", e.ID)
		}
		scope := inst.ClaudeScope
		if scope == "" {
			scope = "user"
		}
		opts := shed.InstallOptions{Scope: scope, Clients: inst.Clients()}
		interactive := term.IsTerminal(int(os.Stdin.Fd()))
		return installOne(e, inst, shed.Values{in.Key: value}, opts, false, interactive)
	}
	group := e.SecretGroup(in)
	if err := secrets.Set(group, in.Key, value); err != nil {
		return err
	}
	fmt.Printf("%s Saved %s for %s\n", green("✓"), bold(in.Key), bold(e.ID))
	if others := sharing(reg, e, group, in.Key); len(others) > 0 {
		fmt.Printf("  %s\n", dim("Also used by "+strings.Join(others, ", ")+" (they share it)."))
	}
	if inst != nil {
		fmt.Println(dim("  It's used the next time Claude Code or Codex starts the server (in Claude Code: /mcp)."))
	}
	return nil
}

func pickSecret(e *registry.Entry, key []string) (registry.Input, error) {
	var ins []registry.Input
	for _, in := range e.Inputs {
		if in.Secret {
			ins = append(ins, in)
		}
	}
	if len(key) == 1 {
		for _, in := range ins {
			if in.Key == key[0] {
				return in, nil
			}
		}
		return registry.Input{}, fmt.Errorf("%s has no secret %s", e.ID, key[0])
	}
	switch len(ins) {
	case 0:
		return registry.Input{}, fmt.Errorf("%s has no secrets", e.ID)
	case 1:
		return ins[0], nil
	}
	return registry.Input{}, usagef("%s has several secrets; say which: %s", e.ID, strings.Join(e.SecretKeys(), ", "))
}

func readSecret(e *registry.Entry, in registry.Input) (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return prompt(e, in)
	}
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("reading %s from stdin: %w", in.Key, err)
	}
	return strings.TrimSpace(line), nil
}

// sharing lists the other tools that read the same keyring entry.
func sharing(reg *registry.Registry, self *registry.Entry, group, key string) []string {
	var out []string
	for _, e := range reg.Entries {
		if e.ID == self.ID {
			continue
		}
		for _, in := range e.Inputs {
			if in.Secret && in.Key == key && e.SecretGroup(in) == group {
				out = append(out, e.ID)
			}
		}
	}
	return out
}
