package cli

import (
	"fmt"
	"strings"
)

// Commands whose arguments are tool ids, for completion.
var takesIDs = []string{"install", "uninstall", "update", "upgrade", "approve", "remove", "tools"}

// runCompletion prints a completion script for bash, zsh or fish. Tool ids
// are completed by calling the hidden `toolshed __ids`.
func runCompletion(args []string) error {
	if len(args) != 1 {
		return usagef("completion needs a shell: bash, zsh or fish")
	}
	var b strings.Builder
	switch args[0] {
	case "bash":
		bashCompletion(&b)
	case "zsh":
		zshCompletion(&b)
	case "fish":
		fishCompletion(&b)
	default:
		return usagef("unknown shell %q: bash, zsh or fish", args[0])
	}
	fmt.Print(b.String())
	return nil
}

// runIDs prints every tool id, one per line (for completion scripts).
func runIDs([]string) error {
	reg, err := loadRegistry()
	if err != nil {
		return nil // completion should never print errors
	}
	for _, e := range reg.Entries {
		fmt.Println(e.ID)
	}
	return nil
}

func visibleCommands() []command {
	var out []command
	for _, c := range commands {
		if c.name != "exec" && !strings.HasPrefix(c.name, "__") {
			out = append(out, c)
		}
	}
	return out
}

func names() string {
	var n []string
	for _, c := range visibleCommands() {
		n = append(n, c.name)
	}
	return strings.Join(n, " ")
}

func bashCompletion(b *strings.Builder) {
	fmt.Fprintf(b, `# bash completion for toolshed
_toolshed() {
    local cur=${COMP_WORDS[COMP_CWORD]}
    if [[ $COMP_CWORD -eq 1 ]]; then
        COMPREPLY=($(compgen -W "%s" -- "$cur"))
        return
    fi
    case ${COMP_WORDS[1]} in
        %s) COMPREPLY=($(compgen -W "$(toolshed __ids 2>/dev/null)" -- "$cur")) ;;
        secret)
            if [[ $COMP_CWORD -eq 2 ]]; then COMPREPLY=($(compgen -W "set" -- "$cur"))
            elif [[ $COMP_CWORD -eq 3 ]]; then COMPREPLY=($(compgen -W "$(toolshed __ids 2>/dev/null)" -- "$cur")); fi ;;
        completion) COMPREPLY=($(compgen -W "bash zsh fish" -- "$cur")) ;;
    esac
}
complete -F _toolshed toolshed
`, names(), strings.Join(takesIDs, "|"))
}

func zshCompletion(b *strings.Builder) {
	b.WriteString("#compdef toolshed\n# zsh completion for toolshed\n_toolshed() {\n    local -a cmds ids\n    cmds=(\n")
	for _, c := range visibleCommands() {
		fmt.Fprintf(b, "        '%s:%s'\n", c.name, strings.NewReplacer(":", `\:`, "'", `'\''`).Replace(c.help))
	}
	fmt.Fprintf(b, `    )
    if (( CURRENT == 2 )); then
        _describe 'command' cmds
        return
    fi
    case $words[2] in
        %s) ids=(${(f)"$(toolshed __ids 2>/dev/null)"}); _describe 'tool' ids ;;
        secret)
            if (( CURRENT == 3 )); then _values 'action' set
            elif (( CURRENT == 4 )); then ids=(${(f)"$(toolshed __ids 2>/dev/null)"}); _describe 'tool' ids; fi ;;
        completion) _values 'shell' bash zsh fish ;;
    esac
}
if [[ "${zsh_eval_context[-1]}" == loadautofunc ]]; then
    _toolshed "$@"
else
    compdef _toolshed toolshed
fi
`, strings.Join(takesIDs, "|"))
}

func fishCompletion(b *strings.Builder) {
	b.WriteString("# fish completion for toolshed\ncomplete -c toolshed -f\n")
	for _, c := range visibleCommands() {
		fmt.Fprintf(b, "complete -c toolshed -n __fish_use_subcommand -a %s -d '%s'\n", c.name, strings.ReplaceAll(c.help, "'", `\'`))
	}
	fmt.Fprintf(b, "complete -c toolshed -n '__fish_seen_subcommand_from %s' -a '(toolshed __ids 2>/dev/null)'\n", strings.Join(takesIDs, " "))
	b.WriteString(`complete -c toolshed -n '__fish_seen_subcommand_from secret; and not __fish_seen_subcommand_from set' -a set
complete -c toolshed -n '__fish_seen_subcommand_from set' -a '(toolshed __ids 2>/dev/null)'
complete -c toolshed -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
`)
}
