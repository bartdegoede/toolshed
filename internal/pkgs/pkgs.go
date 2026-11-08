// Package pkgs finds the npm or PyPI package an npx or uvx tool runs, and
// pins it to an exact version.
//
// `npx -y some-server` runs whatever was published last, every time the agent
// starts it, with the tool's secrets in its environment. A compromised or
// broken release would reach you without you doing anything. Pinned, a tool
// only changes when you run `toolshed upgrade`.
package pkgs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bartdegoede/toolshed/internal/registry"
)

// Ref is the package an npx or uvx entry runs.
type Ref struct {
	Kind    string // registry.KindNPX or registry.KindUVX
	Name    string // npm or PyPI package name
	Version string // what it's pinned to; "" when it isn't
	Exact   bool   // Version is one exact release (not a range or tag)

	extras string // PyPI extras, e.g. "[cli]"
	index  int    // position of the package in Entry.Args
	prefix string // "--from=" when the package came from that flag
}

// Find returns the package an entry runs. ok is false for anything that
// isn't an npx or uvx entry with a registry package (a git URL, a local path,
// or flags toolshed doesn't know, since it then can't tell which argument is
// the package).
func Find(e *registry.Entry) (Ref, bool) {
	switch e.Command {
	case "npx":
		return find(e.Args, registry.KindNPX)
	case "uvx":
		return find(e.Args, registry.KindUVX)
	}
	return Ref{}, false
}

// Flags that take no value, and flags whose value is the package (pkgFlag)
// or something else (skip), per tool.
var (
	boolFlags = map[string][]string{
		registry.KindNPX: {"-y", "--yes", "-q", "--quiet"},
		registry.KindUVX: {"-q", "--quiet", "--isolated", "--refresh", "--offline", "--no-cache"},
	}
	pkgFlags = map[string][]string{
		registry.KindNPX: {"-p", "--package"},
		registry.KindUVX: {"--from"},
	}
	skipFlags = map[string][]string{
		registry.KindUVX: {"--with", "--python", "-p", "--index-url", "--default-index", "--extra-index-url"},
	}
)

func find(args []string, kind string) (Ref, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case has(boolFlags[kind], a):
			continue
		case has(pkgFlags[kind], a):
			if i+1 >= len(args) {
				return Ref{}, false
			}
			return parse(kind, args[i+1], i+1, "")
		case kind == registry.KindUVX && strings.HasPrefix(a, "--from="):
			return parse(kind, strings.TrimPrefix(a, "--from="), i, "--from=")
		case has(skipFlags[kind], a):
			i++
			continue
		case strings.HasPrefix(a, "-"):
			return Ref{}, false
		default:
			return parse(kind, a, i, "")
		}
	}
	return Ref{}, false
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func parse(kind, spec string, index int, prefix string) (Ref, bool) {
	if spec == "" || strings.Contains(spec, ":") || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") {
		return Ref{}, false // a URL, git spec or local path: nothing to pin
	}
	r := Ref{Kind: kind, index: index, prefix: prefix}
	if kind == registry.KindNPX {
		r.Name = spec
		if i := strings.LastIndex(spec, "@"); i > 0 { // > 0: "@scope/name" starts with one
			r.Name, r.Version = spec[:i], spec[i+1:]
		}
		if strings.Count(r.Name, "/") > 1 || (strings.Contains(r.Name, "/") && !strings.HasPrefix(r.Name, "@")) {
			return Ref{}, false // a GitHub shorthand like user/repo
		}
	} else {
		name, version := spec, ""
		if i := strings.IndexAny(spec, "=<>!~@"); i >= 0 {
			name, version = spec[:i], strings.TrimLeft(spec[i:], "=@")
			if strings.ContainsAny(version, "<>!~,*") || strings.HasPrefix(spec[i:], "=") && !strings.HasPrefix(spec[i:], "==") {
				version = spec[i:] // a range: keep it as written, it's not exact
			}
		}
		if i := strings.IndexByte(name, '['); i >= 0 {
			name, r.extras = name[:i], name[i:]
		}
		r.Name, r.Version = name, version
	}
	r.Exact = isExact(r.Version)
	return r, r.Name != ""
}

// isExact reports whether v names one release ("2.1.0", "2026.11.4",
// "1.0.0-rc.1") rather than a tag ("latest") or a range ("^2", ">=1").
func isExact(v string) bool {
	if v == "" || v[0] < '0' || v[0] > '9' {
		return false
	}
	return !strings.ContainsAny(v, "^~<>=!*|, ") && !strings.Contains(v, ".x") && !strings.Contains(v, ".X")
}

// Spec is the package argument pinned to version, as npx or uvx takes it.
func (r Ref) Spec(version string) string {
	if r.Kind == registry.KindNPX {
		return r.Name + "@" + version
	}
	return r.Name + r.extras + "==" + version
}

// Pin returns e's arguments with the package pinned to version.
func (r Ref) Pin(e *registry.Entry, version string) []string {
	args := append([]string(nil), e.Args...)
	args[r.index] = r.prefix + r.Spec(version)
	return args
}

// Timeout bounds one version lookup.
var Timeout = 10 * time.Second

// Latest asks the package registry for the newest release. npm uses
// npm_config_registry when set (as npm does); PyPI can be pointed elsewhere
// with TOOLSHED_PYPI_URL (it needs PyPI's JSON API).
func Latest(ctx context.Context, r Ref) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	var u string
	var out struct {
		Version string `json:"version"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if r.Kind == registry.KindNPX {
		base := firstEnv("npm_config_registry", "NPM_CONFIG_REGISTRY")
		if base == "" {
			base = "https://registry.npmjs.org"
		}
		u = strings.TrimRight(base, "/") + "/" + url.PathEscape(r.Name) + "/latest"
	} else {
		base := firstEnv("TOOLSHED_PYPI_URL")
		if base == "" {
			base = "https://pypi.org"
		}
		u = strings.TrimRight(base, "/") + "/pypi/" + url.PathEscape(r.Name) + "/json"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("looking up %s: %w", r.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%s isn't on %s", r.Name, registryName(r.Kind))
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("looking up %s: %s", r.Name, resp.Status)
	}
	// PyPI's JSON lists every release, so allow it some room.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("looking up %s: %w", r.Name, err)
	}
	v := out.Version
	if r.Kind == registry.KindUVX {
		v = out.Info.Version
	}
	if !isExact(v) {
		return "", fmt.Errorf("looking up %s: unexpected version %q", r.Name, v)
	}
	return v, nil
}

// ErrNothingToPin is returned by PinLatest for entries without a package, or
// whose package is already pinned to an exact version.
var ErrNothingToPin = errors.New("nothing to pin")

// PinLatest pins an unpinned npx or uvx entry to the newest release, in
// place, and returns that version.
func PinLatest(ctx context.Context, e *registry.Entry) (string, error) {
	r, ok := Find(e)
	if !ok || r.Exact {
		return "", ErrNothingToPin
	}
	v, err := Latest(ctx, r)
	if err != nil {
		return "", err
	}
	e.Args = r.Pin(e, v)
	return v, nil
}

func registryName(kind string) string {
	if kind == registry.KindNPX {
		return "npm"
	}
	return "PyPI"
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
