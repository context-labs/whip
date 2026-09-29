package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func document(name string) session.DefinitionDocument {
	return session.DefinitionDocument{ID: name, Name: name, Defaults: session.ConfigPatch{
		Tools: map[string]session.ToolDeclaration{"lookup": {InputSchema: json.RawMessage(`{"type":"object"}`)}},
		Hooks: map[string]session.HookDeclaration{"before_tool": {}, "before_spawn": {}, "turn_start": {}},
	}}
}

func bind(t *testing.T, r *Registry, name string) (*Peer, Lease) {
	t.Helper()
	peer, err := r.Peer()
	if err != nil {
		t.Fatal(err)
	}
	lease, err := peer.Bind(t.Context(), document(name), Coverage{Tools: []string{"lookup"}, Hooks: []string{"turn_start", "before_tool", "before_spawn"}})
	if err != nil {
		t.Fatal(err)
	}
	return peer, lease
}

func acquire(t *testing.T, r *Registry, lease Lease, kind Kind, name string) *Call {
	t.Helper()
	call, err := r.Acquire(t.Context(), lease.Definition, kind, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(call.Close)
	return call
}

func toolRequest() Request {
	return Request{SessionID: "session", TurnID: "turn", CellID: "cell", OperationID: "operation", Operation: "tools.lookup", Arguments: json.RawMessage(`{"id":9007199254740993}`)}
}

type outcome struct {
	result Result
	err    error
}

func invoke(ctx context.Context, call *Call, request Request) <-chan outcome {
	done := make(chan outcome, 1)
	go func() { result, err := call.Invoke(ctx, request, nil); done <- outcome{result, err} }()
	return done
}

func next(t *testing.T, peer *Peer) Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	event, err := peer.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func result(t *testing.T, done <-chan outcome) outcome {
	t.Helper()
	select {
	case value := <-done:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("invocation did not join")
		return outcome{}
	}
}

func TestReservationPublicationOwnershipAndExactOneShotResult(t *testing.T) {
	r := New()
	defer r.Close()
	peer, lease := bind(t, r, "definition")
	foreign, _ := r.Peer()
	defer foreign.Close()
	call := acquire(t, r, lease, Tool, "lookup")
	pending, err := peer.Pending(lease, "")
	if err != nil || len(pending.Items) != 0 {
		t.Fatal("reservation published before durable dispatch", pending, err)
	}
	if err := call.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	request := toolRequest()
	done := invoke(t.Context(), call, request)
	event := next(t, peer)
	if event.Type != "invoke" || event.Invocation.Request.OperationID != "operation" || event.Invocation.Request.TurnID != "turn" || event.ID != call.ID() {
		t.Fatal("invocation lost scope", event)
	}
	event.Invocation.Request.Arguments[0] = '!'
	pending, err = peer.Pending(lease, "")
	if err != nil || string(pending.Items[0].Request.Arguments) != string(request.Arguments) {
		t.Fatal("event aliased registry", pending, err)
	}
	pending.Items[0].Request.Arguments[0] = '!'
	for _, attempt := range []struct {
		peer       *Peer
		epoch      string
		generation int64
		kind       Kind
	}{
		{foreign, r.Epoch(), lease.Generation, Tool}, {peer, "old_epoch", lease.Generation, Tool}, {peer, r.Epoch(), lease.Generation + 1, Tool}, {peer, r.Epoch(), lease.Generation, Hook},
	} {
		if err := attempt.peer.Settle(attempt.epoch, attempt.generation, call.ID(), attempt.kind, Result{Value: json.RawMessage(`null`)}); !errors.Is(err, ErrConflict) {
			t.Fatal("foreign result accepted", err)
		}
	}
	if err := peer.Settle(r.Epoch(), lease.Generation, call.ID(), Tool, Result{Value: json.RawMessage(`bad`)}); err == nil {
		t.Fatal("invalid result accepted")
	}
	if _, err := call.Invoke(t.Context(), Request{}, nil); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate invoke changed active call", err)
	}
	exact := json.RawMessage(`{"count":9007199254740993}`)
	if err := peer.Settle(r.Epoch(), lease.Generation, call.ID(), Tool, Result{Value: exact}); err != nil {
		t.Fatal(err)
	}
	exact[0] = '!'
	settled := result(t, done)
	if settled.err != nil || string(settled.result.Value) != `{"count":9007199254740993}` {
		t.Fatal("exact result lost", settled)
	}
	if err := peer.Settle(r.Epoch(), lease.Generation, call.ID(), Tool, Result{Value: json.RawMessage(`null`)}); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate result accepted", err)
	}
	if _, err := call.Invoke(t.Context(), toolRequest(), nil); !errors.Is(err, ErrConflict) {
		t.Fatal("completed call replayed", err)
	}
}

