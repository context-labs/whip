package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/jpeg"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/proto"
)

// TestDesktopNativeRod is an opt-in native Electron spike, not a headless mock.
// Start apps/desktop/scripts/browser-spike.cjs and pass its private control file
// in BROWSER_SPIKE_CONTROL. An absent fixture is a skip, never native evidence.
func TestDesktopNativeRod(t *testing.T) {
	path := os.Getenv("BROWSER_SPIKE_CONTROL")
	if path == "" {
		t.Skip("native Electron fixture required: set BROWSER_SPIKE_CONTROL")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		WSURL     string `json:"wsURL"`
		TargetID  string `json:"targetId"`
		SessionID string `json:"sessionId"`
		Origin    string `json:"origin"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(fixture.WSURL)
	if err != nil || endpoint.Scheme != "ws" || endpoint.Hostname() != "127.0.0.1" {
		t.Fatal("fixture must expose an authenticated loopback shim, not a public debug listener")
	}
	if fixture.TargetID == "" || fixture.SessionID == "" || fixture.Origin == "" {
		t.Fatal("native fixture control is incomplete")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ws := &cdp.WebSocket{}
	// Rod defaults to key "nil", tolerated by Chromium but rejected by ws.
	header := http.Header{"Sec-WebSocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="}}
	if err := ws.Connect(ctx, fixture.WSURL, header); err != nil {
		t.Fatalf("connect native fixture: %s", strings.ReplaceAll(err.Error(), fixture.WSURL, "[private endpoint]"))
	}
	t.Cleanup(func() { _ = ws.Close() })
	client := &desktopRecordingClient{CDPClient: cdp.New().Start(ws)}
	backend, err := NewDesktopBackend(ctx, client, fixture.TargetID, ws.Close)
	if err != nil {
		t.Fatal(err)
	}
	b := backend.(*desktopBackend)
	rb := b.browser
	// The production adapter owns only its scoped transport, not the guest.
	t.Cleanup(func() { _ = b.Close() })

	navigate := func(path string) string {
		t.Helper()
		var loader string
		wait := rb.EachEvent(func(event *proto.PageFrameNavigated) bool {
			if event.Frame.ParentID != "" {
				return false
			}
			loader = string(event.Frame.LoaderID)
			return true
		})
		if err := b.Navigate(ctx, fixture.Origin+path); err != nil {
			t.Fatal(err)
		}
		wait()
		if loader == "" {
			t.Fatal("no committed main-frame document identity")
		}
		return loader
	}
	first := navigate("/rod-first")
	second := navigate("/rod-second")
	if first == second {
		t.Fatal("navigation did not change the document identity")
	}
	t.Log("native main-frame CDP loader identity advances on committed navigation (not grant/generation enforcement)")

	info, err := b.Info(ctx)
	if err != nil || info.URL != fixture.Origin+"/rod-second" || !strings.Contains(info.Title, "Guest") {
		t.Fatalf("info: %+v, %v", info, err)
	}
	if found, err := b.WaitElement(ctx, "#name", true); err != nil || !found {
		t.Fatalf("waitFor: %v, %v", found, err)
	}
	if err := b.Fill(ctx, "#name", "Rod native proof"); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Eval(ctx, "document.querySelector('#name').value"); err != nil || got != `"Rod native proof"` {
		focus, _ := b.Eval(ctx, "JSON.stringify({active: document.activeElement?.id, focused: document.hasFocus(), selection: window.getSelection().toString()})")
		t.Errorf("fill: %s, %v; focus=%s", got, err, focus)
	}
	if err := b.PressKey(ctx, "End"); err != nil {
		t.Fatal(err)
	}
	if err := b.TypeText(ctx, "!"); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Eval(ctx, "document.querySelector('#name').value"); err != nil || got != `"Rod native proof!"` {
		t.Errorf("type/key: %s, %v", got, err)
	}
	tree, err := b.AXTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var nodes []struct {
		Role string `json:"role"`
		Name string `json:"name"`
		ID   int    `json:"backendDOMNodeId"`
	}
	if err := json.Unmarshal([]byte(tree), &nodes); err != nil {
		t.Fatal(err)
	}
	buttonID := 0
	for _, node := range nodes {
		if node.Role == "button" && node.Name == "Act" {
			buttonID = node.ID
		}
	}
	if buttonID == 0 {
		t.Fatal("AX tree did not expose the guest button")
	}
	x, y, err := b.BoxModel(ctx, buttonID)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ClickAt(ctx, x, y); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Eval(ctx, "document.querySelector('#result').textContent"); err != nil || got != `"clicked"` {
		t.Fatalf("AX -> box -> click: %s, %v", got, err)
	}
	if got, err := b.Eval(ctx, "document.querySelector('iframe').contentDocument.querySelector('#frame-input').getAttribute('aria-label')"); err != nil || got != `"Frame input"` {
		t.Fatalf("same-origin subframe DOM: %s, %v", got, err)
	}
	shot, err := b.Screenshot(ctx, 640)
	if err != nil {
		t.Fatal(err)
	}
	image, err := jpeg.DecodeConfig(bytes.NewReader(shot))
	if err != nil || image.Width == 0 || image.Height == 0 || max(image.Width, image.Height) > 640 {
		t.Errorf("bounded JPEG screenshot: %+v, %v", image, err)
	}
	t.Logf("existing Backend helpers + native guest JPEG: %dx%d, %d bytes", image.Width, image.Height, len(shot))
	tabs, err := b.Tabs(ctx)
	if err != nil || len(tabs) != 1 || tabs[0].TargetID != fixture.TargetID {
		t.Fatalf("scoped tabs: %+v, %v", tabs, err)
	}
	if err := b.UseTab(ctx, "private-app-target"); err == nil {
		t.Fatal("useTab escaped the granted guest")
	}
	if err := b.UploadFiles(ctx, "#upload", []string{"/definitely-not-authorized/mac-file"}); err == nil {
		t.Fatal("upload must reject Mac filesystem paths")
	}
	for _, method := range []string{
		"Browser.close", "Browser.setDownloadBehavior", "Target.createTarget",
		"Storage.getCookies", "Network.getAllCookies", "Network.getCookies",
		"DOM.setFileInputFiles", "Page.printToPDF", "Target.exposeDevToolsProtocol",
	} {
		if _, err := client.Call(ctx, fixture.SessionID, method, map[string]any{}); err == nil {
			t.Errorf("native shim admitted forbidden method %s", method)
		}
	}
	if _, err := client.Call(ctx, "foreign-session", "Runtime.evaluate", map[string]any{"expression": "1"}); err == nil {
		t.Fatal("native shim admitted foreign session")
	}
	if _, err := client.Call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": "private-app-target", "flatten": true}); err == nil {
		t.Fatal("native shim admitted foreign target")
	}

	short, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	start := time.Now()
	_, err = b.Eval(short, "new Promise(resolve => setTimeout(() => { window.lateEffect = true; resolve(true) }, 350))")
	stop()
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("CDP wait cancellation: %v after %v", err, time.Since(start))
	}
	// This deliberately proves the LIMIT of transport cancellation: already
	// delivered page work can still mutate. Production must say outcome_unknown.
	got, err := b.Eval(ctx, "new Promise(resolve => setTimeout(() => resolve(window.lateEffect === true), 500))")
	if err != nil || got != "true" {
		t.Fatalf("expected delivered mutation to outlive cancelled wait: %s, %v", got, err)
	}
	t.Log("context cancellation bounds caller wait; delivered page mutation still completes: outcome_unknown required")
	t.Logf("observed CDP methods (includes deliberately rejected probes): %s", strings.Join(client.methods(), ", "))
}

type desktopRecordingClient struct {
	rod.CDPClient
	mu   sync.Mutex
	seen []string
}

func (c *desktopRecordingClient) Call(ctx context.Context, session, method string, params any) ([]byte, error) {
	c.mu.Lock()
	if !slices.Contains(c.seen, method) {
		c.seen = append(c.seen, method)
	}
	c.mu.Unlock()
	return c.CDPClient.Call(ctx, session, method, params)
}

func (c *desktopRecordingClient) methods() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	methods := slices.Clone(c.seen)
	slices.Sort(methods)
	return methods
}
