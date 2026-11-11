package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/shed"
)

type listRow struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Source  string   `json:"source"`
	Clients []string `json:"clients"`
	Scope   string   `json:"claude_scope,omitempty"`
}

func kind(e *registry.Entry) string {
	switch {
	case e.IsRemote():
		return "url"
	case e.Command == "npx" || e.Command == "uvx":
		return e.Command
	default:
		return "command"
	}
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	installed := clients.Detect()
	rows := make([]listRow, 0, len(reg.Entries))
	for _, e := range reg.Entries {
		r := listRow{ID: e.ID, Name: e.DisplayName(), Kind: kind(e), Source: e.Source, Clients: []string{}}
		if inst := installed[e.ID]; inst != nil {
			r.Clients, r.Scope = inst.Clients(), inst.Scope(clients.Claude)
		}
		rows = append(rows, r)
	}
	var other []string
	for name := range installed {
		if reg.Get(name) == nil {
			other = append(other, name)
		}
	}
	sort.Strings(other)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"tools": rows, "unmanaged": append([]string{}, other...)})
	}

	width := 4
	for _, r := range rows {
		width = max(width, len(r.ID))
	}
	section := ""
	for _, r := range rows {
		if r.Source != section {
			section = r.Source
			title := "Your tools"
			if section == registry.SourceBuiltin {
				title = "Examples"
			}
			fmt.Printf("\n%s\n", bold(title))
		}
		fmt.Printf("  %-*s  %-7s  %s\n", width, r.ID, dim(r.Kind), status(r))
	}
	if len(other) > 0 {
		fmt.Printf("\n%s %s\n", bold("Also in your clients"), dim("(not managed by toolshed)"))
		fmt.Printf("  %s\n", dim(strings.Join(other, ", ")))
	}
	fmt.Println()
	return nil
}

func status(r listRow) string {
	if len(r.Clients) == 0 {
		return dim("—")
	}
	var parts []string
	for _, c := range r.Clients {
		s := green("● ") + c
		if c == clients.Claude && r.Scope != "" && r.Scope != "user" {
			s += dim(" (" + r.Scope + ")")
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "  ")
}

func runTools(args []string) error {
	fs := flag.NewFlagSet("tools", flag.ContinueOnError)
	ids, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(ids) != 1 {
		return usagef("tools needs exactly one id")
	}
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	e, err := lookup(reg, ids[0])
	if err != nil {
		return err
	}
	tools, err := shed.Probe(context.Background(), e)
	if err != nil {
		return err
	}
	fmt.Printf("%s %s offers %d tool%s\n", green("✓"), bold(e.ID), len(tools), plural(len(tools)))
	printToolList(tools, 0)
	return nil
}

func runEdit(args []string) error {
	if len(args) > 0 {
		return usagef("edit takes no arguments")
	}
	path := registry.UserPath()
	if err := registry.EnsureUserFile(); err != nil {
		return err
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return runValidate(nil)
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	args, err := parse(fs, args)
	if err != nil {
		return err
	}
	path := registry.UserPath()
	if len(args) > 1 {
		return usagef("validate takes at most one file")
	}
	if len(args) == 1 {
		path = args[0]
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f, err := registry.Parse(data)
	if err != nil {
		return fmt.Errorf("%s:\n  %s", path, strings.ReplaceAll(err.Error(), "\n", "\n  "))
	}
	fmt.Printf("%s %s: %d tool%s\n", green("✓"), path, len(f.Tools), plural(len(f.Tools)))
	return nil
}

// runHeaders is the headers helper Claude Code and Codex run for remote tools
// with secrets (see shed.HeadersArgs). It prints the headers as JSON on
// stdout, which is all the clients read.
func runHeaders(args []string) error {
	call, err := shed.ParseHeadersArgs(args)
	if err != nil {
		return usagef("%v", err)
	}
	headers, err := call.Headers()
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(headers)
}

// runExec is what Claude Code and Codex launch for tools with secrets:
//
//	toolshed exec <id> --secret KEY=group ... -- <command> <args...>
//
// It loads the secrets from the keyring into the environment and replaces
// itself with the real server. It deliberately doesn't read the registry, so
// registrations keep working wherever the registry lives.
func runExec(args []string) error {
	var refs []shed.SecretRef
	i := 1
	for ; i < len(args) && args[i] != "--"; i++ {
		if args[i] != "--secret" || i+1 >= len(args) {
			return usagef("unexpected argument %q", args[i])
		}
		k, g, ok := strings.Cut(args[i+1], "=")
		if !ok {
			return usagef("--secret expects KEY=group")
		}
		refs = append(refs, shed.SecretRef{Key: k, Group: g})
		i++
	}
	if len(args) < 1 || i+1 >= len(args) {
		return usagef("exec needs <id> ... -- <command>")
	}
	command := args[i+1:]
	bin, err := exec.LookPath(command[0])
	if err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	return syscall.Exec(bin, command, shed.ExecEnv(nil, refs))
}
