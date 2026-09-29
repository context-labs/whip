package browserhost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestScreenshotExactByteLimitAndPartialFailure(t *testing.T) {
	for _, partialFailure := range []bool{false, true} {
		name := "complete"
		if partialFailure {
			name = "partial-native-failure"
		}
		t.Run(name, func(t *testing.T) {
			h, p, _ := fixture(t)
			a := attach(t, h, p, root, "human-tab")
			l := runLease(t, h, root, a.Scope.AttachmentID)
			done := make(chan error, 1)
			go func() {
				_, e := l.Run(context.Background(), "screenshot", allow, func(ctx context.Context, b *Batch) error {
					r, err := b.CDP(ctx, "", "Page.captureScreenshot", json.RawMessage(`{}`))
					if partialFailure {
						if err == nil || len(r.Image) != 0 {
							return errors.New("partial image treated as complete")
						}
						return nil
					}
					if err == nil && len(r.Image) != MaxScreenshotBytes {
						return errors.New("image byte bound was not exact")
					}
					return err
				})
				done <- e
			}()
			begin := next(t, p).Command
			if e := p.Settle(response(begin)); e != nil {
				t.Fatal(e)
			}
			shot := next(t, p).Command
			data := []byte(strings.Repeat("x", MaxScreenshotBytes))
			if partialFailure {
				data = data[:64<<10]
			}
			for offset := 0; offset < len(data); offset += 64 << 10 {
				if e := p.UploadScreenshot(shot.CommandID, "root", shot.Scope.ProviderEpoch, shot.Scope.AttachmentGeneration, offset, data[offset:offset+(64<<10)]); e != nil {
					t.Fatal(e)
				}
			}
			if !partialFailure {
				if e := p.UploadScreenshot(shot.CommandID, "root", shot.Scope.ProviderEpoch, shot.Scope.AttachmentGeneration, len(data), []byte{1}); e == nil {
					t.Fatal("screenshot bound not enforced")
				}
			}
			r := response(shot)
			if partialFailure {
				r.Error = &Failure{Kind: "unsupported_operation", Message: "capture failed"}
			} else {
				sum := sha256.Sum256(data)
				r.Screenshot = &Screenshot{Size: len(data), MediaType: "image/jpeg", Digest: hex.EncodeToString(sum[:])}
			}
			if e := p.Settle(r); e != nil {
				t.Fatal(e)
			}
			end := next(t, p).Command
			if e := p.Settle(response(end)); e != nil {
				t.Fatal(e)
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
			h.mu.Lock()
			bytes := h.uploadBytes
			h.mu.Unlock()
			if bytes != 0 {
				t.Fatal("image buffer was not released")
			}
		})
	}
}

func TestEventByteBoundCancelsAndReleasesQueue(t *testing.T) {
	h, p, _ := fixture(t)
	a := attach(t, h, p, root, "human-tab")
	l := runLease(t, h, root, a.Scope.AttachmentID)
	done := make(chan error, 1)
	go func() {
		_, e := l.Run(context.Background(), "events", allow, func(ctx context.Context, _ *Batch) error { <-ctx.Done(); return ctx.Err() })
		l.Close()
		done <- e
	}()
	begin := next(t, p).Command
	if e := p.Settle(response(begin)); e != nil {
		t.Fatal(e)
	}
	event := Event{RootID: "root", ProviderEpoch: a.Scope.ProviderEpoch, TabID: a.Scope.TabID, TabGeneration: a.Scope.TabGeneration, AttachmentID: a.Scope.AttachmentID, AttachmentGeneration: a.Scope.AttachmentGeneration, Kind: "cdp", Method: "Runtime.consoleAPICalled", Params: json.RawMessage(`{"data":"` + strings.Repeat("x", (256<<10)-20) + `"}`)}
	overflow := false
	for i := 1; i <= 9; i++ {
		event.Sequence = uint64(i)
		e := p.Event(event)
		if errors.Is(e, ErrBusy) {
			overflow = true
			break
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	if !overflow {
		t.Fatal("event byte bound not enforced")
	}
	if e := <-done; !errors.Is(e, ErrUnknown) {
		t.Fatalf("event overflow outcome: %v", e)
	}
	h.mu.Lock()
	bytes := h.eventBytes
	h.mu.Unlock()
	if bytes != 0 {
		t.Fatal("event allocation retained")
	}
}

func TestAuthorityRecheckStopsNativeSuffix(t *testing.T) {
	h, p, _ := fixture(t)
	a := attach(t, h, p, root, "human-tab")
	l := runLease(t, h, root, a.Scope.AttachmentID)
	allowed := true
	done := make(chan error, 1)
	go func() {
		_, e := l.Run(context.Background(), "recheck", func(context.Context) error {
			if !allowed {
				return errors.New("permission revoked")
			}
			return nil
		}, func(ctx context.Context, b *Batch) error {
			_, err := b.CDP(ctx, "", "Runtime.evaluate", json.RawMessage(`{}`))
			if err != nil {
				return err
			}
			allowed = false
			_, err = b.CDP(ctx, "", "Runtime.evaluate", json.RawMessage(`{}`))
			return err
		})
		done <- e
	}()
	begin := next(t, p).Command
	if e := p.Settle(response(begin)); e != nil {
		t.Fatal(e)
	}
	command := next(t, p).Command
	if e := p.Settle(response(command)); e != nil {
		t.Fatal(e)
	}
	if e := <-done; e == nil {
		t.Fatal("revoked authority executed suffix")
	}
	h.mu.Lock()
	queued := len(p.queue)
	h.mu.Unlock()
	if queued != 0 {
		t.Fatal("native call occurred after authority recheck failed")
	}
}

func TestNoHostMutationBeforeDispatchedCheck(t *testing.T) {
	h, p, _ := fixture(t)
	c, e := h.Resolve(context.Background(), root, "open", Arguments{URL: "https://example.test"})
	if e != nil {
		t.Fatal(e)
	}
	l, e := c.Acquire(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	if _, e = l.Execute(context.Background(), "pending-permission", func(context.Context) error { return errors.New("not dispatched") }, commit); e == nil {
		t.Fatal("undispatched operation executed")
	}
	h.mu.Lock()
	queued := len(p.queue)
	created := len(c.provider.created)
	h.mu.Unlock()
	if queued != 0 || created != 0 {
		t.Fatal("unapproved host effect")
	}
}

func TestPreviewExpansionRotatesResourceAfterCommitOnly(t *testing.T) {
	h, p, _ := fixture(t)
	c, e := h.Resolve(context.Background(), root, "open", Arguments{URL: "http://127.0.0.1:3000", PreviewHostID: "host"})
	if e != nil {
		t.Fatal(e)
	}
	l, e := c.Acquire(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	var original Attachment
	go func() {
		var err error
		original, err = l.Execute(context.Background(), "open-preview", allow, commit)
		done <- err
	}()
	command := next(t, p).Command
	if e = p.Settle(response(command)); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	l.Close()
	expand, e := h.Resolve(context.Background(), root, "allow_preview_port", Arguments{AttachmentID: original.Scope.AttachmentID, Port: 4000})
	if e != nil {
		t.Fatal(e)
	}
	if expand.Scope().Resource() == original.Scope.Resource() {
		t.Fatal("expanded route omitted from consent")
	}
	held, e := expand.Acquire(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer held.Close()
	go func() {
		_, err := held.Execute(context.Background(), "expand-preview", allow, func(context.Context, Attachment) error { return errors.New("durable publication failed") })
		done <- err
	}()
	command = next(t, p).Command
	if e = p.Settle(response(command)); e != nil {
		t.Fatal(e)
	}
	if e = <-done; !errors.Is(e, ErrUnknown) {
		t.Fatalf("expansion publication outcome: %v", e)
	}
	if len(h.Attachments(root)) != 0 {
		t.Fatal("failed durable expansion kept old agent control")
	}
	// No close-page or route-removal command is sent: human preview lifetime is separate.
	h.mu.Lock()
	queued := 0
	for _, n := range p.queue {
		if n.value.Command != nil {
			queued++
		}
	}
	h.mu.Unlock()
	if queued != 0 {
		t.Fatal("agent retirement affected human preview route")
	}
}

func TestExplicitOfferReplacesOwnCandidateWithCAS(t *testing.T) {
	h := New()
	defer h.Close()
	p, e := h.OpenPeer()
	if e != nil {
		t.Fatal(e)
	}
	o := testOffer()
	o.Availability = true
	o.Tabs = nil
	o.PreviewHosts = nil
	b, e := p.Bind(o)
	if e != nil {
		t.Fatal(e)
	}
	captured, e := h.Resolve(context.Background(), root, "open", Arguments{URL: "https://example.test"})
	if e != nil {
		t.Fatal(e)
	}
	explicit := testOffer()
	if _, e = p.Bind(explicit); !errors.Is(e, ErrStale) {
		t.Fatalf("candidate replacement lacked CAS: %v", e)
	}
	explicit.ExpectedProviderEpoch = b.ProviderEpoch
	selected, e := p.Bind(explicit)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = captured.Acquire(context.Background()); !errors.Is(e, ErrStale) {
		t.Fatalf("candidate authority survived replacement: %v", e)
	}
	if e = p.Unbind("root", selected.ProviderEpoch); e != nil {
		t.Fatal(e)
	}
	if _, e = h.Resolve(context.Background(), root, "open", Arguments{URL: "https://example.test"}); e == nil {
		t.Fatal("old candidate silently reappeared after release")
	}
}
