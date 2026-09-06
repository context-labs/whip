package daemon

import (
	"encoding/json"
	"fmt"
	"github.com/context-labs/whip/internal/session"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
)

func validateActualEvent(t *testing.T, kind string, raw json.RawMessage) {
	t.Helper()
	typ, ok := protocol.EventPayloads()[kind]
	if !ok {
		t.Fatalf("unregistered emitted kind %s", kind)
	}
	var reference protocol.ContentEventPayload
	if json.Unmarshal(raw, &reference) == nil && reference.Truncated {
		typ = reflect.TypeFor[protocol.ContentEventPayload]()
	}
	schema, err := protocol.SchemaFor(typ)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	var payload any
	if err = json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if err = resolved.Validate(payload); err != nil {
		t.Fatalf("%s actual payload %s: %v", kind, raw, err)
	}
}
func TestV2ActualAgentEventsMatchSchemas(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"typed response\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":3,\"total_tokens\":15}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()
	store, root, _ := openRecursiveRuntime(t, llm.New(provider.URL, "key"), 1)
	receipt, err := root.Submit(t.Context(), "emit usage")
	if err != nil {
		t.Fatal(err)
	}
	if result := waitReceipt(t, receipt); result.Err != nil {
		t.Fatal(result.Err)
	}
	events, _, err := store.ReplayEvents(t.Context(), root.ID(), 0, 128)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, event := range events {
		validateActualEvent(t, event.Kind, event.Payload.Inline)
		seen[event.Kind] = true
		if event.Kind == "stream.usage" {
			var usage protocol.StreamEvent
			if err = json.Unmarshal(event.Payload.Inline, &usage); err != nil || usage.Usage == nil || usage.Usage.Used != 12 {
				t.Fatalf("usage payload %s %v", event.Payload.Inline, err)
			}
		}
	}
	for _, kind := range []string{"stream.usage", "stream.text", "turn.started", "turn.succeeded"} {
		if !seen[kind] {
			t.Fatalf("missing actual event %s", kind)
		}
	}
}

func TestV2ReferencedEventPayloadMatchesSchema(t *testing.T) {
	fixture := newV2Fixture(t, &fakeRunner{})
	raw, _ := json.Marshal(protocol.StreamEvent{Text: strings.Repeat("large event", 10000)})
	if _, err := fixture.store.AppendRootEvent(t.Context(), fixture.rootID, "stream.text", session.RuntimePayload{Data: raw, MediaType: "application/json"}); err != nil {
		t.Fatal(err)
	}
	for _, transport := range []string{"unix", "websocket"} {
		client := fixture.dial(transport, "event-schema-"+transport)
		var replay protocol.ReplayResult
		if err := client.Call(t.Context(), "events.replay", protocol.ReplayParams{RootID: fixture.rootID, Limit: 128}, &replay); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, event := range replay.Events {
			validateActualEvent(t, event.Kind, event.Payload)
			if event.Kind == "stream.text" {
				var value protocol.ContentEventPayload
				_ = json.Unmarshal(event.Payload, &value)
				if !value.Truncated || value.Content.ReferenceID == "" {
					t.Fatal("large event missing content variant")
				}
				found = true
			}
		}
		if !found {
			t.Fatal("missing referenced event")
		}
	}
}
