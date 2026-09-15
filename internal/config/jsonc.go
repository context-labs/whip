package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// stripJSONC removes // line and /* block */ comments and trailing commas from
// JSONC source, leaving string literals untouched. It rewrites stripped bytes
// to spaces (keeping newlines) so line/column numbers in any downstream parse
// error still line up with the original file.
func stripJSONC(src []byte) ([]byte, error) {
	out := make([]byte, len(src))
	copy(out, src)
	i := 0
	n := len(src)
	inString := false
	for i < n {
		c := src[i]
		if inString {
			switch c {
			case '\\': // skip the escaped byte
				i++
			case '"':
				inString = false
			}
			i++
			continue
		}
		switch c {
		case '"':
			inString = true
			i++
		case '/':
			if i+1 < n && src[i+1] == '/' { // line comment
				j := i
				for j < n && src[j] != '\n' {
					j++
				}
				blank(out, i, j)
				i = j
			} else if i+1 < n && src[i+1] == '*' { // block comment
				j := i + 2
				for j+1 < n && (src[j] != '*' || src[j+1] != '/') {
					j++
				}
				if j+1 >= n {
					return nil, errors.New("unterminated block comment")
				}
				blank(out, i, j+2)
				i = j + 2
			} else {
				i++
			}
		default:
			i++
		}
	}
	if inString {
		return nil, errors.New("unterminated string literal")
	}
	return removeTrailingCommas(out), nil
}

// blank replaces src[start:end] with spaces, preserving newlines.
func blank(b []byte, start, end int) {
	for k := start; k < end; k++ {
		if b[k] != '\n' {
			b[k] = ' '
		}
	}
}

// removeTrailingCommas deletes a comma that is immediately followed (after
// whitespace) by a closing } or ], outside string literals. Runs after comment
// stripping so comments can't hide the closer.
func removeTrailingCommas(src []byte) []byte {
	out := make([]byte, 0, len(src))
	i := 0
	n := len(src)
	inString := false
	for i < n {
		c := src[i]
		if inString {
			out = append(out, c)
			if c == '\\' && i+1 < n {
				out = append(out, src[i+1])
				i += 2
				continue
			}
			if c == '"' {
				inString = false
			}
			i++
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			i++
			continue
		}
		if c == ',' {
			// look ahead past whitespace for a closer
			j := i + 1
			for j < n && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
				j++
			}
			if j < n && (src[j] == '}' || src[j] == ']') {
				i++ // drop the comma
				continue
			}
		}
		out = append(out, c)
		i++
	}
	return out
}

// parseJSONC decodes JSONC (JSON with comments and trailing commas) into v.
func parseJSONC(data []byte, v any) error {
	stripped, err := stripJSONC(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes.TrimSpace(stripped), v)
}

// ParseJSONC decodes a JSONC document (comments and trailing commas allowed)
// into v. Other agents write JSONC too (OpenCode's opencode.jsonc), so MCP
// discovery shares the one parser instead of growing a second.
func ParseJSONC(data []byte, v any) error { return parseJSONC(data, v) }

// ReadJSON reads a small JSON file from the loopy dir into v. Missing file
// is an error the caller treats as "empty" — these are optional state files.
func ReadJSON(name string, v any) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// WriteJSON writes v as JSON to a small file in the loopy dir.
func WriteJSON(name string, v any) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o600)
}
