// Package browser implements explicit native Chrome and scoped Desktop drivers.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// Mode selects which browser whipcode drives.
type Mode string

const (
	// ModeLive attaches only to the explicitly configured live endpoint or
	// profile. It never launches or closes human Chrome.
	ModeLive Mode = "live"
	// ModeDedicated launches a separate Chrome instance with a whip-owned
	// profile under the explicitly owned native runtime directory.
	ModeDedicated Mode = "dedicated"
	// ModeHeadless is ModeDedicated without a window.
	ModeHeadless Mode = "headless"
	// ModeExtension drives the user's real, logged-in Chrome tab through the
	// whipcode extension (chrome.debugger CDP tunnel via extrelay). The only way
	// to drive the default profile on Chrome ≥ 136, where direct CDP is
	// blocked. Requires the unpacked extension loaded + a tab pinned via the
	// toolbar icon (`whipcode browser install` sets it up).
	ModeExtension Mode = "extension"
)

// ErrPermissionBlocked reports Chrome 144+'s per-connection "Allow remote
// debugging?" popup (or the chrome://inspect toggle being off) standing
// between whipcode and a live browser. The user must act in Chrome; retry
// after they confirm.
var ErrPermissionBlocked = errors.New("chrome permission-blocked")

// ErrNoLiveBrowser means the explicitly selected live browser is unavailable.
var ErrNoLiveBrowser = errors.New("no live browser with remote debugging found")

// Backend is the browser driver contract. *Browser implements it with rod;
// tests substitute fakes, and native hosts select Rod or ChromeDP explicitly.
type Backend interface {
	// Info reports the attached page's URL, title, viewport, and scroll
	// position, or the pending native JS dialog when one is open.
	Info(ctx context.Context) (PageInfo, error)
	// Navigate loads url in the controlled tab (creating it if needed) and
	// waits for load.
	Navigate(ctx context.Context, url string) error
	// Back steps the tab history.
	Back(ctx context.Context) error
	// Eval evaluates a JS expression in the tab and returns the JSON value.
	Eval(ctx context.Context, expression string) (string, error)
	// ClickAt dispatches trusted mouse events at viewport coordinates.
	ClickAt(ctx context.Context, x, y float64) error
	// TypeText inserts text into the focused element.
	TypeText(ctx context.Context, text string) error
	// PressKey sends a named key (Enter, Tab, Backspace, ArrowDown, …) with
	// real key events so framework listeners fire.
	PressKey(ctx context.Context, key string) error
	// Fill focuses a selector, clears it, and types — safe for
	// React/Vue-controlled inputs.
	Fill(ctx context.Context, selector, text string) error
	// Scroll dispatches a mouse-wheel event at the viewport center.
	Scroll(ctx context.Context, dy float64) error
	// WaitLoad blocks until document.readyState is complete or ctx expires.
	WaitLoad(ctx context.Context) error
	// WaitElement polls for a selector; visible additionally requires layout.
	WaitElement(ctx context.Context, selector string, visible bool) (bool, error)
	// Screenshot returns a JPEG of the viewport, downscaled so no side
	// exceeds maxDim (0 = no limit) — sized for direct use as a vision
	// image part.
	Screenshot(ctx context.Context, maxDim int) (jpeg []byte, err error)
	// AXTree returns the page's accessibility tree as compact JSON
	// (role/name/backendDOMNodeId per node). Large — callers filter.
	AXTree(ctx context.Context) (string, error)
	// BoxModel returns the viewport-px center of the node, for AXTree →
	// ClickAt workflows.
	BoxModel(ctx context.Context, backendNodeID int) (x, y float64, err error)
	// Tabs lists open page targets.
	Tabs(ctx context.Context) ([]Tab, error)
	// UseTab switches control to the given target id.
	UseTab(ctx context.Context, targetID string) error
	// UploadFiles sets files on a file input matched by selector.
	UploadFiles(ctx context.Context, selector string, paths []string) error
	// Close delegates to the native owner of the exact captured transport.
	Close() error
	// Mode returns the captured mode for destination policy.
	Mode() Mode
	// HandleDialog accepts or dismisses the next pending native JS dialog,
	// blocking briefly for one to appear.
	HandleDialog(accept bool, promptText string) error
}

// PageInfo mirrors browser-harness's page_info() helper.
type PageInfo struct {
	URL, Title            string
	Width, Height         int
	ScrollX, ScrollY      float64
	PageWidth, PageHeight float64
	Dialog                *Dialog `json:",omitempty"`
}

// Dialog is a pending native JS dialog (alert/confirm/prompt/beforeunload).
// The page's JS thread is frozen until HandleDialog responds.
type Dialog struct {
	Type, Message, DefaultPrompt string
}

// Tab is one open page target.
type Tab struct {
	TargetID, Title, URL string
}

