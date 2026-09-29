// Package helperprogram parses the retained browser/computer helper language.
// It never evaluates JavaScript, starts a process, resolves a resource or grants
// authority. Consumers validate their complete operation vocabulary before work.
package helperprogram

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	MaxBytes      = 64 << 10
	MaxCalls      = 128
	MaxArguments  = 32
	MaxPrintDepth = 16
)

var (
	identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	bareWord   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
)

// Call contains only literal arguments, except print's one optional nested Call.
// JSON numbers retain their spelling as json.Number; consumers impose numeric
// ranges without converting large integers through float64.
type Call struct {
	Name      string
	Arguments []any
}

func Parse(code string) ([]Call, error) {
	if len(code) == 0 || len(code) > MaxBytes || !utf8.ValidString(code) || strings.ContainsRune(code, 0) {
		return nil, errors.New("helper program must be bounded UTF-8 text")
	}
	statements, err := splitStatements(code)
	if err != nil {
		return nil, err
	}
	result := make([]Call, 0, len(statements))
	for index, statement := range statements {
		call, err := parseCall(statement, 0)
		if err != nil {
			return nil, fmt.Errorf("helper statement %d: %w", index+1, err)
		}
		result = append(result, call)
	}
	if len(result) == 0 {
		return nil, errors.New("helper program contains no calls")
	}
	return result, nil
}

func splitStatements(code string) ([]string, error) {
	// Whole-line comments precede scanning so apostrophes in a human step label
	// cannot open a string that consumes subsequent real statements.
	lines := make([]string, 0, strings.Count(code, "\n")+1)
	for line := range strings.SplitSeq(code, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "//") {
			lines = append(lines, line)
		}
	}
	code = strings.Join(lines, "\n")
	var result []string
	start, depth := 0, 0
	var quote byte
	escaped := false
	appendCall := func(end int) error {
		if text := strings.TrimSpace(code[start:end]); text != "" {
			if len(result) == MaxCalls {
				return errors.New("helper program call limit exceeded")
			}
			result = append(result, text)
		}
		start = end + 1
		return nil
	}
	for index := range len(code) {
		value := code[index]
		switch {
		case escaped:
			escaped = false
		case quote != 0:
			switch value {
			case '\\':
				escaped = true
			case quote:
				quote = 0
			}
		case value == '\'' || value == '"':
			quote = value
		case value == '(':
			depth++
			if depth > MaxPrintDepth+1 {
				return nil, errors.New("helper nesting limit exceeded")
			}
		case value == ')':
			depth--
			if depth < 0 {
				return nil, errors.New("unbalanced helper call")
			}
		case (value == ';' || value == '\n') && depth == 0:
			if err := appendCall(index); err != nil {
				return nil, err
			}
		}
	}
	if quote != 0 || depth != 0 || escaped {
		return nil, errors.New("unclosed helper string or call")
	}
	if err := appendCall(len(code)); err != nil {
		return nil, err
	}
	return result, nil
}

func parseCall(text string, depth int) (Call, error) {
	open := strings.IndexByte(text, '(')
	if open < 1 || !strings.HasSuffix(text, ")") {
		return Call{}, errors.New("expected helper(args)")
	}
	name := strings.TrimSpace(text[:open])
	if !identifier.MatchString(name) {
		return Call{}, errors.New("invalid helper name")
	}
	inner := strings.TrimSpace(text[open+1 : len(text)-1])
	if name == "print" {
		if inner == "" {
			return Call{}, errors.New("print requires one argument")
		}
		if open := strings.IndexByte(inner, '('); open > 0 && identifier.MatchString(strings.TrimSpace(inner[:open])) && strings.HasSuffix(inner, ")") {
			if depth == MaxPrintDepth {
				return Call{}, errors.New("print nesting limit exceeded")
			}
			nested, err := parseCall(inner, depth+1)
			if err != nil {
				return Call{}, err
			}
			return Call{Name: name, Arguments: []any{nested}}, nil
		}
	}
	arguments, err := parseArguments(inner)
	if err != nil {
		return Call{}, err
	}
	if name == "print" && len(arguments) != 1 {
		return Call{}, errors.New("print requires one argument")
	}
	return Call{Name: name, Arguments: arguments}, nil
}

func parseArguments(text string) ([]any, error) {
	if text == "" {
		return []any{}, nil
	}
	parts := []string{}
	start, depth := 0, 0
	var quote byte
	escaped := false
	for index := range len(text) {
		value := text[index]
		switch {
		case escaped:
			escaped = false
		case quote != 0:
			switch value {
			case '\\':
				escaped = true
			case quote:
				quote = 0
			}
		case value == '\'' || value == '"':
			quote = value
		case value == '[' || value == '{':
			depth++
		case value == ']' || value == '}':
			depth--
			if depth < 0 {
				return nil, errors.New("unbalanced helper argument")
			}
		case value == ',' && depth == 0:
			parts = append(parts, strings.TrimSpace(text[start:index]))
			start = index + 1
			if len(parts) == MaxArguments {
				return nil, errors.New("helper argument limit exceeded")
			}
		}
	}
	if quote != 0 || depth != 0 {
		return nil, errors.New("unclosed helper argument")
	}
	parts = append(parts, strings.TrimSpace(text[start:]))
	result := make([]any, len(parts))
	for index, part := range parts {
		value, err := parseValue(part)
		if err != nil {
			return nil, fmt.Errorf("invalid helper argument %d", index+1)
		}
		result[index] = value
	}
	return result, nil
}

func parseValue(text string) (any, error) {
	if len(text) >= 2 && text[0] == '\'' && text[len(text)-1] == '\'' {
		var quoted strings.Builder
		quoted.WriteByte('"')
		for index := 1; index < len(text)-1; index++ {
			value := text[index]
			if value == '\\' && index+1 < len(text)-1 {
				index++
				if text[index] != '\'' {
					quoted.WriteByte('\\')
				}
				quoted.WriteByte(text[index])
			} else {
				if value == '"' {
					quoted.WriteByte('\\')
				}
				quoted.WriteByte(value)
			}
		}
		quoted.WriteByte('"')
		text = quoted.String()
	}
	var result any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err == nil {
		if decoder.Decode(new(any)) != io.EOF {
			return nil, errors.New("trailing helper argument data")
		}
		return result, nil
	}
	if bareWord.MatchString(text) {
		return text, nil
	}
	return nil, errors.New("invalid helper literal")
}
