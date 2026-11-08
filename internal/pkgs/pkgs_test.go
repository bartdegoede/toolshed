package pkgs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	toolshed "github.com/bartdegoede/toolshed"
	"github.com/bartdegoede/toolshed/internal/registry"
)

func TestFind(t *testing.T) {
	cases := []struct {
		cmd     string
		args    []string
		ok      bool
		name    string
		version string
		exact   bool
	}{
		{"npx", []string{"-y", "@modelcontextprotocol/server-memory"}, true, "@modelcontextprotocol/server-memory", "", false},
		{"npx", []string{"-y", "@scope/pkg@2.1.0", "--port", "1"}, true, "@scope/pkg", "2.1.0", true},
		{"npx", []string{"pkg@latest"}, true, "pkg", "latest", false},
		{"npx", []string{"-y", "pkg@^2"}, true, "pkg", "^2", false},
		{"npx", []string{"-p", "pkg@1.0.0", "pkg-bin"}, true, "pkg", "1.0.0", true},
		{"npx", []string{"github:user/repo"}, false, "", "", false},
		{"npx", []string{"user/repo"}, false, "", "", false},
		{"npx", []string{"./local"}, false, "", "", false},
		{"npx", []string{"--node-options=x", "pkg"}, false, "", "", false},
		{"uvx", []string{"mcp-server-fetch"}, true, "mcp-server-fetch", "", false},
		{"uvx", []string{"mcp-server-fetch==2026.11.4"}, true, "mcp-server-fetch", "2026.11.4", true},
		{"uvx", []string{"tool@1.2"}, true, "tool", "1.2", true},
		{"uvx", []string{"tool[cli]>=1.2"}, true, "tool", ">=1.2", false},
		{"uvx", []string{"--from", "pkg[x]==1.0", "pkg-cmd"}, true, "pkg", "1.0", true},
		{"uvx", []string{"--python", "3.12", "--with", "extra", "tool"}, true, "tool", "", false},
		{"uvx", []string{"git+https://github.com/x/y"}, false, "", "", false},
		{"node", []string{"server.js"}, false, "", "", false},
	}
	for _, c := range cases {
		r, ok := Find(&registry.Entry{Command: c.cmd, Args: c.args})
		if ok != c.ok || r.Name != c.name || r.Version != c.version || r.Exact != c.exact {
			t.Errorf("%s %v: got ok=%v %+v", c.cmd, c.args, ok, r)
		}
	}
}

// Built-in npx/uvx examples are pinned, like `toolshed add` pins new tools.
func TestBuiltinPackagesArePinned(t *testing.T) {
	f, err := registry.Parse(toolshed.DefaultRegistry)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range f.Tools {
		if r, ok := Find(&e); ok && !r.Exact {
			t.Errorf("built-in %s isn't pinned to a version: %v", e.ID, e.Args)
		}
	}
}

func TestPin(t *testing.T) {
	cases := []struct {
		cmd  string
		args []string
		want []string
	}{
		{"npx", []string{"-y", "@scope/pkg", "."}, []string{"-y", "@scope/pkg@3.0.0", "."}},
		{"npx", []string{"-y", "pkg@latest"}, []string{"-y", "pkg@3.0.0"}},
		{"uvx", []string{"tool[cli]"}, []string{"tool[cli]==3.0.0"}},
		{"uvx", []string{"--from=tool==1", "cmd"}, []string{"--from=tool==3.0.0", "cmd"}},
	}
	for _, c := range cases {
		e := &registry.Entry{Command: c.cmd, Args: c.args}
		r, _ := Find(e)
		if got := r.Pin(e, "3.0.0"); !slices.Equal(got, c.want) {
			t.Errorf("%v: got %v, want %v", c.args, got, c.want)
		}
	}
}

func TestLatestAndPinLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/@scope%2Fpkg/latest":
			_, _ = w.Write([]byte(`{"name":"@scope/pkg","version":"2.1.0"}`))
		case "/pypi/tool/json":
			_, _ = w.Write([]byte(`{"info":{"version":"2026.11.4"},"releases":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("npm_config_registry", srv.URL+"/")
	t.Setenv("TOOLSHED_PYPI_URL", srv.URL)

	e := &registry.Entry{Command: "npx", Args: []string{"-y", "@scope/pkg"}}
	v, err := PinLatest(context.Background(), e)
	if err != nil || v != "2.1.0" || e.Args[1] != "@scope/pkg@2.1.0" {
		t.Fatalf("npm: %q %v %v", v, err, e.Args)
	}
	if _, err := PinLatest(context.Background(), e); err != ErrNothingToPin {
		t.Fatalf("already pinned: got %v", err)
	}

	py := &registry.Entry{Command: "uvx", Args: []string{"tool"}}
	if v, err := PinLatest(context.Background(), py); err != nil || v != "2026.11.4" || py.Args[0] != "tool==2026.11.4" {
		t.Fatalf("pypi: %q %v %v", v, err, py.Args)
	}

	missing := &registry.Entry{Command: "npx", Args: []string{"nope"}}
	if _, err := PinLatest(context.Background(), missing); err == nil {
		t.Fatal("expected an error for an unknown package")
	}
	if missing.Args[0] != "nope" {
		t.Fatal("a failed lookup must leave the entry alone")
	}
}
