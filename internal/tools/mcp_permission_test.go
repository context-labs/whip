package tools

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

type pausedMCPDecisionLedger struct {
	*countingLedger
	before chan struct{}
	commit chan struct{}
	after  chan struct{}
	finish chan struct{}
}

func (l *pausedMCPDecisionLedger) Decide(ctx context.Context, admission capability.Admission, id string, decision capability.Decision) (capability.Ticket, error) {
	close(l.before)
	<-l.commit
	ticket, err := l.countingLedger.Decide(ctx, admission, id, decision)
	close(l.after)
	<-l.finish
	return ticket, err
}

func TestMCPLatePermissionDecisionsCannotOutliveInvocation(t *testing.T) {
	_, ledger, provider, authority := newMCPServices(t)
	paused := &pausedMCPDecisionLedger{
		countingLedger: ledger,
		before:         make(chan struct{}), commit: make(chan struct{}),
		after: make(chan struct{}), finish: make(chan struct{}),
	}
	var commitOnce, finishOnce sync.Once
	commit := func() { commitOnce.Do(func() { close(paused.commit) }) }
	finish := func() { finishOnce.Do(func() { close(paused.finish) }) }
	defer commit()
	defer finish()
	services := NewServices()
	services.SetExternalPermissions(true)
	services.SetMCPProvider(func() MCPProvider { return provider })
	if err := services.BindDispatcher(paused, ledger.Workspaces(), ledger.Processes(), authority); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := services.InvokeMCP(ctx, "my-server", "write.raw", nil)
		result <- err
	}()
	var id string
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for id == "" {
		services.mu.RLock()
		for pendingID := range services.permissions {
			id = pendingID
		}
		services.mu.RUnlock()
		if id != "" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("MCP call did not register a permission waiter")
		case <-tick.C:
		}
	}
	cancel()
	select {
	case <-paused.before:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not begin terminalization")
	}

	// The waiter is gone, but its durable record is still pending. A human
	// decision at this exact point can be cached until the invocation finishes.
	allow := capability.Decision{Allow: true, PrincipalID: "trusted-client"}
	if err := services.ResolvePermission(id, allow); err != nil {
		t.Fatalf("decision before terminalization error=%v", err)
	}
	services.mu.RLock()
	cached := len(services.permissions)
	services.mu.RUnlock()
	if cached != 1 {
		t.Fatalf("preterminal decision count=%d, want 1", cached)
	}
	commit()
	select {
	case <-paused.after:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not commit")
	}
	if err := services.ResolvePermission(id, allow); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("decision after terminalization error=%v", err)
	}
	finish()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled call error=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled invocation did not finish")
	}
	if err := services.ResolvePermission(id, allow); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("decision after invocation cleanup error=%v", err)
	}
	services.mu.RLock()
	retained := len(services.permissions)
	services.mu.RUnlock()
	if retained != 0 || provider.effects.Load() != 0 {
		t.Fatalf("canceled call retained state or effects: retained=%d effects=%d", retained, provider.effects.Load())
	}
	assertMCPSettled(t, ledger, authority)
}