func TestLeaseReplacementRevokesReservedAndInvokedCalls(t *testing.T) {
	r := New()
	defer r.Close()
	peer, lease := bind(t, r, "definition")
	reserved := acquire(t, r, lease, Tool, "lookup")
	running := acquire(t, r, lease, Tool, "lookup")
	done := invoke(t.Context(), running, toolRequest())
	event := next(t, peer)
	replacement, newLease := bind(t, r, "definition")
	if newLease.Generation <= lease.Generation {
		t.Fatal("generation did not advance")
	}
	if err := reserved.Check(t.Context()); !errors.Is(err, ErrReplaced) {
		t.Fatal("reserved generation remained live", err)
	}
	if _, err := reserved.Invoke(t.Context(), toolRequest(), nil); !errors.Is(err, ErrReplaced) {
		t.Fatal("reserved call borrowed new executor", err)
	}
	if got := result(t, done); !errors.Is(got.err, ErrReplaced) {
		t.Fatal("invoked call did not fail", got)
	}
	cancel := next(t, peer)
	if cancel.Type != "cancel" || cancel.ID != event.ID || cancel.Generation != lease.Generation {
		t.Fatal("old handler was not cancelled", cancel)
	}
	if _, err := peer.Pending(lease, ""); !errors.Is(err, ErrConflict) {
		t.Fatal("old lease observed current calls", err)
	}
	pending, err := replacement.Pending(newLease, "")
	if err != nil || len(pending.Items) != 0 {
		t.Fatal("invocations replayed to replacement", pending, err)
	}
}

func TestCancellationDisconnectAndCloseAreJoinedWithoutReplay(t *testing.T) {
	for _, finish := range []string{"cancel", "disconnect", "registry"} {
		t.Run(finish, func(t *testing.T) {
			r := New()
			defer r.Close()
			peer, lease := bind(t, r, "definition")
			call := acquire(t, r, lease, Tool, "lookup")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := invoke(ctx, call, toolRequest())
			event := next(t, peer)
			want := context.Canceled
			switch finish {
			case "cancel":
				cancel()
			case "disconnect":
				peer.Close()
				want = ErrDisconnected
			case "registry":
				r.Close()
				want = ErrClosed
			}
			if got := result(t, done); !errors.Is(got.err, want) {
				t.Fatal("wrong terminal result", got)
			}
			if finish == "cancel" {
				notice := next(t, peer)
				if notice.Type != "cancel" || notice.ID != event.ID {
					t.Fatal("missing cancellation", notice)
				}
			}
			if err := peer.Settle(r.Epoch(), lease.Generation, call.ID(), Tool, Result{Value: json.RawMessage(`null`)}); !errors.Is(err, ErrConflict) {
				t.Fatal("late result revived call", err)
			}
			if r.active != 0 || len(r.calls) != 0 {
				t.Fatal("call capacity retained after join")
			}
		})
	}
}

func TestBindWaitAndInvocationDeadlinesUseOwnedContexts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New()
		defer r.Close()
		_, _, ref, err := session.CanonicalDefinition(document("waiting"))
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		_, err = r.Acquire(t.Context(), ref, Tool, "lookup")
		if !errors.Is(err, ErrUnavailable) || time.Since(start) != BindWait {
			t.Fatal("bind wait not bounded", time.Since(start), err)
		}
		peer, lease := bind(t, r, "waiting")
		for _, kind := range []Kind{Tool, Hook} {
			name, request, want := "lookup", toolRequest(), 5*time.Minute
			if kind == Hook {
				name = "turn_start"
				request = Request{SessionID: "session", TurnID: "turn", Input: "work"}
				want = 30 * time.Second
			}
			call := acquire(t, r, lease, kind, name)
			start = time.Now()
			done := invoke(t.Context(), call, request)
			event, err := peer.Next(t.Context())
			if err != nil || event.Invocation.Deadline.Sub(start) != want {
				t.Fatal("wrong default deadline", event, err)
			}
			settled := <-done
			if !errors.Is(settled.err, context.DeadlineExceeded) || time.Since(start) != want {
				t.Fatal("invocation exceeded deadline", time.Since(start), settled)
			}
			if _, err := peer.Next(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestBoundedWaitersWakeOnShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New()
		defer r.Close()
		_, _, ref, _ := session.CanonicalDefinition(document("waiting"))
		done := make(chan error, MaxBindWaiters)
		for range MaxBindWaiters {
			go func() { _, err := r.Acquire(t.Context(), ref, Tool, "lookup"); done <- err }()
		}
		synctest.Wait()
		if _, err := r.Acquire(t.Context(), ref, Tool, "lookup"); !errors.Is(err, ErrCapacity) {
			t.Fatal("unbounded waiters", err)
		}
		r.Close()
		for range MaxBindWaiters {
			if err := <-done; !errors.Is(err, ErrClosed) {
				t.Fatal("waiter not joined", err)
			}
		}
		if r.waiting != 0 || r.active != 0 {
			t.Fatal("waiter capacity leaked")
		}
	})
}

