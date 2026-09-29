package runclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestOutputReplacesPreviewWithExactCommitAndCanonicalToolBatch(t *testing.T) {
	var stdout, stderr bytes.Buffer
	o, err := NewOutput("json", false, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	page := client.Observation{Epoch: "epoch", Preview: &protocol.MessagePreview{AttemptID: "attempt", TurnID: "turn", MessageID: "answer", Text: "hel", Reasoning: "think", Calls: []protocol.CallPreview{{Name: "not_authority", Arguments: "{"}}}}
	if err := o.Observe(page, "turn"); err != nil {
		t.Fatal(err)
	}
	page.Preview.Text = "hello"
	if err := o.Observe(page, "turn"); err != nil {
		t.Fatal(err)
	}
	page.Preview = nil
	page.Messages = []protocol.Message{{ID: "answer", Role: "assistant", TurnID: new(protocol.ID("turn")), Parts: []protocol.Part{{Type: "text", Text: "hello world"}, {Type: "tool_call", Call: &protocol.ToolCall{ID: "call", Name: "repl", Arguments: json.RawMessage(`{"code":"print(1)"}`)}}}}, {ID: "result", Role: "tool", TurnID: new(protocol.ID("turn")), Parts: []protocol.Part{{Type: "tool_result", Result: &protocol.ToolResult{CallID: "call", Output: "1"}}}}, {ID: "final", Role: "assistant", TurnID: new(protocol.ID("turn")), Parts: []protocol.Part{{Type: "text", Text: "done"}}}}
	if err := o.Observe(page, "turn"); err != nil {
		t.Fatal(err)
	}
	if err := o.Finish(nil); err != nil {
		t.Fatal(err)
	}
	expected := []map[string]string{{"type": "text", "delta": "hel"}, {"type": "reasoning", "delta": "think"}, {"type": "text", "delta": "lo"}, {"type": "text", "delta": " world"}, {"type": "tool_start", "name": "repl", "args": `{"code":"print(1)"}`}, {"type": "tool_end", "name": "repl", "result": "1"}, {"type": "text", "delta": "done"}, {"type": "done", "text": "done"}}
	if got := events(t, stdout.String()); !reflect.DeepEqual(got, expected) {
		t.Fatalf("got %#v\nwant %#v", got, expected)
	}
	if stderr.Len() != 0 {
		t.Fatal(stderr.String())
	}
}

func TestOutputDiscardsOldAttemptAndEpochWithoutTreatingPreviewCallsAsTools(t *testing.T) {
	for _, transition := range []string{"attempt", "epoch", "replacement", "missing"} {
		t.Run(transition, func(t *testing.T) {
			var stdout bytes.Buffer
			o, _ := NewOutput("json", false, &stdout, &bytes.Buffer{})
			page := client.Observation{Epoch: "old", Preview: &protocol.MessagePreview{TurnID: "turn", AttemptID: "a", MessageID: "m", Text: "🙂x", Reasoning: "ephemeral"}}
			if err := o.Observe(page, "turn"); err != nil {
				t.Fatal(err)
			}
			switch transition {
			case "attempt":
				page.Preview.AttemptID = "b"
			case "epoch":
				page.Epoch = "new"
			case "replacement":
				page.Preview.Text = "new"
			case "missing":
				page.Preview = nil
			}
			if err := o.Observe(page, "turn"); err != nil {
				t.Fatal(err)
			}
			got := events(t, stdout.String())
			if len(got) < 3 || !reflect.DeepEqual(got[2], map[string]string{"type": "discard", "chars": "2"}) {
				t.Fatal(got)
			}
		})
	}
}

func TestOutputOnlyShowsCurrentTurnAndDefersPreviewUntilCaughtUp(t *testing.T) {
	var stdout bytes.Buffer
	o, _ := NewOutput("text", false, &stdout, &bytes.Buffer{})
	page := client.Observation{Cursor: client.ObservationCursor{After: 1}, Snapshot: protocol.HistorySnapshot{ThroughSequence: 2}, Epoch: "epoch", Messages: []protocol.Message{{Role: "assistant", TurnID: new(protocol.ID("previous")), Parts: []protocol.Part{{Type: "text", Text: "private earlier output"}}}}, Preview: &protocol.MessagePreview{TurnID: "turn", AttemptID: "a", MessageID: "m", Text: "future"}}
	if err := o.Observe(page, "turn"); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatal(stdout.String())
	}
	page.Cursor.After = 2
	if err := o.Observe(page, "turn"); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "future" {
		t.Fatal(stdout.String())
	}
}

func TestOutputDecisionNoticesAndWriteFailure(t *testing.T) {
	for _, quiet := range []bool{false, true} {
		var stdout, stderr bytes.Buffer
		o, _ := NewOutput("text", quiet, &stdout, &stderr)
		o.Event("text", map[string]string{"delta": "answer"})
		o.Event("permission_pending", map[string]string{"detail": "outside.txt"})
		o.Event("question_pending", map[string]string{"detail": "Continue?"})
		if err := o.Finish(nil); err != nil {
			t.Fatal(err)
		}
		if stdout.String() != "answer\n" || quiet && stderr.Len() != 0 || !quiet && !strings.Contains(stderr.String(), "Continue?") {
			t.Fatal(stdout.String(), stderr.String())
		}
	}
	broken := errors.New("closed pipe")
	o, _ := NewOutput("json", false, errorWriter{broken}, &bytes.Buffer{})
	o.Event("text", map[string]string{"delta": "answer"})
	if !errors.Is(o.Finish(nil), broken) {
		t.Fatal(o.Err())
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }
func events(t *testing.T, body string) []map[string]string {
	t.Helper()
	var result []map[string]string
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		var event map[string]string
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err, body)
		}
		result = append(result, event)
	}
	return result
}
