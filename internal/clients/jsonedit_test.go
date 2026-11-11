package clients

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stdio(cmd string, args ...string) []jsonKV {
	kv := []jsonKV{{"command", cmd}}
	if len(args) > 0 {
		kv = append(kv, jsonKV{"args", args})
	}
	return kv
}

// Every case checks the exact output: the editor may only touch the keys it
// owns in one server, and new keys must look like their neighbours.
func TestEditJSONServer(t *testing.T) {
	cases := []struct {
		name, in string
		srv      string
		set      []jsonKV // nil: remove
		drop     []string
		want     string
	}{
		{
			name: "update in place, keeping the user's keys (agy's own style)",
			in: `{
  "mcpServers": {
    "local-one": {
      "args": [
        "-c",
        "exec cat"
      ],
      "command": "sh",
      "disabled": false,
      "timeoutSeconds": 30
    }
  }
}
`,
			srv: "local-one", set: stdio("sh", "-c", "exec tac"), drop: []string{"env", "url", "serverUrl", "headers"},
			want: `{
  "mcpServers": {
    "local-one": {
      "args": [
        "-c",
        "exec tac"
      ],
      "command": "sh",
      "disabled": false,
      "timeoutSeconds": 30
    }
  }
}
`,
		},
		{
			name: "a new server after the others",
			in: `{
  "mcpServers": {
    "a": {
      "command": "x"
    }
  },
  "other": true
}
`,
			srv: "b", set: append(stdio("y", "1"), jsonKV{"env", map[string]string{"K": "v"}}),
			want: `{
  "mcpServers": {
    "a": {
      "command": "x"
    },
    "b": {
      "command": "y",
      "args": [
        "1"
      ],
      "env": {
        "K": "v"
      }
    }
  },
  "other": true
}
`,
		},
		{
			name: "a new file",
			in:   "",
			srv:  "a", set: stdio("x"),
			want: `{
  "mcpServers": {
    "a": {
      "command": "x"
    }
  }
}
`,
		},
		{
			name: "no servers section yet, tabs",
			in:   "{\n\t\"theme\": \"dark\"\n}\n",
			srv:  "a", set: stdio("x"),
			want: "{\n\t\"theme\": \"dark\",\n\t\"mcpServers\": {\n\t\t\"a\": {\n\t\t\t\"command\": \"x\"\n\t\t}\n\t}\n}\n",
		},
		{
			name: "comments and trailing commas survive",
			in: `{
  // mine
  "mcpServers": {
    "a": { "command": "x", "timeoutSeconds": 3, }, // keep me
    /* b is next */
  },
}
`,
			srv: "a", set: stdio("y", "1"),
			want: `{
  // mine
  "mcpServers": {
    "a": { "command": "y", "timeoutSeconds": 3, "args": ["1"], }, // keep me
    /* b is next */
  },
}
`,
		},
		{
			name: "a one-line file stays on one line",
			in:   `{"mcpServers": {"a": {"command": "x"}}}`,
			srv:  "b", set: stdio("y", "1"),
			want: `{"mcpServers": {"a": {"command": "x"}, "b": {"command": "y", "args": ["1"]}}}`,
		},
		{
			name: "remove the last server, keeping a trailing comma",
			in: `{
  "mcpServers": {
    "a": {"command": "x"},
    "b": {"command": "y"}, // b
  }
}
`,
			srv: "b", set: nil,
			want: `{
  "mcpServers": {
    "a": {"command": "x"}, // b
  }
}
`,
		},
		{
			name: "remove the only server",
			in:   "{\n  \"mcpServers\": {\n    \"a\": {\"command\": \"x\"}\n  },\n  \"other\": 1\n}\n",
			srv:  "a", set: nil,
			want: "{\n  \"mcpServers\": {},\n  \"other\": 1\n}\n",
		},
		{
			name: "switch transport: the other transport's keys go",
			in:   "{\n  \"mcpServers\": {\n    \"a\": {\n      \"command\": \"x\",\n      \"env\": {\"K\": \"v\"},\n      \"disabled\": true\n    }\n  }\n}\n",
			srv:  "a", set: []jsonKV{{"serverUrl", "https://x/mcp"}}, drop: []string{"command", "args", "env"},
			want: "{\n  \"mcpServers\": {\n    \"a\": {\n      \"disabled\": true,\n      \"serverUrl\": \"https://x/mcp\"\n    }\n  }\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := editJSONServer([]byte(tc.in), "mcpServers", tc.srv, tc.set, tc.drop)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
			if err := checkJSONEdit([]byte(tc.in), got, "mcpServers", tc.srv, tc.set, tc.drop); err != nil {
				t.Fatalf("the edit's own check refuses it: %v", err)
			}
		})
	}
}

