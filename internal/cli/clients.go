package cli

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/shed"
)

// runClients shows or sets the clients toolshed installs tools into.
func runClients(args []string) error {
	fs := flag.NewFlagSet("clients", flag.ContinueOnError)
	names, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		if _, err := shed.LoadSettings(); err != nil {
			return fmt.Errorf("%w (choose again to replace it: toolshed clients <name>)", err)
		}
		printClients()
		return nil
	}
	for i, n := range names {
		names[i] = strings.ToLower(n)
		if clients.Get(names[i]) == nil {
			return usagef("unknown client %q: use %s", n, clients.Names("or"))
		}
	}
	if err := shed.SaveClients(names); err != nil {
		return err
	}
	chosen, _ := shed.DefaultClients()
	fmt.Printf("%s toolshed installs tools into %s\n", green("✓"), clients.LabelList(chosen, "and"))
	for _, c := range chosen {
		if !clients.Available(c) {
			fmt.Printf("  %s %s isn't on PATH yet; tools go there once it is\n", dim("·"), clients.Label(c))
		}
	}
	printLeftBehind(chosen)
	return nil
}

func printClients() {
	chosen, ok := shed.DefaultClients()
	if ok {
		fmt.Printf("toolshed installs tools into %s.\n\n", clients.LabelList(chosen, "and"))
	} else {
		fmt.Printf("You haven't chosen clients yet, so toolshed installs tools into every one on PATH.\n\n")
	}
	width := 0
	for _, c := range clients.All {
		width = max(width, len(c))
	}
	for _, c := range clients.All {
		pad := strings.Repeat(" ", width-len(c))
		switch avail := clients.Available(c); {
		case contains(chosen, c) && avail:
			fmt.Printf("  %s %s%s  %s\n", green("✓"), c, pad, clients.Label(c))
		case contains(chosen, c):
			fmt.Printf("  %s %s%s  %s %s\n", amber("!"), c, pad, clients.Label(c), dim("chosen, not on PATH"))
		case avail:
			fmt.Printf("  %s %s%s  %s %s\n", dim("·"), c, pad, clients.Label(c), dim("on PATH, not used"))
		default:
			fmt.Printf("  %s %s%s  %s %s\n", dim("·"), c, pad, clients.Label(c), dim("not on PATH"))
		}
	}
	if ok {
		printLeftBehind(chosen)
	}
	fmt.Printf("\nChange it with %s (--client picks one for a single install).\n", bold("toolshed clients <name>..."))
}

// printLeftBehind points out tools still registered in clients that are no
// longer chosen: changing the choice doesn't remove anything.
func printLeftBehind(chosen []string) {
	reg, err := loadRegistry()
	if err != nil {
		return
	}
	installed := clients.Detect()
	for _, c := range clients.All {
		if contains(chosen, c) {
			continue
		}
		var ids []string
		for _, e := range reg.Entries {
			if inst := installed[e.ID]; inst != nil && inst.Has(c) {
				ids = append(ids, e.ID)
			}
		}
		if len(ids) > 0 {
			fmt.Printf("  %s %s still has %s, left as it is (%s removes one)\n", dim("·"), clients.Label(c), strings.Join(ids, ", "),
				bold("toolshed uninstall <id> --client "+c))
		}
	}
}

// chooseClients asks which clients to install into, once, and saves the
// answer. An empty answer takes every client on PATH.
func chooseClients() error {
	avail := clients.AvailableClients()
	fmt.Printf("toolshed can install tools into %s.\n", clients.LabelList(avail, "and"))
	for {
		fmt.Printf("Which should it use? %s [%s] ", dim("(names, separated by spaces)"), strings.Join(avail, " "))
		line, err := stdin.ReadString('\n')
		if err != nil && line == "" {
			fmt.Println()
			return errors.New("no clients chosen; run `toolshed clients <name>...` or pass --client")
		}
		names := strings.Fields(strings.ToLower(strings.ReplaceAll(line, ",", " ")))
		if len(names) == 0 {
			names = avail
		}
		var bad []string
		for _, n := range names {
			if clients.Get(n) == nil {
				bad = append(bad, n)
			}
		}
		if len(bad) > 0 {
			fmt.Printf("%s unknown: %s. Use %s.\n", amber("!"), strings.Join(bad, ", "), clients.Names("or"))
			continue
		}
		if err := shed.SaveClients(names); err != nil {
			return err
		}
		chosen, _ := shed.DefaultClients()
		fmt.Printf("%s Saved: toolshed installs into %s. Change it with %s.\n\n", green("✓"), clients.LabelList(chosen, "and"), bold("toolshed clients"))
		return nil
	}
}
