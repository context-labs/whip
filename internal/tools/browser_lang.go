// browser_lang.go parses and runs browser_exec programs. The language is
// deliberately not a JS engine: each statement is one helper call
// (`name(arg, ...)`), arguments are JSON values, and `print(expr)` where
// expr is a helper call or a quoted string. js(...) payloads pass through
// to the page verbatim. Simple semantics keep the model reliable and the
// parser ~100 lines with no eval surface in whip itself.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/context-labs/whip/internal/browser"
)

// helperStmt is one parsed helper call (shared by browser_exec and
// computer_exec — same mini-language).
type helperStmt struct {
	name string
	args []any
	raw  string // original text, for error messages
}

func (s helperStmt) String() string { return s.raw }

// parseBrowserProgram splits code into statements. Lines starting with #
// or // are comments (the first one doubles as the TUI step label).
// Semicolons split multiple calls on one line, except inside js("...")
// string arguments (the JSON string parser handles that; splitting is
// quote-aware).
func parseHelperProgram(code string) ([]helperStmt, error) {
	var out []helperStmt
	for _, chunk := range splitStatements(code) {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" || strings.HasPrefix(chunk, "#") || strings.HasPrefix(chunk, "//") {
			continue
		}
		st, err := parseStatement(chunk)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	if len(out) == 0 {
		return nil, errors.New("no helper calls in code — e.g. goto(\"https://example.com\"); print(info())")
	}
	return out, nil
}

// splitStatements splits on newlines and semicolons outside string literals.
func splitStatements(code string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	// A chunk that starts with # or // is a comment — strip it BEFORE the
	// quote-aware scan, or an apostrophe inside a comment ("TextEdit's")
	// opens a phantom quote that swallows the following real statement.
	var lines []string
	for ln := range strings.SplitSeq(code, "\n") {
		if t := strings.TrimSpace(ln); strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") {
			continue
		}
		lines = append(lines, ln)
	}
	for _, r := range strings.Join(lines, "\n") {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != 0:
			cur.WriteRune(r)
			escaped = true
		case quote != 0:
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
			cur.WriteRune(r)
		case r == '\n' || r == ';':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// parseStatement parses `name(arg, ...)` or `print(expr)` where expr is a
// nested helper call or string literal.
func parseStatement(s string) (helperStmt, error) {
	open := strings.Index(s, "(")
	if open <= 0 || !strings.HasSuffix(s, ")") {
		return helperStmt{}, fmt.Errorf("malformed statement %q — expected name(args...)", s)
	}
	name := strings.TrimSpace(s[:open])
	inner := s[open+1 : len(s)-1]
	if name == "print" {
		inner = strings.TrimSpace(inner)
		if inner == "" {
			return helperStmt{}, errors.New("print() needs an argument")
		}
		// print(helper(...)) nests the call; print("lit") passes the string.
		if strings.Contains(inner, "(") && strings.HasSuffix(inner, ")") {
			sub, err := parseStatement(inner)
			if err != nil {
				return helperStmt{}, err
			}
			return helperStmt{name: "print", args: []any{sub}, raw: s}, nil
		}
	}
	args, err := parseArgs(inner)
	if err != nil {
		return helperStmt{}, fmt.Errorf("%s: %w", s, err)
	}
	return helperStmt{name: name, args: args, raw: s}, nil
}

// parseArgs parses a JSON-ish argument list: strings (double or single
// quoted), numbers, booleans, arrays, objects.
func parseArgs(s string) ([]any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var parts []string
	var cur strings.Builder
	var quote rune
	depth, escaped := 0, false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != 0:
			cur.WriteRune(r)
			escaped = true
		case quote != 0:
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
			cur.WriteRune(r)
		case r == '[' || r == '{':
			depth++
			cur.WriteRune(r)
		case r == ']' || r == '}':
			depth--
			cur.WriteRune(r)
		case r == ',' && depth == 0:
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if last := strings.TrimSpace(cur.String()); last != "" {
		parts = append(parts, last)
	}
	args := make([]any, len(parts))
	for i, p := range parts {
		v, err := parseValue(p)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i+1, err)
		}
		args[i] = v
	}
	return args, nil
}