func TestBoundedPeersLeasesAndReservations(t *testing.T) {
	r := New()
	defer r.Close()
	peers := make([]*Peer, 0, MaxPeers)
	for index := range MaxPeers {
		peer, err := r.Peer()
		if err != nil {
			t.Fatal(index, err)
		}
		peers = append(peers, peer)
	}
	if _, err := r.Peer(); !errors.Is(err, ErrCapacity) {
		t.Fatal("unbounded peers", err)
	}
	for index := range MaxLeases {
		peer := peers[index/MaxPeerLeases]
		doc := document(fmt.Sprintf("d%d", index))
		lease, err := peer.Bind(t.Context(), doc, Coverage{Tools: []string{"lookup"}, Hooks: []string{"before_tool", "before_spawn", "turn_start"}})
		if err != nil {
			t.Fatal(index, err)
		}
		if index%MaxPeerLeases == 0 {
			for range MaxPeerCalls {
				acquire(t, r, lease, Tool, "lookup")
			}
		}
	}
	if _, err := peers[10].Bind(t.Context(), document("overflow"), Coverage{Tools: []string{"lookup"}, Hooks: []string{"before_tool", "before_spawn", "turn_start"}}); !errors.Is(err, ErrCapacity) {
		t.Fatal("unbounded leases", err)
	}
	_, _, ref, _ := session.CanonicalDefinition(document("d0"))
	if _, err := r.Acquire(t.Context(), ref, Tool, "lookup"); !errors.Is(err, ErrCapacity) {
		t.Fatal("unbounded reservations", err)
	}
	peers[0].Close()
	if r.active != MaxCalls-MaxPeerCalls {
		t.Fatal("disconnect did not release reservations")
	}
	if _, err := r.Peer(); err != nil {
		t.Fatal("peer capacity was not released", err)
	}
}

func TestBoundedPublicationAndPendingPages(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New()
		defer r.Close()
		peer, lease := bind(t, r, "large")
		request := toolRequest()
		request.Arguments = json.RawMessage(`{"value":"` + strings.Repeat("a", MaxRequestBytes-32<<10) + `"}`)
		done := make([]<-chan outcome, 0, MaxPeerCalls)
		for range MaxPeerCalls {
			done = append(done, invoke(t.Context(), acquire(t, r, lease, Tool, "lookup"), request))
		}
		synctest.Wait()
		if r.queuedBytes > MaxQueuedBytes || r.queuedBytes < MaxQueuedBytes/2 {
			t.Fatal("publication byte bound not exercised", r.queuedBytes)
		}
		count, after := 0, ""
		for {
			page, err := peer.Pending(lease, after)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(page)
			if len(page.Items) > 4 || len(raw) > 4*MaxRequestBytes {
				t.Fatal("pending page exceeds wire budget", len(raw))
			}
			count += len(page.Items)
			if page.Next == "" {
				break
			}
			if page.Next <= after {
				t.Fatal("nonadvancing cursor")
			}
			after = page.Next
		}
		if count == 0 || count == MaxPeerCalls {
			t.Fatal("backpressure did not bound publication", count)
		}
		peer.Close()
		failures := 0
		for _, finished := range done {
			got := <-finished
			if errors.Is(got.err, ErrCapacity) {
				failures++
			} else if !errors.Is(got.err, ErrDisconnected) {
				t.Fatal(got)
			}
		}
		if failures != MaxPeerCalls-count || r.active != 0 || r.queuedBytes != 0 {
			t.Fatal("queue capacity or work leaked", failures, count, r.active, r.queuedBytes)
		}
	})
}
