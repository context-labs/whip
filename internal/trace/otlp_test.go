package trace

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func exportRow() session.TraceRow {
	return session.TraceRow{Sequence: 9007199254740993, RootID: "root", SessionID: "child", TurnID: "turn", SourceKind: "attempt", SourceID: "attempt", SpanID: session.TraceSpanID("attempt", "attempt"), Span: &session.TraceSpan{TraceID: session.TraceID("turn"), Kind: "llm", Name: "turn model", State: "dispatched", StartNS: 1790600000000000000, Attributes: []session.TraceAttribute{{Key: "whip.cost.nano_usd", Count: new(int64(9007199254740993))}, {Key: "whip.input.body_available", Flag: new(false)}}}}
}

func TestOTLPExactIntegersOpenSpanConventionAndNoInventedInput(t *testing.T) {
	row := exportRow()
	out := NewExport("root", 9007199254740993)
	if err := out.Add(row, nil, nil); err != nil {
		t.Fatal(err)
	}
	body := out.Bytes()
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"intValue":"9007199254740993"`) || !strings.Contains(string(body), `"startTimeUnixNano":"1790600000000000000"`) || !strings.Contains(string(body), `"endTimeUnixNano":"1790600000000000000"`) || !strings.Contains(string(body), `"status":{"code":0}`) {
		t.Fatal(string(body))
	}
	if strings.Contains(string(body), "gen_ai.input.messages") {
		t.Fatal("invented historical input")
	}
	if !strings.Contains(string(body), `"key":"whip.span.in_progress","value":{"boolValue":true}`) {
		t.Fatal(string(body))
	}
	row.Span.State = "succeeded"
	row.Span.EndNS = new(row.Span.StartNS + 1000)
	out = NewExport("root", 7)
	if err := out.Add(row, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out.Bytes()), `"status":{"code":1}`) {
		t.Fatal(string(out.Bytes()))
	}
	if spans, traces := out.Counts(); spans != 1 || traces != 1 {
		t.Fatal(spans, traces)
	}
	if err := out.Add(session.TraceRow{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if spans, _ := out.Counts(); spans != 1 {
		t.Fatal("tombstone exported as span")
	}
}

func TestOTLPBoundsRejectWithoutPartialAppend(t *testing.T) {
	out := NewExport("root", 1)
	row := exportRow()
	row.Span.Attributes = append(row.Span.Attributes, session.TraceAttribute{Key: "preview", Text: new(strings.Repeat("界", 2048))})
	count := 0
	for {
		before := len(out.Bytes())
		err := out.Add(row, nil, nil)
		if errors.Is(err, ErrLimit) {
			if len(out.Bytes()) != before {
				t.Fatal("partial append")
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count < 1 || len(out.Bytes()) > session.MaxContentBytes || !json.Valid(out.Bytes()) {
		t.Fatal(count, len(out.Bytes()))
	}
}