func parseValue(s string) (any, error) {
	// single-quoted strings → JSON
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		s = `"` + strings.ReplaceAll(strings.ReplaceAll(s[1:len(s)-1], `\`, `\\`), `"`, `\"`) + `"`
	}
	var v any
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		// Bare word: treat as string (forgive press(Enter) over press("Enter"))
		if strings.TrimSpace(s) != "" && !strings.ContainsAny(s, " \t") {
			return s, nil
		}
		return nil, fmt.Errorf("bad value %q", s)
	}
	if n, ok := v.(json.Number); ok {
		if f, err := n.Float64(); err == nil {
			return f, nil
		}
	}
	return v, nil
}

// exec runs one statement, returning printed output and an optional
// screenshot JPEG.
func (s helperStmt) exec(ctx context.Context, b browser.Backend, allowPrivateURLs bool) (out string, shot []byte, err error) {
	if s.name == "print" {
		switch a := s.args[0].(type) {
		case helperStmt:
			sub, shot, err := a.exec(ctx, b, allowPrivateURLs)
			return sub, shot, err
		case string:
			return a, nil, nil
		default:
			data, _ := json.Marshal(a)
			return string(data), nil, nil
		}
	}
	argStr := func(i int) (string, error) {
		if i >= len(s.args) {
			return "", fmt.Errorf("%s: missing arg %d", s.name, i+1)
		}
		switch v := s.args[i].(type) {
		case string:
			return v, nil
		case float64:
			return fmt.Sprintf("%v", v), nil
		default:
			data, _ := json.Marshal(v)
			return string(data), nil
		}
	}
	argNum := func(i int) (float64, error) {
		if i >= len(s.args) {
			return 0, fmt.Errorf("%s: missing arg %d", s.name, i+1)
		}
		f, ok := s.args[i].(float64)
		if !ok {
			return 0, fmt.Errorf("%s: arg %d must be a number", s.name, i+1)
		}
		return f, nil
	}
	argBool := func(i int, def bool) bool {
		if i >= len(s.args) {
			return def
		}
		v, _ := s.args[i].(bool)
		return v
	}

	switch s.name {
	case "goto":
		url, err := argStr(0)
		if err != nil {
			return "", nil, err
		}
		if err := browser.CheckURL(ctx, url); err != nil {
			return "", nil, err
		}
		if b.Mode() != browser.ModeLive {
			if err := browser.CheckPrivateURL(ctx, url, allowPrivateURLs); err != nil {
				return "", nil, err
			}
		}
		return "", nil, b.Navigate(ctx, url)
	case "back":
		return "", nil, b.Back(ctx)
	case "info":
		info, err := b.Info(ctx)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.Marshal(info)
		return string(data), nil, nil
	case "js":
		expr, err := argStr(0)
		if err != nil {
			return "", nil, err
		}
		res, err := b.Eval(ctx, expr)
		return res, nil, err
	case "click":
		x, err := argNum(0)
		if err != nil {
			return "", nil, err
		}
		y, err := argNum(1)
		if err != nil {
			return "", nil, err
		}
		return "", nil, b.ClickAt(ctx, x, y)
	case "type":
		text, err := argStr(0)
		if err != nil {
			return "", nil, err
		}
		return "", nil, b.TypeText(ctx, text)
	case "press":
		key, err := argStr(0)
		if err != nil {
			return "", nil, err
		}
		return "", nil, b.PressKey(ctx, key)
	case "fill":
		sel, err := argStr(0)
		if err != nil {
			return "", nil, err
		}
		text, err := argStr(1)
		if err != nil {
			return "", nil, err
		}
		return "", nil, b.Fill(ctx, sel, text)
	case "scroll":
		dy, err := argNum(0)
		if err != nil {
			dy = -300 // browser-harness's default wheel delta
		}
		return "", nil, b.Scroll(ctx, dy)
	case "waitLoad":
		return "", nil, b.WaitLoad(ctx)
	case "waitFor":
		sel, err := argStr(0)
		if err != nil {
			return "", nil, err
		}
		found, err := b.WaitElement(ctx, sel, argBool(1, false))
		if err != nil {
			return "", nil, err
		}
		return strconv.FormatBool(found), nil, nil
	case "ax":
		tree, err := b.AXTree(ctx)
		return tree, nil, err
	case "box":
		id, err := argNum(0)
		if err != nil {
			return "", nil, err
		}
		x, y, err := b.BoxModel(ctx, int(id))
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf(`{"x":%.1f,"y":%.1f}`, x, y), nil, nil
	case "tabs":
		tabs, err := b.Tabs(ctx)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.Marshal(tabs)
		return string(data), nil, nil
	case "useTab":
		id, err := argStr(0)
		if err != nil {
			return "", nil, err
		}
		return "", nil, b.UseTab(ctx, id)
	case "upload":
		sel, err := argStr(0)
		if err != nil {
			return "", nil, err
		}
		if len(s.args) < 2 {
			return "", nil, errors.New("upload: missing paths array")
		}
		var paths []string
		switch v := s.args[1].(type) {
		case []any:
			for _, p := range v {
				ps, ok := p.(string)
				if !ok {
					return "", nil, errors.New("upload: paths must be strings")
				}
				paths = append(paths, ps)
			}
		case string:
			paths = []string{v}
		default:
			return "", nil, errors.New("upload: paths must be an array or string")
		}
		return "", nil, b.UploadFiles(ctx, sel, paths)
	case "dialog":
		accept := argBool(0, true)
		prompt, _ := argStr(1)
		return "", nil, b.HandleDialog(accept, prompt)
	case "screenshot":
		jpeg, err := b.Screenshot(ctx, 1568)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("(screenshot captured: %d bytes, jpeg, ≤1568px)", len(jpeg)), jpeg, nil
	default:
		return "", nil, fmt.Errorf("unknown helper %q — see the tool description for the list", s.name)
	}
}
