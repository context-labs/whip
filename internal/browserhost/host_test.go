package browserhost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Host, *Peer, Binding) {
	t.Helper()
	h := New()
	t.Cleanup(h.Close)
	p, e := h.OpenPeer()
	if e != nil {
		t.Fatal(e)
	}
	b, e := p.Bind(testOffer())
	if e != nil {
		t.Fatal(e)
	}
	return h, p, b
}

func testOffer() Offer {
	return Offer{RootID: "root", Version: 2, DesktopID: "desktop", WindowID: "window", OfferRevision: "offer", CreateProfileID: "profile", Tabs: []OfferedTab{{TabID: "human-tab", TabGeneration: "tab-generation", ProfileID: "profile", DocumentRevision: "doc1", URL: "https://user:password@example.test/", Title: "human\npage"}}, PreviewHosts: []Preview{{HostID: "host", HostIdentity: "runtime", ConnectionGeneration: "connection", EnvironmentID: "environment", Loopback: "127.0.0.1", Ports: []int{3000}}}}
}

var root = Identity{RootID: "root", AgentID: "root"}

func allow(context.Context) error              { return nil }
func commit(context.Context, Attachment) error { return nil }
func next(t *testing.T, p *Peer) Notification {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	n, e := p.Next(ctx)
	if e != nil {
		t.Fatal(e)
	}
	return n
}

func response(c *Command) CommandResult {
	return CommandResult{CommandID: c.CommandID, RootID: c.Identity.RootID, ProviderEpoch: c.Scope.ProviderEpoch, AttachmentGeneration: c.Scope.AttachmentGeneration, DocumentRevision: "doc1", Result: json.RawMessage(`{}`)}
}

