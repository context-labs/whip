package daemon

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestProtocolCodecRejectsInvalidAndOversizedValues(t *testing.T) {
	if _, err := requestDigest("root", "r", "submit", json.RawMessage(`{`)); err == nil {
		t.Fatal("invalid request payload was digested")
	}
	if _, err := marshalFrame(rpcMessage{Result: strings.Repeat("x", MaxFrameSize)}); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversized marshal = %v", err)
	}
	for _, frame := range [][]byte{nil, []byte(`{`), []byte(`{"jsonrpc":"1.0"}`)} {
		if _, err := decodeFrame(frame); err == nil {
			t.Fatalf("invalid frame %q decoded", frame)
		}
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
