package session

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeValueWireRepresentations(t *testing.T) {
	for _, tc := range []struct{ media, body, field string }{{"application/json", `{"ok":true}`, "inline"}, {"text/plain", "café <x>", "text"}, {"application/octet-stream", string([]byte{0, 255, 1}), "binary"}} {
		t.Run(tc.field, func(t *testing.T) {
			original := RuntimeValue{Inline: []byte(tc.body), MediaType: tc.media}
			raw, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"`+tc.field+`":`) {
				t.Fatal(string(raw))
			}
			var restored RuntimeValue
			if err = json.Unmarshal(raw, &restored); err != nil || !bytes.Equal(restored.Inline, original.Inline) {
				t.Fatalf("roundtrip %s: %v", raw, err)
			}
		})
	}
	var invalid RuntimeValue
	if json.Unmarshal([]byte(`{"inline":{},"text":"ambiguous"}`), &invalid) == nil {
		t.Fatal("accepted ambiguous representation")
	}
}
