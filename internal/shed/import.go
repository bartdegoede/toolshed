package shed

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

// Import is a server registered in Claude Code or Codex that toolshed doesn't
// manage yet, turned into a registry entry.
type Import struct {
	Name    string         // the name it's registered under
	Rename  bool           // Entry.ID differs from Name: re-register under the ID, remove the old name
	Entry   registry.Entry // what to add to the registry
	Values  Values         // its current env values: the inputs to install with
	Vars    []ImportVar    // its env vars, for the user to sort into secrets and fixed env
	Inst    *clients.Installation
	Clients []string // the clients to re-register it in
	Notes   []string // things the user should know (a client left alone, …)
	Skip    string   // why it can't be imported, if it can't
}

// ImportVar is one environment variable of an imported server.
type ImportVar struct {
	Key    string
	Value  string
	Secret bool // the guess (SecretName); callers let the user change it
}

// Imports finds the servers Claude Code and Codex have that aren't in the
// registry, including ones registered through `toolshed exec` whose entry was
// deleted (their secrets are already in the keyring). Sorted by name.
func Imports(reg *registry.Registry, installed map[string]*clients.Installation) []Import {
	var out []Import
	for name, inst := range installed {
		if reg.Get(name) != nil {
			continue
		}
		im := importOne(name, inst)
		if im.Skip == "" && reg.Get(im.Entry.ID) != nil {
			im.Skip = "it would become " + im.Entry.ID + ", which is already in your toolshed"
		}
		out = append(out, im)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func importOne(name string, inst *clients.Installation) Import {
	im := Import{Name: name, Inst: inst, Clients: inst.Clients(), Values: Values{}}
	cfg := inst.Config // Claude's when Claude has it, else Codex's
	if inst.ClaudeScope != "" && inst.Codex && !sameServer(inst.Config, inst.CodexConfig) {
		im.Clients = []string{clients.Claude}
		im.Notes = append(im.Notes, "Codex runs something else under this name, so it's left as it is")
	}
	id := sanitize(name)
	if !registry.IsValidID(id) {
		im.Skip = fmt.Sprintf("can't make a tool id from the name %q", name)
		return im
	}
	if id != name {
		im.Rename = true
		im.Notes = append(im.Notes, fmt.Sprintf("toolshed ids are lowercase with dashes, so it becomes %s", id))
	}
	e := registry.Entry{ID: id}
	switch {
	case cfg.Type == "sse":
		im.Skip = "it uses the old SSE transport, which toolshed doesn't support"
		return im
	case cfg.URL != "":
		e.URL, e.HeaderCmd = cfg.URL, cfg.HeadersHelper
		if c, ok := UnwrapHeaders(cfg.HeadersHelper); ok {
			// Registered by toolshed, then deleted from the registry.
			e.HeaderCmd, e.Headers = "", c.Templates
			for _, k := range sortedKeys(c.Secrets) {
				in := registry.Input{Key: k, Secret: true}
				if c.Secrets[k] != id {
					in.Share = c.Secrets[k]
				}
				e.Inputs = append(e.Inputs, in)
			}
			for _, k := range sortedKeys(c.Values) {
				im.Vars = append(im.Vars, ImportVar{Key: k, Value: c.Values[k]})
			}
		} else if len(cfg.Headers) > 0 && cfg.HeadersHelper == "" {
			// Static headers, usually a token in plain text: each becomes an
			// input, so a secret one can move to the keyring.
			e.Headers = map[string]string{}
			for _, name := range sortedKeys(cfg.Headers) {
				v := cfg.Headers[name]
				key := headerKey(name)
				tmpl := "${" + key + "}"
				if strings.EqualFold(name, "Authorization") && strings.HasPrefix(v, "Bearer ") {
					key = strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_TOKEN"
					tmpl, v = "Bearer ${"+key+"}", strings.TrimPrefix(v, "Bearer ")
				}
				e.Headers[name] = tmpl
				im.Vars = append(im.Vars, ImportVar{Key: key, Value: v, Secret: v != "" && SecretHeader(name)})
			}
		}
		if c := inst.CodexConfig; inst.Codex && (c.BearerTokenEnvVar != "" || len(c.EnvHeaders) > 0) {
			im.Notes = append(im.Notes, "Codex reads its token from your environment (bearer_token_env_var / env_http_headers); that setting stays as it is")
		}
	default:
		cmd, refs, wrapped := Unwrap(cfg)
		if len(cmd) == 0 || cmd[0] == "" {
			im.Skip = "its registration has no command"
			return im
		}
		e.Command, e.Args = cmd[0], cmd[1:]
		if wrapped {
			// Registered by toolshed, then deleted from the registry: its
			// secrets are already in the keyring, under their groups.
			for _, k := range sortedKeys(refs) {
				in := registry.Input{Key: k, Secret: true}
				if refs[k] != id {
					in.Share = refs[k]
				}
				e.Inputs = append(e.Inputs, in)
			}
		}
		for _, k := range sortedKeys(cfg.Env) {
			v := cfg.Env[k]
			im.Vars = append(im.Vars, ImportVar{Key: k, Value: v, Secret: v != "" && (SecretName(k) || SecretValue(v))})
		}
	}
	im.Entry = e
	return im
}

// Build turns the vars (as the user sorted them) into the entry's inputs and
// the values to install with. Every var becomes an input, not fixed env, so
// no value ever lands in the registry file (which may be shared): secrets go
// to the keyring, the rest stay in the clients' config, just like values
// typed at `toolshed install`.
func (im *Import) Build() registry.Entry {
	e := im.Entry
	e.Inputs = append([]registry.Input(nil), e.Inputs...)
	for _, v := range im.Vars {
		in := registry.Input{Key: v.Key, Secret: v.Secret && v.Value != ""}
		if v.Value == "" {
			in.Optional = true
		}
		e.Inputs = append(e.Inputs, in)
		im.Values[v.Key] = v.Value
	}
	return e
}

// SecretHeader guesses from an HTTP header's name whether it carries a
// credential.
func SecretHeader(name string) bool {
	n := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	return SecretName(n) || strings.Contains(n, "KEY") || n == "COOKIE"
}

// headerKey turns a header name into an input key: X-Api-Key → X_API_KEY.
func headerKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	k := strings.Trim(b.String(), "_")
	if k == "" || k[0] >= '0' && k[0] <= '9' {
		k = "H_" + k
	}
	return k
}

// SecretValue guesses from a value whether it's a secret whatever its name:
// a URL with a password in it (postgresql://app:hunter2@db/app).
func SecretValue(v string) bool {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil || u.User == nil {
		return false
	}
	_, hasPassword := u.User.Password()
	return hasPassword
}

// SecretName guesses from an env var's name whether it holds a secret.
func SecretName(key string) bool {
	k := strings.ToUpper(key)
	for _, w := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "APIKEY", "ACCESS_KEY", "PRIVATE_KEY", "CREDENTIAL", "AUTH", "COOKIE", "SESSION"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return strings.HasSuffix(k, "_KEY") || strings.HasSuffix(k, "_PAT") || k == "PAT"
}

// sameServer reports whether two registrations run the same thing with the
// same environment.
func sameServer(a, b clients.Config) bool {
	if a.URL != "" || b.URL != "" {
		return a.URL == b.URL
	}
	ca, _, _ := Unwrap(a)
	cb, _, _ := Unwrap(b)
	return slices.Equal(ca, cb) && maps.Equal(a.Env, b.Env)
}

func sanitize(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), "_", "-")
}

