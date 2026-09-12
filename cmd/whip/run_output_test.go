package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/daemon"
)

func TestRunJSONOutputPreservesToolAndPendingEvents(t *testing.T) {
	var output bytes.Buffer
	run := &runOutput{enc: json.NewEncoder(&output)}
	for _, event := range []daemon.ProtocolEvent{
		{Kind: "stream.text", Payload: json.RawMessage(`{"text":"first"}`)},
		{Kind: "stream.reasoning", Payload: json.RawMessage(`{"text":"considering"}`)},
		{Kind: "stream.tool.started", Payload: json.RawMessage(`{"name":"read_file","args":"{\"path\":\"x.txt\"}"}`)},
		{Kind: "stream.tool.completed", Payload: json.RawMessage(`{"name":"read_file","result":"contents"}`)},
		{Kind: "permission.pending", Payload: json.RawMessage(`{"path":"outside.txt"}`)},
		{Kind: "question.pending", Payload: json.RawMessage(`{"question":"Continue?"}`)},
		{Kind: "stream.text", Payload: json.RawMessage(`invalid`)},
		{Kind: "unrecognized", Payload: json.RawMessage(`{}`)},
	} {
		run.event(event)
	}
	run.finish("", errors.New("provider unavailable"))
	want := []map[string]string{
		{"type": "text", "delta": "first"},
		{"type": "reasoning", "delta": "considering"},
		{"type": "tool_start", "name": "read_file", "args": `{"path":"x.txt"}`},
		{"type": "tool_end", "name": "read_file", "result": "contents"},
		{"type": "permission_pending", "detail": `{"path":"outside.txt"}`},
		{"type": "question_pending", "detail": `{"question":"Continue?"}`},
		{"type": "error", "error": "provider unavailable"},
	}
	var got []map[string]string
	for line := range strings.SplitSeq(strings.TrimSpace(output.String()), "\n") {
		var event map[string]string
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid JSON event: %v", err)
		}
		got = append(got, event)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stream contract changed:\ngot  %#v\nwant %#v", got, want)
	}
}

func TestRunTextOutputReportsPendingInputUnlessQuiet(t *testing.T) {
	for _, quiet := range []bool{false, true} {
		var stdout string
		stderr := captureStderr(t, func() {
			stdout = captureStdout(t, func() {
				output := newRunOutput("text", quiet)
				output.event(daemon.ProtocolEvent{Kind: "stream.text", Payload: json.RawMessage(`{"text":"answer"}`)})
				output.event(daemon.ProtocolEvent{Kind: "stream.tool.started", Payload: json.RawMessage(`{"name":"read_file"}`)})
				output.event(daemon.ProtocolEvent{Kind: "permission.pending", Payload: json.RawMessage(`{"path":"outside.txt"}`)})
				output.event(daemon.ProtocolEvent{Kind: "question.pending", Payload: json.RawMessage(`{"question":"Continue?"}`)})
				output.finish("answer", nil)
			})
		})
		if stdout != "answer\n" {
			t.Errorf("quiet=%t polluted answer stream: %q", quiet, stdout)
		}
		if quiet {
			if stderr != "" {
				t.Errorf("quiet mode emitted notices: %s", stderr)
			}
			continue
		}
		for _, want := range []string{"read_file", "permission pending:", "outside.txt", "question pending:", "Continue?"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("missing headless notice %q: %s", want, stderr)
			}
		}
	}
}
