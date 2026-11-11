package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
	"github.com/bartdegoede/toolshed/internal/shed"
)

// Kinds of tool the add form can create.
const (
	kindCommand = registry.KindCommand
	kindNPX     = registry.KindNPX
	kindUVX     = registry.KindUVX
	kindURL     = registry.KindURL
)

// addSpec is what the add form collects.
type addSpec struct {
	Kind, Source, ID, Name, Secrets, Description string
	Token                                        bool // remote: send a bearer token from the keyring
}

// sourceTitle is the prompt for the kind-specific "what to run" field.
func sourceTitle(kind string) (title, placeholder string) {
	switch kind {
	case kindNPX:
		return "npm package (plus any extra args)", "@modelcontextprotocol/server-everything"
	case kindUVX:
		return "PyPI package (plus any extra args)", "mcp-server-time"
	case kindURL:
		return "Server URL", "https://mcp.example.com/mcp"
	}
	return "Command line", "my-cli mcp --stdio"
}

// toEntry turns the add form's answers into a registry entry.
func (s addSpec) toEntry() (registry.Entry, error) {
	secrets := strings.FieldsFunc(s.Secrets, func(r rune) bool { return r == ',' || r == ' ' })
	spec := registry.Spec{Kind: s.Kind, Source: s.Source, ID: s.ID, Name: s.Name, Description: s.Description, Secrets: secrets}
	if s.Kind != kindURL {
		return spec.Entry()
	}
	spec.Secrets = nil
	if s.Token {
		id := strings.TrimSpace(s.ID)
		if id == "" {
			id = registry.SuggestID(s.Kind, s.Source)
		}
		key := strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_TOKEN"
		spec.Secrets = []string{key}
		spec.Headers = map[string]string{"Authorization": "Bearer ${" + key + "}"}
	}
	return spec.Entry()
}

// newAddForm builds the "add a tool" wizard, writing answers into spec.
func newAddForm(spec *addSpec, reg *registry.Registry, width int) *huh.Form {
	spec.Kind = kindNPX
	srcTitle, srcPlaceholder := sourceTitle(spec.Kind)
	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What kind of tool?").
				Options(
					huh.NewOption("npm package      npx -y <package>", kindNPX),
					huh.NewOption("Python package   uvx <package>", kindUVX),
					huh.NewOption("Command on PATH  any CLI that speaks MCP", kindCommand),
					huh.NewOption("Remote server    https://…", kindURL),
				).
				Value(&spec.Kind),
			huh.NewInput().
				Title(srcTitle).Placeholder(srcPlaceholder).
				TitleFunc(func() string { t, _ := sourceTitle(spec.Kind); return t }, &spec.Kind).
				PlaceholderFunc(func() string { _, p := sourceTitle(spec.Kind); return p }, &spec.Kind).
				Value(&spec.Source).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("required")
					}
					return nil
				}),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("ID").
				DescriptionFunc(func() string {
					return "The name Claude Code and Codex will see. Leave empty for " + orDash(registry.SuggestID(spec.Kind, spec.Source))
				}, &spec.Source).
				PlaceholderFunc(func() string { return registry.SuggestID(spec.Kind, spec.Source) }, &spec.Source).
				Value(&spec.ID).
				Validate(func(s string) error {
					id := strings.TrimSpace(s)
					if id == "" {
						id = registry.SuggestID(spec.Kind, spec.Source)
					}
					if !registry.IsValidID(id) {
						return errors.New("use lowercase letters, digits and dashes")
					}
					if e := reg.Get(id); e != nil && e.Source == registry.SourceUser {
						return fmt.Errorf("you already have a tool called %s", id)
					}
					return nil
				}),
			huh.NewInput().Title("Display name").Description("Optional.").Value(&spec.Name),
			huh.NewInput().Title("Description").Description("Optional, one line.").Value(&spec.Description),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Does it need an API token?").
				Description("It's sent as \"Authorization: Bearer <token>\". You'll be asked for it at install;\nit's stored in your OS keyring, never in a config file. Other headers: toolshed add --header.").
				Affirmative("Yes").Negative("No").
				Value(&spec.Token),
		).WithHideFunc(func() bool { return spec.Kind != kindURL }),
		huh.NewGroup(
			huh.NewInput().
				Title("Secret environment variables").
				Description("Comma-separated, e.g. API_TOKEN. You'll be asked for values at install;\nthey're stored in your OS keyring, never in a config file.").
				Placeholder("none").
				Value(&spec.Secrets),
		).WithHideFunc(func() bool { return spec.Kind == kindURL }),
	).WithTheme(formTheme()).WithWidth(width).WithShowHelp(false)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// installForm collects input values and scope for an install.
type installForm struct {
	values map[string]*string
	scope  string
}

// newInstallForm returns nil when there's nothing to ask.
func newInstallForm(e *registry.Entry, vals shed.Values, width int) (*huh.Form, *installForm) {
	st := &installForm{values: map[string]*string{}, scope: "user"}
	var fields []huh.Field
	for _, in := range e.Inputs {
		v := vals[in.Key]
		st.values[in.Key] = &v
		title := in.Label
		if title == "" {
			title = in.Key
		}
		desc := in.Help
		if in.Secret {
			desc = strings.TrimSpace(desc + "\n🔒 stored in your OS keyring")
		}
		if in.Optional {
			title += " (optional)"
		}
		f := huh.NewInput().Title(title).Description(desc).Value(st.values[in.Key])
		if in.Secret {
			f = f.EchoMode(huh.EchoModePassword)
		}
		if !in.Optional {
			f = f.Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("required")
				}
				return nil
			})
		}
		fields = append(fields, f)
	}
	// Every client has a project scope (Claude: .mcp.json, Codex: .codex/config.toml).
	if root := clients.GitRoot(); root != "" {
		fields = append(fields, huh.NewSelect[string]().
			Title("Install for").
			Options(
				huh.NewOption("All projects", "user"),
				huh.NewOption("This repo only ("+filepath.Base(root)+")", "project"),
			).
			Value(&st.scope))
	}
	if len(fields) == 0 {
		return nil, st
	}
	form := huh.NewForm(huh.NewGroup(fields...).Title("Install " + e.DisplayName())).
		WithTheme(formTheme()).WithWidth(width).WithShowHelp(false)
	return form, st
}

func (f *installForm) collected() shed.Values {
	out := shed.Values{}
	for k, v := range f.values {
		if s := strings.TrimSpace(*v); s != "" {
			out[k] = s
		}
	}
	return out
}

func newConfirmForm(title, desc string, ok *bool, width int) *huh.Form {
	return huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Description(desc).Affirmative("Yes").Negative("No").Value(ok),
	)).WithTheme(formTheme()).WithWidth(width).WithShowHelp(false)
}
