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

// runImport adopts the servers your clients already have: it adds
// each one to the registry, moves the env vars that hold secrets into the
// keyring, and re-registers the server through `toolshed exec`, so the
// plain-text values leave the clients' config files.
func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	yes := fs.Bool("y", false, "import everything and use the guessed secrets without asking")
	dry := fs.Bool("dry-run", false, "only show what would be imported")
	noInstall := fs.Bool("no-install", false, "only add to your registry; leave the clients' config as it is")
	names, err := parse(fs, args)
	if err != nil {
		return err
	}
	interactive := !*yes && !*dry && term.IsTerminal(int(os.Stdin.Fd()))
	if !interactive && !*yes && !*dry {
		return fmt.Errorf("not a terminal: pass -y to import with the guessed secrets, or --dry-run to see them first")
	}
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	installed := clients.Detect()
	all := shed.Imports(reg, installed)
	ims := all
	if len(names) > 0 {
		ims = nil
		for _, n := range names {
			found := false
			for _, im := range all {
				if im.Name == n {
					ims, found = append(ims, im), true
				}
			}
			switch {
			case found:
			case installed[n] != nil:
				return fmt.Errorf("%s is already in your toolshed", n)
			default:
				return fmt.Errorf("none of %s has a server called %s", clients.Labels("or"), n)
			}
		}
	}
	if len(ims) == 0 {
		fmt.Println(dim("Nothing to import: every server your clients have is already in your toolshed."))
		return nil
	}
	if !secrets.Available() && !*dry {
		fmt.Printf("%s no OS keyring here, so secrets stay in the clients' config\n", amber("!"))
	}

	var errs []error
	imported := 0
	for i := range ims {
		im := &ims[i]
		if i > 0 {
			fmt.Println()
		}
		ok, err := importOne(reg, im, interactive, *dry, *noInstall)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %s: %v\n", red("✗"), im.Name, err)
			errs = append(errs, fmt.Errorf("%s: %w", im.Name, err))
		}
		if ok {
			imported++
		}
	}
	if *dry {
		fmt.Printf("\n%s\n", dim("Nothing changed (--dry-run)."))
	} else if imported > 0 {
		fmt.Printf("\n%s Imported %d server%s. Check them with %s\n", green("✓"), imported, plural(imported), bold("toolshed doctor"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d of %d failed", len(errs), len(ims))
	}
	return nil
}

func importOne(reg *registry.Registry, im *shed.Import, interactive, dry, noInstall bool) (bool, error) {
	where := strings.Join(im.Clients, ", ")
	if s := im.Inst.Scope(clients.Claude); s != "" && s != "user" {
		where += " (" + s + ")"
	}
	if im.Skip != "" {
		fmt.Printf("%s %s %s\n", dim("·"), bold(im.Name), dim("skipped: "+im.Skip))
		return false, nil
	}
	fmt.Printf("%s  %s\n  %s\n", bold(im.Name), dim(where), im.Entry.CommandLine())
	for _, n := range im.Notes {
		fmt.Printf("  %s\n", dim(n))
	}
	if interactive && !ask("Import it?", true) {
		return false, nil
	}
	for i := range im.Vars {
		v := &im.Vars[i]
		label := fmt.Sprintf("  %-24s %s", v.Key, dim(mask(v.Value)))
		switch {
		case v.Value == "":
			fmt.Printf("%s  %s\n", label, dim("empty: kept as is"))
		case interactive:
			v.Secret = ask(label+"  secret?", v.Secret)
		case v.Secret && secrets.Available():
			fmt.Printf("%s  → keyring\n", label)
		case v.Secret:
			fmt.Printf("%s  → secret\n", label)
		default:
			fmt.Printf("%s  → plain value\n", label)
		}
	}
	if dry {
		return false, nil
	}
	res, err := im.Apply(!noInstall)
	for _, n := range res.Notes {
		fmt.Printf("  %s %s\n", amber("!"), n)
	}
	if err != nil {
		return false, err
	}
	done := "added to your registry"
	if res.Installed {
		done = "re-registered through toolshed"
		if len(res.Moved) > 0 {
			done += "; " + strings.Join(res.Moved, ", ") + " moved to your keyring"
		}
	}
	fmt.Printf("  %s %s %s\n", green("✓"), bold(res.Entry.ID), done)
	return true, nil
}

// ask asks a yes/no question; an empty answer takes def.
func ask(q string, def bool) bool {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	fmt.Printf("%s %s ", q, hint)
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		fmt.Println()
		return def
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return def
	case "y", "yes":
		return true
	}
	return false
}

// mask shows enough of a value to recognise it, not enough to leak it.
func mask(v string) string {
	switch n := len([]rune(v)); {
	case n == 0:
		return `""`
	case n <= 8:
		return "****"
	default:
		return string([]rune(v)[:4]) + fmt.Sprintf("… (%d chars)", n)
	}
}
