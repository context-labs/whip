package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestQueuePromotionAtRealModelBoundary(t *testing.T) {
	for _, engine := range []string{"starlark", "quickjs"} {
		t.Run(engine, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if calls.Add(1) == 1 {
					close(entered)
					select {
					case <-release:
					case <-request.Context().Done():
						return
					}
				}
				streamText(w, "done")
			}))
			defer server.Close()
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			store, root, _ := openRecursiveRuntime(t, llm.New(server.URL, "fixture"), 2, engine)
			initial, err := root.Submit(t.Context(), "initial")
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-initial.Done():
				t.Fatalf("initial turn ended before model: %+v", waitReceipt(t, initial))
			case <-time.After(15 * time.Second):
				t.Fatal("model did not start")
			}
			var queued []*Receipt
			for _, text := range []string{"A", "B", "C"} {
				_, receipt, err := root.AdmitCommand(t.Context(), session.CommandAdmission{ClientID: "queue-boundary", CommandID: text, Kind: "submit", Operation: "submit", RequestDigest: text, Payload: session.RuntimePayload{Data: []byte(text)}})
				if err != nil {
					t.Fatal(err)
				}
				queued = append(queued, receipt)
			}
			items, err := store.LoadQueuedInbox(t.Context(), root.ID(), root.ID(), 0, 10)
			if err != nil || len(items) != 3 {
				t.Fatalf("queue: %+v %v", items, err)
			}
			turn, err := store.RunningTurnID(t.Context(), root.ID(), root.ID())
			if err != nil {
				t.Fatal(err)
			}
			if result, err := store.ControlInbox(t.Context(), root.ID(), root.ID(), items[1].Seq, turn, false, nil); err != nil || result.Status != "steering" {
				t.Fatalf("steer: %+v %v", result, err)
			}
			unblock()
			for _, receipt := range append([]*Receipt{initial}, queued...) {
				if result := waitReceipt(t, receipt); result.Err != nil {
					t.Fatal(result.Err)
				}
			}
			var inputs []string
			for _, message := range store.RawMessages(root.ID()) {
				if message.Role == "user" && message.Authored {
					inputs = append(inputs, message.Content)
				}
			}
			if len(inputs) != 4 || inputs[0] != "initial" || inputs[1] != "B" || inputs[2] != "A" || inputs[3] != "C" || calls.Load() != 4 {
				t.Fatalf("delivered inputs: %v, model calls: %d", inputs, calls.Load())
			}
		})
	}
}

func TestQueueControlsThroughRuntimeCommand(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "runtime.db"))
	rootID := createRoot(t, store)
	started := make(chan struct{}, 1)
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{turn: func(ctx context.Context, _ string, _ bool) (string, error) {
			started <- struct{}{}
			<-ctx.Done()
			return "", ctx.Err()
		}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	client := runtimeUnixClient(t, value)
	admit := func(id string) int64 {
		t.Helper()
		result, err := root.AcceptCommand(t.Context(), session.CommandAdmission{ClientID: "fixture-client", CommandID: id, Kind: "submit", Operation: "submit", RequestDigest: id, Payload: session.RuntimePayload{Data: []byte(id)}})
		if err != nil {
			t.Fatal(err)
		}
		return result.Command.IngressSeq
	}
	admit("running")
	<-started
	steer := admit("steer-me")
	remove := admit("remove-me")
	turn, _ := store.RunningTurnID(t.Context(), rootID, rootID)
	control := func(id, op string, seq int64, target string) session.InboxControlResult {
		t.Helper()
		payload, _ := json.Marshal(struct {
			ID   string `json:"id"`
			Seq  int64  `json:"inbox_seq,string"`
			Turn string `json:"turn_id,omitempty"`
		}{rootID, seq, target})
		got, err := client.Command(t.Context(), CommandParams{Scope: "root", RootID: rootID, CommandID: id, Operation: op, Payload: payload})
		if err != nil || got.Status != "succeeded" {
			t.Fatalf("command: %+v %v", got, err)
		}
		var result session.InboxControlResult
		if err := json.Unmarshal(got.Result, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	for range 2 {
		if got := control("promote", "inbox.steer", steer, turn); got.Status != "steering" {
			t.Fatal(got)
		}
		if got := control("remove", "inbox.remove", remove, ""); got.Status != "removed" {
			t.Fatal(got)
		}
	}
	items, err := root.ClaimSteers(t.Context(), rootID, turn)
	if err != nil || len(items) != 1 || items[0].Seq != steer {
		t.Fatalf("delivery: %+v %v", items, err)
	}
	if got := control("too-late", "inbox.remove", steer, ""); got.Status != "already_started" {
		t.Fatal(got)
	}
	running, _ := store.LoadCommand(t.Context(), "fixture-client", "running")
	if running.Status != "running" {
		t.Fatalf("queue control stopped turn: %+v", running)
	}
}
