package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

// scriptedModel answers each provider request with the next scripted text and
// records what it was asked.
func scriptedModel(t *testing.T, responses ...string) (*llm.Client, func() []llm.Request) {
	t.Helper()
	var mu sync.Mutex
	var requests []llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		index := len(requests)
		requests = append(requests, request)
		mu.Unlock()
		text := "no script"
		if index < len(responses) {
			text = responses[index]
		}
		streamText(w, text)
	}))
	t.Cleanup(server.Close)
	client := llm.New(server.URL, "key")
	client.MaxRetries = 0
	return client, func() []llm.Request { mu.Lock(); defer mu.Unlock(); return append([]llm.Request(nil), requests...) }
}

// A definition's output contract is stated in the prompt, checked on the final
// message, corrected once through an ephemeral notice, and returned beside the
// text; a second mismatch fails the turn.
func TestOutputContractValidatesTheFinalMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIP_HOME", t.TempDir())
	client, requests := scriptedModel(t,
		"Here you go: the summary is fine.",                  // turn 1, attempt 1: not JSON
		"```json\n{\"summary\": \"ticket 42 is open\"}\n```", // turn 1, attempt 2: valid, fenced
		"{\"summary\": 42}",                                  // turn 2, attempt 1: wrong type
		"still not it",                                       // turn 2, attempt 2: fails the turn
	)
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	definition := agentdef.Coding()
	definition.ID = "contract"
	definition.Output = json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"],"additionalProperties":false}`)
	document, err := agentdef.Encode(definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registerDefinition(t.Context(), store, document, "contract-test"); err != nil {
		t.Fatal(err)
	}
	rootID := createDefinitionRoot(t, store, "contract")
	_, root, _ := openPromptRuntime(t, store, rootID, client)
	result := submitCommand(t, root, store, "sdk", "turn-1", "summarize ticket 42")
	if result.Status != "succeeded" {
		t.Fatalf("turn = %+v", result)
	}
	var text protocol.TextResult
	if err := json.Unmarshal(result.Outcome.Inline, &text); err != nil {
		t.Fatalf("outcome %q (status %s, ref %q): %v", result.Outcome.Inline, result.Status, result.Outcome.ReferenceID, err)
	}
	if text.Text != "```json\n{\"summary\": \"ticket 42 is open\"}\n```" || string(text.Output) != `{"summary":"ticket 42 is open"}` {
		t.Fatalf("submit result = %+v", text)
	}
	seen := requests()
	if len(seen) != 2 {
		t.Fatalf("model requests = %d, want the final message and one correction", len(seen))
	}
	if !strings.Contains(seen[0].Messages[0].Content, "Output contract: your final assistant message for this turn must be exactly one JSON value matching this schema") {
		t.Fatalf("system prompt lacks the contract:\n%s", seen[0].Messages[0].Content)
	}
	var corrected bool
	for _, message := range seen[1].Messages {
		if message.Role == "system" && strings.Contains(message.Content, "Output contract: your previous final message did not match the required schema") {
			corrected = true
		}
	}
	if !corrected {
		t.Fatalf("correction notice missing from the retry request: %+v", seen[1].Messages)
	}
	failed := submitCommand(t, root, store, "sdk", "turn-2", "summarize ticket 43")
	if failed.Status != "failed" || !strings.Contains(string(failed.Outcome.Inline), "output_invalid") {
		t.Fatalf("second mismatch did not fail the turn: %+v %s", failed, failed.Outcome.Inline)
	}
	if len(requests()) != 4 {
		t.Fatalf("model requests = %d, want two per turn", len(requests()))
	}
}

// submitCommand runs one root turn under a client command identity, the way
// the SDK's session.run does, and returns the settled command record.
func submitCommand(t *testing.T, root *Session, store *session.Store, clientID, commandID, text string) session.CommandRecord {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"text": text})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := requestDigest("root", root.ID(), "submit", raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.AcceptCommand(t.Context(), session.CommandAdmission{
		ClientID: clientID, CommandID: commandID, Kind: "submit", Operation: "submit", RequestDigest: digest,
		Payload: session.RuntimePayload{Data: []byte(text), MediaType: "text/plain", Source: "submit"},
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		record, err := store.LoadCommand(t.Context(), clientID, commandID)
		if err != nil {
			t.Fatal(err)
		}
		switch record.Status {
		case "succeeded", "failed", "cancelled", "interrupted":
			return record
		}
		if time.Now().After(deadline) {
			t.Fatalf("command %s did not settle: %+v", commandID, record)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
