package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/shed"
)

// runMCP serves toolshed itself over MCP, so an agent can find, add and
// install MCP servers on the user's behalf. Secret values are deliberately
// never accepted through it: they would end up in the conversation
// transcript, so the agent is told to send the user to the TUI or terminal.
func runMCP(args []string) error {
	if len(args) > 0 {
		return usagef("mcp takes no arguments")
	}
	return newMCPServer().Run(context.Background(), &mcp.StdioTransport{})
}

type idArgs struct {
	ID string `json:"id" jsonschema:"the server's id, as shown by list_servers"`
}

type addArgs struct {
	ID          string   `json:"id,omitempty" jsonschema:"short lowercase id; derived from source when omitted"`
	Kind        string   `json:"kind" jsonschema:"npx (npm package), uvx (Python package), command (executable on PATH) or url (remote server)"`
	Source      string   `json:"source" jsonschema:"the package name, command line, or URL (extra arguments may follow packages and commands)"`
	Name        string   `json:"name,omitempty" jsonschema:"display name"`
	Description string   `json:"description,omitempty" jsonschema:"one-line description"`
	SecretEnv   []string `json:"secret_env,omitempty" jsonschema:"env var names the server needs as secrets (e.g. API tokens); the user enters the values themselves"`
}

type installArgs struct {
	ID     string            `json:"id" jsonschema:"the server's id"`
	Scope  string            `json:"scope,omitempty" jsonschema:"user (all projects, default) or project (this repository only, Claude Code)"`
	Values map[string]string `json:"values,omitempty" jsonschema:"values for non-secret inputs; never secrets"`
}

func newMCPServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "toolshed", Version: Version}, &mcp.ServerOptions{
		Instructions: "toolshed manages the MCP servers available to Claude Code and Codex. " +
			"Use list_servers to see what's available and installed, add_server to register a new one, " +
			"inspect_server to start it and see its tools, and install_server to make it available. " +
			"Newly installed servers load when the client restarts or reconnects (/mcp in Claude Code). " +
			"Never ask the user to paste secrets into the chat; secrets are entered in the toolshed TUI or terminal.",
	})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	changes := &mcp.ToolAnnotations{DestructiveHint: ptr(false)}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_servers",
		Description: "List every MCP server in the user's toolshed and the built-in examples, with where each is installed.",
		Annotations: readOnly,
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		reg, err := loadRegistry()
		if err != nil {
			return toolError(err), nil, nil
		}
		installed := clients.Detect()
		type item struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Kind        string   `json:"kind"`
			Source      string   `json:"source"`
			InstalledIn []string `json:"installed_in"`
			Secrets     []string `json:"secret_inputs,omitempty"`
		}
		var items []item
		for _, e := range reg.Entries {
			it := item{ID: e.ID, Name: e.DisplayName(), Description: e.Summary(), Kind: kind(e), Source: e.Source, InstalledIn: []string{}}
			if inst := installed[e.ID]; inst != nil {
				it.InstalledIn = inst.Clients()
			}
			for _, in := range e.Inputs {
				if in.Secret {
					it.Secrets = append(it.Secrets, in.Key)
				}
			}
			items = append(items, it)
		}
		return jsonResult(items), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "inspect_server",
		Description: "Start a server (or connect to a remote one) and list the tools it offers. Use it to check a server works before installing it.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
		reg, err := loadRegistry()
		if err != nil {
			return toolError(err), nil, nil
		}
		e, err := lookup(reg, a.ID)
		if err != nil {
			return toolError(err), nil, nil
		}
		tools, err := shed.Probe(ctx, e)
		if err != nil {
			return toolError(err), nil, nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s offers %d tool%s:\n", e.ID, len(tools), plural(len(tools)))
		for _, t := range tools {
			fmt.Fprintf(&b, "- %s: %s\n", t.Name, t.Description)
		}
		return textResult(b.String()), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "add_server",
		Description: "Add an MCP server to the user's toolshed (it is not installed yet). " +
			"Examples: kind=npx source=@modelcontextprotocol/server-memory; kind=url source=https://mcp.deepwiki.com/mcp.",
		Annotations: changes,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a addArgs) (*mcp.CallToolResult, any, error) {
		e, err := registry.Spec{Kind: a.Kind, Source: a.Source, ID: a.ID, Name: a.Name, Description: a.Description, Secrets: a.SecretEnv}.Entry()
		if err != nil {
			return toolError(err), nil, nil
		}
		if reg, err := loadRegistry(); err == nil {
			if existing := reg.Get(e.ID); existing != nil && existing.Source == registry.SourceBuiltin {
				return toolError(fmt.Errorf("%s is already a built-in example; install it directly", e.ID)), nil, nil
			}
		}
		if err := registry.AddUserEntry(e); err != nil {
			return toolError(err), nil, nil
		}
		msg := fmt.Sprintf("Added %s. Next: inspect_server to check it, then install_server.", e.ID)
		if len(e.Inputs) > 0 {
			msg = fmt.Sprintf("Added %s. It needs secrets (%s): ask the user to run `toolshed install %s` in a terminal (or press i in `toolshed`) so they can enter them privately.",
				e.ID, inputKeys(e.Inputs), e.ID)
		}
		return textResult(msg), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "install_server",
		Description: "Install a server from the toolshed into Claude Code and Codex. Fails if it needs secrets the user hasn't entered yet.",
		Annotations: changes,
	}, func(_ context.Context, _ *mcp.CallToolRequest, a installArgs) (*mcp.CallToolResult, any, error) {
		reg, err := loadRegistry()
		if err != nil {
			return toolError(err), nil, nil
		}
		e, err := lookup(reg, a.ID)
		if err != nil {
			return toolError(err), nil, nil
		}
		for k := range a.Values {
			for _, in := range e.Inputs {
				if in.Key == k && in.Secret {
					return toolError(fmt.Errorf("%s is a secret: never pass secrets through the chat; ask the user to run `toolshed install %s` in a terminal", k, e.ID)), nil, nil
				}
			}
		}
		opts := shed.InstallOptions{Scope: a.Scope}
		if opts.Scope == "project" && clients.GitRoot() == "" {
			return toolError(fmt.Errorf("project scope needs a git repository")), nil, nil
		}
		installed := clients.Detect()
		vals, missing := shed.Resolve(e, installed[e.ID], a.Values)
		if len(missing) > 0 {
			return toolError(fmt.Errorf("%s needs %s first: ask the user to run `toolshed install %s` in a terminal to enter it privately",
				e.ID, inputKeys(missing), e.ID)), nil, nil
		}
		done, err := shed.Install(e, vals, opts)
		if err != nil {
			return toolError(err), nil, nil
		}
		return textResult(fmt.Sprintf("Installed %s into %s. It becomes available after the client reconnects (in Claude Code: /mcp, or restart).",
			e.ID, strings.Join(done, " and "))), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "uninstall_server",
		Description: "Remove a server from Claude Code and Codex. It stays in the toolshed and its secrets are kept.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true)},
	}, func(_ context.Context, _ *mcp.CallToolRequest, a idArgs) (*mcp.CallToolResult, any, error) {
		reg, err := loadRegistry()
		if err != nil {
			return toolError(err), nil, nil
		}
		e, err := lookup(reg, a.ID)
		if err != nil {
			return toolError(err), nil, nil
		}
		inst := clients.Detect()[e.ID]
		if inst == nil {
			return textResult(e.ID + " isn't installed."), nil, nil
		}
		if err := shed.Uninstall(reg, e, inst, false); err != nil {
			return toolError(err), nil, nil
		}
		return textResult("Uninstalled " + e.ID + "."), nil, nil
	})
	return s
}

func inputKeys(ins []registry.Input) string {
	keys := make([]string, len(ins))
	for i, in := range ins {
		keys[i] = in.Key
	}
	return strings.Join(keys, ", ")
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}
}

func jsonResult(v any) *mcp.CallToolResult {
	data, _ := json.MarshalIndent(v, "", "  ")
	return textResult(string(data))
}

func ptr[T any](v T) *T { return &v }
