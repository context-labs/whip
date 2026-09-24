// Package memory is whip's durable memory: plain markdown files of
// checkbox bullets that the model appends to with `remember` and marks done
// with `forget`, and the user can read, edit, or delete in any editor.
//
// Two scopes:
//   - installation: ~/.whipcode/memory.md — facts about the user, injected into
//     every session
//   - session: ~/.whipcode/sessions/<id>.memory.md — continuation notes for one
//     conversation
//
// Retrieval is always-inject with a hard cap (maxEntries short lines): at
// this scale injecting the whole list beats any retrieval layer — nothing is
// missed and the user can see exactly what the model sees (that's the
// auditability constraint). The file format is stable; a future smarter
// recall would only change the read path, never the files.
//
// Format: one line per entry, "- [ ] the fact" open, "- [x]" done. Done
// lines stay in the file (strike, not silent rewrite) but are never injected.
package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/context-labs/whip/internal/config"
)

const (
	maxEntries = 50
	// Entries must stay one physical line (the file format is line-based), but
	// 300 chars forced research takeaways to be split across several entries.
	// The injection budget stays bounded by maxEntries, not by line length.
	maxEntryLength = 2000
)

// Entry is one numbered memory line. Entries are numbered across the whole
// file (done lines keep their numbers, so deletion targets stay stable).
type Entry struct {
	N    int    // 1-based line number among entry lines
	Text string // the fact, without the checkbox
	Done bool   // "- [x]" — struck, not injected
}

// Scope is one memory file.
type Scope struct {
	Path string // absolute path of the .md file
	Name string // "installation" or "session" — used in the injected header
}

// Installation returns the ~/.whipcode/memory.md scope.
func Installation() Scope {
	dir, err := config.Dir()
	if err != nil {
		return Scope{}
	}
	return Scope{Path: filepath.Join(dir, "memory.md"), Name: "installation"}
}

// Session returns the per-session scope ("" id yields a no-scope zero value).
func Session(id string) Scope {
	if id == "" {
		return Scope{}
	}
	dir, err := config.Dir()
	if err != nil {
		return Scope{}
	}
	return Scope{Path: filepath.Join(dir, "sessions", id+".memory.md"), Name: "session"}
}

// Entries parses a memory file. A missing file is an empty list, not an
// error. Any line that isn't a checkbox bullet is preserved on write but
// ignored here, so prose headers survive round-trips.
func (s Scope) Entries() []Entry {
	if s.Path == "" {
		return nil
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil
	}
	var out []Entry
	for line := range strings.Lines(string(data)) {
		line = strings.TrimRight(line, "\n")
		rest, ok := strings.CutPrefix(line, "- [ ] ")
		done := false
		if !ok {
			if rest, ok = strings.CutPrefix(line, "- [x] "); !ok {
				continue
			}
			done = true
		}
		out = append(out, Entry{N: len(out) + 1, Text: rest, Done: done})
	}
	return out
}

// Remember appends one entry. Returns an error when the open list is at the
// cap — the model is told to forget something first.
func (s Scope) Remember(text string) error {
	if s.Path == "" {
		return errors.New("no memory scope for this session yet")
	}
	text = strings.Join(strings.Fields(text), " ") // one line, no stray whitespace
	if text == "" {
		return errors.New("text is required")
	}
	if len(text) > maxEntryLength {
		return fmt.Errorf("keep it under %d chars; summarize it first", maxEntryLength)
	}
	open := 0
	for _, e := range s.Entries() {
		if !e.Done {
			open++
		}
	}
	if open >= maxEntries {
		return fmt.Errorf("memory is full (%d entries); forget something stale first", maxEntries)
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintf(f, "- [ ] %s\n", text)
	cerr := f.Close() // close flushes; a failed close can mean lost data
	if werr != nil {
		return werr
	}
	return cerr
}

// Forget marks entry n done ("- [x]"): a visible strike, not a deletion, so
// the file keeps an audit trail the user can edit by hand.
func (s Scope) Forget(n int) error {
	if s.Path == "" {
		return errors.New("no memory scope for this session yet")
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return errors.New("no memories yet")
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	seen := 0
	for i, line := range lines {
		if strings.HasPrefix(line, "- [ ] ") || strings.HasPrefix(line, "- [x] ") {
			seen++
			if seen == n {
				if strings.HasPrefix(line, "- [x] ") {
					return fmt.Errorf("entry %d is already marked done", n)
				}
				lines[i] = "- [x] " + strings.TrimPrefix(line, "- [ ] ")
				return os.WriteFile(s.Path, []byte(strings.Join(lines, "\n")+"\n"), 0o600) //nolint:gosec // G703: s.Path is a whip-owned memory file
			}
		}
	}
	return fmt.Errorf("no memory entry %d", n)
}

// PromptBlock renders open entries from both scopes as the per-turn system
// prompt section; "" when nothing is open anywhere.
func PromptBlock(scopes ...Scope) string {
	var b strings.Builder
	for _, s := range scopes {
		var lines []string
		for _, e := range s.Entries() {
			if !e.Done {
				lines = append(lines, fmt.Sprintf("- %d. %s", e.N, e.Text))
			}
		}
		if len(lines) > 0 {
			fmt.Fprintf(&b, "\n\nSaved %s memory (%s — edit or delete lines there directly; forget marks an entry done):\n%s",
				s.Name, s.Path, strings.Join(lines, "\n"))
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n\n<memory>" + b.String() + "\n</memory>"
}
