// Package toolshed exposes build-time assets for the toolshed CLI.
//
// The only asset today is the default registry: registry.yaml at the
// repository root is compiled into the binary so a fresh install works with
// zero configuration. Forks customise their catalog by editing that file;
// end users can override it at runtime (see internal/mcp/load.go).
package toolshed

import _ "embed"

// DefaultRegistry is the contents of registry.yaml, embedded at build time.
//
//go:embed registry.yaml
var DefaultRegistry []byte
