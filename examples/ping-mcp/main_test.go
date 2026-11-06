package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeNtfy records the last published message.
func fakeNtfy(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tk_test" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("NTFY_SERVER", srv.URL)
	t.Setenv("NTFY_TOKEN", "tk_test")
	return srv, &got
}

func call(t *testing.T, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := newServer().Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "notify", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestNotifyPublishes(t *testing.T) {
	_, got := fakeNtfy(t)
	t.Setenv("NTFY_TOPIC", "my-secret-topic")

	res := call(t, map[string]any{"message": "Tests passed", "title": "CI", "priority": "high", "tags": []string{"tada"}})
	if res.IsError {
		t.Fatalf("unexpected error: %v", res.Content[0].(*mcp.TextContent).Text)
	}
	g := *got
	if g["topic"] != "my-secret-topic" || g["message"] != "Tests passed" || g["title"] != "CI" || g["priority"] != float64(4) {
		t.Fatalf("unexpected payload: %v", g)
	}
}

func TestNotifyWithoutTopicExplainsSetup(t *testing.T) {
	t.Setenv("NTFY_TOPIC", "")
	res := call(t, map[string]any{"message": "hi"})
	if !res.IsError {
		t.Fatal("expected a tool error without a topic")
	}
}

func TestNotifyReportsServerErrors(t *testing.T) {
	fakeNtfy(t)
	t.Setenv("NTFY_TOPIC", "t")
	t.Setenv("NTFY_TOKEN", "wrong")
	if res := call(t, map[string]any{"message": "hi"}); !res.IsError {
		t.Fatal("expected a tool error on 401")
	}
}

func TestPriority(t *testing.T) {
	for in, want := range map[string]int{"": 3, "min": 1, "LOW": 2, "high": 4, "urgent": 5, "bogus": 3} {
		if got := priority(in); got != want {
			t.Errorf("priority(%q) = %d, want %d", in, got, want)
		}
	}
}