func attach(t *testing.T, h *Host, p *Peer, owner Identity, tab string) Attachment {
	t.Helper()
	c, e := h.Resolve(context.Background(), owner, "attach", Arguments{TabID: tab})
	if e != nil {
		t.Fatal(e)
	}
	l, e := c.Acquire(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	done := make(chan error, 1)
	var out Attachment
	go func() {
		var err error
		out, err = l.Execute(context.Background(), "attach-operation", allow, commit)
		done <- err
	}()
	cmd := next(t, p).Command
	if cmd == nil || cmd.Kind != "attach" {
		t.Fatalf("command: %#v", cmd)
	}
	if e = p.Settle(response(cmd)); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	return out
}

func runLease(t *testing.T, h *Host, owner Identity, id string) *Lease {
	t.Helper()
	c, e := h.Resolve(context.Background(), owner, "run", Arguments{AttachmentID: id})
	if e != nil {
		t.Fatal(e)
	}
	l, e := c.Acquire(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(l.Close)
	return l
}

func TestOfferSelectionCASAndPassiveResolution(t *testing.T) {
	h, p, b := fixture(t)
	c, e := h.Resolve(context.Background(), root, "open", Arguments{URL: "https://example.test"})
	if e != nil {
		t.Fatal(e)
	}
	h.mu.Lock()
	if len(h.selected["root"].attachments) != 0 {
		t.Error("resolution retained attachment")
	}
	h.mu.Unlock()
	if _, e = p.Bind(testOffer()); !errors.Is(e, ErrStale) {
		t.Fatalf("missing CAS: %v", e)
	}
	o := testOffer()
	o.ExpectedProviderEpoch = b.ProviderEpoch
	replacement, e := p.Bind(o)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Acquire(context.Background()); !errors.Is(e, ErrStale) {
		t.Fatalf("old capture: %v", e)
	}
	if e = p.Unbind("root", b.ProviderEpoch); !errors.Is(e, ErrStale) {
		t.Fatalf("stale release: %v", e)
	}
	if e = p.Unbind("root", replacement.ProviderEpoch); e != nil {
		t.Fatal(e)
	}
	if e = p.Unbind("root", replacement.ProviderEpoch); e != nil {
		t.Fatal(e)
	}
	other, e := h.OpenPeer()
	if e != nil {
		t.Fatal(e)
	}
	if e = other.Unbind("root", replacement.ProviderEpoch); !errors.Is(e, ErrStale) {
		t.Fatalf("cross-peer release: %v", e)
	}
	o = testOffer()
	o.Availability = true
	o.Tabs = nil
	o.PreviewHosts = nil
	if _, e = p.Bind(o); e != nil {
		t.Fatal(e)
	}
	if _, e = other.Bind(o); e != nil {
		t.Fatal(e)
	}
	if _, e = h.Resolve(context.Background(), root, "open", Arguments{URL: "https://example.test"}); e == nil {
		t.Fatal("ambiguous availability selected")
	}
}

func TestPreviewScopeAndOwnerAreExplicit(t *testing.T) {
	h, p, _ := fixture(t)
	for _, url := range []string{"http://localhost:3000", "http://user@127.0.0.1:3000", "http://127.0.0.2:3000", "file:///tmp/x", "http://127.0.0.1:0"} {
		if _, e := h.Resolve(context.Background(), root, "open", Arguments{PreviewHostID: "host", URL: url}); e == nil {
			t.Errorf("allowed %s", url)
		}
	}
	c, e := h.Resolve(context.Background(), root, "open", Arguments{PreviewHostID: "host", URL: "http://127.0.0.1:4000/path"})
	if e != nil {
		t.Fatal(e)
	}
	scope := c.Scope()
	if fmt.Sprint(scope.Preview.Ports) != "[3000 4000]" {
		t.Fatal(scope.Preview)
	}
	resource := scope.Resource()
	scope.AttachmentID = "child-private"
	scope.AttachmentGeneration = "new-generation"
	if scope.Resource() != resource {
		t.Fatal("private handle changed delegated resource")
	}
	scope.Preview.Ports[0] = 1
	if c.Scope().Resource() != resource {
		t.Fatal("scope alias")
	}
	a := attach(t, h, p, root, "human-tab")
	if a.URL != "https://example.test/" || a.Title != "humanpage" {
		t.Fatalf("unsafe metadata: %#v", a)
	}
	child := Identity{RootID: "root", AgentID: "child"}
	if _, e = h.Resolve(context.Background(), child, "run", Arguments{AttachmentID: a.Scope.AttachmentID}); !errors.Is(e, ErrStale) {
		t.Fatalf("foreign handle: %v", e)
	}
	if _, e = h.Resolve(context.Background(), child, "attach", Arguments{TabID: "human-tab"}); e == nil {
		t.Fatal("child inherited human offer")
	}
}

func TestCommandExactHolderScreenshotAndNoReplay(t *testing.T) {
	h, p, _ := fixture(t)
	a := attach(t, h, p, root, "human-tab")
	l := runLease(t, h, root, a.Scope.AttachmentID)
	other, e := h.OpenPeer()
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	var calls atomic.Int32
	go func() {
		_, err := l.Run(context.Background(), "run-operation", func(ctx context.Context) error { calls.Add(1); return ctx.Err() }, func(ctx context.Context, b *Batch) error {
			if _, err := b.CDP(ctx, "foreign", "Runtime.evaluate", json.RawMessage(`{}`)); err == nil {
				return errors.New("foreign CDP accepted")
			}
			if _, err := b.CDP(ctx, "", "Browser.close", json.RawMessage(`{}`)); err == nil {
				return errors.New("browser close accepted")
			}
			result, err := b.CDP(ctx, "", "Page.captureScreenshot", json.RawMessage(`{}`))
			if err == nil && string(result.Image) != "jpeg-data" {
				return errors.New("image missing")
			}
			return err
		})
		done <- err
	}()
	begin := next(t, p).Command
	if begin.Kind != "begin" {
		t.Fatal(begin)
	}
	if e = p.Settle(response(begin)); e != nil {
		t.Fatal(e)
	}
	shot := next(t, p).Command
	if shot.Kind != "cdp" {
		t.Fatal(shot)
	}
	if e = other.UploadScreenshot(shot.CommandID, "root", shot.Scope.ProviderEpoch, shot.Scope.AttachmentGeneration, 0, []byte("jpeg-data")); !errors.Is(e, ErrStale) {
		t.Fatalf("foreign upload: %v", e)
	}
	if e = p.UploadScreenshot(shot.CommandID, "root", shot.Scope.ProviderEpoch, shot.Scope.AttachmentGeneration, 1, []byte("jpeg-data")); e == nil {
		t.Fatal("out-of-order upload")
	}
	if e = p.UploadScreenshot(shot.CommandID, "root", shot.Scope.ProviderEpoch, shot.Scope.AttachmentGeneration, 0, []byte("jpeg-data")); e != nil {
		t.Fatal(e)
	}
	result := response(shot)
	sum := sha256.Sum256([]byte("jpeg-data"))
	result.Screenshot = &Screenshot{Size: 9, MediaType: "image/jpeg", Digest: hex.EncodeToString(sum[:])}
	if e = p.Settle(result); e != nil {
		t.Fatal(e)
	}
	if e = p.Settle(result); !errors.Is(e, ErrStale) {
		t.Fatalf("duplicate result: %v", e)
	}
	end := next(t, p).Command
	if end.Kind != "end" {
		t.Fatal(end)
	}
	if e = p.Settle(response(end)); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if calls.Load() < 6 {
		t.Fatal("authority not checked before each call")
	}
	if _, e = l.Run(context.Background(), "retry", allow, func(context.Context, *Batch) error { return nil }); !errors.Is(e, ErrStale) {
		t.Fatalf("replay: %v", e)
	}
	h.mu.Lock()
	remaining := h.uploadBytes
	h.mu.Unlock()
	if remaining != 0 {
		t.Fatal("upload allocation retained")
	}
}

func TestCancellationRetiresDeliveredBatchAndCloseJoins(t *testing.T) {
	h, p, _ := fixture(t)
	a := attach(t, h, p, root, "human-tab")
	l := runLease(t, h, root, a.Scope.AttachmentID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := l.Run(ctx, "cancel-operation", allow, func(ctx context.Context, b *Batch) error {
			_, err := b.CDP(ctx, "", "Runtime.evaluate", json.RawMessage(`{}`))
			return err
		})
		l.Close()
		done <- e
	}()
	begin := next(t, p).Command
	if e := p.Settle(response(begin)); e != nil {
		t.Fatal(e)
	}
	command := next(t, p).Command
	cancel()
	if e := <-done; !errors.Is(e, ErrUnknown) {
		t.Fatalf("cancel outcome: %v", e)
	}
	if e := p.Settle(response(command)); !errors.Is(e, ErrStale) {
		t.Fatalf("late reply: %v", e)
	}
	if len(h.Attachments(root)) != 0 {
		t.Fatal("cancelled attachment remains live")
	}
	closed := make(chan struct{})
	go func() { p.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("close did not join")
	}
}

func TestInventoryScopeSanitizationAndSuccessfulEmpty(t *testing.T) {
	h, p, _ := fixture(t)
	a := attach(t, h, p, root, "human-tab")
	done := make(chan error, 1)
	go func() {
		tabs, e := h.ListTabs(context.Background(), root)
		if e == nil && (len(tabs) != 1 || tabs[0].AttachmentID != a.Scope.AttachmentID || tabs[0].URL != "https://example.test/" || tabs[0].Title != "safe" || tabs[0].Requestable) {
			e = fmt.Errorf("wrong projection: %#v", tabs)
		}
		done <- e
	}()
	req := next(t, p).Inventory
	if req == nil || len(req.Tabs) != 1 {
		t.Fatal(req)
	}
	bad := InventoryResult{RequestID: req.RequestID, RootID: "root", ProviderEpoch: req.Binding.ProviderEpoch, Tabs: []Tab{{TabID: "foreign", TabGeneration: "generation", State: "available"}}}
	if e := p.SettleInventory(bad); e == nil {
		t.Fatal("unrequested inventory accepted")
	}
	bad.Tabs = []Tab{{TabID: "human-tab", TabGeneration: "tab-generation", State: "available", Requestable: true, AttachmentID: "forged", URL: "https://secret:credential@example.test/", Title: "sa\nfe"}}
	if e := p.SettleInventory(bad); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	child := Identity{RootID: "root", AgentID: "child"}
	go func() {
		tabs, e := h.ListTabs(context.Background(), child)
		if e == nil && len(tabs) != 0 {
			e = errors.New("child inventory leaked")
		}
		done <- e
	}()
	req = next(t, p).Inventory
	if len(req.Tabs) != 0 {
		t.Fatal("child inventory offered human tabs")
	}
	if e := p.SettleInventory(InventoryResult{RequestID: req.RequestID, RootID: "root", ProviderEpoch: req.Binding.ProviderEpoch, Tabs: []Tab{}}); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}

func TestEventGapRetiresOnlyLineageAndDocumentCancelsBatch(t *testing.T) {
	h, p, _ := fixture(t)
	a := attach(t, h, p, root, "human-tab")
	l := runLease(t, h, root, a.Scope.AttachmentID)
	done := make(chan error, 1)
	go func() {
		_, e := l.Run(context.Background(), "observe", allow, func(ctx context.Context, _ *Batch) error { <-ctx.Done(); return ctx.Err() })
		l.Close()
		done <- e
	}()
	cmd := next(t, p).Command
	if e := p.Settle(response(cmd)); e != nil {
		t.Fatal(e)
	}
	event := Event{RootID: "root", ProviderEpoch: a.Scope.ProviderEpoch, TabID: a.Scope.TabID, TabGeneration: a.Scope.TabGeneration, AttachmentID: a.Scope.AttachmentID, AttachmentGeneration: a.Scope.AttachmentGeneration, Sequence: 1, Kind: "document", DocumentRevision: "other-document"}
	if e := p.Event(event); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e == nil {
		t.Fatal("document change did not cancel")
	}
	if e := p.Event(event); !errors.Is(e, ErrEventStale) {
		t.Fatalf("stale event: %v", e)
	}
}

func TestGlobalAndOutboundBounds(t *testing.T) {
	h := New()
	defer h.Close()
	var p *Peer
	for range MaxPeers {
		var e error
		p, e = h.OpenPeer()
		if e != nil {
			t.Fatal(e)
		}
	}
	if _, e := h.OpenPeer(); !errors.Is(e, ErrBusy) {
		t.Fatal("peer bound missing")
	}
	h.mu.Lock()
	for range 64 {
		if !p.notifyLocked(Notification{Revoked: &Revoked{RootID: "root", Reason: "release"}}) {
			t.Fatal("early overflow")
		}
	}
	if p.notifyLocked(Notification{Revoked: &Revoked{RootID: "root", Reason: "overflow"}}) {
		t.Fatal("outbound bound missing")
	}
	h.mu.Unlock()
	if _, e := p.Next(context.Background()); !errors.Is(e, ErrClosed) {
		t.Fatalf("overflow connection remains live: %v", e)
	}
	o := testOffer()
	o.Tabs[0].Title = strings.Repeat("x", 513)
	p, e := h.OpenPeer()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Bind(o); e == nil {
		t.Fatal("unbounded title")
	}
}
