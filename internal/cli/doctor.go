package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/shed"
)

// runDoctor checks every installed tool (see shed.Doctor) and exits non-zero
// if any of them is broken.
func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	noStart := fs.Bool("no-start", false, "don't start the servers (faster)")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	avail := clients.AvailableClients()
	installed := clients.Detect()
	if !*asJSON && !*noStart {
		fmt.Println(dim("Starting each installed server to check it works…"))
	}
	report := shed.Doctor(context.Background(), reg, installed, shed.DoctorOptions{Start: !*noStart})
	orphans := shed.Orphans(reg, installed)
	var unmanaged []string
	for name := range installed {
		if reg.Get(name) == nil && !contains(orphans, name) {
			unmanaged = append(unmanaged, name)
		}
	}
	sort.Strings(unmanaged)

	broken := 0
	for _, h := range report {
		if h.HasErrors() {
			broken++
		}
	}
	if len(avail) == 0 {
		broken++
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{
			"clients": append([]string{}, avail...), "tools": report,
			"orphans": append([]string{}, orphans...), "unmanaged": append([]string{}, unmanaged...),
		}); err != nil {
			return err
		}
	} else {
		printDoctor(avail, report, orphans, unmanaged)
	}
	if broken > 0 {
		return fmt.Errorf("%d problem%s to fix", broken, plural(broken))
	}
	return nil
}

func printDoctor(avail []string, report []shed.Health, orphans, unmanaged []string) {
	for _, c := range clients.All {
		if contains(avail, c) {
			fmt.Printf("%s %s\n", green("✓"), c)
		} else {
			fmt.Printf("%s %s %s\n", dim("·"), c, dim("not installed"))
		}
	}
	if len(avail) == 0 {
		fmt.Printf("%s none of %s is on PATH\n", red("✗"), clients.Labels("or"))
	}
	fmt.Println()

	width := 4
	for _, h := range report {
		width = max(width, len(h.ID))
	}
	for _, h := range report {
		mark := green("✓")
		switch {
		case h.HasErrors():
			mark = red("✗")
		case !h.OK():
			mark = amber("!")
		}
		info := strings.Join(h.Clients, ", ")
		if h.Tools >= 0 {
			info += fmt.Sprintf(" · %d tool%s", h.Tools, plural(h.Tools))
		}
		fmt.Printf("%s %-*s  %s\n", mark, width, h.ID, dim(info))
		for _, f := range h.Findings {
			line := "    " + f.Text
			if f.Fix != "" {
				line += dim("  → " + f.Fix)
			}
			fmt.Println(line)
		}
	}
	if len(report) == 0 {
		fmt.Println(dim("Nothing installed yet."))
	}
	for _, name := range orphans {
		fmt.Printf("%s %s is registered through toolshed but isn't in your registry anymore%s\n",
			amber("!"), bold(name), dim("  → toolshed import "+name+" to get it back, or remove it from the client"))
	}
	if len(unmanaged) > 0 {
		fmt.Printf("%s %d other server%s not managed by toolshed: %s%s\n", dim("·"), len(unmanaged), plural(len(unmanaged)), strings.Join(unmanaged, ", "), dim("  → toolshed import"))
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
