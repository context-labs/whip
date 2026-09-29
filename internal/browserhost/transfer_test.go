package browserhost

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestTransferProvisionalChildWaitsForCommitAndReservations(t *testing.T) {
	for _, failCommit := range []bool{false, true} {
		t.Run(strconv.FormatBool(failCommit), func(t *testing.T) {
			h, p, _ := fixture(t)
			parent := attach(t, h, p, root, "human-tab")
			child := Identity{RootID: "root", AgentID: "prospective-child"}
			capture, e := h.PrepareTransfer(context.Background(), root, child, []string{parent.Scope.AttachmentID})
			if e != nil {
				t.Fatal(e)
			}
			prospective := capture.Attachments()[0]
			if prospective.Scope.AttachmentID == parent.Scope.AttachmentID || prospective.Scope.AttachmentGeneration == parent.Scope.AttachmentGeneration || prospective.Scope.Resource() != parent.Scope.Resource() {
				t.Fatal("handoff did not preserve lineage with fresh private identity")
			}
			l, e := capture.Acquire(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			entered := make(chan struct{})
			releaseCommit := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				_, err := l.Execute(context.Background(), "spawn-operation", allow, func(_ context.Context, children []Attachment) error {
					if len(children) != 1 || children[0].Owner != child {
						return errors.New("wrong child")
					}
					close(entered)
					<-releaseCommit
					if failCommit {
						return errors.New("uncertain SQL acknowledgement")
					}
					return nil
				})
				done <- err
			}()
			command := next(t, p).Command
			if command.Kind != "transfer" {
				t.Fatal(command)
			}
			if e = p.Settle(response(command)); e != nil {
				t.Fatal(e)
			}
			<-entered
			// Simulates an already awake scheduler claiming the committed child while
			// the committing caller is paused before receiving its SQL acknowledgement.
			pending, e := h.Resolve(context.Background(), child, "run", Arguments{AttachmentID: prospective.Scope.AttachmentID})
			if e != nil {
				t.Fatalf("provisional child missing: %v", e)
			}
			acquired := make(chan *Lease, 1)
			acquireErr := make(chan error, 1)
			go func() {
				lease, err := pending.Acquire(context.Background())
				if err != nil {
					acquireErr <- err
					return
				}
				acquired <- lease
			}()
			waitQueue(t, parent, capture.provider, 1)
			select {
			case got := <-acquired:
				got.Close()
				t.Fatal("child ran before transfer commit")
			case err := <-acquireErr:
				t.Fatalf("child rejected instead of waiting: %v", err)
			default:
			}
			close(releaseCommit)
			err := <-done
			if failCommit {
				if !errors.Is(err, ErrUnknown) {
					t.Fatalf("commit uncertainty: %v", err)
				}
				l.Close()
				select {
				case got := <-acquired:
					got.Close()
					t.Fatal("failed commit activated child")
				case <-acquireErr:
				case <-time.After(2 * time.Second):
					t.Fatal("child wait not cancelled")
				}
				if len(h.Attachments(root)) != 0 || len(h.Attachments(child)) != 0 {
					t.Fatal("ambiguous handoff preserved authority")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				select {
				case got := <-acquired:
					got.Close()
					t.Fatal("tab released before operation settlement owner closes")
				default:
				}
				l.Close()
				select {
				case got := <-acquired:
					got.Close()
				case err := <-acquireErr:
					t.Fatal(err)
				case <-time.After(2 * time.Second):
					t.Fatal("child admission stuck")
				}
				if _, e = h.Resolve(context.Background(), root, "run", Arguments{AttachmentID: parent.Scope.AttachmentID}); !errors.Is(e, ErrStale) {
					t.Fatalf("parent still executes: %v", e)
				}
				if len(h.Attachments(child)) != 1 {
					t.Fatal("child not active")
				}
				h.RevokeLineage("root", parent.Scope.Resource())
				if len(h.Attachments(child)) != 0 {
					t.Fatal("ancestor revoke missed child")
				}
			}
		})
	}
}

func waitQueue(t *testing.T, a Attachment, v *provider, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		v.peer.host.mu.Lock()
		tab := v.tabs[a.Scope.TabID]
		v.peer.host.mu.Unlock()
		tab.mu.Lock()
		got := len(tab.queue)
		tab.mu.Unlock()
		if got == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("expected tab waiter did not arrive")
}

func TestTransferKnownRejectionVersusUncertainDelivery(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(strconv.FormatBool(uncertain), func(t *testing.T) {
			h, p, _ := fixture(t)
			a := attach(t, h, p, root, "human-tab")
			c, e := h.PrepareTransfer(context.Background(), root, Identity{RootID: "root", AgentID: "child"}, []string{a.Scope.AttachmentID})
			if e != nil {
				t.Fatal(e)
			}
			l, e := c.Acquire(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			committed := false
			go func() {
				_, err := l.Execute(ctx, "transfer", allow, func(context.Context, []Attachment) error { committed = true; return nil })
				done <- err
			}()
			command := next(t, p).Command
			if uncertain {
				cancel()
			} else {
				r := response(command)
				r.Error = &Failure{Kind: "browser_busy", Message: "atomic handoff rejected"}
				if e = p.Settle(r); e != nil {
					t.Fatal(e)
				}
			}
			err := <-done
			l.Close()
			if err == nil || committed {
				t.Fatal("failed handoff committed")
			}
			if uncertain {
				if !errors.Is(err, ErrUnknown) || len(h.Attachments(root)) != 0 {
					t.Fatalf("uncertainty retained parent: %v", err)
				}
			} else {
				if len(h.Attachments(root)) != 1 {
					t.Fatal("known atomic rejection retired parent")
				}
			}
			if _, e = l.Execute(context.Background(), "retry", allow, func(context.Context, []Attachment) error { return nil }); !errors.Is(e, ErrStale) {
				t.Fatalf("handoff replay: %v", e)
			}
		})
	}
}

func TestTransferBusyTabHasNoPartialEffect(t *testing.T) {
	h, p, _ := fixture(t)
	a := attach(t, h, p, root, "human-tab")
	active := runLease(t, h, root, a.Scope.AttachmentID)
	c, e := h.PrepareTransfer(context.Background(), root, Identity{RootID: "root", AgentID: "child"}, []string{a.Scope.AttachmentID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Acquire(context.Background()); !errors.Is(e, ErrBusy) {
		t.Fatalf("busy transfer: %v", e)
	}
	if len(h.Attachments(root)) != 1 {
		t.Fatal("reservation failure retired parent")
	}
	active.Close()
	h.mu.Lock()
	queued := len(p.queue)
	h.mu.Unlock()
	if queued != 0 {
		t.Fatal("reservation failure sent native command")
	}
}

func TestWholeBatchQueueBoundCancellationAndRecheck(t *testing.T) {
	h, p, binding := fixture(t)
	a := attach(t, h, p, root, "human-tab")
	active := runLease(t, h, root, a.Scope.AttachmentID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 4)
	for i := range 4 {
		c, e := h.Resolve(context.Background(), root, "run", Arguments{AttachmentID: a.Scope.AttachmentID})
		if e != nil {
			t.Fatal(e)
		}
		go func() {
			l, err := c.Acquire(ctx)
			if l != nil {
				l.Close()
			}
			done <- err
		}()
		waitQueue(t, a, active.capture.provider, i+1)
	}
	c, e := h.Resolve(context.Background(), root, "run", Arguments{AttachmentID: a.Scope.AttachmentID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Acquire(context.Background()); !errors.Is(e, ErrBusy) {
		t.Fatalf("unbounded tab queue: %v", e)
	}
	offer := testOffer()
	offer.ExpectedProviderEpoch = binding.ProviderEpoch
	if _, e = p.Bind(offer); e != nil {
		t.Fatal(e)
	}
	for range 4 {
		if e = <-done; e == nil {
			t.Fatal("replaced provider admitted waiter")
		}
	}
	active.Close()
	cancel()
}
