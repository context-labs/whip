package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"

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

func TestV2LargeStreamEventsPreserveCallIdentity(t *testing.T) {
	fixture := newV2Fixture(t, &fakeRunner{})
	root := &Session{store: fixture.store, meta: session.Meta{ID: fixture.rootID}, supervisor: newSupervisor()}
	defer root.supervisor.cancel()
	for _, kind := range []string{"stream.tool.call", "stream.tool.started", "stream.tool.completed"} {
		event := StreamEvent{AgentID: "child", TurnID: "child:turn:1", ID: "call-1", Name: "rlm_exec"}
		if kind == "stream.tool.completed" {
			event.Result = strings.Repeat("result", 2000)
		} else {
			event.Args = `{"code":"` + strings.Repeat("x", 10000) + `"}`
		}
		if err := root.recordStreamEvent(&streamEnvelope{kind: kind, event: event}); err != nil {
			t.Fatal(err)
		}
	}
	for _, transport := range []string{"unix", "websocket"} {
		client := fixture.dial(transport, "large-stream-"+transport)
		var replay protocol.ReplayResult
		if err := client.Call(t.Context(), "events.replay", protocol.ReplayParams{RootID: fixture.rootID, Limit: 128}, &replay); err != nil {
			t.Fatal(err)
		}
		seen := 0
		for _, event := range replay.Events {
			if !strings.HasPrefix(event.Kind, "stream.tool.") {
				continue
			}
			seen++
			validateActualEvent(t, event.Kind, event.Payload)
			var payload protocol.ContentEventPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if len(event.Payload) > session.InlineValueLimit || !payload.Truncated || payload.AgentID != "child" || payload.TurnID != "child:turn:1" || payload.ID != "call-1" || payload.Name != "rlm_exec" {
				t.Fatalf("lost stream identity: %s", event.Payload)
			}
			body, _, err := fixture.store.ReadContent(t.Context(), payload.Content.ReferenceID, fixture.rootID, "", 0, session.MaxContentRead)
			if err != nil || len(body) < 10000 {
				t.Fatalf("full event was not retained: %d bytes, %v", len(body), err)
			}
		}
		if seen != 3 {
			t.Fatalf("got %d tool events", seen)
		}
	}
}
