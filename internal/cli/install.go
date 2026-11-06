package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/shed"
)

func runInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	project := fs.Bool("project", false, "install for this repository only (Claude Code)")
	client := fs.String("client", "", "only install into claude or codex")
	yes := fs.Bool("y", false, "never prompt; take values from --set and the environment")
	var set multi
	fs.Var(&set, "set", "input value KEY=VALUE (repeatable)")
	ids, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return usagef("install needs at least one id")
	}
	overrides, err := keyValues("set", set)
	if err != nil {
		return err
	}
	opts := shed.InstallOptions{Scope: "user"}
	if *project {
		if clients.GitRoot() == "" {
			return fmt.Errorf("--project only works inside a git repository")
		}
		opts.Scope = "project"
	}
	if *client != "" {
		if *client != clients.Claude && *client != clients.Codex {
			return usagef("--client must be claude or codex")
		}
		opts.Clients = []string{*client}
	}
	interactive := !*yes && term.IsTerminal(int(os.Stdin.Fd()))

	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	installed := clients.Detect()
	var failed []string
	for _, id := range ids {
		e, err := lookup(reg, id)
		if err == nil {
			err = installOne(e, installed[id], overrides, opts, interactive)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %s: %v\n", red("✗"), id, err)
			failed = append(failed, id)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d failed", len(failed), len(ids))
	}
	return nil
}

func installOne(e *registry.Entry, inst *clients.Installation, overrides shed.Values, opts shed.InstallOptions, interactive bool) error {
	given := shed.Values{}
	for _, in := range e.Inputs {
		if v, ok := overrides[in.Key]; ok {
			given[in.Key] = v
		} else if v := os.Getenv(in.Key); v != "" && !interactive {
			given[in.Key] = v // automation: honour exported env vars
		}
	}
	for k := range overrides {
		if !hasInput(e, k) {
			return fmt.Errorf("%s has no input %s", e.ID, k)
		}
	}
	vals, missing := shed.Resolve(e, inst, given)
	if interactive {
		for _, in := range e.Inputs {
			if _, ok := vals[in.Key]; ok {
				continue
			}
			v, err := prompt(e, in)
			if err != nil {
				return err
			}
			if v != "" {
				vals[in.Key] = v
			}
		}
		_, missing = shed.Resolve(e, inst, vals)
	}
	if len(missing) > 0 {
		keys := make([]string, len(missing))
		for i, in := range missing {
			keys[i] = in.Key
		}
		return fmt.Errorf("missing %s (pass --set %s=…)", strings.Join(keys, ", "), keys[0])
	}

	done, err := shed.Install(e, vals, opts)
	if len(done) > 0 {
		where := strings.Join(done, ", ")
		if opts.Scope == "project" {
			where += " (this repo)"
		}
		fmt.Printf("%s %s → %s\n", green("✓"), bold(e.ID), where)
	}
	return err
}

func hasInput(e *registry.Entry, key string) bool {
	for _, in := range e.Inputs {
		if in.Key == key {
			return true
		}
	}
	return false
}

var stdin = bufio.NewReader(os.Stdin)

func prompt(e *registry.Entry, in registry.Input) (string, error) {
	label := in.Label
	if label == "" {
		label = in.Key
	}
	if in.Help != "" {
		fmt.Println(dim("  " + in.Help))
	}
	suffix := ""
	if in.Optional {
		suffix = dim(" (optional)")
	}
	fmt.Printf("  %s %s%s: ", e.ID, bold(label), suffix)
	if in.Secret {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return strings.TrimSpace(string(b)), err
	}
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("reading %s: %w", in.Key, err)
	}
	return strings.TrimSpace(line), nil
}

func runUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also delete the tool's secrets from the keyring")
	ids, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return usagef("uninstall needs at least one id")
	}
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	installed := clients.Detect()
	var errs []error
	for _, id := range ids {
		e, err := lookup(reg, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if installed[id] == nil && !*purge {
			fmt.Printf("%s %s isn't installed\n", dim("·"), id)
			continue
		}
		if err := shed.Uninstall(reg, e, installed[id], *purge); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		fmt.Printf("%s Uninstalled %s\n", green("✓"), bold(id))
	}
	return errors.Join(errs...)
}

// runUpdate re-registers installed tools so changes to the registry (or to
// toolshed's own path after an upgrade) reach the clients. Earlier input
// values and the Claude scope are kept.
func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	ids, err := parse(fs, args)
	if err != nil {
		return err
	}
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	installed := clients.Detect()
	if len(ids) == 0 {
		for _, e := range reg.Entries {
			if installed[e.ID] != nil {
				ids = append(ids, e.ID)
			}
		}
	}
	var errs []error
	for _, id := range ids {
		e, err := lookup(reg, id)
		inst := installed[id]
		if err == nil && inst == nil {
			err = fmt.Errorf("not installed")
		}
		if err == nil {
			vals, _ := shed.Resolve(e, inst, nil)
			scope := inst.ClaudeScope
			if scope == "" || scope == "local" {
				scope = "user"
			}
			_, err = shed.Install(e, vals, shed.InstallOptions{Scope: scope, Clients: inst.Clients()})
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		fmt.Printf("%s Updated %s\n", green("✓"), bold(id))
	}
	if len(ids) == 0 {
		fmt.Println(dim("Nothing installed yet."))
	}
	return errors.Join(errs...)
}

func runRemove(args []string) error {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	ids, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(ids) != 1 {
		return usagef("remove needs exactly one id")
	}
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	e, err := lookup(reg, ids[0])
	if err != nil {
		return err
	}
	if e.Source != registry.SourceUser {
		return fmt.Errorf("%s is a built-in example; hide all examples with `examples: false` in %s", e.ID, registry.UserPath())
	}
	if inst := clients.Detect()[e.ID]; inst != nil {
		if err := shed.Uninstall(reg, e, inst, true); err != nil {
			return err
		}
	}
	if err := registry.RemoveUserEntry(e.ID); err != nil {
		return err
	}
	fmt.Printf("%s Removed %s\n", green("✓"), bold(e.ID))
	return nil
}