// Browser owns one rod browser connection plus the controlled page.
type Browser struct {
	mode           Mode
	browser        *rod.Browser
	page           *rod.Page
	closeTransport func() error
}

// Driver names for the browser subsystem's two implementations.
const (
	DriverRod      = "rod"      // default — battle-tested here
	DriverChromedp = "chromedp" // the spike fallback (chromedp-spike branch)
)

// internalURL matches browser-harness's INTERNAL prefix set.
func internalURL(u string) bool {
	for _, p := range []string{"chrome://", "chrome-untrusted://", "devtools://", "chrome-extension://", "about:"} {
		if strings.HasPrefix(u, p) {
			return true
		}
	}
	return false
}

// attachPage selects a supported target only within the captured connection:
// first a real page, then a reusable blank page, otherwise a new blank target.
func (b *Browser) attachPage() error {
	pages, err := b.browser.Pages()
	if err != nil {
		return fmt.Errorf("list pages: %w", err)
	}
	var blank *rod.Page
	for _, p := range pages {
		info, err := p.Info()
		if err != nil {
			continue
		}
		if info.Type != "page" {
			continue
		}
		if !internalURL(info.URL) {
			b.page = p
			break
		}
		if blank == nil && (info.URL == "about:blank" || strings.HasPrefix(info.URL, "chrome://newtab") || strings.HasPrefix(info.URL, "edge://newtab")) {
			blank = p
		}
	}
	if b.page == nil {
		b.page = blank
	}
	if b.page == nil {
		b.page, err = b.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
		if err != nil {
			return fmt.Errorf("create tab: %w", err)
		}
	}
	// ponytail: browser-harness enables Page/DOM/Runtime/Network up front;
	// rod enables domains lazily per call. Skip the explicit enable (it
	// deadlocked against headless-shell's event stream in e2e).
	return nil
}

// Mode returns the session's browser mode (for mode-dependent policy).
func (b *Browser) Mode() Mode { return b.mode }

// Close releases only the transport and process held by this native owner.
func (b *Browser) Close() error {
	if b.closeTransport != nil {
		return b.closeTransport()
	}
	return nil
}

func (b *Browser) Info(ctx context.Context) (PageInfo, error) {
	if d, err := b.pendingDialog(ctx); err == nil && d != nil {
		return PageInfo{Dialog: d}, nil
	}
	res, err := b.runtimeEval(ctx, `JSON.stringify({url:location.href,title:document.title,w:innerWidth,h:innerHeight,sx:scrollX,sy:scrollY,pw:document.documentElement.scrollWidth,ph:document.documentElement.scrollHeight})`)
	if err != nil {
		return PageInfo{}, err
	}
	var raw struct {
		URL, Title     string
		W, H           int
		SX, SY, PW, PH float64
	}
	if err := json.Unmarshal([]byte(res.Result.Value.String()), &raw); err != nil {
		return PageInfo{}, err
	}
	return PageInfo{URL: raw.URL, Title: raw.Title, Width: raw.W, Height: raw.H, ScrollX: raw.SX, ScrollY: raw.SY, PageWidth: raw.PW, PageHeight: raw.PH}, nil
}

func (b *Browser) pendingDialog(ctx context.Context) (*Dialog, error) {
	// ponytail: browser-harness buffers Page.javascriptDialogOpening events on
	// a background reader; v1 surfaces dialogs only through HandleDialog when
	// an action hangs on one. Generalize to an event buffer if agents trip
	// on unexpected alerts.
	return nil, nil //nolint:nilnil // nil dialog = none pending; Info's `err == nil && d != nil` check relies on that contract
}

// HandleDialog accepts or dismisses the next pending native dialog,
// blocking up to 2s for one to appear.
func (b *Browser) HandleDialog(accept bool, promptText string) error {
	wait, handle := b.page.Timeout(2 * time.Second).HandleDialog()
	_ = wait()
	return handle(&proto.PageHandleJavaScriptDialog{Accept: accept, PromptText: promptText})
}

func (b *Browser) Navigate(ctx context.Context, url string) error {
	if b.page == nil {
		return errors.New("no attached tab")
	}
	p := b.page.Context(ctx)
	if err := p.Navigate(url); err != nil {
		return err
	}
	// Poll readyState via our raw-CDP eval instead of rod's WaitLoad (which
	// evals a rAF-loop helper that wedges against some page/server combos).
	return b.WaitLoad(ctx)
}

func (b *Browser) Back(ctx context.Context) error {
	p := b.page.Context(ctx)
	if err := p.NavigateBack(); err != nil {
		return err
	}
	return b.WaitLoad(ctx)
}

