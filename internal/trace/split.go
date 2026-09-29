package trace

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

// SplitOTLP preserves all span fields, JSON numbers and resource/scope metadata. It accepts
// only this encoder's single-resource, single-scope bounded export. A span that
// cannot fit rejects the complete split; callers never send a partial prefix.
func SplitOTLP(data []byte, maxBytes int) ([][]byte, error) {
	if maxBytes <= 0 || len(data) > session.MaxContentBytes {
		return nil, errors.New("OTLP export or batch limit exceeds bounds")
	}
	var document struct {
		ResourceSpans []struct {
			Resource   json.RawMessage `json:"resource"`
			ScopeSpans []struct {
				Scope json.RawMessage   `json:"scope"`
				Spans []json.RawMessage `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if len(document.ResourceSpans) != 1 || len(document.ResourceSpans[0].ScopeSpans) != 1 {
		return nil, errors.New("OTLP export requires one resource and scope")
	}
	spans := document.ResourceSpans[0].ScopeSpans[0].Spans
	if len(spans) > 4096 {
		return nil, errors.New("OTLP export exceeds 4096 spans")
	}
	if len(data) <= maxBytes {
		return [][]byte{data}, nil
	}
	encode := func(batch []json.RawMessage) ([]byte, error) {
		document.ResourceSpans[0].ScopeSpans[0].Spans = batch
		return json.Marshal(document)
	}
	empty, err := encode([]json.RawMessage{})
	if err != nil {
		return nil, err
	}
	overhead := len(empty)
	if overhead > maxBytes {
		return nil, fmt.Errorf("OTLP resource and scope exceed batch limit %d", maxBytes)
	}
	if len(spans) == 0 {
		return [][]byte{empty}, nil
	}
	var batches [][]byte
	batch, size := []json.RawMessage{}, overhead
	for index, span := range spans {
		// Compact whitespace exactly as the enclosing json.Marshal does; otherwise
		// an exact-size raw span could be rejected because of formatting alone.
		raw, err := json.Marshal(span)
		if err != nil {
			return nil, err
		}
		if len(raw) > maxBytes-overhead {
			return nil, fmt.Errorf("OTLP span %d cannot fit in batch limit %d", index, maxBytes)
		}
		separator := min(len(batch), 1)
		if len(raw) > maxBytes-size-separator {
			body, err := encode(batch)
			if err != nil {
				return nil, err
			}
			batches = append(batches, body)
			batch, size, separator = []json.RawMessage{}, overhead, 0
		}
		batch = append(batch, raw)
		size += len(raw) + separator
	}
	body, err := encode(batch)
	if err != nil {
		return nil, err
	}
	return append(batches, body), nil
}
