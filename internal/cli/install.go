package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"golang.org/x/term"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
	"github.com/bartdegoede/toolshed/internal/shed"
)

func runInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	project := fs.Bool("project", false, "install for this repository only (.mcp.json for Claude Code, .codex/config.toml for Codex, .agents/mcp_config.json for Antigravity)")
	user := fs.Bool("user", false, "install for every project (the default for a new tool)")
	client := fs.String("client", "", "only install into "+clients.Names("or"))
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
	if *project && *user {
		return usagef("--project and --user don't go together")
	}
	opts := shed.InstallOptions{Scope: "user"}
	if *project {
		if clients.GitRoot() == "" {
			return fmt.Errorf("--project only works inside a git repository")
		}
		opts.Scope = "project"
	}
	if *client != "" {
		if clients.Get(*client) == nil {
			return usagef("--client must be %s", clients.Names("or"))
		}
		opts.Clients = []string{*client}
	}
	interactive := !*yes && stdinIsTerminal()
	if interactive && len(opts.Clients) == 0 && shed.NeedsClientChoice() {
		if err := chooseClients(); err != nil {
			return err
		}
	}

	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	installed := clients.Detect()
	var failed []string
	for _, id := range ids {
		e, err := lookup(reg, id)
		if err == nil {
			o := opts
			if inst := installed[id]; inst != nil && !*project && !*user {
				// Installed already: again where it is. --project / --user move it.
				o = shed.KeepScopes(inst)
				o.Clients = opts.Clients
			}
			err = installOne(e, installed[id], overrides, o, *yes, interactive)
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

func installOne(e *registry.Entry, inst *clients.Installation, overrides shed.Values, opts shed.InstallOptions, yes, interactive bool) error {
	if ok, err := reviewChanges(e, inst, yes, interactive); err != nil || !ok {
		return err
	}
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

	res, err := shed.Install(e, vals, opts)
	if len(res.Clients) > 0 {
		where := strings.Join(res.Clients, ", ")
		if opts.Scope == "project" {
			where += " (this repo)"
		}
		fmt.Printf("%s %s → %s\n", green("✓"), bold(e.ID), where)
	}
	for _, n := range res.Notes {
		fmt.Printf("  %s %s\n", amber("!"), n)
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

// stdinIsTerminal is swapped in tests, to drive prompts through stdin.
var stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

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
	client := fs.String("client", "", "only from "+clients.Names("or"))
	scope := fs.String("scope", "", "only the registration at user, local or project scope (also one the client ignores here; see doctor)")
	ids, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return usagef("uninstall needs at least one id")
	}
	if *client != "" && clients.Get(*client) == nil {
		return usagef("--client must be %s", clients.Names("or"))
	}
	if *scope != "" && *scope != "user" && *scope != "local" && *scope != "project" {
		return usagef("--scope must be user, local or project")
	}
	if *purge && (*client != "" || *scope != "") {
		return usagef("--purge removes the tool's secrets, so it only goes with a full uninstall (no --client or --scope)")
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
		if *client != "" || *scope != "" {
			removed, err := shed.UninstallCopy(e, installed[id], *client, *scope)
			if len(removed) > 0 {
				where := strings.Join(removed, ", ")
				if *scope != "" {
					where += " (" + *scope + " scope)"
				}
				fmt.Printf("%s Removed %s from %s\n", green("✓"), bold(id), where)
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", id, err))
			}
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
// values and the Claude scope are kept. If what a tool runs has changed, the
// user sees the change and confirms it first.
func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	yes := fs.Bool("y", false, "accept changes to what tools run without asking")
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
	interactive := !*yes && term.IsTerminal(int(os.Stdin.Fd()))
	var errs []error
	for _, id := range ids {
		e, err := lookup(reg, id)
		inst := installed[id]
		if err == nil && inst == nil {
			err = fmt.Errorf("not installed")
		}
		ok := false
		if err == nil {
			ok, err = reviewChanges(e, inst, *yes, interactive)
		}
		if err == nil && ok {
			err = reregister(e, inst)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		if ok {
			fmt.Printf("%s Updated %s\n", green("✓"), bold(id))
		}
	}
	if len(ids) == 0 {
		fmt.Println(dim("Nothing installed yet."))
	}
	return errors.Join(errs...)
}

// reregister installs e again where it's installed now, keeping earlier
// values and each client's scope.
func reregister(e *registry.Entry, inst *clients.Installation) error {
	vals, _ := shed.Resolve(e, inst, nil)
	_, err := shed.Install(e, vals, shed.KeepScopes(inst))
	return err
}

// reviewChanges shows what changed about an installed tool since it was
// installed (see shed.Changes) and asks before going on. With yes it only
// shows them; without a terminal it refuses. ok is false if the user said no.
func reviewChanges(e *registry.Entry, inst *clients.Installation, yes, interactive bool) (ok bool, err error) {
	changes := shed.Changes(e, inst)
	if len(changes) == 0 {
		return true, nil
	}
	fmt.Printf("%s %s has changed since you installed it. It now:\n", amber("!"), bold(e.ID))
	for _, c := range changes {
		fmt.Printf("    %s\n", c)
	}
	if keys := e.SecretKeys(); len(keys) > 0 && secrets.Available() {
		fmt.Printf("  and it gets %s from your keyring.\n", bold(strings.Join(keys, ", ")))
	}
	switch {
	case yes:
		return true, nil
	case !interactive:
		return false, fmt.Errorf("%s changed since it was installed; check the change above and pass -y to accept it", e.ID)
	}
	fmt.Print("Accept the change? [y/N] ")
	line, _ := stdin.ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
		fmt.Println(dim("Kept the installed version of " + e.ID + "."))
		return false, nil
	}
	return true, nil
}

// runApprove shows what a tool an agent added would run and, once the user
// confirms, lets toolshed start and install it.
func runApprove(args []string) error {
	fs := flag.NewFlagSet("approve", flag.ContinueOnError)
	yes := fs.Bool("y", false, "approve without asking")
	ids, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(ids) != 1 {
		return usagef("approve needs exactly one id")
	}
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	e, err := lookup(reg, ids[0])
	if err != nil {
		return err
	}
	if !e.Pending {
		fmt.Printf("%s %s doesn't need approval\n", dim("·"), e.ID)
		return nil
	}
	fmt.Printf("Your agent added %s. Approving it lets toolshed run:\n\n  %s\n", bold(e.ID), e.CommandLine())
	for _, k := range sortedKeys(e.Env) {
		fmt.Printf("  %s\n", dim(k+"="+e.Env[k]))
	}
	if keys := e.SecretKeys(); len(keys) > 0 {
		fmt.Printf("\n  with these secrets from your keyring: %s\n", bold(strings.Join(keys, ", ")))
	}
	fmt.Println()
	if !*yes {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("not a terminal: pass -y to approve %s without asking", e.ID)
		}
		fmt.Print("Approve? [y/N] ")
		line, _ := stdin.ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Println(dim("Not approved."))
			return nil
		}
	}
	if err := registry.ApproveUserEntry(e.ID); err != nil {
		return err
	}
	fmt.Printf("%s Approved %s. Install it with %s\n", green("✓"), bold(e.ID), bold("toolshed install "+e.ID))
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
