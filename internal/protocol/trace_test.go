package protocol

import (
	"encoding/json"
	"testing"
)

func TestTraceContractsExactCountersTypedAttributesAndBounds(t *testing.T) {
	for _, value := range []string{`{"key":"counter","text":null,"count":"9007199254740993","flag":null}`, `{"key":"text","text":"model","count":null,"flag":null}`, `{"key":"flag","text":null,"count":null,"flag":false}`} {
		if err := validateTraceAttribute(value); err != nil {
			t.Fatal(value, err)
		}
	}
	for _, value := range []string{`{"key":"x","text":null,"count":9007199254740993,"flag":null}`, `{"key":"x","text":null,"count":null,"flag":null}`, `{"key":"x","text":"yes","count":null,"flag":true}`} {
		if err := validateTraceAttribute(value); err == nil {
			t.Fatal("accepted", value)
		}
	}
	for _, value := range []string{`{"root_id":"root","after":"0","expected_revision":null,"trace_id":"bad","roots_only":false,"limit":1,"max_bytes":4096}`, `{"root_id":"root","after":"0","expected_revision":null,"trace_id":"","roots_only":false,"limit":2049,"max_bytes":4096}`} {
		if err := Validate("TracePageParams", json.RawMessage(value)); err == nil {
			t.Fatal("accepted", value)
		}
	}
}

func validateTraceAttribute(value string) error {
	return Validate("TracePageResult", []byte(`{"observed_at_ns":"1790600000000001000","revision":"1","next":"1","has_more":false,"items":[{"sequence":"1","root_id":"root","session_id":"root","turn_id":"turn","source_kind":"turn","source_id":"turn","span_id":"0123456789abcdef","span":{"trace_id":"0123456789abcdef0123456789abcdef","parent_span_id":null,"kind":"agent","name":"turn.prompt","state":"running","start_ns":"1790600000000000000","end_ns":null,"attributes":[`+value+`]}}]}`))
}
