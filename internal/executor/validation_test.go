package executor

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestExactCoverageAndInvocationShape(t *testing.T) {
	r := New()
	defer r.Close()
	peer, err := r.Peer()
	if err != nil {
		t.Fatal(err)
	}
	for _, coverage := range []Coverage{
		{},
		{Tools: []string{"lookup"}},
		{Tools: []string{"lookup", "lookup"}, Hooks: []string{"turn_start", "before_tool", "before_spawn"}},
		{Tools: []string{"other"}, Hooks: []string{"turn_start", "before_tool", "before_spawn"}},
		{Tools: []string{"lookup"}, Hooks: []string{"turn_start", "before_tool", "before_tool"}},
	} {
		if _, err := peer.Bind(t.Context(), document("definition"), coverage); err == nil {
			t.Fatal("partial or extra handler coverage accepted", coverage)
		}
	}
	peer, lease := bind(t, r, "definition")
	lease.Tools[0] = "mutated"
	for _, kind := range []Kind{Tool, Hook, "other"} {
		if _, err := r.Acquire(t.Context(), lease.Definition, kind, "missing"); err == nil {
			t.Fatal("undeclared handler accepted", kind)
		}
	}
	for _, change := range []func(*Request){
		func(r *Request) { r.SessionID = "" },
		func(r *Request) { r.TurnID = "" },
		func(r *Request) { r.CellID = "" },
		func(r *Request) { r.OperationID = "" },
		func(r *Request) { r.Operation = "tools.other" },
		func(r *Request) { r.Arguments = json.RawMessage(`null`) },
		func(r *Request) { r.Arguments = json.RawMessage(`[1]`) },
		func(r *Request) { r.Arguments = json.RawMessage("{\"x\":\"\xff\"}") },
		func(r *Request) {
			r.Arguments = json.RawMessage(`{"x":"` + strings.Repeat("a", MaxRequestBytes) + `"}`)
		},
		func(r *Request) { r.Timeout = -time.Second },
		func(r *Request) { r.Timeout = 15*time.Minute + time.Nanosecond },
		func(r *Request) { r.Input = "other context" },
	} {
		call := acquire(t, r, lease, Tool, "lookup")
		request := toolRequest()
		change(&request)
		if _, err := call.Invoke(t.Context(), request, nil); err == nil {
			t.Fatal("invalid invocation accepted", request.Timeout)
		}
		if page, err := peer.Pending(lease, ""); err != nil || len(page.Items) != 0 {
			t.Fatal("invalid invocation published", page, err)
		}
	}
	if r.active != 0 || r.queuedBytes != 0 {
		t.Fatal("invalid calls leaked capacity")
	}
}

func TestHookRepliesKeepKindSpecificBoundedFields(t *testing.T) {
	for _, name := range []string{"before_tool", "before_spawn", "turn_start"} {
		t.Run(name, func(t *testing.T) {
			r := New()
			defer r.Close()
			peer, lease := bind(t, r, "definition")
			call := acquire(t, r, lease, Hook, name)
			request := Request{SessionID: "session", TurnID: "turn"}
			valid := Result{Decision: "allow"}
			switch name {
			case "before_tool":
				request.CellID, request.Operation = "cell", "files.read"
				request.Arguments = json.RawMessage(`{"path":"a"}`)
				valid.Arguments = json.RawMessage(`{"count":9007199254740993}`)
			case "before_spawn":
				request.CellID, request.Operation = "cell", "agents.spawn"
				request.Spawn = json.RawMessage(`{"name":"child"}`)
				valid.Spawn = json.RawMessage(`{"name":"rewritten"}`)
			case "turn_start":
				request.Input = "preview"
				valid.Context = strings.Repeat("c", 4<<10)
			}
			done := invoke(t.Context(), call, request)
			next(t, peer)
			for _, invalid := range []Result{
				{Value: json.RawMessage(`null`)},
				{Decision: "unknown"},
				{Arguments: json.RawMessage(`null`)},
				{Spawn: json.RawMessage(`[]`)},
				{Failure: "failed", Decision: "allow"},
				{Failure: strings.Repeat("f", MaxProgressBytes+1)},
				{Context: strings.Repeat("c", (4<<10)+1)},
				{Arguments: json.RawMessage(`{"x":"` + strings.Repeat("a", MaxResultBytes) + `"}`)},
			} {
				if err := peer.Settle(r.Epoch(), lease.Generation, call.ID(), Hook, invalid); err == nil {
					t.Fatal("invalid hook reply settled the call")
				}
			}
			if err := peer.Progress(r.Epoch(), lease.Generation, call.ID(), "tool-only"); !errors.Is(err, ErrConflict) {
				t.Fatal("hook accepted tool progress", err)
			}
			if err := peer.Settle(r.Epoch(), lease.Generation, call.ID(), Hook, valid); err != nil {
				t.Fatal(err)
			}
			got := result(t, done)
			if got.err != nil || string(got.result.Arguments) != string(valid.Arguments) || string(got.result.Spawn) != string(valid.Spawn) || got.result.Context != valid.Context {
				t.Fatal("hook result changed", got)
			}
		})
	}
}