// Eval runs Runtime.evaluate directly (ByValue + AwaitPromise, per
// browser-harness's js()): the expression is evaluated as-is, with an
// IIFE-retry when Chrome reports an illegal top-level return — both
// "document.title" and "const x = 1; return x" work. rod's page.Eval wraps
// the snippet as a function body, which breaks bare expressions.
func (b *Browser) Eval(ctx context.Context, expression string) (string, error) {
	res, err := b.runtimeEval(ctx, expression)
	if err != nil && strings.Contains(err.Error(), "Illegal return statement") {
		res, err = b.runtimeEval(ctx, "(function(){"+expression+"})()")
	}
	if err != nil {
		return "", err
	}
	if res.ExceptionDetails != nil || (res.Result.Subtype == "error") {
		desc := res.Result.Description
		if res.ExceptionDetails != nil && res.ExceptionDetails.Text != "" {
			desc = res.ExceptionDetails.Text + ": " + desc
		}
		return "", fmt.Errorf("JavaScript evaluation failed: %s; expression: %.160s", desc, expression)
	}
	if res.Result.Type == proto.RuntimeRemoteObjectTypeUndefined {
		return "null", nil
	}
	data, err := res.Result.Value.MarshalJSON()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (b *Browser) runtimeEval(ctx context.Context, expression string) (*proto.RuntimeEvaluateResult, error) {
	return proto.RuntimeEvaluate{
		Expression:    expression,
		ReturnByValue: true,
		AwaitPromise:  true,
	}.Call(b.page.Context(ctx))
}

func (b *Browser) ClickAt(ctx context.Context, x, y float64) error {
	m := proto.InputDispatchMouseEvent{Type: proto.InputDispatchMouseEventTypeMousePressed, X: x, Y: y, Button: proto.InputMouseButtonLeft, ClickCount: 1}
	if err := m.Call(b.page.Context(ctx)); err != nil {
		return err
	}
	m.Type = proto.InputDispatchMouseEventTypeMouseReleased
	return m.Call(b.page.Context(ctx))
}

func (b *Browser) TypeText(ctx context.Context, text string) error {
	return proto.InputInsertText{Text: text}.Call(b.page.Context(ctx))
}

// keyDefs maps key names to codes, per browser-harness's _KEYS table.
var keyDefs = map[string]struct {
	Code string
	Key  int
	Text string
}{
	"Enter":      {"Enter", 13, "\r"},
	"Tab":        {"Tab", 9, "\t"},
	"Backspace":  {"Backspace", 8, ""},
	"Escape":     {"Escape", 27, ""},
	"Delete":     {"Delete", 46, ""},
	" ":          {"Space", 32, " "},
	"ArrowLeft":  {"ArrowLeft", 37, ""},
	"ArrowUp":    {"ArrowUp", 38, ""},
	"ArrowRight": {"ArrowRight", 39, ""},
	"ArrowDown":  {"ArrowDown", 40, ""},
	"Home":       {"Home", 36, ""},
	"End":        {"End", 35, ""},
	"PageUp":     {"PageUp", 33, ""},
	"PageDown":   {"PageDown", 34, ""},
}

// PressKey sends platform-neutral named keys and trusted Unicode text.
func (b *Browser) PressKey(ctx context.Context, key string) error {
	def, named := keyDefs[key]
	if !named && utf8.RuneCountInString(key) != 1 {
		return fmt.Errorf("unknown key %q", key)
	}
	p := b.page.Context(ctx)
	down := proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeKeyDown, Key: key}
	if named {
		down.Code, down.WindowsVirtualKeyCode = def.Code, def.Key
	}
	if err := down.Call(p); err != nil {
		return err
	}
	if !named || key == " " {
		if err := b.TypeText(ctx, key); err != nil {
			return err
		}
	} else if def.Text != "" {
		if err := (proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeChar, Key: key, Code: def.Code, Text: def.Text}).Call(p); err != nil {
			return err
		}
	}
	down.Type = proto.InputDispatchKeyEventTypeKeyUp
	return down.Call(p)
}

// Fill replaces the selected value using trusted text insertion.
func (b *Browser) Fill(ctx context.Context, selector, text string) error {
	return fillInput(ctx, b, selector, text)
}

func (b *Browser) Scroll(ctx context.Context, dy float64) error {
	info, err := b.Info(ctx)
	if err != nil {
		return err
	}
	return (proto.InputDispatchMouseEvent{
		Type: proto.InputDispatchMouseEventTypeMouseWheel,
		X:    float64(info.Width) / 2, Y: float64(info.Height) / 2,
		DeltaX: 0, DeltaY: dy,
	}).Call(b.page.Context(ctx))
}

