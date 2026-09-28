package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"
)

// ValidateStateJSON checks the bounded JSON tree without decoding numbers to
// float64 or materializing a second large object graph. The engine adapters use
// the same maximum depth for values crossing the process boundary.
func ValidateStateJSON(data []byte) error {
	if len(data) == 0 || len(data) > MaxStateValueBytes || !utf8.Valid(data) || !json.Valid(data) {
		return fmt.Errorf("%w: state requires valid UTF-8 JSON up to 64 MiB", ErrInvalid)
	}
	depth := 0
	quoted := false
	for i := 0; i < len(data); i++ {
		char := data[i]
		if quoted {
			switch char {
			case '\\':
				i++ // json.Valid already checked the escape's syntax and bounds.
				if data[i] == 'u' {
					code, _ := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
					i += 4
					if code >= 0xd800 && code <= 0xdbff {
						if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
							return fmt.Errorf("%w: unpaired Unicode surrogate", ErrInvalid)
						}
						low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
						if err != nil || low < 0xdc00 || low > 0xdfff {
							return fmt.Errorf("%w: unpaired Unicode surrogate", ErrInvalid)
						}
						i += 6
					} else if code >= 0xdc00 && code <= 0xdfff {
						return fmt.Errorf("%w: unpaired Unicode surrogate", ErrInvalid)
					}
				}
			case '"':
				quoted = false
			}
			continue
		}
		switch char {
		case '"':
			quoted = true
		case '[', '{':
			depth++
			if depth > 100 {
				return fmt.Errorf("%w: state exceeds maximum depth of 100", ErrInvalid)
			}
		case ']', '}':
			depth--
		}
	}
	return nil
}

// AppendStateJSON preserves number lexemes. Strings concatenate and arrays
// append their elements; other JSON types have no implicit append operation.
func AppendStateJSON(current, suffix []byte) ([]byte, error) {
	for _, value := range [][]byte{current, suffix} {
		if err := ValidateStateJSON(value); err != nil {
			return nil, err
		}
	}
	current, suffix = bytes.TrimSpace(current), bytes.TrimSpace(suffix)
	var result []byte
	switch {
	case current[0] == '[' && suffix[0] == '[':
		left, right := bytes.TrimSpace(current[1:len(current)-1]), bytes.TrimSpace(suffix[1:len(suffix)-1])
		size := len(left) + len(right) + 2
		if len(left) > 0 && len(right) > 0 {
			size++
		}
		if size > MaxStateValueBytes {
			return nil, fmt.Errorf("%w: appended state exceeds 64 MiB", ErrInvalid)
		}
		result = make([]byte, 0, size)
		result = append(result, '[')
		result = append(result, left...)
		if len(left) > 0 && len(right) > 0 {
			result = append(result, ',')
		}
		result = append(result, right...)
		result = append(result, ']')
	case current[0] == '"' && suffix[0] == '"':
		size := len(current) + len(suffix) - 2
		if size > MaxStateValueBytes {
			return nil, fmt.Errorf("%w: appended state exceeds 64 MiB", ErrInvalid)
		}
		result = make([]byte, 0, size)
		result = append(result, current[:len(current)-1]...)
		result = append(result, suffix[1:]...)
	default:
		return nil, fmt.Errorf("%w: append requires two strings or two arrays", ErrInvalid)
	}
	if err := ValidateStateJSON(result); err != nil {
		return nil, err
	}
	return result, nil
}
