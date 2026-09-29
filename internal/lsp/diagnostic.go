// Package lsp implements whip's Language Server Protocol support: a small
// stdlib-only LSP client over stdio that feeds compiler/linter diagnostics
// back into the model's tool results after write/edit calls.
//
// Design notes (see .ai-docs/plans/lsp-diagnostics/README.md):
//   - Diagnostics are wait-free: didOpen/didChange carry a version, the
//     server pushes textDocument/publishDiagnostics, and a per-file channel
//     close wakes waiters (opencode polls with timeouts —
//     packages/opencode/src/lsp/client.ts:160-170; ours is one close).
//   - The registry is data: gopls is the single built-in; users add servers
//     via the "lsp" block in config.json (opencode hardcodes ~35 servers in
//     packages/opencode/src/lsp/server.ts).
//   - Sibling-file diagnostics are reported alongside the edited file's —
//     opencode renders only the touched file (tool/edit.ts:197-202).
package lsp

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Severity levels, per the LSP spec.
const (
	SeverityError   = 1
	SeverityWarning = 2
	SeverityInfo    = 3
	SeverityHint    = 4
)

// Diagnostic is one textDocument/publishDiagnostics entry.
type Diagnostic struct {
	Line     int // 1-based
	Col      int // 1-based
	Severity int // Severity* constant
	Message  string
}

// maxPerFile caps diagnostics rendered per file (opencode uses 20,
// lsp/diagnostic.ts:3). maxSiblingFiles caps how many other files' errors a
// single tool result mentions.
const (
	maxPerFile      = 20
	maxSiblingFiles = 5
)

// maxMsgLen caps one diagnostic message (rust-analyzer can emit multi-KB
// messages; the model doesn't need the tail).
const maxMsgLen = 300

// format renders one diagnostic, ported from opencode's pretty()
// (lsp/diagnostic.ts:5-17): "ERROR [12:5] undefined: foo".
func format(d Diagnostic) string {
	sev := "ERROR"
	switch d.Severity {
	case SeverityWarning:
		sev = "WARN"
	case SeverityInfo:
		sev = "INFO"
	case SeverityHint:
		sev = "HINT"
	}
	msg := diagnosticText(d.Message)
	return fmt.Sprintf("%s [%d:%d] %s", sev, d.Line, d.Col, msg)
}

// block renders the <diagnostics file="…">…</diagnostics> wrapper, ported
// from opencode's report() (lsp/diagnostic.ts:19-28) with one widening: we
// keep warnings as well as errors (opencode is errors-only) because vet-style
// warnings are nearly free tokens.
func block(file string, diags []Diagnostic) string {
	var kept []Diagnostic
	for _, d := range diags {
		if d.Severity <= SeverityWarning {
			kept = append(kept, d)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	over := 0
	if len(kept) > maxPerFile {
		over = len(kept) - maxPerFile
		kept = kept[:maxPerFile]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n<diagnostics file=%q>\n", file)
	for _, d := range kept {
		b.WriteString(format(d))
		b.WriteByte('\n')
	}
	if over > 0 {
		fmt.Fprintf(&b, "... and %d more\n", over)
	}
	b.WriteString("</diagnostics>")
	return b.String()
}

// Report builds the tool-output suffix for one edited file: the file's own
// errors+warnings, then errors-only blocks for sibling files the edit broke
// (sorted by path, capped at maxSiblingFiles). Returns "" when there is
// nothing to report.
func Report(edited string, editedDiags []Diagnostic, siblings map[string][]Diagnostic) string {
	var sb strings.Builder
	sb.WriteString(block(edited, editedDiags))
	var names []string
	for p, diags := range siblings {
		if p == edited {
			continue
		}
		for _, d := range diags {
			if d.Severity == SeverityError {
				names = append(names, p)
				break
			}
		}
	}
	if len(names) == 0 {
		return sb.String()
	}
	sort.Strings(names)
	shown := min(len(names), maxSiblingFiles)
	for _, p := range names[:shown] {
		var errorsOnly []Diagnostic
		for _, d := range siblings[p] {
			if d.Severity == SeverityError {
				errorsOnly = append(errorsOnly, d)
			}
		}
		sb.WriteString(block(p, errorsOnly))
	}
	plural := "file"
	if len(names) > 1 {
		plural = "files"
	}
	if len(names) > shown {
		fmt.Fprintf(&sb, "\n(cached errors in %d other %s, %d shown)", len(names), plural, shown)
	} else {
		fmt.Fprintf(&sb, "\n(cached errors in another %s)", plural)
	}
	return sb.String()
}

// siblingErrors returns cached error-bearing files in the edited file's
// directory.
//
// ponytail: same-directory only — gopls reports package-level breakage in
// the same dir for the common case; widen to the module root if cross-dir
// breakage proves common.
func siblingErrors(edited string, all map[string][]Diagnostic) map[string][]Diagnostic {
	dir := filepath.Dir(edited)
	out := map[string][]Diagnostic{}
	for p, ds := range all {
		if p == edited || filepath.Dir(p) != dir {
			continue
		}
		kept := make([]Diagnostic, len(ds))
		copy(kept, ds)
		for _, d := range ds {
			if d.Severity == SeverityError {
				out[p] = kept
				break
			}
		}
	}
	return out
}