func TestQueuedCancellationAndEncodedEventBounds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New()
		defer r.Close()
		peer, lease := bind(t, r, "definition")
		call := acquire(t, r, lease, Tool, "lookup")
		done := invoke(t.Context(), call, toolRequest())
		synctest.Wait()
		call.Close()
		if got := <-done; !errors.Is(got.err, context.Canceled) {
			t.Fatal(got)
		}
		if len(peer.queue) != 0 || r.queuedBytes != 0 {
			t.Fatal("undelivered call was retained or generated a cancellation")
		}
		// JSON escaping can exceed the event bound even while raw input fits.
		request := toolRequest()
		request.Arguments = json.RawMessage(`{"value":"` + strings.Repeat("<", MaxRequestBytes/4) + `"}`)
		call = acquire(t, r, lease, Tool, "lookup")
		if _, err := call.Invoke(t.Context(), request, nil); err == nil {
			t.Fatal("encoded event limit was not checked")
		}
		if r.active != 0 || r.queuedBytes != 0 {
			t.Fatal("encoded rejection leaked capacity")
		}
	})
}

func TestProgressCoalescesAndParentDeadlineWins(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New()
		defer r.Close()
		peer, lease := bind(t, r, "definition")
		call := acquire(t, r, lease, Tool, "lookup")
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		entered, release := make(chan struct{}), make(chan struct{})
		done := make(chan outcome, 1)
		go func() {
			value, err := call.Invoke(ctx, toolRequest(), func(string) {
				close(entered)
				<-release
			})
			done <- outcome{value, err}
		}()
		event := next(t, peer)
		if time.Until(event.Invocation.Deadline) != time.Second {
			t.Fatal("parent deadline was replaced")
		}
		if err := peer.Progress(r.Epoch(), lease.Generation, call.ID(), "first"); err != nil {
			t.Fatal(err)
		}
		<-entered
		for _, value := range []string{"second", "latest"} {
			if err := peer.Progress(r.Epoch(), lease.Generation, call.ID(), value); err != nil {
				t.Fatal(err)
			}
		}
		if len(call.progress) != 1 || <-call.progress != "latest" {
			t.Fatal("progress was not coalesced")
		}
		if err := peer.Progress(r.Epoch(), lease.Generation, call.ID(), strings.Repeat("x", MaxProgressBytes+1)); err == nil {
			t.Fatal("unbounded progress accepted")
		}
		cancel()
		close(release)
		if got := <-done; !errors.Is(got.err, context.Canceled) {
			t.Fatal(got)
		}
		if err := peer.Progress(r.Epoch(), lease.Generation, call.ID(), "late"); !errors.Is(err, ErrConflict) {
			t.Fatal("late progress accepted", err)
		}
	})
}

func TestWaitingBindCancellationAndGenerationExhaustion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New()
		defer r.Close()
		_, _, ref, err := session.CanonicalDefinition(document("definition"))
		if err != nil {
			t.Fatal(err)
		}
		ready := make(chan *Call, 1)
		failed := make(chan error, 1)
		go func() {
			call, err := r.Acquire(t.Context(), ref, Tool, "lookup")
			if err != nil {
				failed <- err
				return
			}
			ready <- call
		}()
		synctest.Wait()
		peer, _ := bind(t, r, "definition")
		select {
		case call := <-ready:
			call.Close()
		case err := <-failed:
			t.Fatal(err)
		}
		peer.Close()
		ctx, cancel := context.WithCancel(t.Context())
		go func() { _, err := r.Acquire(ctx, ref, Tool, "lookup"); failed <- err }()
		synctest.Wait()
		cancel()
		if err := <-failed; !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled bind wait continued", err)
		}
		peer, lease := bind(t, r, "definition")
		call := acquire(t, r, lease, Tool, "lookup")
		r.generation = math.MaxInt64
		if _, err := peer.Bind(t.Context(), document("definition"), Coverage{Tools: []string{"lookup"}, Hooks: []string{"turn_start", "before_tool", "before_spawn"}}); !errors.Is(err, ErrCapacity) {
			t.Fatal("generation overflow accepted", err)
		}
		if err := call.Check(t.Context()); err != nil {
			t.Fatal("failed bind changed the prior lease", err)
		}
		call.Close()
		if r.active != 0 || r.waiting != 0 {
			t.Fatal("reservation/wait capacity leaked")
		}
	})
}
