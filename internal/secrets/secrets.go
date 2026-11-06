// Package secrets stores tool secrets in the OS keyring (macOS Keychain or
// Linux Secret Service) under service "toolshed", account "<group>/<KEY>".
//
// Secrets never go into Claude Code or Codex config files: tools that need
// them are registered as `toolshed exec <id>`, which reads the keyring and
// execs the real server with the values in its environment.
package secrets

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	gokeyring "github.com/zalando/go-keyring"
)

// Service is the keyring service name. Overridable at build time:
//
//	-ldflags "-X github.com/bartdegoede/toolshed/internal/secrets.Service=acme"
var Service = "toolshed"

// DisableEnv, when non-empty, turns the keyring off: secrets are then written
// into the client config as plain env vars. For CI and headless machines.
const DisableEnv = "TOOLSHED_NO_KEYRING"

// ErrNotFound is returned by Get when no value is stored.
var ErrNotFound = errors.New("secret not found")

func account(group, key string) string { return group + "/" + key }

// Get returns a stored secret.
func Get(group, key string) (string, error) {
	v, err := gokeyring.Get(Service, account(group, key))
	if errors.Is(err, gokeyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return v, err
}

// Set stores a secret.
func Set(group, key, value string) error {
	if err := gokeyring.Set(Service, account(group, key), value); err != nil {
		return fmt.Errorf("store %s in keyring: %w", account(group, key), err)
	}
	return nil
}

// Delete removes a secret; deleting a missing secret is not an error.
func Delete(group, key string) error {
	err := gokeyring.Delete(Service, account(group, key))
	if err == nil || errors.Is(err, gokeyring.ErrNotFound) {
		return nil
	}
	return err
}

var (
	availableOnce sync.Once
	available     bool
)

// Available reports whether the OS keyring works, memoized per process.
//
// The probe is a read, not a write: on macOS a write against a machine with
// no default keychain pops a modal "Keychain Not Found" dialog. Reads don't,
// but they also don't detect that state, so on macOS we first check that the
// default keychain file exists.
func Available() bool {
	availableOnce.Do(func() {
		if os.Getenv(DisableEnv) != "" {
			return
		}
		if runtime.GOOS == "darwin" && !macKeychainExists() {
			return
		}
		_, err := gokeyring.Get(Service, "_probe")
		available = err == nil || errors.Is(err, gokeyring.ErrNotFound)
	})
	return available
}

func macKeychainExists() bool {
	out, err := exec.Command("security", "default-keychain").Output()
	if err != nil {
		return false
	}
	path := strings.Trim(strings.TrimSpace(string(out)), `"`)
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// UseMockForTest swaps in an in-memory keyring and sets Available.
func UseMockForTest(avail bool) {
	gokeyring.MockInit()
	availableOnce = sync.Once{}
	availableOnce.Do(func() {})
	available = avail
}
