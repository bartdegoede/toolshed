package shed

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

// Remote servers authenticate with HTTP headers. An entry's `headers` may use
// its inputs (`Authorization: Bearer ${GITHUB_TOKEN}`). With a keyring,
// toolshed registers a headers helper that both clients run when they
// connect (Claude Code: headersHelper, Codex: http_headers_helper):
//
//	toolshed headers <id> --secret KEY=group ... --set KEY=value ... --header 'Name=template' ...
//
// It reads the secrets from the keyring and prints the headers as JSON, so
// the token is never in a config file. Like `toolshed exec`, everything it
// needs is in its arguments.

// LiteralSecretHeaders lists headers that look like they carry a credential
// (Authorization, X-Api-Key, Cookie, …) but have the value written in, not
// as ${KEY}. That value would end up in the registry file in plain text.
func LiteralSecretHeaders(e *registry.Entry) []string {
	var out []string
	for _, name := range sortedKeys(e.Headers) {
		if SecretHeader(name) && len(registry.HeaderKeys(e.Headers[name])) == 0 {
			out = append(out, name)
		}
	}
	return out
}

// HeadersArgs builds the arguments of `toolshed headers` (after the binary).
func HeadersArgs(e *registry.Entry, refs []SecretRef, plain map[string]string) []string {
	args := []string{"headers", e.ID}
	for _, r := range refs {
		args = append(args, "--secret", r.Key+"="+r.Group)
	}
	for _, k := range sortedKeys(plain) {
		args = append(args, "--set", k+"="+plain[k])
	}
	for _, name := range sortedKeys(e.Headers) {
		args = append(args, "--header", name+"="+e.Headers[name])
	}
	return args
}

// HeadersCall is a parsed `toolshed headers` helper.
type HeadersCall struct {
	Self      string            // the toolshed binary it runs
	ID        string            //
	Templates map[string]string // header name → template
	Secrets   map[string]string // key → keyring group
	Values    map[string]string // non-secret key → value
}

// ParseHeadersArgs parses the arguments of `toolshed headers` (after
// "headers").
func ParseHeadersArgs(args []string) (HeadersCall, error) {
	c := HeadersCall{Templates: map[string]string{}, Secrets: map[string]string{}, Values: map[string]string{}}
	if len(args) == 0 {
		return c, errors.New("headers needs an id")
	}
	c.ID = args[0]
	for i := 1; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return c, fmt.Errorf("%s needs a value", args[i])
		}
		k, v, ok := strings.Cut(args[i+1], "=")
		if !ok || k == "" {
			return c, fmt.Errorf("%s expects NAME=VALUE, got %q", args[i], args[i+1])
		}
		switch args[i] {
		case "--secret":
			c.Secrets[k] = v
		case "--set":
			c.Values[k] = v
		case "--header":
			c.Templates[k] = v
		default:
			return c, fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	return c, nil
}

// Headers resolves the call: secrets from the keyring, then the templates.
func (c HeadersCall) Headers() (map[string]string, error) {
	vals := map[string]string{}
	for k, v := range c.Values {
		vals[k] = v
	}
	if len(c.Secrets) > 0 && !secrets.Available() {
		return nil, errors.New("no OS keyring available")
	}
	for k, g := range c.Secrets {
		v, err := secrets.Get(g, k)
		if err != nil {
			return nil, fmt.Errorf("%s: %s isn't in your keyring; run `toolshed install %s`", c.ID, k, c.ID)
		}
		vals[k] = v
	}
	out, missing := registry.ExpandHeaders(c.Templates, vals)
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s: no value for %s", c.ID, strings.Join(missing, ", "))
	}
	return out, nil
}

// UnwrapHeaders recognises a headers helper toolshed registered.
func UnwrapHeaders(helper string) (HeadersCall, bool) {
	words, err := shellSplit(helper)
	if err != nil || len(words) < 3 || filepath.Base(words[0]) != "toolshed" || words[1] != "headers" {
		return HeadersCall{}, false
	}
	c, err := ParseHeadersArgs(words[2:])
	if err != nil {
		return HeadersCall{}, false
	}
	c.Self = words[0]
	return c, true
}

