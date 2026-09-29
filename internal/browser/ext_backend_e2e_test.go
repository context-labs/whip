package browser

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browser/extrelay"
	"github.com/go-rod/rod"
)

// TestBackendThroughRelay drives whip's real *Browser (the Backend methods
// native batches call — Navigate, ClickAt, TypeText, Screenshot, AXTree, Eval)
// through the extension relay, with a fake extension answering CDP the way
// chrome.debugger does. Proves the rod Backend is reused unchanged end-to-end.
func TestBackendThroughRelay(t *testing.T) {
	rel, err := extrelay.NewRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer rel.Close()

	// Fake extension: pin a tab, then answer every CDP command.
	ext := dialRelayExt(t, rel)
	defer ext.close()
	ext.send(t, `{"method":"whip.attached","params":{"tabId":7,"title":"Example","url":"https://example.com/"}}`)
	time.Sleep(100 * time.Millisecond)
	var group sync.WaitGroup
	group.Go(func() { ext.answerLoop(t) })
	defer func() { ext.close(); group.Wait() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Same explicit transport construction OpenNative uses, against the test relay.
	wire, err := dialNativeWire(ctx, ctx, rel.CDPURL())
	if err != nil {
		t.Fatal(err)
	}
	b := &Browser{mode: ModeExtension, closeTransport: wire.Close}
	b.browser = rod.New().Context(ctx).Client(wire)
	if err := b.browser.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	b.browser = b.browser.Context(ctx)
	if err := b.attachPage(); err != nil {
		t.Fatalf("attachPage: %v", err)
	}
	defer b.Close()

	if err := b.Navigate(ctx, "https://example.com/"); err != nil {
		t.Errorf("Navigate: %v", err)
	}
	if err := b.ClickAt(ctx, 10, 10); err != nil {
		t.Errorf("ClickAt: %v", err)
	}
	if err := b.TypeText(ctx, "hello"); err != nil {
		t.Errorf("TypeText: %v", err)
	}
	jpeg, err := b.Screenshot(ctx, 1568)
	if err != nil || len(jpeg) == 0 {
		t.Errorf("Screenshot: %v len=%d", err, len(jpeg))
	}
	ax, err := b.AXTree(ctx)
	if err != nil || !strings.Contains(ax, "Example") {
		t.Errorf("AXTree: %v (%.120s)", err, ax)
	}
	if _, err := b.Eval(ctx, "document.title"); err != nil {
		t.Errorf("Eval: %v", err)
	}
	info, err := b.Info(ctx)
	if err != nil || !strings.Contains(info.URL, "example.com") {
		t.Errorf("Info: %v %+v", err, info)
	}
}
