// Package skills discovers SKILL.md files and renders them into the system prompt.
package skills

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Skill is one discovered skill.
type Skill struct {
	Name        string
	Description string
	Path        string // path to the SKILL.md
	// DisableModelInvocation excludes the skill from the system-prompt
	// catalog: it can only be invoked explicitly ($name). Per the Agent
	// Skills spec frontmatter field disable-model-invocation.
	DisableModelInvocation bool
	// Warning is non-empty when the skill loaded but violates the Agent
	// Skills spec (bad name, over-long description) — surfaced in the
	// startup report so a broken skill is never silent.
	Warning string
}

// ScanProblem is a SKILL.md that failed to load (bad frontmatter, unreadable).
// Scan used to skip these silently; pi's startup [Skill conflicts] block
// showed how valuable naming them is.
type ScanProblem struct {
	Path string
	Err  string
}

// DefaultDirs returns whip's skill locations: project .agents/skills, then
// user ~/.whip/skills and ~/.agents/skills.
func DefaultDirs() []string {
	var dirs []string
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(wd, ".agents", "skills"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".whip", "skills"))
		dirs = append(dirs, filepath.Join(home, ".agents", "skills"))
	}
	return dirs
}

// Scan reads <dir>/<skill>/SKILL.md for each dir, skipping anything
// unreadable. Loaded-but-degraded skills carry a Warning (e.g. description
// truncated); anything that fails to parse is silently skipped (a SKILL.md
// with no frontmatter is usually just a stray doc) but counted — callers
// that want the conflicts view use ScanDetailed.
func Scan(dirs ...string) []Skill {
	sk, _ := ScanDetailed(dirs...)
	return sk
}

// ScanDetailed is Scan plus the problems found: directories whose SKILL.md
// exists but failed to parse, and parse-level warnings.
func ScanDetailed(dirs ...string) ([]Skill, []ScanProblem) {
	var out []Skill
	var problems []ScanProblem
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(d, e.Name(), "SKILL.md")
			if _, err := os.Stat(p); err != nil {
				continue // no SKILL.md: not a skill, not a problem
			}
			s, err := parse(p)
			if err != nil {
				problems = append(problems, ScanProblem{Path: p, Err: err.Error()})
				continue
			}
			if s.Name == "" {
				s.Name = e.Name()
			}
			if w := validate(s); w != "" {
				s.Warning = w
			}
			out = append(out, s)
		}
	}
	return out, problems
}

// parse reads name/description from the YAML frontmatter. Values may be
// single-line scalars or YAML block scalars (>/| with -/+ chomping) — the
// folded style is how claude-code's skill tooling writes long descriptions,
// and treating the indicator as the value renders "description: >-" in the
// catalog, hiding the skill from model-triggering entirely.
// ponytail: still not a real YAML parser; nested maps/sequences are ignored.
func parse(path string) (Skill, error) {
	f, err := os.Open(path) //nolint:gosec // G304: reading caller-discovered skill files is the function's contract
	if err != nil {
		return Skill{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return Skill{}, fmt.Errorf("%s: no frontmatter", path)
	}
	s := Skill{Path: path}
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		key, v, ok := cutKey(line)
		if !ok {
			continue
		}
		if isBlockScalarIndicator(v) {
			// Block scalars are already plain values: no unquoting, and no
			// TrimSpace — chomping indicators own the trailing newlines.
			v = readBlockScalar(sc, v, indentOf(line))
		} else {
			v = unquote(v)
		}
		switch key {
		case "name":
			s.Name = v
		case "description":
			s.Description = v
		case "disable-model-invocation":
			s.DisableModelInvocation = strings.TrimSpace(v) == "true"
		}
	}
	return s, sc.Err()
}

