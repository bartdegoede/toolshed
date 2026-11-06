// toolshed is a package manager for Model Context Protocol servers used by
// Claude Code and Codex. The binary's job is to parse os.Args and hand off
// to the cli package; all subcommand logic lives in cmd/toolshed/internal/cli
// so it can be unit-tested without spawning a subprocess.
//
// Build-time linker flag: -ldflags "-X main.Version=<semver>".
package main

import (
	"os"

	"github.com/bartdegoede/toolshed/internal/cli"
)

// Version is populated at build time by goreleaser (or the justfile) via
// -ldflags. Leaving the default "dev" makes local builds identifiable.
var Version = "dev"

func main() {
	cli.Version = Version
	os.Exit(cli.Run(os.Args))
}