// ImportResult is what Apply did.
type ImportResult struct {
	Entry     registry.Entry
	Installed bool     // re-registered through toolshed (false: only added to the registry)
	Moved     []string // secrets now in the keyring
	Notes     []string // why it wasn't installed, client notes, …
}

// Apply imports im as the user sorted its vars: it re-installs the server
// where it is (unless install is false, its command is missing, or a secret
// is gone from the keyring), removes an old name, and adds it to the
// registry. The CLI and the TUI both use it.
func (im *Import) Apply(install bool) (ImportResult, error) {
	e := im.Build()
	res := ImportResult{Entry: e}
	vals, missing := Resolve(&e, nil, im.Values)
	switch {
	case !install:
	case len(missing) > 0:
		keys := make([]string, len(missing))
		for i, in := range missing {
			keys[i] = in.Key
		}
		res.Notes = append(res.Notes, strings.Join(keys, ", ")+" isn't in your keyring anymore, so it's only added to your registry")
		install = false
	case CheckCommand(&e) != nil:
		res.Notes = append(res.Notes, e.Command+" isn't on PATH, so it's only added to your registry")
		install = false
	}
	if install {
		opts := KeepScopes(im.Inst)
		opts.Clients = im.Clients
		r, err := Install(&e, vals, opts)
		if err != nil {
			return res, err
		}
		res.Installed = true
		res.Notes = append(res.Notes, r.Notes...)
		if im.Rename {
			for _, c := range im.Clients {
				if err := clients.Unregister(c, im.Name, im.Inst.Scope(c)); err != nil {
					res.Notes = append(res.Notes, fmt.Sprintf("couldn't remove the old %s from %s: %v", im.Name, c, err))
				}
			}
		}
		if secrets.Available() {
			res.Moved = e.SecretKeys()
		}
	}
	if err := registry.AddUserEntry(e); err != nil {
		return res, err
	}
	return res, nil
}
