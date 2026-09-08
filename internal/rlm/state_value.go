package rlm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.starlark.net/starlark"
)

// stateToGo accepts JSON trees only. Unlike the generic tool adapter it must
// never stringify integers or silently coerce tuples and bytes.
func stateToGo(value starlark.Value) (any, error) {
	return stateJSONValue(value, make(map[starlark.Value]bool), 0)
}

func stateJSONValue(value starlark.Value, active map[starlark.Value]bool, depth int) (any, error) {
	if depth > 100 {
		return nil, errors.New("state value exceeds maximum depth of 100")
	}
	switch value := value.(type) {
	case starlark.NoneType:
		return nil, nil //nolint:nilnil // Starlark None is a valid JSON null, represented by a nil interface.
	case starlark.Bool:
		return bool(value), nil
	case starlark.String:
		if !utf8.ValidString(string(value)) {
			return nil, errors.New("state strings require valid UTF-8")
		}
		return string(value), nil
	case starlark.Int:
		return json.Number(value.String()), nil
	case starlark.Float:
		number := float64(value)
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, errors.New("state requires finite floats")
		}
		encoded := strconv.FormatFloat(number, 'g', -1, 64)
		if !strings.ContainsAny(encoded, ".eE") {
			encoded += ".0"
		}
		return json.Number(encoded), nil
	case *starlark.List:
		if active[value] {
			return nil, errors.New("state values cannot contain cycles")
		}
		active[value] = true
		defer delete(active, value)
		items := make([]any, value.Len())
		for i := range items {
			item, err := stateJSONValue(value.Index(i), active, depth+1)
			if err != nil {
				return nil, err
			}
			items[i] = item
		}
		return items, nil
	case *starlark.Dict:
		if active[value] {
			return nil, errors.New("state values cannot contain cycles")
		}
		active[value] = true
		defer delete(active, value)
		items := make(map[string]any, value.Len())
		for _, pair := range value.Items() {
			key, ok := pair[0].(starlark.String)
			if !ok || !utf8.ValidString(string(key)) {
				return nil, errors.New("state dictionaries require valid UTF-8 string keys")
			}
			item, err := stateJSONValue(pair[1], active, depth+1)
			if err != nil {
				return nil, err
			}
			items[string(key)] = item
		}
		return items, nil
	default:
		return nil, fmt.Errorf("state does not support %s", value.Type())
	}
}
