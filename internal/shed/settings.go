package shed

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/registry"
)

// Settings are this machine's toolshed settings, in ~/.toolshed/config.yaml.
// They're kept apart from the registry, which can be shared: which coding
// agents you use is a choice per machine.
type Settings struct {
	// Clients are the clients a tool is installed into when no --client is
	// given. Empty means you haven't chosen yet: every client on PATH.
	Clients []string `yaml:"clients,omitempty"`
}

// SettingsPath is the settings file: $TOOLSHED_HOME/config.yaml.
func SettingsPath() string {
	return filepath.Join(registry.Home(), "config.yaml")
}

// LoadSettings reads the settings file; a missing file is no settings.
// Client names this toolshed doesn't know (written by a newer one) are left
// out.
func LoadSettings() (Settings, error) {
	var s Settings
	data, err := os.ReadFile(SettingsPath())
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("%s: %w", SettingsPath(), err)
	}
	var known []string
	for _, c := range s.Clients {
		if clients.Get(c) != nil && !contains(known, c) {
			known = append(known, c)
		}
	}
	s.Clients = known
	return s, nil
}

// SaveClients stores the clients to install into, in display order.
func SaveClients(names []string) error {
	var s Settings
	for _, c := range clients.All {
		if contains(names, c) {
			s.Clients = append(s.Clients, c)
		}
	}
	for _, c := range names {
		if clients.Get(c) == nil {
			return fmt.Errorf("unknown client %q: use %s", c, clients.Names("or"))
		}
	}
	if len(s.Clients) == 0 {
		return fmt.Errorf("choose at least one client")
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	data = append([]byte("# toolshed's settings for this machine. Change clients with `toolshed clients`.\n"), data...)
	path := SettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// DefaultClients are the clients a tool goes into when none are named: the
// ones you chose (chosen is true), or else every client.
func DefaultClients() (names []string, chosen bool) {
	s, err := LoadSettings()
	if err != nil || len(s.Clients) == 0 {
		return clients.All, false
	}
	return s.Clients, true
}

// NeedsClientChoice reports whether a front end that can ask should ask
// which clients to use before installing: nothing's chosen yet and more than
// one client is on PATH, so "all of them" may not be what you want. Not for
// a settings file it can't read: Install says what's wrong with it, rather
// than the answer quietly replacing it.
func NeedsClientChoice() bool {
	s, err := LoadSettings()
	return err == nil && len(s.Clients) == 0 && len(clients.AvailableClients()) > 1
}

// Unused are the clients on PATH that toolshed doesn't install into,
// because you left them out of your choice.
func Unused() []string {
	names, chosen := DefaultClients()
	if !chosen {
		return nil
	}
	var out []string
	for _, c := range clients.AvailableClients() {
		if !contains(names, c) {
			out = append(out, c)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
