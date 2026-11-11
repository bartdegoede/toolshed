// Package shed implements toolshed's operations (install, uninstall, exec,
// probe) on top of the registry, the client adapters and the keyring. The CLI
// and the TUI are both thin front ends over this package.
package shed

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/secrets"
)

// Values maps input keys to the values the user chose.
type Values map[string]string

// Resolve fills in input values from, in order: explicit overrides, the
// currently installed config (so updates keep earlier choices), the keyring
// (secrets), and defaults. It returns the values plus the required inputs
// that are still missing, which the caller should prompt for.
func Resolve(e *registry.Entry, inst *clients.Installation, overrides Values) (Values, []registry.Input) {
	vals := Values{}
	var missing []registry.Input
	for _, in := range e.Inputs {
		v := overrides[in.Key]
		if v == "" && inst != nil {
			cfg := inst.Config()
			v = cfg.Env[in.Key] // only present for non-secrets (or keyring-less installs)
			if c, ok := UnwrapHeaders(cfg.HeadersHelper); v == "" && ok {
				v = c.Values[in.Key] // a remote tool's non-secret values
			}
		}
		if v == "" && in.Secret && secrets.Available() {
			v, _ = secrets.Get(e.SecretGroup(in), in.Key)
		}
		if v == "" {
			v = in.Default
		}
		if v != "" {
			vals[in.Key] = v
		} else if !in.Optional {
			missing = append(missing, in)
		}
	}
	return vals, missing
}

// InstallOptions controls where a tool is installed.
type InstallOptions struct {
	Scope   string            // "user" (default), "local" or "project"; each client uses its nearest (Codex has no "local")
	Scopes  map[string]string // per-client scopes, overriding Scope
	Clients []string          // targets; empty means every available client the entry supports
}

func (o InstallOptions) scopeFor(client string) string {
	if s := o.Scopes[client]; s != "" {
		return s
	}
	return clients.Get(client).Scope(o.Scope)
}

// KeepScopes is the InstallOptions for installing a tool again exactly where
// it is now: the same clients, each at its current scope. Scope is the first
// client's, which is what the CLI reports.
func KeepScopes(inst *clients.Installation) InstallOptions {
	opts := InstallOptions{Scope: "user", Scopes: map[string]string{}, Clients: inst.Clients()}
	for i, c := range opts.Clients {
		opts.Scopes[c] = inst.Scope(c)
		if i == 0 {
			opts.Scope = inst.Scope(c)
		}
	}
	return opts
}

// Move is a client in which an install would move a tool to another scope.
type Move struct {
	Client, From, To string
}

// Moves returns the clients in which installing e with opts would move it
// from where inst has it to another scope. Install does that (see
// clients.Register); front ends use this to describe it, or refuse it.
func Moves(e *registry.Entry, inst *clients.Installation, opts InstallOptions) []Move {
	if inst == nil {
		return nil
	}
	if opts.Scope == "" {
		opts.Scope = "user"
	}
	var out []Move
	for _, c := range Targets(e, opts) {
		if from, to := inst.Scope(c), opts.scopeFor(c); from != "" && to != "" && from != to {
			out = append(out, Move{Client: c, From: from, To: to})
		}
	}
	return out
}

// Result is what Install did.
type Result struct {
	Clients []string // the clients it's now registered in
	Notes   []string // things the user should know
}

