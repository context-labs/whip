package trace

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestSplitOTLPBoundsOrderExactNumbersAndNoPartialResult(t *testing.T) {
	encode := func(spans string) []byte {
		return []byte(`{"resourceSpans":[{"resource":{"attributes":[]},"scopeSpans":[{"scope":{"name":"whip.runtime"},"spans":[` + spans + `]}]}]}`)
	}
	first := `{"spanId":"one","name":"first","value":9007199254740993}`
	second := `{"spanId":"two","name":"other","value":9007199254740994}`
	limit := len(encode(first))
	batches, err := SplitOTLP(encode(first+","+second), limit)
	if err != nil || len(batches) != 2 {
		t.Fatal(len(batches), err)
	}
	for index, want := range []string{first, second} {
		if len(batches[index]) != limit || !json.Valid(batches[index]) || !strings.Contains(string(batches[index]), want) {
			t.Fatal(string(batches[index]))
		}
	}
	for _, size := range []int{-1, 0, 1, limit - 1} {
		if batches, err := SplitOTLP(encode(first), size); err == nil || len(batches) != 0 {
			t.Fatal(size, batches, err)
		}
	}
	if batches, err := SplitOTLP(encode(first+`,{"name":"`+strings.Repeat("x", limit)+`"}`), limit); err == nil || len(batches) != 0 {
		t.Fatal("partial batches escaped", batches, err)
	}
	empty := encode("")
	if batches, err := SplitOTLP(append(empty, ' '), len(empty)); err != nil || len(batches) != 1 || len(batches[0]) != len(empty) {
		t.Fatal(batches, err)
	}
	for _, data := range [][]byte{[]byte("not json"), []byte(`{"resourceSpans":[]}`), []byte(strings.Repeat(" ", session.MaxContentBytes+1)), encode(strings.Repeat(`{},`, 4096) + `{}`)} {
		if _, err := SplitOTLP(data, limit); err == nil {
			t.Fatal("accepted invalid/unbounded export")
		}
	}
}