// remoteConfig is BuildConfig for remote servers.
func remoteConfig(e *registry.Entry, vals Values, scope string) (clients.Config, error) {
	cfg := clients.Config{Type: "http", URL: e.URL, HeadersHelper: e.HeaderCmd}
	if len(e.Headers) == 0 {
		return cfg, nil
	}
	var refs []SecretRef
	plain := map[string]string{}
	for _, in := range e.Inputs {
		v := vals[in.Key]
		if !in.Secret || !secrets.Available() {
			if v != "" {
				plain[in.Key] = v
			}
			continue
		}
		refs = append(refs, SecretRef{Key: in.Key, Group: e.SecretGroup(in)})
		if v != "" {
			if err := secrets.Set(e.SecretGroup(in), in.Key, v); err != nil {
				return clients.Config{}, err
			}
		}
	}
	if len(refs) == 0 { // nothing secret, or no keyring: plain headers
		cfg.Headers, _ = registry.ExpandHeaders(e.Headers, plain)
		return cfg, nil
	}
	self, err := wrapperPath(scope)
	if err != nil {
		return clients.Config{}, err
	}
	words := append([]string{self}, HeadersArgs(e, refs, plain)...)
	for i, w := range words {
		words[i] = registry.ShellQuote(w)
	}
	cfg.HeadersHelper = strings.Join(words, " ")
	return cfg, nil
}

// wrapperPath is how a registration calls toolshed: its absolute path, or
// the bare command for project scope (shared with teammates).
func wrapperPath(scope string) (string, error) {
	if scope == "project" {
		return "toolshed", nil
	}
	return selfPath()
}

// selfPath is swapped in tests, whose binary isn't called toolshed.
var selfPath = SelfPath

// EntryHeaders resolves a remote entry's headers for Probe: the values it's
// installed with, its secrets from the keyring, and input defaults.
func EntryHeaders(e *registry.Entry) (map[string]string, error) {
	vals, _ := Resolve(e, clients.Detect()[e.ID], nil)
	out, missing := registry.ExpandHeaders(e.Headers, vals)
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s needs %s first: run `toolshed install %s`", e.ID, strings.Join(missing, ", "), e.ID)
	}
	return out, nil
}

// remoteAuthChanges compares how a registration authenticates with what the
// entry wants. Values are never compared, only where they come from.
func remoteAuthChanges(e *registry.Entry, cfg clients.Config) []string {
	describe := func(helper string, headers map[string]string) string {
		if c, ok := UnwrapHeaders(helper); ok {
			return "headers " + strings.Join(sortedKeys(c.Templates), ", ") + " from your keyring"
		}
		if helper != "" {
			return "headers from: " + helper
		}
		if len(headers) > 0 {
			return "headers " + strings.Join(sortedKeys(headers), ", ")
		}
		return "no auth headers"
	}
	was := describe(cfg.HeadersHelper, cfg.Headers)
	switch {
	case e.HeaderCmd != "":
		if cfg.HeadersHelper != e.HeaderCmd {
			return []string{fmt.Sprintf("gets its auth headers from %s (was: %s)", e.HeaderCmd, was)}
		}
	case len(e.Headers) > 0:
		if c, ok := UnwrapHeaders(cfg.HeadersHelper); ok {
			var out []string
			if !mapsEqual(c.Templates, e.Headers) {
				out = append(out, fmt.Sprintf("sends %s (was: %s)", headerList(e.Headers), headerList(c.Templates)))
			}
			for _, in := range e.Inputs {
				if g, ok := c.Secrets[in.Key]; in.Secret && ok && g != e.SecretGroup(in) {
					out = append(out, fmt.Sprintf("gets %s from %s's keyring entry (was: %s's)", in.Key, e.SecretGroup(in), g))
				} else if in.Secret && !ok && secrets.Available() {
					out = append(out, fmt.Sprintf("gets the secret %s from your keyring (%s)", in.Key, e.SecretGroup(in)))
				}
			}
			return out
		}
		if !sameKeys(cfg.Headers, e.Headers) {
			return []string{fmt.Sprintf("sends %s (was: %s)", headerList(e.Headers), was)}
		}
	default:
		if _, ok := UnwrapHeaders(cfg.HeadersHelper); ok || cfg.HeadersHelper != "" {
			return []string{fmt.Sprintf("sends no auth headers (was: %s)", was)}
		}
	}
	return nil
}

func headerList(h map[string]string) string {
	parts := make([]string, 0, len(h))
	for _, k := range sortedKeys(h) {
		parts = append(parts, k+": "+h[k])
	}
	return strings.Join(parts, ", ")
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func sameKeys(a, b map[string]string) bool {
	ka, kb := sortedKeys(a), sortedKeys(b)
	sort.Strings(ka)
	sort.Strings(kb)
	return strings.Join(ka, "\x00") == strings.Join(kb, "\x00")
}

// shellSplit splits a POSIX shell command line into words: single quotes,
// double quotes (with \" \\ \$ \` escapes) and backslashes. It's only used on
// helpers toolshed wrote, so it rejects anything fancier.
func shellSplit(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, errors.New("unterminated quote")
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
			inWord = true
		case c == '"':
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0 {
					i++
				} else if s[i] == '$' || s[i] == '`' {
					return nil, errors.New("expansion")
				}
				cur.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, errors.New("unterminated quote")
			}
			inWord = true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case strings.IndexByte("$`;&|<>(){}*?!#~", c) >= 0:
			return nil, errors.New("not a plain command")
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}