// Targets returns the clients an install would write to.
func Targets(e *registry.Entry, opts InstallOptions) []string {
	want := opts.Clients
	if len(want) == 0 {
		want = clients.All
	}
	var out []string
	for _, c := range want {
		if !e.SupportsClient(c) || !clients.Available(c) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// Install stores secrets, builds the client config and registers the tool
// with every target client. It fails if any client fails.
func Install(e *registry.Entry, vals Values, opts InstallOptions) (Result, error) {
	if opts.Scope == "" {
		opts.Scope = "user"
	}
	if err := CheckApproved(e); err != nil {
		return Result{}, err
	}
	targets := Targets(e, opts)
	if len(targets) == 0 {
		return Result{}, fmt.Errorf("no target client: install %s (or check %s's clients list)", clients.Labels("or"), e.ID)
	}
	if err := CheckCommand(e); err != nil {
		return Result{}, err
	}
	inProject := false
	for _, c := range targets {
		inProject = inProject || opts.scopeFor(c) == "project"
	}
	for _, in := range e.Inputs {
		if vals[in.Key] == "" && !in.Optional {
			return Result{}, fmt.Errorf("%s needs a value for %s", e.ID, in.Key)
		}
		// Project scope writes .mcp.json / .codex/config.toml, meant to be committed.
		// Without a keyring the secret would go into it in plain text.
		if inProject && in.Secret && vals[in.Key] != "" && !secrets.Available() {
			return Result{}, fmt.Errorf("%s: no OS keyring, so %s would be written into the repository's config in plain text; install it for your user instead", e.ID, in.Key)
		}
	}
	before := clients.Detect()[e.ID]
	moves := Moves(e, before, opts)
	var res Result
	var errs []error
	for _, c := range targets {
		scope := opts.scopeFor(c)
		if scope == "" {
			// The client has nothing like the scope asked for (Antigravity
			// and Claude Code's private "local"): skip it rather than make
			// the tool global or shared, and take it out if it's somewhere
			// else there now, as moving it would.
			asked := opts.Scope
			if s := opts.Scopes[c]; s != "" {
				asked = s
			}
			if before != nil && before.Has(c) {
				if err := clients.Unregister(c, e.ID, before.Scope(c)); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", c, err))
					continue
				}
				res.Notes = append(res.Notes, fmt.Sprintf("%s has no %s scope, so %s was removed from it", clients.Label(c), asked, e.ID))
				continue
			}
			res.Notes = append(res.Notes, fmt.Sprintf("%s has no %s scope, so %s isn't installed there", clients.Label(c), asked, e.ID))
			continue
		}
		cfg, err := BuildConfig(e, vals, scope)
		if err != nil {
			return res, err
		}
		if why := clients.Get(c).Unsupported(cfg); why != "" {
			res.Notes = append(res.Notes, fmt.Sprintf("%s isn't installed in %s: %s", e.ID, clients.Label(c), why))
			continue
		}
		if err := clients.Register(c, e.ID, cfg, scope); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c, err))
			continue
		}
		res.Clients = append(res.Clients, c)
		for _, mv := range moves {
			if mv.Client == c {
				res.Notes = append(res.Notes, fmt.Sprintf("%s: moved from %s to %s scope", clients.Label(c), mv.From, mv.To))
			}
		}
		res.Notes = append(res.Notes, clients.Get(c).Notes(scope)...)
	}
	if len(res.Clients) == 0 && len(errs) == 0 {
		return res, fmt.Errorf("%s couldn't be installed anywhere: %s", e.ID, strings.Join(res.Notes, "; "))
	}
	return res, errors.Join(errs...)
}

// ErrPending is returned for tools an agent added that the user hasn't
// approved yet.
var ErrPending = errors.New("added by your agent and not approved yet")

// CheckApproved refuses tools an agent added until the user has seen what
// they run and approved them. Install and Probe both call it, so no front
// end (CLI, TUI, MCP) can start an unapproved command.
func CheckApproved(e *registry.Entry) error {
	if e.Pending {
		return fmt.Errorf("%s was %w: review what it runs with `toolshed approve %s` (or select it in `toolshed`)", e.ID, ErrPending, e.ID)
	}
	return nil
}

