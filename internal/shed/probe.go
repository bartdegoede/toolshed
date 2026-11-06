package shed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bartdegoede/toolshed/internal/registry"
)

// Tool is one tool a server advertises.
type Tool struct {
	Name        string
	Description string
	Destructive bool // the server says this tool may modify or delete things
}

// ProbeTimeout bounds how long Probe waits for a server.
var ProbeTimeout = 20 * time.Second

// Probe starts (or connects to) a server, performs the MCP handshake and
// returns the tools it advertises. It is the "does this actually work?"
// check: if Probe succeeds, Claude Code and Codex will be able to talk to it.
func Probe(ctx context.Context, e *registry.Entry) ([]Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	var transport mcp.Transport
	if e.IsRemote() {
		client := http.DefaultClient
		if e.HeaderCmd != "" {
			headers, err := runHeadersHelper(ctx, e.HeaderCmd)
			if err != nil {
				return nil, err
			}
			client = &http.Client{Transport: headerTransport{headers, http.DefaultTransport}}
		}
		transport = &mcp.StreamableClientTransport{Endpoint: e.URL, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true}
	} else {
		if err := CheckCommand(e); err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, e.Command, e.Args...)
		cmd.Env = EntryEnv(e)
		transport = &mcp.CommandTransport{Command: cmd, TerminateDuration: time.Second}
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "toolshed", Version: "probe"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", e.ID, err)
	}
	defer func() { _ = session.Close() }()

	var tools []Tool
	for t, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list tools: %w", err)
		}
		tool := Tool{Name: t.Name, Description: firstLine(t.Description)}
		if a := t.Annotations; a != nil && !a.ReadOnlyHint && (a.DestructiveHint == nil || *a.DestructiveHint) {
			tool.Destructive = true
		}
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

func runHeadersHelper(ctx context.Context, command string) (map[string]string, error) {
	out, err := exec.CommandContext(ctx, "sh", "-c", command).Output()
	if err != nil {
		return nil, fmt.Errorf("headers_helper failed: %w", err)
	}
	var headers map[string]string
	if err := json.Unmarshal(out, &headers); err != nil {
		return nil, fmt.Errorf("headers_helper must print a JSON object: %w", err)
	}
	return headers, nil
}

type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range t.headers {
		r.Header.Set(k, v)
	}
	return t.base.RoundTrip(r)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