// WaitLoad polls document.readyState == complete (helpers.py wait_for_load)
// with a 15s cap when ctx has no earlier deadline.
func (b *Browser) WaitLoad(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		res, err := b.Eval(ctx, "document.readyState")
		if err == nil && res == `"complete"` {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// WaitElement polls for a selector, per helpers.py wait_for_element.
func (b *Browser) WaitElement(ctx context.Context, selector string, visible bool) (bool, error) {
	sel, _ := json.Marshal(selector)
	expr := fmt.Sprintf(`!!document.querySelector(%s)`, sel)
	if visible {
		expr = fmt.Sprintf(`(()=>{const e=document.querySelector(%s);if(!e)return false;if(typeof e.checkVisibility==='function')return e.checkVisibility({checkOpacity:true,checkVisibilityCSS:true});const s=getComputedStyle(e);return s.display!=='none'&&s.visibility!=='hidden'&&s.opacity!=='0'})()`, sel)
	}
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	for {
		res, err := b.Eval(ctx, expr)
		if err != nil {
			return false, err
		}
		if res == "true" {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, nil
		case <-t.C:
		}
	}
}

// Screenshot captures the viewport as JPEG (quality 80), downscaled via the
// clip scale factor so no side exceeds maxDim — the vision-embed ladder from
// browser-use's _native_screenshot_result (1568px keeps under model caps).
func (b *Browser) Screenshot(ctx context.Context, maxDim int) ([]byte, error) {
	quality := 80
	p := b.page.Context(ctx)
	req := &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatJpeg, Quality: &quality}
	if maxDim > 0 {
		metrics, err := proto.PageGetLayoutMetrics{}.Call(p)
		if err == nil && metrics.CSSLayoutViewport != nil {
			w := float64(metrics.CSSLayoutViewport.ClientWidth)
			h := float64(metrics.CSSLayoutViewport.ClientHeight)
			scale := 1.0
			if max(w, h) > float64(maxDim) {
				scale = float64(maxDim) / max(w, h)
			}
			req.Clip = &proto.PageViewport{X: 0, Y: 0, Width: w, Height: h, Scale: scale}
		}
	}
	shot, err := req.Call(p)
	if err != nil {
		return nil, err
	}
	return shot.Data, nil
}

// AXTree returns the full accessibility tree as compact JSON, filtered to
// the fields an agent needs (role, name, backendDOMNodeId) — per
// browser-harness's cdp("Accessibility.getFullAXTree") guidance, pre-filtered
// in Go instead of Python.
func (b *Browser) AXTree(ctx context.Context) (string, error) {
	res, err := proto.AccessibilityGetFullAXTree{}.Call(b.page.Context(ctx))
	if err != nil {
		return "", err
	}
	type node struct {
		Role          string `json:"role"`
		Name          string `json:"name"`
		BackendNodeID int    `json:"backendDOMNodeId"`
	}
	out := make([]node, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		if n.Ignored {
			continue
		}
		nn := node{BackendNodeID: int(n.BackendDOMNodeID)}
		if n.Role != nil {
			nn.Role = n.Role.Value.String()
		}
		if n.Name != nil {
			nn.Name = n.Name.Value.String()
		}
		if nn.Role == "" && nn.Name == "" {
			continue
		}
		out = append(out, nn)
	}
	data, err := json.Marshal(out)
	return string(data), err
}

// BoxModel returns the viewport-px center of a node's content quad.
func (b *Browser) BoxModel(ctx context.Context, backendNodeID int) (float64, float64, error) {
	res, err := proto.DOMGetBoxModel{BackendNodeID: proto.DOMBackendNodeID(backendNodeID)}.Call(b.page.Context(ctx))
	if err != nil {
		return 0, 0, err
	}
	q := res.Model.Content // [x1,y1,x2,y2,x3,y3,x4,y4]
	var sx, sy float64
	for i := range 4 {
		sx += q[i*2]
		sy += q[i*2+1]
	}
	return sx / 4, sy / 4, nil
}

func (b *Browser) Tabs(ctx context.Context) ([]Tab, error) {
	pages, err := b.browser.Context(ctx).Pages()
	if err != nil {
		return nil, err
	}
	var out []Tab
	for _, p := range pages {
		info, err := p.Info()
		if err != nil || info.Type != "page" {
			continue
		}
		out = append(out, Tab{TargetID: string(p.TargetID), Title: info.Title, URL: info.URL})
	}
	return out, nil
}

func (b *Browser) UseTab(ctx context.Context, targetID string) error {
	page, err := b.browser.Context(ctx).PageFromTarget(proto.TargetTargetID(targetID))
	if err != nil {
		return err
	}
	b.page = page
	return nil
}

// UploadFiles sets files on an <input type=file> (helpers.py upload_file).
func (b *Browser) UploadFiles(ctx context.Context, selector string, paths []string) error {
	p := b.page.Context(ctx)
	el, err := p.Element(selector)
	if err != nil {
		return fmt.Errorf("upload: element not found: %s", selector)
	}
	return el.SetFiles(paths)
}
