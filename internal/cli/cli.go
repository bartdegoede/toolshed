// Package cli implements toolshed's command line. With no arguments it starts
// the TUI; every subcommand is a thin wrapper over internal/shed.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/tui"
)

// Version is set from main via -ldflags.
var Version = "dev"

// Exit codes.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// usageError marks errors that should exit with ExitUsage.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

type command struct {
	name, args, help string
	run              func(args []string) error
}

var commands []command

func init() {
	commands = []command{
		{"add", "<id> [flags] [-- command args...]", "Add a tool to your toolshed", runAdd},
		{"import", "[<name>...] [-y] [--dry-run] [--no-install]", "Adopt the servers Claude Code and Codex already have", runImport},
		{"install", "<id>... [--project] [--client c] [--set K=V] [-y]", "Install tools into Claude Code / Codex", runInstall},
		{"uninstall", "<id>... [--purge]", "Remove tools from Claude Code / Codex", runUninstall},
		{"update", "[<id>...] [-y]", "Re-register installed tools with current settings", runUpdate},
		{"upgrade", "[<id>...] [--dry-run]", "Move npx/uvx tools to the newest release of their package", runUpgrade},
		{"approve", "<id> [-y]", "Review and allow a tool your agent added", runApprove},
		{"secret", "set <id> [KEY]", "Replace a tool's secret (e.g. a rotated token)", runSecret},
		{"remove", "<id>", "Uninstall a tool and delete it from your toolshed", runRemove},
		{"list", "[--json]", "Show every tool and where it's installed", runList},
		{"doctor", "[--no-start] [--json]", "Check that every installed tool works", runDoctor},
		{"tools", "<id>", "Start a tool's server and list the tools it offers", runTools},
		{"edit", "", "Open your registry in $EDITOR", runEdit},
		{"mcp", "", "Serve toolshed over MCP, so your agent can add tools itself", runMCP},
		{"validate", "[file]", "Check a registry file", runValidate},
		{"completion", "bash|zsh|fish", "Print a shell completion script", runCompletion},
		{"exec", "<id> [--secret KEY=group]... -- <command> [args...]", "Run a server with its secrets (used by Claude/Codex)", runExec},
		{"headers", "<id> [--secret KEY=group]... [--header 'Name=template']...", "Print a remote tool's auth headers (used by Claude/Codex)", runHeaders},
		{"__ids", "", "", runIDs},
	}
}

// Run dispatches os.Args and returns the process exit code.
func Run(args []string) int {
	if len(args) < 2 {
		tui.Version = Version
		if err := tui.Run(); err != nil {
			fmt.Fprintln(os.Stderr, red("error: ")+err.Error())
			return ExitError
		}
		return ExitOK
	}
	name := args[1]
	switch name {
	case "-v", "--version", "version":
		fmt.Println("toolshed", Version)
		return ExitOK
	case "-h", "--help", "help":
		printHelp(os.Stdout)
		return ExitOK
	}
	for _, c := range commands {
		if c.name == name {
			err := c.run(args[2:])
			switch {
			case err == nil:
				return ExitOK
			case errors.Is(err, flag.ErrHelp):
				return ExitOK
			case errors.As(err, new(usageError)):
				fmt.Fprintf(os.Stderr, "%s\nusage: toolshed %s %s\n", err, c.name, c.args)
				return ExitUsage
			default:
				fmt.Fprintln(os.Stderr, red("error: ")+err.Error())
				return ExitError
			}
		}
	}
	fmt.Fprintf(os.Stderr, "unknown command %q\n\n", name)
	printHelp(os.Stderr)
	return ExitUsage
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, "%s — add MCP servers and tools to Claude Code and Codex\n\n", bold("toolshed"))
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  toolshed                 Open the interactive toolshed")
	for _, c := range visibleCommands() {
		fmt.Fprintf(w, "  toolshed %-15s %s\n", c.name, c.help)
	}
	fmt.Fprintf(w, `
Adding tools:
  toolshed add memory --npx @modelcontextprotocol/server-memory
  toolshed add fetch --uvx mcp-server-fetch
  toolshed add docs --url https://mcp.context7.com/mcp
  toolshed add jira --secret JIRA_TOKEN -- my-jira-cli mcp --site acme

Your tools live in %s.
Secrets go to the OS keyring, never into Claude or Codex config files.
`, registry.UserPath())
}

// usagef returns an error that exits with ExitUsage.
func usagef(format string, a ...any) error {
	return usageError{fmt.Sprintf(format, a...)}
}

// parse parses flags that may be interleaved with positional arguments
// (`install hello --project` and `install --project hello` both work).
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fs.SetOutput(os.Stdout)
				fs.PrintDefaults()
				return nil, err
			}
			return nil, usagef("%v", err)
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// multi is a repeatable string flag.
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

// keyValues parses repeated KEY=VALUE flags.
func keyValues(flagName string, items []string) (map[string]string, error) {
	out := map[string]string{}
	for _, kv := range items {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, usagef("--%s expects KEY=VALUE, got %q", flagName, kv)
		}
		out[k] = v
	}
	return out, nil
}

func loadRegistry() (*registry.Registry, error) { return registry.Load() }

func lookup(reg *registry.Registry, id string) (*registry.Entry, error) {
	if e := reg.Get(id); e != nil {
		return e, nil
	}
	return nil, fmt.Errorf("no tool %q in your toolshed (see `toolshed list`)", id)
}

// ── output styling (plain when not a terminal or NO_COLOR is set) ──

var color = term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == ""

func paint(code, s string) string {
	if !color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func bold(s string) string  { return paint("1", s) }
func dim(s string) string   { return paint("2", s) }
func green(s string) string { return paint("32", s) }
func red(s string) string   { return paint("31", s) }
func amber(s string) string { return paint("33", s) }
