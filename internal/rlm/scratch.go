package rlm

import (
	"context"
	"maps"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

// Scratch snapshot caps. A snapshot travels in one protocol frame, so the
// aggregate stays well under the default 1 MiB frame limit.
const (
	snapshotVariableBytes = 256 << 10
	snapshotTotalBytes    = 768 << 10
)

// SnapshotManifest reports what a scratch snapshot captured.
type SnapshotManifest struct {
	Saved   []string      `json:"saved"`
	Skipped []SkippedName `json:"skipped,omitempty"`
	Bytes   int           `json:"bytes"`
}

// SkippedName names a global left out of a snapshot or a restore, with why.
type SkippedName struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// RestoreReport reports what a fresh worker revived from a snapshot.
type RestoreReport struct {
	Restored []string      `json:"restored"`
	Failed   []SkippedName `json:"failed,omitempty"`
}

// ScratchStore persists one kernel's scratch snapshot between worker
// processes. Load returns an empty program when nothing is stored.
type ScratchStore interface {
	Load(ctx context.Context) (program string, manifest SnapshotManifest, err error)
	Save(ctx context.Context, program string, manifest SnapshotManifest) error
}

// A snapshot is Starlark source: `name = repr(value)` for data, `b = a` for
// aliases, and the original text of top-level defs and lambda assignments.
// Restoring is executing it. Closures, self-referential values, and oversized
// values are skipped by name; host modules are re-installed by the worker.

type scratchSource struct {
	kind  string // "def" or "lambda"
	order int
	text  string
}

// recordSources remembers the source of top-level defs and lambda
// assignments so a snapshot can re-declare them.
func (w *worker) recordSources(code string, stmts []syntax.Stmt) {
	for _, stmt := range stmts {
		switch stmt := stmt.(type) {
		case *syntax.DefStmt:
			w.rememberSource(stmt.Name.Name, "def", spanText(code, stmt))
		case *syntax.AssignStmt:
			ident, ok := stmt.LHS.(*syntax.Ident)
			if !ok || stmt.Op != syntax.EQ {
				continue
			}
			rhs := stmt.RHS
			if paren, ok := rhs.(*syntax.ParenExpr); ok {
				rhs = paren.X
			}
			if _, ok := rhs.(*syntax.LambdaExpr); ok {
				w.rememberSource(ident.Name, "lambda", spanText(code, stmt))
			}
		}
	}
}

// rememberSource keeps a definition's text only when it parses back as one
// statement, so a snapshot can never carry a truncated def.
func (w *worker) rememberSource(name, kind, text string) {
	if text == "" {
		return
	}
	if file, err := cellFileOptions.Parse("<source>", text, 0); err != nil || len(file.Stmts) != 1 {
		return
	}
	if w.sources == nil {
		w.sources = map[string]scratchSource{}
	}
	w.nextSource++
	w.sources[name] = scratchSource{kind: kind, order: w.nextSource, text: text}
}

// spanText slices a statement's source out of its cell. Positions count
// runes. Span ends are exclusive for most nodes but some closing tokens
// report their own position, so the slice runs to the end of the last line
// when the exact end does not parse; a top-level statement owns its line.
func spanText(code string, node syntax.Node) string {
	start, end := node.Span()
	lines := strings.Split(code, "\n")
	if start.Line < 1 || int(end.Line) > len(lines) || end.Line < start.Line {
		return ""
	}
	slice := func(toLineEnd bool) string {
		var b strings.Builder
		for line := int(start.Line); line <= int(end.Line); line++ {
			text := lines[line-1]
			from, to := 0, len(text)
			if line == int(start.Line) {
				from = runeOffset(text, int(start.Col))
			}
			if line == int(end.Line) && !toLineEnd {
				to = runeOffset(text, int(end.Col))
			}
			if from > to {
				return ""
			}
			b.WriteString(text[from:to])
			b.WriteByte('\n')
		}
		return b.String()
	}
	exact := slice(false)
	if file, err := cellFileOptions.Parse("<source>", exact, 0); err == nil && len(file.Stmts) == 1 {
		return exact
	}
	return slice(true)
}

// runeOffset converts a 1-based rune column into a byte offset.
func runeOffset(text string, col int) int {
	offset := 0
	for i := 1; i < col && offset < len(text); i++ {
		_, size := utf8.DecodeRuneInString(text[offset:])
		offset += size
	}
	return offset
}

func (w *worker) snapshot() frame {
	program, manifest := w.buildSnapshot()
	return frame{Code: program, Value: manifest}
}

func (w *worker) buildSnapshot() (string, SnapshotManifest) {
	limit := min(snapshotTotalBytes, w.frameBytes*3/4)
	names := make([]string, 0, len(w.globals))
	for name := range w.globals {
		if _, module := w.modules[name]; !module {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var manifest SnapshotManifest
	var program strings.Builder
	skip := func(name, reason string) {
		manifest.Skipped = append(manifest.Skipped, SkippedName{Name: name, Reason: reason})
	}
	emit := func(name, text string) {
		switch {
		case len(text) > snapshotVariableBytes:
			skip(name, "exceeds per-variable cap")
		case program.Len()+len(text) > limit:
			skip(name, "exceeds aggregate cap")
		default:
			program.WriteString(text)
			manifest.Saved = append(manifest.Saved, name)
		}
	}
	type function struct {
		order int
		name  string
		text  string
	}
	var functions []function
	identities := map[any]string{}
	for _, name := range names {
		switch value := w.globals[name].(type) {
		case *starlark.Builtin, *starlarkstruct.Struct:
			// Host modules are re-installed by every worker.
		case *starlark.Function:
			source, known := w.sources[name]
			switch {
			case value.NumFreeVars() > 0:
				skip(name, "closure")
			case known && ((source.kind == "def" && value.Name() == name) || (source.kind == "lambda" && value.Name() == "lambda")):
				functions = append(functions, function{order: source.order, name: name, text: source.text})
			default:
				skip(name, "function source unavailable")
			}
		case starlark.Float:
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				skip(name, "non-finite float")
				continue
			}
			emit(name, name+" = "+value.String()+"\n")
		default:
			if key := identityKey(value); key != nil {
				if first, seen := identities[key]; seen {
					emit(name, name+" = "+first+"\n")
					continue
				}
				identities[key] = name
			}
			if hasCycle(value, map[any]bool{}) {
				skip(name, "self-referential value")
				continue
			}
			emit(name, name+" = "+value.String()+"\n")
		}
	}
	sort.Slice(functions, func(i, j int) bool { return functions[i].order < functions[j].order })
	for _, fn := range functions {
		emit(fn.name, fn.text)
	}
	manifest.Bytes = program.Len()
	return program.String(), manifest
}

// identityKey returns a comparable identity for mutable containers so aliases
// restore as aliases instead of copies.
func identityKey(value starlark.Value) any {
	switch value := value.(type) {
	case *starlark.List:
		return value
	case *starlark.Dict:
		return value
	}
	return nil
}

func hasCycle(value starlark.Value, path map[any]bool) bool {
	switch value := value.(type) {
	case *starlark.List:
		if path[value] {
			return true
		}
		path[value] = true
		defer delete(path, value)
		for index := range value.Len() {
			if hasCycle(value.Index(index), path) {
				return true
			}
		}
	case *starlark.Dict:
		if path[value] {
			return true
		}
		path[value] = true
		defer delete(path, value)
		for _, item := range value.Items() {
			if hasCycle(item[0], path) || hasCycle(item[1], path) {
				return true
			}
		}
	case starlark.Tuple:
		for _, item := range value {
			if hasCycle(item, path) {
				return true
			}
		}
	}
	return false
}

func (w *worker) restore(program string) frame {
	return frame{Value: w.applySnapshot(program)}
}

// applySnapshot executes a snapshot into the current globals. The whole
// program runs as one chunk first so restored functions share one module;
// on failure it replays statement by statement so one bad binding is
// reported by name instead of taking the rest with it.
func (w *worker) applySnapshot(program string) RestoreReport {
	maps.Copy(w.globals, w.modules)
	var report RestoreReport
	if strings.TrimSpace(program) == "" {
		return report
	}
	file, err := cellFileOptions.Parse("<scratch>", program, 0)
	if err != nil {
		report.Failed = append(report.Failed, SkippedName{Name: "<snapshot>", Reason: err.Error()})
		return report
	}
	// Restored functions need their source again for the next snapshot.
	w.recordSources(program, file.Stmts)
	w.restoring = true
	defer func() { w.restoring = false }()
	if err := w.execChunk(file); err == nil {
		report.Restored = boundNames(file.Stmts)
		return report
	}
	maps.Copy(w.globals, w.modules)
	for _, stmt := range file.Stmts {
		names := boundNames([]syntax.Stmt{stmt})
		single, err := cellFileOptions.Parse("<scratch>", spanText(program, stmt), 0)
		if err == nil {
			err = w.execChunk(single)
		}
		if err != nil {
			for _, name := range names {
				report.Failed = append(report.Failed, SkippedName{Name: name, Reason: err.Error()})
			}
			continue
		}
		report.Restored = append(report.Restored, names...)
	}
	return report
}

func (w *worker) execChunk(file *syntax.File) error {
	thread := &starlark.Thread{Name: "rlm-scratch", Print: func(*starlark.Thread, string) {}}
	thread.SetMaxExecutionSteps(w.steps)
	return starlark.ExecREPLChunk(file, thread, w.globals)
}

func boundNames(stmts []syntax.Stmt) []string {
	var names []string
	for _, stmt := range stmts {
		switch stmt := stmt.(type) {
		case *syntax.DefStmt:
			names = append(names, stmt.Name.Name)
		case *syntax.AssignStmt:
			if ident, ok := stmt.LHS.(*syntax.Ident); ok {
				names = append(names, ident.Name)
			}
		}
	}
	return names
}