func TestEditJSONServerRefusesOddLayouts(t *testing.T) {
	for _, in := range []string{
		`[]`,
		`{"mcpServers": []}`,
		`{"mcpServers": {"a": "sh"}}`,
	} {
		if _, err := editJSONServer([]byte(in), "mcpServers", "a", stdio("x"), nil); !errors.Is(err, errJSONLayout) {
			t.Errorf("%s: expected a layout error, got %v", in, err)
		}
	}
}

// The check before writing catches an edit that touched anything else.
func TestCheckJSONEditCatchesCollateralChanges(t *testing.T) {
	before := `{"mcpServers": {"a": {"command": "x"}, "b": {"command": "y"}}, "other": 1}`
	for name, after := range map[string]string{
		"another server":   `{"mcpServers": {"a": {"command": "z"}, "b": {"command": "q"}}, "other": 1}`,
		"another setting":  `{"mcpServers": {"a": {"command": "z"}, "b": {"command": "y"}}, "other": 2}`,
		"a user's key":     `{"mcpServers": {"a": {"command": "z", "lost": false}, "b": {"command": "y"}}, "other": 1}`,
		"not the new name": `{"mcpServers": {"a": {"command": "x"}, "b": {"command": "y"}}, "other": 1}`,
	} {
		if err := checkJSONEdit([]byte(before), []byte(after), "mcpServers", "a", stdio("z"), nil); err == nil {
			t.Errorf("%s: should be caught", name)
		}
	}
}

// writeJSONServer keeps the file's mode and leaves the file alone when the
// edit can't be made.
func TestWriteJSONServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "mcp_config.json")
	if err := writeJSONServer(path, "mcpServers", "a", stdio("x"), nil); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("a new config should be private, got %v", fi.Mode().Perm())
	}
	_ = os.Chmod(path, 0o644)
	if err := writeJSONServer(path, "mcpServers", "a", stdio("y"), nil); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Fatalf("the file's mode should be kept, got %v", fi.Mode().Perm())
	}
	bad := `{"mcpServers": {"a": "sh"}}`
	_ = os.WriteFile(path, []byte(bad), 0o644)
	if err := writeJSONServer(path, "mcpServers", "a", stdio("y"), nil); err == nil || !strings.Contains(err.Error(), "unsupported layout") {
		t.Fatalf("expected a layout error, got %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != bad {
		t.Fatalf("the file changed: %s", data)
	}
}

// Should the editor ever touch more than it was asked to, the check before
// writing refuses the result and the file is left as it was.
func TestWriteJSONServerRefusesABadEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	before := `{"mcpServers": {"a": {"command": "x"}, "b": {"command": "y"}}}`
	_ = os.WriteFile(path, []byte(before), 0o600)
	t.Cleanup(func() { editJSON = editJSONServer })
	editJSON = func(doc []byte, section, name string, set []jsonKV, drop []string) ([]byte, error) {
		return []byte(`{"mcpServers": {"a": {"command": "z"}}}`), nil // and b is gone
	}
	if err := writeJSONServer(path, "mcpServers", "a", stdio("z"), nil); err == nil || !strings.Contains(err.Error(), "nothing was written") {
		t.Fatalf("expected the edit to be refused, got %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != before {
		t.Fatalf("the file changed: %s", data)
	}
}
