package shed

import (
	"reflect"
	"strings"
	"testing"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

func ghEntry() *registry.Entry {
	return &registry.Entry{ID: "gh", URL: "https://api.example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer ${GH_TOKEN}", "X-Team": "${TEAM}"},
		Inputs:  []registry.Input{{Key: "GH_TOKEN", Secret: true}, {Key: "TEAM"}}}
}

// The helper toolshed registers must parse back into what it was built from,
// and resolve the secret from the keyring.
func TestHeadersHelperRoundTrip(t *testing.T) {
	secrets.UseMockForTest(true)
	old := selfPath
	selfPath = func() (string, error) { return "/opt/my tools/bin/toolshed", nil }
	t.Cleanup(func() { selfPath = old })
	e := ghEntry()
	cfg, err := BuildConfig(e, Values{"GH_TOKEN": "s3cret", "TEAM": "core it's"}, "user")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.HeadersHelper, "s3cret") || len(cfg.Headers) != 0 {
		t.Fatalf("the token must not be in the registration: %+v", cfg)
	}
	call, ok := UnwrapHeaders(cfg.HeadersHelper)
	if !ok {
		t.Fatalf("not recognised as toolshed's helper: %s", cfg.HeadersHelper)
	}
	if !reflect.DeepEqual(call.Templates, e.Headers) || call.Secrets["GH_TOKEN"] != "gh" || call.Values["TEAM"] != "core it's" {
		t.Fatalf("parsed: %+v", call)
	}
	headers, err := call.Headers()
	if err != nil {
		t.Fatal(err)
	}
	if headers["Authorization"] != "Bearer s3cret" || headers["X-Team"] != "core it's" {
		t.Fatalf("headers: %v", headers)
	}
	if vals, _ := Resolve(e, inClaude(cfg), nil); vals["TEAM"] != "core it's" || vals["GH_TOKEN"] != "s3cret" {
		t.Fatalf("Resolve should read back an installed remote tool's values: %v", vals)
	}
	if got := Changes(e, inClaude(cfg)); len(got) != 0 {
		t.Fatalf("unchanged entry reported as changed: %v", got)
	}
	e.Headers["Authorization"] = "Token ${GH_TOKEN}"
	if got := Changes(e, inClaude(cfg)); len(got) != 1 || !strings.Contains(got[0], "Token ${GH_TOKEN}") {
		t.Fatalf("header change not reported: %v", got)
	}
}

func TestHeadersWithoutKeyringAreStatic(t *testing.T) {
	secrets.UseMockForTest(false)
	cfg, err := BuildConfig(ghEntry(), Values{"GH_TOKEN": "s3cret", "TEAM": "core"}, "user")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HeadersHelper != "" || cfg.Headers["Authorization"] != "Bearer s3cret" {
		t.Fatalf("without a keyring headers go in the config: %+v", cfg)
	}
}

func TestLiteralSecretHeaders(t *testing.T) {
	e := &registry.Entry{URL: "https://x", Headers: map[string]string{
		"Authorization": "Bearer ghp_literal", "X-Api-Key": "${K}", "X-Team": "core", "Cookie": "a=b"}}
	if got := LiteralSecretHeaders(e); !reflect.DeepEqual(got, []string{"Authorization", "Cookie"}) {
		t.Fatalf("got %v", got)
	}
}

func TestShellSplit(t *testing.T) {
	words, err := shellSplit(`'/opt/my dir/toolshed' headers gh --header 'A=Bearer ${T}' --set 'X=it'\''s' plain\ word "q\"d"`)
	want := []string{"/opt/my dir/toolshed", "headers", "gh", "--header", "A=Bearer ${T}", "--set", "X=it's", "plain word", `q"d`}
	if err != nil || !reflect.DeepEqual(words, want) {
		t.Fatalf("got %q %v", words, err)
	}
	for _, bad := range []string{"echo $(x)", "a; b", "a | b", `"$HOME"`, "'open"} {
		if _, err := shellSplit(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestImportStaticHeaders(t *testing.T) {
	reg := &registry.Registry{}
	installed := map[string]*clients.Installation{
		"hosted": inClaude(clients.Config{Type: "http", URL: "https://h.example/mcp",
			Headers: map[string]string{"Authorization": "Bearer ghp_abc", "X-Team": "core"}}),
	}
	ims := Imports(reg, installed)
	if len(ims) != 1 || ims[0].Skip != "" {
		t.Fatalf("should be importable: %+v", ims)
	}
	im := ims[0]
	e := im.Build()
	if e.Headers["Authorization"] != "Bearer ${HOSTED_TOKEN}" || e.Headers["X-Team"] != "${X_TEAM}" {
		t.Fatalf("templates: %v", e.Headers)
	}
	if im.Values["HOSTED_TOKEN"] != "ghp_abc" || im.Values["X_TEAM"] != "core" {
		t.Fatalf("values: %v", im.Values)
	}
	if !reflect.DeepEqual(e.SecretKeys(), []string{"HOSTED_TOKEN"}) {
		t.Fatalf("only the token is a secret: %+v", e.Inputs)
	}
	if err := registry.ValidateEntry(&e); err != nil {
		t.Fatalf("imported entry is invalid: %v", err)
	}
}
