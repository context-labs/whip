package llm

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMessageHistoryPreservesMultipartOrderAndMetadata(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "text_image_text", body: `{"role":"user","content":[{"type":"text","text":"before \\n image 界"},{"type":"image_url","image_url":{"url":"data:image/png;base64,YWJj"},"w":37,"h":29},{"type":"text","text":"after image 🙂"}],"authored":true,"sent_at":"2026-09-05T16:00:00Z"}`},
		{name: "consecutive_text", body: `{"role":"user","content":[{"type":"text","text":"first"},{"type":"text","text":"second"},{"type":"text","text":"third"}]}`},
		{name: "image_first", body: `{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,YWJj"}},{"type":"text","text":"after image"}]}`},
		{name: "empty_leading_text", body: `{"role":"user","content":[{"type":"text"},{"type":"image_url","image_url":{"url":"data:image/png;base64,YWJj"}},{"type":"text","text":"after image"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var message Message
			if err := json.Unmarshal([]byte(tc.body), &message); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			var got, want map[string]any
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.body), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("persisted multipart content changed order, text, or metadata:\n got %s\nwant %s", encoded, tc.body)
			}
			var restored Message
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(message, restored) {
				t.Fatalf("second persistence round trip changed message:\n got %+v\nwant %+v", restored, message)
			}
		})
	}
}

func TestMessageHistoryNeverSerializesRawSequence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		message Message
	}{
		{name: "text", message: Message{Role: "assistant", Content: "retained response", RawSequence: 813}},
		{name: "multipart", message: Message{Role: "user", Content: "before", Parts: []ContentPart{ImagePart("png", []byte("image")), {Type: "text", Text: "after"}}, RawSequence: 947}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := json.Marshal(tc.message)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.ToLower(string(encoded)), "sequence") {
				t.Fatalf("runtime provenance leaked into persisted/provider JSON: %s", encoded)
			}
			var restored Message
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			if restored.RawSequence != 0 {
				t.Fatalf("JSON invented a transcript sequence: %+v", restored)
			}
			want := tc.message
			want.RawSequence = 0
			if !reflect.DeepEqual(restored, want) {
				t.Fatalf("other message data changed: got %+v, want %+v", restored, want)
			}
		})
	}
}

func TestMessageHistoryUnmarshalResetsPreviouslyPopulatedFields(t *testing.T) {
	t.Parallel()
	stamp := time.Date(2026, time.September, 5, 16, 0, 0, 0, time.UTC)
	old := Message{
		Role: "user", Content: "stale content", Parts: []ContentPart{ImagePart("png", []byte("stale image"))},
		ToolCalls: []ToolCall{{ID: "stale call", Type: "function"}}, ToolCallID: "stale result", Name: "stale tool",
		Authored: true, SentAt: &stamp, Usage: &Usage{PromptTokens: 777}, Model: "stale model", RewoundFrom: "stale rewind", RawSequence: 999,
	}
	for _, tc := range []struct {
		name string
		body string
		want Message
	}{
		{name: "missing_content", body: `{"role":"assistant"}`, want: Message{Role: "assistant"}},
		{name: "null_content", body: `{"role":"assistant","content":null}`, want: Message{Role: "assistant"}},
		{name: "text_content", body: `{"role":"assistant","content":"fresh"}`, want: Message{Role: "assistant", Content: "fresh"}},
		{name: "parts_content", body: `{"role":"user","content":[{"type":"text","text":"fresh"},{"type":"text","text":"tail"}]}`, want: Message{Role: "user", Content: "fresh", Parts: []ContentPart{{Type: "text", Text: "tail"}}}},
		{name: "untrusted_raw_sequence", body: `{"role":"assistant","content":"fresh","raw_sequence":123,"RawSequence":456}`, want: Message{Role: "assistant", Content: "fresh"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := old
			if err := json.Unmarshal([]byte(tc.body), &message); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(message, tc.want) {
				t.Fatalf("reused message leaked stale fields: got %+v, want %+v", message, tc.want)
			}
		})
	}
}
