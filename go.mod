module github.com/bartdegoede/toolshed

go 1.26.0

// `toolshed mcp` let an agent replace an existing tool and have the new
// command started with that tool's keyring secrets. Fixed in v0.1.2.
retract [v0.1.0, v0.1.1]

require (
	charm.land/bubbletea/v2 v2.1.1
	charm.land/huh/v2 v2.0.3
	charm.land/lipgloss/v2 v2.0.6
	github.com/BurntSushi/toml v1.6.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/zalando/go-keyring v0.2.8
	golang.org/x/term v0.47.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	charm.land/bubbles/v2 v2.2.1 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/catppuccin/go v0.3.0 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20261023060817-a45b7c86333f // indirect
	github.com/charmbracelet/x/ansi v0.11.9 // indirect
	github.com/charmbracelet/x/exp/ordered v0.1.0 // indirect
	github.com/charmbracelet/x/exp/strings v0.1.0 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/godbus/dbus/v5 v5.2.4 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/mitchellh/hashstructure/v2 v2.0.2 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/xo/terminfo v1.3.1 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.37.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/time v0.16.0 // indirect
)
