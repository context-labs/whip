package capability

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

// arity maps a command prefix to how many tokens define "the command" —
// longest prefix wins, flags never count. Compact version of opencode's
// generated table (permission/arity.ts); the common cases carry the value.
var arity = map[string]int{
	// one-token commands: the binary alone is the rule
	"ls": 1, "cat": 1, "pwd": 1, "grep": 1, "find": 1, "echo": 1,
	"rm": 1, "mv": 1, "cp": 1, "mkdir": 1, "touch": 1, "which": 1,
	// two-token: binary + subcommand
	"git": 2, "npm": 2, "pnpm": 2, "yarn": 2, "go": 2, "cargo": 2,
	"docker": 2, "kubectl": 2, "brew": 2, "apt": 2, "pip": 2,
	// three-token where the shorter prefix under-specifies
	"npm run": 3, "pnpm run": 3, "go tool": 3, "docker compose": 3, "git submodule": 3,
}

// CommandRule collapses a shell command to its arity rule: the prefix "always"
// should install. Only the first command of a pipeline/chain is considered —
// "git checkout main && rm -rf /" is not a "git checkout" rule.
func CommandRule(command string) string {
	cmd := strings.TrimSpace(command)
	// stop at the first shell operator: the rule covers one command, not a chain
	for i, r := range cmd {
		if r == '&' || r == '|' || r == ';' || r == '>' || r == '<' {
			cmd = strings.TrimSpace(cmd[:i])
			break
		}
	}
	tokens := strings.Fields(cmd)
	for i := 0; i < len(tokens); i++ {
		if strings.Contains(tokens[i], "=") && !strings.HasPrefix(tokens[i], "-") && i == 0 {
			// leading VAR=value assignments aren't part of the command
			tokens = tokens[1:]
			i = -1
		}
	}
	if len(tokens) == 0 {
		return ""
	}
	// longest matching prefix wins
	for n := len(tokens); n > 0; n-- {
		prefix := strings.Join(tokens[:n], " ")
		if a, ok := arity[prefix]; ok {
			return strings.Join(tokens[:min(a, len(tokens))], " ")
		}
	}
	return tokens[0] // unknown command: the binary is the rule
}

// harmlessRedirect matches the redirects that never touch a file of the
// command's choosing: stderr merges and /dev/null.
var harmlessRedirect = regexp.MustCompile(`\s(?:[12]?>&[12]|[12&]?>{1,2}\s*/dev/null|<\s*/dev/null)`)

// CommandRules names the rule of every simple command on a shell line, in
// order and without repeats: a remembered rule covers a chain only when each
// command in it is covered, so an "ls" rule never approves "ls && rm -rf ~".
// ok is false when the line cannot be described by rules at all: command
// substitution, backticks, or a redirect other than a harmless one.
// ponytail: no quote awareness; "echo 'a; b'" yields an extra "b" rule and
// simply prompts. A real shell parser if that bites.
func CommandRules(command string) (rules []string, ok bool) {
	if strings.Contains(command, "`") || strings.Contains(command, "$(") {
		return nil, false
	}
	line := harmlessRedirect.ReplaceAllString(" "+command, " ")
	if strings.ContainsAny(line, "<>") {
		return nil, false
	}
	for _, segment := range strings.FieldsFunc(line, func(r rune) bool { return r == ';' || r == '|' || r == '&' || r == '\n' }) {
		if rule := CommandRule(segment); rule != "" && !slices.Contains(rules, rule) {
			rules = append(rules, rule)
		}
	}
	return rules, len(rules) > 0
}

// PermissionRule names what a prompt is about. command is what a human sees
// (shell command text, or the canonical path); rules is what "always" installs
// and what must all be covered before a prompt is skipped. ok is false when
// the operation has no rule.
func PermissionRule(operation string, arguments json.RawMessage, canonicalPath string) (command string, rules []string, ok bool) {
	switch operation {
	case "bash", "workspace_process", "shell_start":
		var args struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(arguments, &args)
		rules, ok = CommandRules(args.Command)
		return args.Command, rules, ok
	case "write", "edit":
		if canonicalPath == "" {
			return "", nil, false
		}
		return canonicalPath, []string{canonicalPath}, true
	}
	return canonicalPath, nil, false
}

// RuleLabel renders a prompt's rules for humans and events.
func RuleLabel(rules []string) string {
	return strings.Join(rules, ", ")
}
