package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/shed"
)

func runAdd(args []string) error {
	// Everything after "--" is the server command, verbatim.
	var cmdline []string
	for i, a := range args {
		if a == "--" {
			args, cmdline = args[:i], args[i+1:]
			break
		}
	}

	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	url := fs.String("url", "", "remote MCP server URL")
	npx := fs.String("npx", "", "npm package to run with npx")
	uvx := fs.String("uvx", "", "Python package to run with uvx")
	name := fs.String("name", "", "display name")
	desc := fs.String("description", "", "one-line description")
	headers := fs.String("headers-helper", "", "remote only: command printing JSON auth headers")
	install := fs.Bool("install", false, "install right after adding")
	noProbe := fs.Bool("no-check", false, "skip starting the server to list its tools")
	var env, secret, input multi
	fs.Var(&env, "env", "fixed environment variable KEY=VALUE (repeatable)")
	fs.Var(&secret, "secret", "secret env var prompted at install, stored in the keyring (repeatable)")
	fs.Var(&input, "input", "non-secret env var prompted at install (repeatable)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("add needs exactly one id")
	}

	e := registry.Entry{ID: pos[0], Name: *name, Description: *desc, URL: *url, HeaderCmd: *headers}
	kinds := 0
	for _, set := range []bool{*url != "", *npx != "", *uvx != "", len(cmdline) > 0} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		return usagef("pick one of --url, --npx, --uvx, or -- <command>")
	}
	switch {
	case *npx != "":
		e.Command, e.Args = "npx", []string{"-y", *npx}
	case *uvx != "":
		e.Command, e.Args = "uvx", []string{*uvx}
	case len(cmdline) > 0:
		e.Command, e.Args = cmdline[0], cmdline[1:]
	}
	if e.Env, err = keyValues("env", env); err != nil {
		return err
	}
	if len(e.Env) == 0 {
		e.Env = nil
	}
	for _, k := range secret {
		e.Inputs = append(e.Inputs, registry.Input{Key: k, Secret: true})
	}
	for _, k := range input {
		e.Inputs = append(e.Inputs, registry.Input{Key: k})
	}

	if err := registry.AddUserEntry(e); err != nil {
		return err
	}
	fmt.Printf("%s Added %s to %s\n", green("✓"), bold(e.ID), dim(registry.UserPath()))

	if !*noProbe && len(e.Inputs) == 0 {
		printProbe(&e)
	}
	if *install {
		return runInstall([]string{e.ID})
	}
	fmt.Printf("\nInstall it with %s\n", bold("toolshed install "+e.ID))
	return nil
}

// printProbe starts the server and prints its tools. Failure is a warning:
// the server may just need values that are provided at install time.
func printProbe(e *registry.Entry) {
	fmt.Print(dim("  Starting server to check it works… "))
	tools, err := shed.Probe(context.Background(), e)
	if err != nil {
		fmt.Println()
		fmt.Printf("  %s couldn't start it: %v\n", amber("!"), err)
		return
	}
	fmt.Printf("\r  %s %s offers %d tool%s%s\n", green("✓"), e.ID, len(tools), plural(len(tools)), strings.Repeat(" ", 12))
	printToolList(tools, 8)
}

func printToolList(tools []shed.Tool, max int) {
	for i, t := range tools {
		if max > 0 && i == max {
			fmt.Printf("    %s\n", dim(fmt.Sprintf("… and %d more", len(tools)-max)))
			return
		}
		mark := " "
		if t.Destructive {
			mark = amber("⚠")
		}
		fmt.Printf("  %s %-28s %s\n", mark, t.Name, dim(truncate(t.Description, 70)))
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
