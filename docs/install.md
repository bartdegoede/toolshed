# Install

<!-- summary: Homebrew, Go, or a prebuilt binary -->

```sh
brew install bartdegoede/tap/toolshed
```

or with Go (1.26 or newer):

```sh
go install github.com/bartdegoede/toolshed/cmd/toolshed@latest
```

Prebuilt binaries for macOS and Linux (Intel and ARM) are on the
[releases page](https://github.com/bartdegoede/toolshed/releases).

toolshed runs on macOS and Linux. It installs tools into
[Claude Code](https://claude.com/claude-code) and
[Codex](https://github.com/openai/codex), so you need at least one of them on
your `PATH`.

## Shell completions

Homebrew installs completions for commands and tool ids. Otherwise, load them
with `toolshed completion bash|zsh|fish`, for example in your `~/.zshrc`:

```sh
source <(toolshed completion zsh)
```

## Upgrading

`brew upgrade toolshed`, or `go install …@latest` again. Tools you installed
keep working: Homebrew installs are registered through its stable `opt` path.
If you switch between Homebrew and `go install`, run `toolshed doctor`, which
spots registrations that point at a toolshed binary that's gone.
