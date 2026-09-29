package browser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browserconfig"
	"github.com/context-labs/whip/internal/capability"
)

func nativeHostFixture(t *testing.T) *NativeHost {
	t.Helper()
	processes := capability.NewProcessManager()
	t.Cleanup(func() { _ = processes.Close() })
	h := NewNativeHost(t.TempDir(), processes)
	t.Cleanup(func() { _ = h.Close() })
	if err := h.Update(browserconfig.Config{Mode: "headless", Executable: "/explicit/fake"}, DriverRod); err != nil {
		t.Fatal(err)
	}
	h.open = func(_, life context.Context, _ NativeOptions) (*NativeConnection, error) {
		ctx, cancel := context.WithCancel(life)
		return &NativeConnection{lifetime: ctx, cancel: cancel}, nil
	}
	return h
}

func nativeCapture(t *testing.T, h *NativeHost, root, agent, name string) *NativeCapture {
	t.Helper()
	c, err := h.Capture(root, agent, name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func nativeLease(t *testing.T, c *NativeCapture) *NativeLease {
	t.Helper()
	l, err := c.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return l
}
func allowedNative(context.Context) error { return nil }

func TestNativeHostPassiveReadsAndOneUseDispatch(t *testing.T) {
	h := nativeHostFixture(t)
	var opens atomic.Int32
	original := h.open
	h.open = func(ctx, life context.Context, o NativeOptions) (*NativeConnection, error) {
		opens.Add(1)
		return original(ctx, life, o)
	}
	c := nativeCapture(t, h, "root", "root", "default")
	if got := h.List("root"); len(got) != 1 || got[0].State != "prepared" {
		t.Fatal(got)
	}
	if opens.Load() != 0 {
		t.Fatal("metadata launched browser")
	}
	denied := errors.New("dispatch not committed")
	l := nativeLease(t, c)
	if err := l.Run(t.Context(), func(context.Context) error { return denied }, func(context.Context, Backend) error { t.Fatal("ran before dispatch"); return nil }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	l.Close()
	if opens.Load() != 0 {
		t.Fatal("denied dispatch launched")
	}
	l = nativeLease(t, c)
	if err := l.Run(t.Context(), allowedNative, func(context.Context, Backend) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := l.Run(t.Context(), allowedNative, func(context.Context, Backend) error { t.Fatal("reused lease"); return nil }); !errors.Is(err, ErrNativeStale) {
		t.Fatal(err)
	}
	l.Close()
	child := nativeCapture(t, h, "root", "child", "default")
	if child.Description().Resource != c.Description().Resource {
		t.Fatal("explicit delegation cannot name the same resource")
	}
	l = nativeLease(t, child)
	if err := l.Run(t.Context(), allowedNative, func(context.Context, Backend) error { return nil }); err != nil {
		t.Fatal(err)
	}
	l.Close()
	if opens.Load() != 1 {
		t.Fatal("reopened connected resource", opens.Load())
	}
}

func TestNativeHostFailureRequiresExplicitGenerationChange(t *testing.T) {
	h := nativeHostFixture(t)
	c := nativeCapture(t, h, "root", "root", "default")
	before := c.Description()
	l := nativeLease(t, c)
	effectErr := errors.New("lost effect acknowledgement")
	var effects int
	if err := l.Run(t.Context(), allowedNative, func(context.Context, Backend) error { effects++; return effectErr }); !errors.Is(err, effectErr) {
		t.Fatal(err)
	}
	l.Close()
	if _, err := h.Capture("root", "root", "default"); !errors.Is(err, ErrNativeStale) {
		t.Fatal("automatic reopen", err)
	}
	if _, err := h.Reconnect("root", "default", "stale"); !errors.Is(err, ErrNativeStale) {
		t.Fatal(err)
	}
	after, err := h.Reconnect("root", "default", before.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if after.Generation == before.Generation || after.Resource == before.Resource || after.State != "prepared" || effects != 1 {
		t.Fatal(before, after, effects)
	}
	if _, err := c.Acquire(t.Context()); !errors.Is(err, ErrNativeStale) {
		t.Fatal("stale capture acquired", err)
	}
}

func TestNativeHostBatchAndResourceBounds(t *testing.T) {
	h := nativeHostFixture(t)
	for i := range 16 {
		nativeCapture(t, h, fmt.Sprintf("root-%d", i/4), "agent", fmt.Sprintf("named-%d", i))
	}
	if _, err := h.Capture("root-0", "agent", "fifth"); !errors.Is(err, ErrNativeBusy) {
		t.Fatal(err)
	}
	if _, err := h.Capture("new-root", "agent", "new"); !errors.Is(err, ErrNativeBusy) {
		t.Fatal(err)
	}
	c := nativeCapture(t, h, "root-0", "agent", "named-0")
	active := nativeLease(t, c)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	queued := make(chan error, 4)
	for range 4 {
		go func() {
			l, err := c.Acquire(ctx)
			if l != nil {
				l.Close()
			}
			queued <- err
		}()
	}
	deadline := time.After(time.Second)
	for len(c.entry.slots) != 5 {
		select {
		case <-deadline:
			t.Fatal("queue did not fill")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if _, err := c.Acquire(t.Context()); !errors.Is(err, ErrNativeBusy) {
		t.Fatal("sixth batch admitted", err)
	}
	cancel()
	for range 4 {
		if err := nativeWireResult(t, queued); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	if len(c.entry.slots) != 1 {
		t.Fatal("canceled queue retained reservations")
	}
	active.Close()
}

func TestNativeHostChildCancellationAndCloseJoin(t *testing.T) {
	h := nativeHostFixture(t)
	c := nativeCapture(t, h, "root", "child", "default")
	l := nativeLease(t, c)
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- l.Run(t.Context(), allowedNative, func(ctx context.Context, _ Backend) error { close(entered); <-ctx.Done(); return ctx.Err() })
	}()
	<-entered
	h.RevokeOwner("root", "child")
	if err := nativeWireResult(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	l.Close()
	if got := h.List("root"); len(got) != 1 || got[0].State != "ended" {
		t.Fatal(got)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeHostChangingConfigCancelsWaitAndPreservesExplicitSelection(t *testing.T) {
	h := nativeHostFixture(t)
	c := nativeCapture(t, h, "root", "root", "headless:default")
	before := c.Description()
	if _, err := h.Capture("root", "root", "live:override"); err == nil {
		t.Fatal("mode override bypassed host config")
	}
	if err := h.Update(h.options.Config, DriverRod); err != nil {
		t.Fatal(err)
	}
	if after := nativeCapture(t, h, "root", "root", "default").Description(); after.Generation != before.Generation {
		t.Fatal("passive refresh changed generation")
	}
	l := nativeLease(t, c)
	done := make(chan error, 1)
	go func() { defer l.Close(); <-c.Lifetime().Done(); done <- c.Lifetime().Err() }()
	if err := h.Update(browserconfig.Config{Mode: "extension"}, DriverChromedp); err != nil {
		t.Fatal(err)
	}
	if err := nativeWireResult(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	current := nativeCapture(t, h, "root", "root", "default")
	if _, err := h.Capture("other", "other", "default"); !errors.Is(err, ErrNativeBusy) {
		t.Fatal("two relays could overwrite one extension state", err)
	}
	if current.Description().Driver != DriverChromedp {
		t.Fatal(current.Description())
	}
}

func TestNativeHostRealTransportRechecksEveryPrimitive(t *testing.T) {
	for _, driver := range []string{DriverRod, DriverChromedp} {
		t.Run(driver, func(t *testing.T) {
			h := nativeHostFixture(t)
			h.open = OpenNative
			if err := h.Update(browserconfig.Config{Mode: "live", LiveEndpoint: nativeFakeBrowser(t)}, driver); err != nil {
				t.Fatal(err)
			}
			c := nativeCapture(t, h, "root", "root", "default")
			l := nativeLease(t, c)
			denied := errors.New("grant revoked")
			allowed := true
			checks := 0
			err := l.Run(t.Context(), func(context.Context) error {
				checks++
				if !allowed {
					return denied
				}
				return nil
			}, func(ctx context.Context, b Backend) error {
				result, err := b.Eval(ctx, "document.title")
				if err != nil || result != `"Example"` {
					t.Fatalf("%s %v", result, err)
				}
				allowed = false
				_, err = b.Eval(ctx, "mustNotDispatch()")
				return err
			})
			l.Close()
			if !errors.Is(err, denied) || checks < 3 {
				t.Fatal(err, checks)
			}
			if _, err := c.Acquire(t.Context()); !errors.Is(err, ErrNativeStale) {
				t.Fatal("revoked transport reused", err)
			}
		})
	}
}

func TestNativeHostRootRetirementKeepsCapacityUntilJoin(t *testing.T) {
	h := nativeHostFixture(t)
	c := nativeCapture(t, h, "root", "root", "default")
	for _, name := range []string{"two", "three", "four"} {
		nativeCapture(t, h, "root", "root", name)
	}
	lease := nativeLease(t, c)
	entered, finish := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	finishClose := sync.OnceFunc(func() { close(finish) })
	defer finishClose()
	go func() {
		done <- lease.Run(t.Context(), allowedNative, func(ctx context.Context, _ Backend) error { close(entered); <-ctx.Done(); <-finish; return ctx.Err() })
	}()
	<-entered
	retired := make(chan struct{})
	go func() { h.RevokeOwner("root", "root"); close(retired) }()
	<-c.Lifetime().Done()
	if _, err := h.Capture("root", "root", "default"); !errors.Is(err, ErrNativeStale) {
		t.Fatal("replaced still-owned resource", err)
	}
	if _, err := h.Capture("root", "root", "fifth"); !errors.Is(err, ErrNativeBusy) {
		t.Fatal("retiring resources stopped counting", err)
	}
	finishClose()
	if err := nativeWireResult(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	lease.Close()
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("root retirement did not join")
	}
	replacement := nativeCapture(t, h, "root", "root", "default")
	if replacement.Description().Generation == c.Description().Generation {
		t.Fatal("old authority restored")
	}
}
