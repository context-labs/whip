package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

// In-flight counts rise while a tool runs and drain when it finishes, split
// by kind (subagent vs other).
func TestInFlightToolsTracking(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	ag.trackTool("subagent", 1)
	ag.trackTool("read", 1)
	ag.trackTool("bash", 1)
	if ag.subagentInflight.Load() != 1 || ag.otherInflight.Load() != 2 {
		t.Fatalf("counts = %d subagent / %d other, want 1/2", ag.subagentInflight.Load(), ag.otherInflight.Load())
	}
	ag.trackTool("read", -1)
	ag.trackTool("bash", -1)
	if ag.otherInflight.Load() != 0 {
		t.Fatalf("other should drain, got %d", ag.otherInflight.Load())
	}
	ag.trackTool("subagent", -1)
	if ag.subagentInflight.Load() != 0 {
		t.Fatalf("subagent should drain, got %d", ag.subagentInflight.Load())
	}
}

// WaitingOnSubagents is true only while a turn runs AND every in-flight tool
// is a subagent; a bash in flight flips it false.
func TestWaitingOnSubagentsGating(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")

	if ag.WaitingOnSubagents() {
		t.Fatal("no turn running → not waiting")
	}
	ag.running.Store(true)
	if ag.WaitingOnSubagents() {
		t.Fatal("turn running but nothing in flight → mid-generation, not waiting")
	}
	ag.trackTool("subagent", 1)
	if !ag.WaitingOnSubagents() {
		t.Fatal("only a subagent in flight → waiting")
	}
	ag.trackTool("subagent", 1) // two subagents still qualifies
	if !ag.WaitingOnSubagents() {
		t.Fatal("multiple subagents in flight → waiting")
	}
	ag.trackTool("bash", 1)
	if ag.WaitingOnSubagents() {
		t.Fatal("a bash in flight → not waiting on subagents")
	}
	ag.trackTool("bash", -1)
	ag.trackTool("subagent", -2)
	if ag.WaitingOnSubagents() {
		t.Fatal("all tools finished → not waiting")
	}
}

// End-to-end: during a real turn blocked on a foreground subagent, the parent
// reports WaitingOnSubagents; once the subagent's report lands and the turn
// continues, it flips false. The subagent's HTTP call blocks on a latch so the
// window is deterministic.
func TestWaitingOnSubagentsDuringForegroundSubagent(t *testing.T) {
	release := make(chan struct{})
	subStarted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case len(req.Messages) > 0 && strings.HasPrefix(req.Messages[0].Content, "You are a subagent"):
			close(subStarted)
			<-release // hold the subagent's turn until the test has observed the parent
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"sub report"},"finish_reason":"stop"}]}`+"\n\n")
		case len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == "user" && req.Messages[len(req.Messages)-1].Content == "go":
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"subagent","arguments":"{\"prompt\":\"explore\"}"}}]}}]}`+"\n\n")
			fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		default:
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"parent done"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	done := make(chan error, 1)
	go func() {
		_, err := ag.Turn(t.Context(), "go", Events{})
		done <- err
	}()

	select {
	case <-subStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("subagent never started")
	}
	if !ag.WaitingOnSubagents() {
		t.Fatal("parent blocked on a foreground subagent must report WaitingOnSubagents")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if ag.WaitingOnSubagents() {
		t.Fatal("turn finished → no longer waiting")
	}
}

// A steer queued before teardown is handed to OnOrphanedSteer (not dropped)
// and drained out of pending; with no hook installed it stays put so a
// headless caller can still drain it itself.
func TestDrainOrphanedSteers(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")

	// No hook: a steer with no turn running parks for a later drain.
	ag.Steer("keep me")
	ag.finishTurn()
	if got := ag.drainPending(); len(got) != 1 || got[0].text != "keep me" {
		t.Fatalf("no hook: pending should survive, got %+v", got)
	}

	// With the hook: a steer landing mid-turn (running=true) queues, and the
	// teardown drain hands each survivor to the hook in order.
	var surfaced []string
	ag.OnOrphanedSteer = func(text string) { surfaced = append(surfaced, text) }
	ag.running.Store(true)
	ag.Steer("one")
	ag.Steer("two")
	ag.finishTurn()
	if len(surfaced) != 2 || surfaced[0] != "one" || surfaced[1] != "two" {
		t.Fatalf("hook should receive both steers in order, got %v", surfaced)
	}
	if got := ag.drainPending(); len(got) != 0 {
		t.Fatalf("orphaned steers must be drained, got %+v", got)
	}
}

// A Steer landing while NO turn is running (the caller raced a teardown: it
// saw WaitingOnSubagents true, then the turn ended before Steer executed)
// must not park in pending forever — with the hook installed it fires
// immediately; without one it parks for a later drain (headless contract).
func TestSteerOnIdleAgentFiresOrphanHook(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")

	// No hook: parked, not lost, not fired.
	ag.Steer("parked")
	if got := ag.drainPending(); len(got) != 1 {
		t.Fatalf("no hook: steer should park, got %+v", got)
	}

	var fired []string
	ag.OnOrphanedSteer = func(text string) { fired = append(fired, text) }
	ag.Steer("late arrival")
	if len(fired) != 1 || fired[0] != "late arrival" {
		t.Fatalf("idle steer should fire the hook, got %v", fired)
	}
	if got := ag.drainPending(); len(got) != 0 {
		t.Fatalf("fired steer must not also park, got %+v", got)
	}
}

func TestSteerIngressKeepsActorDeliveryAtLoopBoundary(t *testing.T) {
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	var admitted []string
	ag.SetSteerIngress(func(text string) { admitted = append(admitted, text) })
	ag.running.Store(true)
	ag.Steer("persist first")
	if len(admitted) != 1 || admitted[0] != "persist first" || len(ag.drainPending()) != 0 {
		t.Fatalf("producer steer bypassed ingress: admitted=%v", admitted)
	}

	if !ag.DeliverSteer("actor delivery") {
		t.Fatal("running turn rejected actor delivery")
	}
	if got := ag.drainPending(); len(got) != 1 || got[0].text != "actor delivery" {
		t.Fatalf("actor delivery missed loop boundary: %+v", got)
	}
}
