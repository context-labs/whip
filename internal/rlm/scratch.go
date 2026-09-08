package rlm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
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
// processes. The string is a structured JSON snapshot, never executable source.
// Load returns an empty string when nothing is stored.
type ScratchStore interface {
	Load(ctx context.Context) (snapshot string, manifest SnapshotManifest, err error)
	Save(ctx context.Context, snapshot string, manifest SnapshotManifest) error
}

type scratchSource struct {
	name   string
	text   string
	deps   []string
	scopes map[string]resolve.Scope
	reason string
}

// Sources belong to function objects, not mutable global names. A failed cell
// can still install definitions, and their source must replace older records.
func (w *worker) recordSources(code string, stmts []syntax.Stmt) {
	if w.sources == nil {
		w.sources = map[*starlark.Function]scratchSource{}
	}
	for _, stmt := range stmts {
		name, params, ok := helperStatement(stmt)
		if !ok {
			continue
		}
		fn, ok := w.globals[name].(*starlark.Function)
		if !ok || fn.Position().Filename() != fmt.Sprintf("<rlm-cell-%d>", w.nextSource) {
			continue
		}
		pos, _ := stmt.Span()
		if assignment, ok := stmt.(*syntax.AssignStmt); ok {
			expr := assignment.RHS
			for {
				paren, ok := expr.(*syntax.ParenExpr)
				if !ok {
					break
				}
				expr = paren.X
			}
			pos, _ = expr.Span()
		}
		if fn.Position().Line != pos.Line || fn.Position().Col != pos.Col {
			continue
		}
		source := scratchSource{name: name, text: spanText(code, stmt), scopes: map[string]resolve.Scope{}}
		if parsed, err := cellFileOptions.Parse("<source>", source.text, 0); err != nil || len(parsed.Stmts) != 1 {
			source.reason = "function source unavailable"
		}
		for _, param := range params {
			if binary, ok := param.(*syntax.BinaryExpr); ok && binary.Op == syntax.EQ && !literalDefault(binary.Y) {
				source.reason = "default is not an immutable literal"
			}
		}
		seen := map[string]bool{}
		syntax.Walk(stmt, func(node syntax.Node) bool {
			if ident, ok := node.(*syntax.Ident); ok {
				if binding, ok := ident.Binding.(*resolve.Binding); ok && (binding.Scope == resolve.Global || binding.Scope == resolve.Predeclared || binding.Scope == resolve.Universal) && !seen[ident.Name] {
					source.scopes[ident.Name] = binding.Scope
					seen[ident.Name] = true
					source.deps = append(source.deps, ident.Name)
				}
			}
			return true
		})
		w.sources[fn] = source
	}
	// Retain source only for currently exported helpers.
	live := map[*starlark.Function]bool{}
	for _, value := range w.globals {
		if fn, ok := value.(*starlark.Function); ok {
			live[fn] = true
		}
	}
	for fn := range w.sources {
		if !live[fn] {
			delete(w.sources, fn)
		}
	}
}

func helperStatement(stmt syntax.Stmt) (string, []syntax.Expr, bool) {
	switch stmt := stmt.(type) {
	case *syntax.DefStmt:
		return stmt.Name.Name, stmt.Params, true
	case *syntax.AssignStmt:
		name, ok := stmt.LHS.(*syntax.Ident)
		if !ok || stmt.Op != syntax.EQ {
			return "", nil, false
		}
		rhs := stmt.RHS
		for {
			paren, ok := rhs.(*syntax.ParenExpr)
			if !ok {
				break
			}
			rhs = paren.X
		}
		if lambda, ok := rhs.(*syntax.LambdaExpr); ok {
			return name.Name, lambda.Params, true
		}
	}
	return "", nil, false
}

