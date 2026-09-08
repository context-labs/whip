package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	sessionstore "github.com/context-labs/whip/internal/session"
)

func stateResult(value sessionstore.StateValue, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	result := map[string]any{"key": value.Key, "version": value.Version, "author_agent_id": value.AuthorAgentID}
	if value.Payload.ReferenceID != "" {
		result["handle"] = value.Payload.ReferenceID
		result["size"] = value.Payload.Size
		result["media_type"] = value.Payload.MediaType
		return result, nil
	}
	if !utf8.Valid(value.Payload.Inline) || !json.Valid(value.Payload.Inline) {
		return nil, fmt.Errorf("state %q is not valid JSON", value.Key)
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(value.Payload.Inline))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode structured state %q: %w", value.Key, err)
	}
	result["value"] = decoded
	return result, nil
}

func statePageInteger(arguments map[string]any, key string, fallback int64) (int64, error) {
	value, exists := arguments[key]
	if !exists {
		return fallback, nil
	}
	switch value := value.(type) {
	case json.Number:
		return strconv.ParseInt(string(value), 10, 64)
	case int:
		return int64(value), nil
	case int64:
		return value, nil
	case float64:
		if value >= 0 && value < 1<<63 && value == float64(int64(value)) {
			return int64(value), nil
		}
	}
	return 0, fmt.Errorf("%s must be an integer", key)
}

func (host *recursiveHost) statePage(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	limit, err := statePageInteger(arguments, "limit", 20)
	if err != nil || limit < 1 || limit > 100 {
		return nil, errors.New("state page limit must be between 1 and 100")
	}
	afterKey, validKey := stringArgument(arguments, "after_key")
	if _, exists := arguments["after_key"]; exists && !validKey {
		return nil, errors.New("after_key must be a string")
	}
	afterVersion, err := statePageInteger(arguments, "after_version", 0)
	if err != nil || afterVersion < 0 {
		return nil, errors.New("after_version must be a non-negative integer")
	}
	key, _ := stringArgument(arguments, "key")
	node := host.session
	values, err := routeControlValue(node.root, ctx, func(actorCtx context.Context) ([]sessionstore.StateValue, error) {
		if operation == "private_list" {
			return node.root.store.ListPrivateStatePage(actorCtx, node.root.meta.ID, node.id, afterKey, int(limit)+1)
		}
		return node.root.store.BlackboardHistoryPage(actorCtx, node.root.meta.ID, node.id, key, afterVersion, int(limit)+1)
	})
	if err != nil {
		return nil, err
	}
	items := make([]any, 0, min(len(values), int(limit)))
	result := map[string]any{"items": items, "next": nil, "truncated": false}
	for i, value := range values {
		if i >= int(limit) {
			break
		}
		item, err := stateResult(value, nil)
		if err != nil {
			return nil, err
		}
		candidate := append(items, item)
		next := map[string]any{"after_key": value.Key}
		if operation == "blackboard_history" {
			next = map[string]any{"after_version": value.Version}
		}
		result["items"], result["next"], result["truncated"] = candidate, next, true
		encoded, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		if len(encoded) > 64<<10 {
			if len(items) == 0 {
				return nil, errors.New("state record exceeds page response limit")
			}
			break
		}
		items = candidate
	}
	truncated := len(items) < len(values)
	result["items"], result["truncated"] = items, truncated
	result["next"] = nil
	if truncated && len(items) > 0 {
		last := values[len(items)-1]
		if operation == "private_list" {
			result["next"] = map[string]any{"after_key": last.Key}
		} else {
			result["next"] = map[string]any{"after_version": last.Version}
		}
	}
	return result, nil
}

// normalizeStateJSON keeps the public host boundary strict even for callers
// that do not originate in a Starlark worker.
func normalizeStateJSON(value any, depth int) (any, error) {
	if depth > 100 {
		return nil, errors.New("state value exceeds maximum depth of 100")
	}
	switch value := value.(type) {
	case nil, bool, int, int64:
		return value, nil
	case string:
		if !utf8.ValidString(value) {
			return nil, errors.New("state strings require valid UTF-8")
		}
		return value, nil
	case json.Number:
		if !json.Valid([]byte(value)) {
			return nil, errors.New("invalid state JSON number")
		}
		if strings.ContainsAny(string(value), ".eE") {
			number, err := value.Float64()
			if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
				return nil, errors.New("state requires finite floats")
			}
		}
		return value, nil
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, errors.New("state requires finite floats")
		}
		encoded := strconv.FormatFloat(value, 'g', -1, 64)
		if !strings.ContainsAny(encoded, ".eE") {
			encoded += ".0"
		}
		return json.Number(encoded), nil
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			converted, err := normalizeStateJSON(item, depth+1)
			if err != nil {
				return nil, err
			}
			result[i] = converted
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			if !utf8.ValidString(key) {
				return nil, errors.New("state dictionaries require valid UTF-8 keys")
			}
			converted, err := normalizeStateJSON(item, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = converted
		}
		return result, nil
	default:
		return nil, fmt.Errorf("state requires JSON values, got %T", value)
	}
}
