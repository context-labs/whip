// Package workflow runs deterministic multi-agent orchestration scripts —
// a Go port of the pi better-workflows extension (github.com/anishthite/
// better-workflows), itself faithful to Claude Code's Workflow tool. The
// model authors a small JavaScript script that fans work out to subagents
// (agent()), pipelines it (pipeline()), barriers on it (parallel()), and the
// runtime executes it in a goja sandbox with background delivery and a
// resumable journal.
package workflow

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/dop251/goja"
)

// Effort values accepted in meta.effort / meta.phases[].effort, mirrored from
// better-workflows (parse.ts EFFORT_VALUES). A set, not a substring check —
// "gh" is a substring of "high" but not a valid effort.
var effortValues = map[string]bool{
	"off": true, "minimal": true, "low": true,
	"medium": true, "high": true, "xhigh": true,
}

func validEffort(e string) bool { return effortValues[e] }

const effortValuesDesc = "off|minimal|low|medium|high|xhigh"

// MetaPhase is one declared progress phase in the script's meta block.
type MetaPhase struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// Meta is the validated `export const meta = {...}` header of a script.
type Meta struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	WhenToUse   string      `json:"whenToUse,omitempty"`
	Phases      []MetaPhase `json:"phases,omitempty"`
	Model       string      `json:"model,omitempty"`
	Effort      string      `json:"effort,omitempty"`
}

// determinismBlocklist is the author-facing fast feedback; the runtime also
// neuters these inside the VM (parse.ts DETERMINISM_BLOCKLIST).
var determinismBlocklist = regexp.MustCompile(`\bDate\s*\.\s*now\b|\bMath\s*\.\s*random\b|\bnew\s+Date\s*\(\s*\)`)

var metaPrefix = regexp.MustCompile(`^export\s+const\s+meta\s*=`)

// Parse extracts and validates the meta header from a workflow script and
// returns the runnable body (the script with the meta statement stripped).
// Port of parse.ts parseWorkflowScript: the meta object literal is located by
// brace-matching and evaluated in an EMPTY goja runtime, so any reference to a
// variable, function call, or interpolation throws — which is exactly the
// "pure literal" rule.
func Parse(script string) (Meta, string, error) {
	text := strings.TrimLeft(script, " \t\r\n")
	lead := len(script) - len(text)

	if determinismBlocklist.MatchString(text) {
		return Meta{}, "", errors.New("workflow scripts must be deterministic: Date.now() / Math.random() / argless new Date() are unavailable (they break resume); pass timestamps via args and vary randomness by index")
	}

	m := metaPrefix.FindString(text)
	if m == "" {
		return Meta{}, "", errors.New("`export const meta = { name, description, phases? }` must be the first statement in the script")
	}

	// Locate the object literal that follows `=`.
	i := len(m)
	for i < len(text) && isSpace(text[i]) {
		i++
	}
	if i >= len(text) || text[i] != '{' {
		return Meta{}, "", errors.New("meta must be assigned a literal object: `export const meta = { ... }`")
	}

	objEnd := matchBrace(text, i)
	if objEnd == -1 {
		return Meta{}, "", errors.New("meta object literal is not closed (unbalanced braces)")
	}
	objText := text[i : objEnd+1]

	// Template literals evaluate fine in goja (ES6), unlike the TS port's
	// empty vm context — so interpolation is rejected explicitly here, keeping
	// the "pure literal" rule identical to parse.ts.
	if strings.Contains(objText, "`") {
		return Meta{}, "", errors.New("meta must be a PURE LITERAL — no variables, function calls, spreads, or template interpolation (template literal in meta)")
	}

	// Evaluate as a literal in an empty realm: a pure literal succeeds;
	// anything referencing a variable / calling a function throws. The tag
	// mapper is required so Meta's json tags drive the field mapping.
	vm := goja.New()
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	v, err := vm.RunString("(" + objText + ")")
	if err != nil {
		return Meta{}, "", fmt.Errorf("meta must be a PURE LITERAL — no variables, function calls, spreads, or template interpolation (%v)", err)
	}
	var meta Meta
	if err := vm.ExportTo(v, &meta); err != nil {
		return Meta{}, "", fmt.Errorf("meta must be a PURE LITERAL object (%v)", err)
	}
	if err := validateMeta(&meta); err != nil {
		return Meta{}, "", err
	}

	// Strip the whole statement (plus an optional trailing semicolon) to get
	// the runnable body.
	after := objEnd + 1
	for after < len(text) && (text[after] == ';' || isSpace(text[after])) && text[after] != '\n' {
		after++
	}
	body := script[:lead] + text[after:]
	return meta, body, nil
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// matchBrace returns the index of the `}` matching the `{` at open, or -1.
// Skips strings and comments. Port of parse.ts matchBrace.
func matchBrace(s string, open int) int {
	depth := 0
	var inStr byte
	for i := open; i < len(s); i++ {
		c := s[i]
		if inStr != 0 {
			if c == '\\' {
				i++
			} else if c == inStr {
				inStr = 0
			}
			continue
		}
		switch {
		case c == '"' || c == '\'' || c == '`':
			inStr = c
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			nl := strings.IndexByte(s[i:], '\n')
			if nl == -1 {
				return -1
			}
			i += nl
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			end := strings.Index(s[i+2:], "*/")
			if end == -1 {
				return -1
			}
			i += 2 + end + 1
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func validateMeta(m *Meta) error {
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("meta.name is required and must be a non-empty string")
	}
	if strings.TrimSpace(m.Description) == "" {
		return errors.New("meta.description is required and must be a non-empty string")
	}
	for _, p := range m.Phases {
		if p.Title == "" {
			return errors.New("each meta.phases entry must be an object with a string `title`")
		}
		if p.Effort != "" && !validEffort(p.Effort) {
			return fmt.Errorf("meta.phases[].effort must be one of %s when present (got %q)", effortValuesDesc, p.Effort)
		}
	}
	if m.Effort != "" && !validEffort(m.Effort) {
		return fmt.Errorf("meta.effort must be one of %s when present (got %q)", effortValuesDesc, m.Effort)
	}
	return nil
}