func literalDefault(expr syntax.Expr) bool {
	switch expr := expr.(type) {
	case *syntax.Literal:
		if value, ok := expr.Value.(float64); ok {
			return !math.IsNaN(value) && !math.IsInf(value, 0)
		}
		return true
	case *syntax.Ident:
		binding, ok := expr.Binding.(*resolve.Binding)
		return ok && binding.Scope == resolve.Universal && (expr.Name == "None" || expr.Name == "True" || expr.Name == "False")
	case *syntax.ParenExpr:
		return literalDefault(expr.X)
	case *syntax.TupleExpr:
		for _, item := range expr.List {
			if !literalDefault(item) {
				return false
			}
		}
		return true
	case *syntax.UnaryExpr:
		_, numeric := expr.X.(*syntax.Literal)
		return numeric && (expr.Op == syntax.PLUS || expr.Op == syntax.MINUS)
	}
	return false
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

func (w *worker) restore(program string) frame {
	report := w.applySnapshot(program)
	for _, failure := range report.Failed {
		if failure.Name == "<snapshot>" {
			return frame{Error: failure.Reason, Value: report}
		}
	}
	return frame{Value: report}
}

func (w *worker) execChunk(file *syntax.File) error {
	thread := &starlark.Thread{Name: "rlm-scratch", Print: func(*starlark.Thread, string) {}}
	thread.SetMaxExecutionSteps(w.steps)
	return starlark.ExecREPLChunk(file, thread, w.globals)
}

func (w *worker) applySnapshot(program string) RestoreReport {
	report := RestoreReport{Restored: []string{}}
	if program == "" {
		return report
	}
	fail := func(err error) RestoreReport {
		return RestoreReport{Failed: []SkippedName{{Name: "<snapshot>", Reason: err.Error()}}}
	}
	if len(program) > snapshotTotalBytes {
		return fail(errors.New("scratch snapshot exceeds size limit"))
	}
	var snapshot scratchSnapshot
	if err := json.Unmarshal([]byte(program), &snapshot); err != nil {
		return fail(fmt.Errorf("invalid scratch snapshot: %w", err))
	}
	if snapshot.Version != 1 {
		return fail(errors.New("unsupported scratch snapshot version"))
	}
	data, err := decodeScratch(snapshot)
	if err != nil {
		return fail(err)
	}
	for name := range data {
		if _, module := w.modules[name]; module {
			return fail(errors.New("data shadows host module"))
		}
	}
	helpers := map[string]scratchHelper{}
	bad := map[string]string{}
	for _, helper := range snapshot.Helpers {
		if !validScratchName(helper.Name) {
			return fail(errors.New("invalid helper binding name"))
		}
		if _, module := w.modules[helper.Name]; module {
			return fail(errors.New("helper shadows host module"))
		}
		if _, exists := data[helper.Name]; exists {
			return fail(fmt.Errorf("duplicate scratch binding %q", helper.Name))
		}
		if _, exists := helpers[helper.Name]; exists {
			return fail(fmt.Errorf("duplicate helper %q", helper.Name))
		}
		helpers[helper.Name] = helper
		if len(helper.Source) > snapshotVariableBytes {
			bad[helper.Name] = "exceeds per-variable cap"
			continue
		}
		file, err := cellFileOptions.Parse("<scratch-validate>", helper.Source, 0)
		if err != nil || len(file.Stmts) != 1 {
			bad[helper.Name] = "invalid helper definition"
			continue
		}
		name, _, ok := helperStatement(file.Stmts[0])
		if !ok || name != helper.Name {
			bad[helper.Name] = "invalid helper definition"
			continue
		}
	}
	// Validate independently against placeholders. This resolves lexical names
	// without evaluating definitions or defaults, including mutually recursive helpers.
	available := func(name string) bool {
		_, a := data[name]
		_, b := helpers[name]
		_, c := w.modules[name]
		return a || b || c
	}
	for name, helper := range helpers {
		if bad[name] != "" {
			continue
		}
		file, err := cellFileOptions.Parse("<scratch-validate>", helper.Source, 0)
		if err == nil {
			err = resolve.File(file, available, func(name string) bool { _, ok := starlark.Universe[name]; return ok })
		}
		if err != nil {
			bad[name] = "invalid helper dependencies"
			continue
		}
		// Constant-looking names can be rebound in this Starlark dialect.
		// Check resolved defaults so True/False/None really mean literals.
		_, params, _ := helperStatement(file.Stmts[0])
		for _, param := range params {
			if binary, ok := param.(*syntax.BinaryExpr); ok && binary.Op == syntax.EQ && !literalDefault(binary.Y) {
				bad[name] = "default is not an immutable literal"
			}
		}
		deps := []string{}
		syntax.Walk(file, func(node syntax.Node) bool {
			if ident, ok := node.(*syntax.Ident); ok {
				if binding, ok := ident.Binding.(*resolve.Binding); ok && (binding.Scope == resolve.Global || binding.Scope == resolve.Predeclared || binding.Scope == resolve.Universal) {
					deps = append(deps, ident.Name)
				}
			}
			return true
		})
		helper.Deps = deps
		helpers[name] = helper
	}
	propagateHelperFailures(helpers, bad, func(name string) bool {
		_, a := data[name]
		_, b := w.modules[name]
		_, builtin := starlark.Universe[name]
		return a || b || builtin
	})
	var source strings.Builder
	for _, helper := range snapshot.Helpers {
		if bad[helper.Name] == "" {
			source.WriteString(helper.Source)
			source.WriteByte('\n')
		} else {
			report.Failed = append(report.Failed, SkippedName{Name: helper.Name, Reason: bad[helper.Name]})
		}
	}
	// Work in an isolated environment: a corrupt checkpoint cannot erase live data.
	previous := w.globals
	w.globals = data
	maps.Copy(w.globals, w.modules)
	w.restoring = true
	defer func() { w.restoring = false }()
	if source.Len() > 0 {
		w.nextSource++
		file, err := cellFileOptions.Parse(fmt.Sprintf("<rlm-cell-%d>", w.nextSource), source.String(), 0)
		if err == nil {
			err = w.execChunk(file)
		}
		if err != nil {
			w.globals = previous
			return fail(fmt.Errorf("restore helpers: %w", err))
		}
		w.recordSources(source.String(), file.Stmts)
	}
	for _, binding := range snapshot.Bindings {
		report.Restored = append(report.Restored, binding.Name)
	}
	for _, helper := range snapshot.Helpers {
		if bad[helper.Name] == "" {
			report.Restored = append(report.Restored, helper.Name)
		}
	}
	return report
}

func validScratchName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && !unicode.IsLetter(r) && (i == 0 || !unicode.IsDigit(r)) {
			return false
		}
	}
	file, err := cellFileOptions.Parse("<binding>", name+" = None", 0)
	if err != nil || len(file.Stmts) != 1 {
		return false
	}
	assignment, ok := file.Stmts[0].(*syntax.AssignStmt)
	if !ok {
		return false
	}
	ident, ok := assignment.LHS.(*syntax.Ident)
	return ok && ident.Name == name
}