// cutKey splits a top-level frontmatter line into key and raw value. Only
// unindented keys are recognized — an indented line belongs to a block scalar
// or a nested mapping we don't model, and must not shadow a real key.
func cutKey(line string) (key, value string, ok bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return "", "", false
	}
	k, v, found := strings.Cut(line, ":")
	if !found {
		return "", "", false
	}
	return k, v, true
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// isBlockScalarIndicator reports whether a raw frontmatter value is a YAML
// block-scalar header: > (folded) or | (literal), optionally with a -/+
// chomping or digit indentation indicator (e.g. ">-", "|+", ">2").
func isBlockScalarIndicator(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || (v[0] != '>' && v[0] != '|') {
		return false
	}
	for _, r := range v[1:] {
		if r != '-' && r != '+' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// readBlockScalar consumes the indented lines following a block-scalar header
// and folds them per the indicator: literal (|) keeps newlines, folded (>)
// joins lines with spaces. Chomping: default clips to a single trailing
// newline (folded into nothing at the edges), "-" strips it, "+" keeps it.
// This is the common-case subset — indentation indicators are honored as
// "more indented than the key", which is how skill files are actually written.
func readBlockScalar(sc *bufio.Scanner, header string, keyIndent int) string {
	indicator := strings.TrimSpace(header)
	literal := indicator[0] == '|'
	chomp := byte(0)
	for _, r := range indicator[1:] {
		if r == '-' || r == '+' {
			chomp = byte(r)
		}
	}

	var lines []string
	// Determine indentation from the first content line, then take every
	// following line at least that indented (blank lines carry through).
	bodyIndent := -1
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			// Closing frontmatter delimiter consumed along with the scalar;
			// parse's loop will just hit EOF. Skill files keep descriptions
			// early in the frontmatter, so this stays theoretical.
			break
		}
		ind := indentOf(line)
		if trimmed != "" {
			if ind <= keyIndent {
				break
			}
			if bodyIndent == -1 {
				bodyIndent = ind
			}
		}
		lines = append(lines, line)
	}
	// Unread the terminator? bufio.Scanner can't push back — but the only
	// terminators are the frontmatter close (loop is done anyway) or a
	// sibling key. A sibling key after a block scalar IS valid YAML, so
	// note the limitation rather than pretend: keys following a block
	// scalar on a later line are lost. No known SKILL.md does this — the
	// block scalar is always the last field of its file section — and a
	// real YAML parser remains the answer if that changes.
	_ = keyIndent

	// Strip the common body indentation.
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = ""
			continue
		}
		if bodyIndent > 0 && len(line) >= bodyIndent {
			lines[i] = line[bodyIndent:]
		}
	}
	// Leading blank lines belong to the wrapper, not the value.
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	// Trailing blanks matter only for keep ("+") chomping: the delimiter line
	// already contributed one newline, so any beyond the first are real.
	trailing := 0
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
		trailing++
	}

	var v string
	if literal {
		v = strings.Join(lines, "\n")
	} else {
		v = strings.Join(lines, " ")
	}
	if chomp == '+' && len(lines) > 0 {
		v += "\n" + strings.Repeat("\n", trailing)
	}
	return v
}

func unquote(v string) string {
	v = strings.TrimSpace(v)
	for _, q := range []string{`"`, `'`} {
		if strings.HasPrefix(v, q) && strings.HasSuffix(v, q) && len(v) >= 2 {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// Agent Skills spec limits (agentskills.io/specification — the cross-harness
// SKILL.md standard pi/claude-code/codex enforce). These are validity
// ceilings, not prompt-economy budgets: a skill is only "wrong" when it
// breaks portability. Prompt economy is guarded at the block level (see
// TestSkillBlockBudget).
const (
	specMaxName = 64
	specMaxDesc = 1024
)

var specNameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// validate checks a loaded skill against the Agent Skills spec. Returns a
// warning string ("" when spec-clean). Skills with warnings still load —
// portability problems degrade, never disappear (pi does the same).
func validate(s Skill) string {
	var problems []string
	if len(s.Name) > specMaxName {
		problems = append(problems, fmt.Sprintf("name exceeds %d characters (%d)", specMaxName, len(s.Name)))
	}
	if !specNameRe.MatchString(s.Name) {
		problems = append(problems, "name must be lowercase a-z, 0-9, hyphens only")
	}
	if strings.HasPrefix(s.Name, "-") || strings.HasSuffix(s.Name, "-") {
		problems = append(problems, "name must not start or end with a hyphen")
	}
	if strings.Contains(s.Name, "--") {
		problems = append(problems, "name must not contain consecutive hyphens")
	}
	if len(s.Description) > specMaxDesc {
		problems = append(problems, fmt.Sprintf("description exceeds %d characters (%d)", specMaxDesc, len(s.Description)))
	}
	return strings.Join(problems, "; ")
}

// PromptBlock renders the skill catalog for the system prompt in the Agent
// Skills spec format (agentskills.io/integrate-skills): <available_skills>
// of <skill><name>/<description>/<location> entries, XML-escaped. Skills
// with disable-model-invocation are excluded (explicit $name invocation
// only). "" when none.
func PromptBlock(sk []Skill) string {
	var visible []Skill
	for _, s := range sk {
		if !s.DisableModelInvocation {
			visible = append(visible, s)
		}
	}
	if len(visible) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n<available_skills>\nThese skills hold task-specific instructions. When one is relevant, read its SKILL.md with the read tool and follow it. Relative paths in a skill resolve against the skill's directory (the parent of its SKILL.md).\n")
	for _, s := range visible {
		b.WriteString("  <skill>\n")
		fmt.Fprintf(&b, "    <name>%s</name>\n", xmlEscape(s.Name))
		fmt.Fprintf(&b, "    <description>%s</description>\n", xmlEscape(s.Description))
		fmt.Fprintf(&b, "    <location>%s</location>\n", xmlEscape(s.Path))
		b.WriteString("  </skill>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

func xmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	).Replace(s)
}
