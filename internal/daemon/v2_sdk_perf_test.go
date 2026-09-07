//go:build integration && unix

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

// These routes exist only in the opt-in isolated fixture frontend, never in a
// production daemon. Keep timing out of protocol payloads: probes follow normal
// durable event polling, SDK projection, and application rendering.
func registerSDKPerformanceProbes(mux *http.ServeMux, store *session.Store, rootID string) {
	const limit = 128
	origin := time.Now()
	clock := func() float64 { return float64(time.Since(origin)) / float64(time.Millisecond) }
	var count atomic.Uint64
	mux.HandleFunc("GET /control/performance/clock", func(w http.ResponseWriter, _ *http.Request) {
		stamp := clock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]float64{"monotonic_ms": stamp})
	})
	mux.HandleFunc("POST /control/performance/commit", func(w http.ResponseWriter, r *http.Request) {
		index := count.Add(1)
		if index > limit {
			http.Error(w, "performance probe limit reached", http.StatusTooManyRequests)
			return
		}
		marker := fmt.Sprintf(" commit-probe-%03d", index)
		payload, err := json.Marshal(StreamEvent{Text: marker})
		if err != nil {
			http.Error(w, "cannot encode performance probe", http.StatusInternalServerError)
			return
		}
		seq, err := store.AppendRootEvent(r.Context(), rootID, "stream.text", session.RuntimePayload{
			Data: payload, MediaType: "application/json", Source: "stream.text",
		})
		// AppendRootEvent returns only after tx.Commit. This is the immediate
		// post-return observation, not event creation time or an RPC receipt.
		committed := clock()
		if err != nil {
			http.Error(w, "cannot commit performance probe", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Sequence string  `json:"sequence"`
			Marker   string  `json:"marker"`
			Millis   float64 `json:"committed_ms"`
		}{strconv.FormatInt(seq, 10), marker, committed})
	})
}

func TestSDKPerformanceProbesMeasureCommittedEventsAndBoundConcurrentRequests(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	mux := http.NewServeMux()
	registerSDKPerformanceProbes(mux, store, rootID)
	clock := func() float64 {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("GET", "/control/performance/clock", nil))
		var stamp struct {
			Millis float64 `json:"monotonic_ms"`
		}
		if response.Code != http.StatusOK {
			t.Fatalf("clock status = %d", response.Code)
		}
		if err := json.Unmarshal(response.Body.Bytes(), &stamp); err != nil {
			t.Fatal(err)
		}
		return stamp.Millis
	}
	before := clock()
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("POST", "/control/performance/commit", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("probe status = %d: %s", response.Code, response.Body.String())
	}
	var probe struct {
		Sequence string  `json:"sequence"`
		Marker   string  `json:"marker"`
		Millis   float64 `json:"committed_ms"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &probe); err != nil {
		t.Fatal(err)
	}
	if after := clock(); probe.Millis < before || probe.Millis > after {
		t.Fatalf("commit observation %f outside clock bracket [%f,%f]", probe.Millis, before, after)
	}
	events, _, err := store.ReplayEvents(t.Context(), rootID, 0, 128)
	if err != nil || len(events) != 1 || strconv.FormatInt(events[0].Seq, 10) != probe.Sequence || !strings.Contains(string(events[0].Payload.Inline), probe.Marker) {
		t.Fatalf("probe response did not identify an already committed event: %+v, %v", events, err)
	}
	var successes, limited atomic.Int32
	var workers sync.WaitGroup
	for range 160 {
		workers.Go(func() {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest("POST", "/control/performance/commit", nil))
			switch response.Code {
			case http.StatusOK:
				successes.Add(1)
			case http.StatusTooManyRequests:
				limited.Add(1)
			default:
				t.Errorf("concurrent probe status = %d: %s", response.Code, response.Body.String())
			}
		})
	}
	workers.Wait()
	if successes.Load() != 127 || limited.Load() != 33 {
		t.Fatalf("successes=%d limited=%d; want 127 and 33 after the first probe", successes.Load(), limited.Load())
	}
	events, _, err = store.ReplayEvents(t.Context(), rootID, 0, 128)
	if err != nil || len(events) != 128 {
		t.Fatalf("bounded probe events = %d: %v", len(events), err)
	}
}

func streamSDKPerformance(ctx context.Context, root *Session) {
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for index := range 300 {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
				kind: "stream.text", event: StreamEvent{Text: fmt.Sprintf(" delta-%03d", index)},
			}})
		}
	}
}

// The browser performance script opts into this seed before the fixture starts
// serving. Use normal store commits so pagination and content references exercise
// production storage paths, without adding a public test endpoint.
func seedSDKPerformanceHistory(t *testing.T, store *session.Store, rootID, cwd string) {
	t.Helper()
	if _, err := store.EnsureAuthority(t.Context(), rootID); err != nil {
		t.Fatal(err)
	}
	messages := make([]llm.Message, 10_000)
	for index := range messages {
		role := "assistant"
		if index%2 == 0 {
			role = "user"
		}
		messages[index] = llm.Message{
			Role: role, Authored: role == "user",
			Content: fmt.Sprintf("Root message %05d. **Retained history** with selectable text.\n\n%s", index+1, strings.Repeat("Bounded history stays on the execution host. ", 8)),
		}
	}
	// A large body near the latest page must remain an explicit content reference.
	messages[9_990] = llm.Message{Role: "tool", Name: "read", ToolCallID: "large-output", Content: strings.Repeat("Large tool output stays on the host.\n", 40_000)}
	if err := store.Save(rootID, 0, messages, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	for index := range 100 {
		agentID := fmt.Sprintf("perf-child-%03d", index)
		_, err := store.AdmitAgent(t.Context(), session.AgentAdmission{
			RootID: rootID, ParentAgentID: rootID, ChildAgentID: agentID,
			Name: agentID, Model: "model", Provider: "provider", CWD: cwd,
			Prompt: session.RuntimePayload{Data: []byte("seed retained child")},
		})
		if err != nil {
			t.Fatal(err)
		}
		turnID := agentID + "-seed"
		started, err := store.StartAgentTurn(t.Context(), rootID, agentID, turnID)
		if err != nil {
			t.Fatal(err)
		}
		if len(started.Items) != 1 {
			t.Fatalf("unexpected seeded child inbox: %+v", started)
		}
		childMessages := make([]llm.Message, 100)
		for messageIndex := range childMessages {
			childMessages[messageIndex] = llm.Message{Role: "assistant", Content: fmt.Sprintf("%s message %03d. Child-only transcript stays separate from the root.", agentID, messageIndex+1)}
		}
		if err := store.FinishAgentTurn(t.Context(), rootID, agentID, session.AgentTurnCommit{
			TurnID: turnID, Status: "succeeded", AcknowledgedInbox: []int64{started.Items[0].Seq}, Messages: childMessages,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.TerminalizeSubtree(t.Context(), rootID, rootID, agentID, "stopped"); err != nil {
			t.Fatal(err)
		}
	}
}
