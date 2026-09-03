package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SecretCmdTimeout bounds a "!cmd" secret reference.
const SecretCmdTimeout = 5 * time.Second

// ResolveSecret resolves a secret-by-reference configured value at the point
// of use. The config file stores the REFERENCE, never the resolved value:
//
//	"$NAME" / "${NAME}" → os.Getenv(NAME); error if unset or empty.
//	"!cmd args..."      → trimmed stdout of the command; error on failure.
//	anything else       → returned as-is (a literal key; backwards compatible).
//
// This is the exo secrets-as-references pattern (secret_id indirection —
// docs/learnings/other-harnesses/exo.md §10): resolution happens when the
// secret is actually needed for a request, never at load or save, so config
// and the session store hold only references. Resolved values must never be
// passed to LogEvent or written to the event log.
//
// Deliberately conservative: a value merely CONTAINING "$" is a literal (an
// apiKey field can hold a pasted key with "$" in it). Fields that follow the
// shell-template authoring convention (MCP env/header values) use
// ExpandTemplate instead.
func ResolveSecret(v string) (string, error) {
	switch {
	case strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") && isEnvRefBody(v[2:len(v)-1]):
		name := v[2 : len(v)-1]
		if key, def, found := strings.Cut(name, ":-"); found {
			if !isEnvName(key) {
				return "", fmt.Errorf("secret reference ${%s}: invalid variable name %q", name, key)
			}
			if val := os.Getenv(key); val != "" {
				return val, nil
			}
			return os.Expand(def, os.Getenv), nil
		}
		if key, def, found := strings.Cut(name, "-"); found {
			if !isEnvName(key) {
				return "", fmt.Errorf("secret reference ${%s}: invalid variable name %q", name, key)
			}
			if _, ok := os.LookupEnv(key); ok {
				return os.Getenv(key), nil
			}
			return os.Expand(def, os.Getenv), nil
		}
		if val := os.Getenv(name); val != "" {
			return val, nil
		}
		return "", fmt.Errorf("secret reference ${%s}: environment variable unset or empty", name)
	case strings.HasPrefix(v, "$") && len(v) > 1 && !strings.ContainsAny(v[1:], " \t") && isEnvName(v[1:]):
		name := v[1:]
		if val := os.Getenv(name); val != "" {
			return val, nil
		}
		return "", fmt.Errorf("secret reference $%s: environment variable unset or empty", name)
	case strings.HasPrefix(v, "!"):
		fields := strings.Fields(v[1:])
		if len(fields) == 0 {
			return "", fmt.Errorf("secret reference %q: empty command", v)
		}
		ctx, cancel := context.WithTimeout(context.Background(), SecretCmdTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, fields[0], fields[1:]...).Output()
		if err != nil {
			return "", fmt.Errorf("secret reference %q: %w", v, err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	return v, nil
}

// IsWholeRef reports whether a value is nothing but a single variable
// reference ("$NAME", "${NAME}") or a "!cmd" — the forms ResolveSecret
// treats strictly. Anything else is a literal to ResolveSecret.
//
// The body must be a single valid reference (NAME, NAME:-def, NAME-def):
// multi-reference templates like "${A}${B}" or compound values like
// "$HOME/bin:$PATH" are NOT whole refs — ResolveSecret would treat the
// middle as one var name and error, and ResolveEnvMap would then drop a
// value ExpandTemplate expands fine. Those fall through to ExpandTemplate.
func IsWholeRef(v string) bool {
	switch {
	case strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}"):
		return len(v) > 3 && isEnvRefBody(v[2:len(v)-1])
	case strings.HasPrefix(v, "$") && len(v) > 1:
		return !strings.ContainsAny(v[1:], " \t") && isEnvName(v[1:])
	}
	return false
}

// isEnvRefBody reports whether s is the body of a single valid ${...}
// reference: an env name, optionally with a ":-default" or "-default" suffix
// (the default itself may be arbitrary text, including further references).
func isEnvRefBody(s string) bool {
	if key, _, found := strings.Cut(s, ":-"); found {
		return isEnvName(key)
	}
	if key, _, found := strings.Cut(s, "-"); found {
		return isEnvName(key)
	}
	return isEnvName(s)
}

// ExpandTemplate expands embedded "$VAR"/"${VAR}" references inside a larger
// string — the authoring convention in claude/codex MCP env and header
// values ("Bearer $TOKEN", "${VAR:-default}"). Unlike ResolveSecret, which
// guards literal secrets in single-value fields, template fields are
// config-authored: references follow shell rules.
//
//   - "$NAME" / "${NAME}": os.Getenv(NAME); unset or empty without a default
//     is an ERROR (a silent "" produces "Bearer "-style auth corruption that
//     surfaces upstream as an opaque 401 — the import-time-expansion bug).
//   - "${NAME:-def}" falls back to def when unset-or-empty, "${NAME-def}"
//     only when unset (shell semantics).
//   - "$$" is an escaped literal "$"; a "$" not followed by a valid
//     identifier start (letter/underscore/{) is a literal "$" — "price is $5"
//     and "pa$$word" survive untouched.
func ExpandTemplate(v string) (string, error) {
	idx := strings.IndexByte(v, '$')
	if idx < 0 {
		return v, nil
	}
	var b strings.Builder
	b.Grow(len(v))
	b.WriteString(v[:idx])
	for i := idx; i < len(v); {
		c := v[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(v) {
			b.WriteByte('$') // trailing "$" is literal
			break
		}
		switch n := v[i+1]; {
		case n == '$':
			b.WriteByte('$') // $$ escape
			i += 2
		case n == '{':
			end := strings.IndexByte(v[i+2:], '}')
			if end < 0 {
				return "", fmt.Errorf("template %q: unterminated ${", v)
			}
			val, err := templateVar(v[i+2:i+2+end], v)
			if err != nil {
				return "", err
			}
			b.WriteString(val)
			i += 2 + end + 1
		case n == '_' || n >= 'a' && n <= 'z' || n >= 'A' && n <= 'Z':
			j := i + 1
			for j < len(v) && (v[j] == '_' || v[j] >= 'a' && v[j] <= 'z' || v[j] >= 'A' && v[j] <= 'Z' || v[j] >= '0' && v[j] <= '9') {
				j++
			}
			val, err := templateVar(v[i+1:j], v)
			if err != nil {
				return "", err
			}
			b.WriteString(val)
			i = j
		default: // "$5", "$-", "$ " …: not a reference
			b.WriteByte('$')
			i++
		}
	}
	return b.String(), nil
}

// templateVar resolves one reference token ("NAME", "NAME:-def", "NAME-def");
// orig is the full template for error context.
func templateVar(name, orig string) (string, error) {
	if key, def, found := strings.Cut(name, ":-"); found {
		if !isEnvName(key) {
			return "", fmt.Errorf("template %q: invalid variable name %q", orig, key)
		}
		if val := os.Getenv(key); val != "" {
			return val, nil
		}
		return ExpandTemplate(def) // the default may itself reference vars
	}
	if key, def, found := strings.Cut(name, "-"); found {
		if !isEnvName(key) {
			return "", fmt.Errorf("template %q: invalid variable name %q", orig, key)
		}
		if _, ok := os.LookupEnv(key); ok {
			return os.Getenv(key), nil
		}
		return ExpandTemplate(def)
	}
	if !isEnvName(name) {
		return "", fmt.Errorf("template %q: invalid variable name %q", orig, name)
	}
	if val := os.Getenv(name); val != "" {
		return val, nil
	}
	return "", fmt.Errorf("template %q: environment variable $%s unset or empty (no default)", orig, name)
}

// isEnvName reports whether s is a conventional environment variable name.
func isEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// ResolveEnvMap resolves a stdio MCP server's configured env values at spawn
// time (the point of use). Whole-value references ("$VAR", "${VAR}", "!cmd")
// go through ResolveSecret; templates ("Bearer $TOKEN"-style) through
// ExpandTemplate. An entry whose reference cannot resolve is DROPPED, never
// spawned as "KEY=" — an empty override would mask a var the child could
// inherit from whip's own environment, and codex's semantics for a missing
// var are "absent", not "empty".
func ResolveEnvMap(env map[string]string) (map[string]string, error) {
	if len(env) == 0 {
		return env, nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		var rv string
		var err error
		if strings.HasPrefix(v, "!") || IsWholeRef(v) {
			rv, err = ResolveSecret(v)
		} else {
			rv, err = ExpandTemplate(v)
		}
		if err != nil {
			// Dropped, not fatal: sibling vars still apply. Log the drop (key
			// name only, never the value — it may hold a secret) so a vanished
			// env var is diagnosable instead of silently missing.
			logf("mcp.env", "dropping env %s: %v", k, err)
			continue
		}
		out[k] = rv
	}
	return out, nil
}

// ResolveHeader resolves one remote-MCP header value at connect time: whole
// references and "!cmd" strictly via ResolveSecret, templates via
// ExpandTemplate, literals untouched. The caller drops the header on error
// so the connect fails cleanly upstream instead of sending a half-resolved
// reference.
func ResolveHeader(v string) (string, error) {
	if strings.HasPrefix(v, "!") || IsWholeRef(v) {
		return ResolveSecret(v)
	}
	return ExpandTemplate(v)
}