// BuildConfig produces the client registration for an entry. Secret values
// are written to the keyring and the server is wrapped in `toolshed exec`;
// without a keyring they fall back to plain env vars in the client config.
//
// For project scope the wrapper is the bare `toolshed` command rather than
// this binary's absolute path, because .mcp.json is shared through the
// repository and teammates have toolshed installed in other places.
func BuildConfig(e *registry.Entry, vals Values, scope string) (clients.Config, error) {
	if e.IsRemote() {
		return remoteConfig(e, vals, scope)
	}
	env := map[string]string{}
	for k, v := range e.Env {
		env[k] = v
	}
	var refs []SecretRef
	for _, in := range e.Inputs {
		v := vals[in.Key]
		if !in.Secret || !secrets.Available() {
			if v != "" {
				env[in.Key] = v
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
	cfg := clients.Config{Type: "stdio", Command: e.Command, Args: e.Args, Env: env}
	if len(refs) > 0 {
		self, err := wrapperPath(scope)
		if err != nil {
			return clients.Config{}, err
		}
		cfg.Command, cfg.Args = self, ExecArgs(e, refs)
	}
	if len(cfg.Env) == 0 {
		cfg.Env = nil
	}
	return cfg, nil
}

// CheckCommand verifies a local server's command can be found, so a typo
// fails at install instead of as a silent "failed to connect" in the client.
func CheckCommand(e *registry.Entry) error {
	if e.IsRemote() {
		return nil
	}
	if _, err := exec.LookPath(e.Command); err != nil {
		return fmt.Errorf("%s: command %q not found on PATH — install it first", e.ID, e.Command)
	}
	return nil
}

// UninstallCopy removes one registration of the tool: in client (or, with
// client "", every client), at scope (or, with scope "", the one each client
// uses here). Unlike Uninstall it also reaches copies a client hides behind
// another scope (see doctor), and never touches secrets. It returns the
// clients it removed it from.
func UninstallCopy(e *registry.Entry, inst *clients.Installation, client, scope string) ([]string, error) {
	var removed []string
	var errs []error
	if inst != nil {
		for _, c := range inst.Clients() {
			if client != "" && c != client {
				continue
			}
			at := scope
			if at == "" {
				at = inst.Scope(c)
			}
			if _, ok := inst.At(c, at); !ok {
				continue
			}
			if err := clients.Unregister(c, e.ID, at); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", c, err))
				continue
			}
			removed = append(removed, c)
		}
	}
	if len(removed) == 0 && len(errs) == 0 {
		where := "anywhere"
		if client != "" {
			where = "in " + clients.Label(client)
		}
		if scope != "" {
			where += " at " + scope + " scope"
		}
		return nil, fmt.Errorf("%s isn't registered %s", e.ID, where)
	}
	return removed, errors.Join(errs...)
}

// Uninstall removes the tool from every client it's registered in. With
// purge it also deletes its secrets, except groups still used by another
// installed tool.
func Uninstall(reg *registry.Registry, e *registry.Entry, inst *clients.Installation, purge bool) error {
	var errs []error
	if inst != nil {
		for _, c := range inst.Clients() {
			if err := clients.Unregister(c, e.ID, inst.Scope(c)); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", c, err))
			}
		}
	}
	if purge && secrets.Available() {
		installed := clients.Detect()
		for _, in := range e.Inputs {
			if !in.Secret || groupInUse(reg, installed, e.ID, e.SecretGroup(in)) {
				continue
			}
			if err := secrets.Delete(e.SecretGroup(in), in.Key); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func groupInUse(reg *registry.Registry, installed map[string]*clients.Installation, self, group string) bool {
	for _, other := range reg.Entries {
		if other.ID == self || installed[other.ID] == nil {
			continue
		}
		for _, in := range other.Inputs {
			if in.Secret && other.SecretGroup(in) == group {
				return true
			}
		}
	}
	return false
}

// SecretRef names a keyring entry to load into the environment.
type SecretRef struct{ Key, Group string }

// ExecArgs builds the self-contained arguments for `toolshed exec`:
//
//	exec <id> --secret KEY=group ... -- <command> <args...>
//
// Everything exec needs is in the arguments, so a registered server keeps
// working even if the registry file moves or isn't visible to the client.
func ExecArgs(e *registry.Entry, refs []SecretRef) []string {
	args := []string{"exec", e.ID}
	for _, r := range refs {
		args = append(args, "--secret", r.Key+"="+r.Group)
	}
	return append(append(args, "--", e.Command), e.Args...)
}

// ExecEnv returns the environment for running a server: the current
// environment plus extra fixed values, with secrets from the keyring filled
// in for any key not already set (explicit env always wins).
func ExecEnv(extra map[string]string, refs []SecretRef) []string {
	env := os.Environ()
	have := map[string]bool{}
	for _, kv := range env {
		have[kv[:strings.IndexByte(kv+"=", '=')]] = true
	}
	add := func(k, v string) {
		if !have[k] && v != "" {
			env = append(env, k+"="+v)
			have[k] = true
		}
	}
	for k, v := range extra {
		add(k, v)
	}
	if secrets.Available() {
		for _, r := range refs {
			v, _ := secrets.Get(r.Group, r.Key)
			add(r.Key, v)
		}
	}
	return env
}

// EntryEnv is ExecEnv for running an entry directly (used by Probe): fixed
// env, input defaults, and the entry's secrets from the keyring.
func EntryEnv(e *registry.Entry) []string {
	extra := map[string]string{}
	for k, v := range e.Env {
		extra[k] = v
	}
	var refs []SecretRef
	for _, in := range e.Inputs {
		if in.Secret {
			refs = append(refs, SecretRef{Key: in.Key, Group: e.SecretGroup(in)})
		} else if in.Default != "" {
			extra[in.Key] = in.Default
		}
	}
	return ExecEnv(extra, refs)
}

// SelfPath is the absolute path of the running toolshed binary, with
// Homebrew's versioned Cellar path rewritten to the stable opt symlink so
// registrations survive `brew upgrade`.
func SelfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	return CellarToOpt(exe), nil
}

// CellarToOpt rewrites /…/Cellar/<formula>/<version>/rest to /…/opt/<formula>/rest.
func CellarToOpt(p string) string {
	i := strings.Index(p, "/Cellar/")
	if i < 0 {
		return p
	}
	parts := strings.SplitN(p[i+len("/Cellar/"):], "/", 3)
	if len(parts) < 3 {
		return p
	}
	return filepath.Join(p[:i], "opt", parts[0], parts[2])
}
