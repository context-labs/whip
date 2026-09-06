package session

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// RuntimeValueWire carries JSON as JSON and text as text. Only binary payloads
// use base64. This presentation encoding does not alter the stored byte values.
type RuntimeValueWire struct {
	Inline      json.RawMessage `json:"inline,omitempty"`
	Text        *string         `json:"text,omitempty"`
	Binary      []byte          `json:"binary,omitempty"`
	ReferenceID string          `json:"reference_id"`
	Digest      string          `json:"digest"`
	Size        int64           `json:"size,string"`
	MediaType   string          `json:"media_type"`
	Source      string          `json:"source"`
}

func (v RuntimeValue) MarshalJSON() ([]byte, error) {
	wire := RuntimeValueWire{ReferenceID: v.ReferenceID, Digest: v.Digest, Size: v.Size, MediaType: v.MediaType, Source: v.Source}
	if v.Inline != nil {
		media := strings.TrimSpace(strings.Split(v.MediaType, ";")[0])
		switch {
		case (media == "application/json" || strings.HasSuffix(media, "+json")) && json.Valid(v.Inline):
			wire.Inline = json.RawMessage(v.Inline)
		case strings.HasPrefix(media, "text/") && utf8.Valid(v.Inline):
			text := string(v.Inline)
			wire.Text = &text
		default:
			wire.Binary = v.Inline
		}
	}
	return json.Marshal(wire)
}
func (v *RuntimeValue) UnmarshalJSON(raw []byte) error {
	var wire RuntimeValueWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	count := 0
	if wire.Inline != nil {
		count++
	}
	if wire.Text != nil {
		count++
	}
	if wire.Binary != nil {
		count++
	}
	if count > 1 {
		return errors.New("runtime value has multiple inline representations")
	}
	*v = RuntimeValue{ReferenceID: wire.ReferenceID, Digest: wire.Digest, Size: wire.Size, MediaType: wire.MediaType, Source: wire.Source}
	switch {
	case wire.Inline != nil:
		v.Inline = wire.Inline
	case wire.Text != nil:
		v.Inline = []byte(*wire.Text)
	case wire.Binary != nil:
		v.Inline = wire.Binary
	}
	return nil
}
