// chromedp.go implements the Backend interface on chromedp — the spike
// driver for the head-to-head against rod (.ai-docs/plans/chromedp-spike).
// Same raw-CDP posture as the rod backend: most methods are thin wrappers
// over cdproto calls run through chromedp.Run on a target-bound context.

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// chromedpBackend holds a chromedp allocator (browser-level) plus a
// target-bound context for the controlled tab.
type chromedpBackend struct {
	mode      Mode
	allocCtx  context.Context
	allocStop context.CancelFunc
	// browserCtx is the first chromedp context: it owns the browser
	// connection, so cancelling it closes the browser (or the launched
	// process). It lives until Close.
	browserCtx context.Context
	browserOff context.CancelFunc
	// targetCtx drives the controlled tab: browserCtx itself, or a child
	// bound to the tab UseTab picked (tabOff detaches that child).
	targetCtx context.Context
	tabOff    context.CancelFunc
	closed    bool
	obtained  Obtained
}

// openChromedp connects per mode: live attaches via the discovered WS URL
// (remote allocator), falling back hermes-style to a launched dedicated
// instance when none is debuggable; dedicated/headless reattach to a
// still-running whipcode Chrome for the profile, else launch via the default
// allocator (chromedp's own launcher, headed off in headless mode).
func openChromedp(ctx context.Context, mode Mode, sessionName string, env []string) (*chromedpBackend, error) {
	b := &chromedpBackend{mode: mode}
	var allocCtx context.Context
	var cancel context.CancelFunc
	switch mode {
	case ModeLive:
		ws, err := DiscoverLiveWS(ctx)
		if err != nil {
			if !errors.Is(err, ErrNoLiveBrowser) {
				return nil, err
			}
			return openChromedp(ctx, ModeDedicated, sessionName, env) // fallback
		}
		allocCtx, cancel = remote.NewAllocator(ctx, ws)
		b.obtained = ObtainedLive
	case ModeDedicated, ModeHeadless:
		if env != nil {
			return nil, errors.New("chromedp cannot launch with an isolated environment; use the rod driver")
		}
		// Note: chromedp dedicated does NOT reattach — Close kills its Chrome
		// (ExecAllocator cancel kills the process; there's no detach-only
		// path as with rod). A prior detached dedicated Chrome belongs to the
		// rod driver; chromedp just launches fresh.
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", mode == ModeHeadless),
			chromedp.Flag("remote-debugging-port", "0"), // string — int hits "invalid exec pool flag"
			// chromedp >= 0.21 defaults to a pipe; a debugging port needs the
			// websocket transport, which keeps the 0.16 launch behavior.
			remote.WebSocket,
		)
		if home, err := os.UserHomeDir(); err == nil {
			opts = append(opts, chromedp.UserDataDir(dedicatedProfileDir(home, sessionName)))
		}
		if bin := os.Getenv("ROD_BROWSER_BIN"); bin != "" { // same override hook
			opts = append(opts, chromedp.ExecPath(bin))
		}
		allocCtx, cancel = chromedp.NewExecAllocator(ctx, opts...)
		b.obtained = ObtainedLaunched
	default:
		return nil, fmt.Errorf("unknown browser mode %q", mode)
	}
	b.allocCtx, b.allocStop = allocCtx, cancel

	browserCtx, browserOff := chromedp.NewContext(allocCtx)
	b.browserCtx, b.browserOff, b.targetCtx = browserCtx, browserOff, browserCtx
	// Ensure the browser + first tab exist.
	if err := chromedp.Do(browserCtx); err != nil {
		_ = b.Close()
		return nil, fmt.Errorf("chromedp connect: %w", err)
	}
	return b, nil
}

func (b *chromedpBackend) run(ctx context.Context, f func(ctx context.Context, t *chromedp.Target) error) error {
	c, off := b.bound(ctx)
	defer off()
	return chromedp.Do(c, chromedp.Func(f))
}

// bound is the target-bound context that chromedp actions execute on; the
// caller's ctx bounds it via a deadline wrap when present.
func (b *chromedpBackend) bound(ctx context.Context) (context.Context, context.CancelFunc) {
	if d, ok := ctx.Deadline(); ok {
		return context.WithDeadline(b.targetCtx, d)
	}
	return b.targetCtx, func() {}
}

func ptr[T any](v T) *T { return &v }

func (b *chromedpBackend) Info(ctx context.Context) (PageInfo, error) {
	var rawJSON string
	err := b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		var err error
		rawJSON, err = chromedp.Evaluate[string](`JSON.stringify({url:location.href,title:document.title,w:innerWidth,h:innerHeight,sx:scrollX,sy:scrollY,pw:document.documentElement.scrollWidth,ph:document.documentElement.scrollHeight})`)(ctx, t)
		return err
	})
	if err != nil {
		return PageInfo{}, err
	}
	var raw struct {
		URL, Title     string
		W, H           int
		SX, SY, PW, PH float64
	}
	// chromedp.Evaluate unmarshals the JS value into the target.
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		return PageInfo{}, err
	}
	return PageInfo{URL: raw.URL, Title: raw.Title, Width: raw.W, Height: raw.H, ScrollX: raw.SX, ScrollY: raw.SY, PageWidth: raw.PW, PageHeight: raw.PH}, nil
}

func (b *chromedpBackend) Navigate(ctx context.Context, url string) error {
	c, off := b.bound(ctx)
	defer off()
	if err := chromedp.Do(c, chromedp.Navigate(url)); err != nil {
		return err
	}
	return b.WaitLoad(ctx)
}

func (b *chromedpBackend) Back(ctx context.Context) error {
	var entries []*page.NavigationEntry
	var current int
	err := b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		res, err := cdp.Call(ctx, t, page.GetNavigationHistory, cdp.Empty{})
		if err != nil {
			return err
		}
		current, entries = int(res.CurrentIndex), res.Entries
		return nil
	})
	if err != nil {
		return err
	}
	if current <= 0 {
		return nil
	}
	prev := entries[current-1]
	return b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, page.NavigateToHistoryEntry, page.NavigateToHistoryEntryParams{EntryID: prev.ID})
		return err
	})
}

func (b *chromedpBackend) Eval(ctx context.Context, expression string) (string, error) {
	var res *runtime.RemoteObject
	var exp *runtime.ExceptionDetails
	err := b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		r, err := cdp.Call(ctx, t, runtime.Evaluate, runtime.EvaluateParams{
			Expression:    expression,
			ReturnByValue: ptr(true),
			AwaitPromise:  ptr(true),
		})
		res, exp = r.Result, r.ExceptionDetails
		return err
	})
	if err != nil {
		return "", err
	}
	if exp != nil {
		desc := exp.Text
		if exp.Exception != nil && exp.Exception.Description != "" {
			desc = exp.Exception.Description
		}
		// browser-harness's js() retries illegal top-level return in an IIFE,
		// as the rod backend does. Chrome reports the SyntaxError as exception
		// details, not as a protocol error.
		if strings.Contains(desc, "Illegal return statement") && !strings.HasPrefix(expression, "(function(){") {
			return b.Eval(ctx, "(function(){"+expression+"})()")
		}
		return "", fmt.Errorf("JavaScript evaluation failed: %s; expression: %.160s", desc, expression)
	}
	if res == nil || res.Type == "undefined" {
		return "null", nil
	}
	return string(res.Value), nil
}

func (b *chromedpBackend) ClickAt(ctx context.Context, x, y float64) error {
	press := input.DispatchMouseEventParams{Type: chromedp.MousePressed, X: x, Y: y, Button: input.MouseButtonLeft, ClickCount: 1}
	release := press
	release.Type = chromedp.MouseReleased
	return b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		if _, err := cdp.Call(ctx, t, input.DispatchMouseEvent, press); err != nil {
			return err
		}
		_, err := cdp.Call(ctx, t, input.DispatchMouseEvent, release)
		return err
	})
}

func (b *chromedpBackend) TypeText(ctx context.Context, text string) error {
	return b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, input.InsertText, input.InsertTextParams{Text: text})
		return err
	})
}

// cdKeys mirrors the rod backend's keyDefs table.
var cdKeys = map[string]struct {
	code string
	key  int64
	text string
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

func (b *chromedpBackend) PressKey(ctx context.Context, name string) error {
	def, ok := cdKeys[name]
	if !ok && len(name) == 1 {
		def = struct {
			code string
			key  int64
			text string
		}{name, int64(name[0]), name}
		ok = true
	}
	if !ok {
		return fmt.Errorf("unknown key %q", name)
	}
	return b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		key := func(p input.DispatchKeyEventParams) error {
			_, err := cdp.Call(ctx, t, input.DispatchKeyEvent, p)
			return err
		}
		down := input.DispatchKeyEventParams{
			Type:                  input.DispatchKeyEventTypeKeyDown,
			Key:                   name,
			Code:                  def.code,
			WindowsVirtualKeyCode: def.key,
			NativeVirtualKeyCode:  def.key,
		}
		if err := key(down); err != nil {
			return err
		}
		if def.text != "" {
			if err := key(input.DispatchKeyEventParams{Type: input.DispatchKeyEventTypeChar, Text: def.text, Key: name, Code: def.code}); err != nil {
				return err
			}
		}
		return key(input.DispatchKeyEventParams{Type: input.DispatchKeyEventTypeKeyUp, Key: name, Code: def.code, WindowsVirtualKeyCode: def.key, NativeVirtualKeyCode: def.key})
	})
}

func (b *chromedpBackend) Fill(ctx context.Context, selector, text string) error {
	sel, _ := json.Marshal(selector)
	focused, err := b.Eval(ctx, fmt.Sprintf(`(()=>{const e=document.querySelector(%s);if(!e)return false;e.focus();return true})()`, sel))
	if err != nil {
		return err
	}
	if focused != "true" {
		return fmt.Errorf("fill: element not found: %s", selector)
	}
	if _, err := b.Eval(ctx, fmt.Sprintf(`(()=>{const e=document.querySelector(%s);if(!e)return;const s=window.getSelection(),r=document.createRange();e.select&&e.select();r.selectNodeContents(e);s.removeAllRanges();s.addRange(r)})()`, sel)); err != nil {
		return err
	}
	if err := b.PressKey(ctx, "Backspace"); err != nil {
		return err
	}
	for _, ch := range text {
		if err := b.PressKey(ctx, string(ch)); err != nil {
			return err
		}
	}
	_, err = b.Eval(ctx, fmt.Sprintf(`(()=>{const e=document.querySelector(%s);if(!e)return;e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}))})()`, sel))
	return err
}

// wheelParams is Input.dispatchMouseEvent for a wheel. Chrome requires both
// deltas on a mouseWheel, but cdproto's DispatchMouseEventParams tags them
// omitzero, so a vertical-only scroll (deltaX 0) is rejected with "'deltaX'
// and 'deltaY' are expected for mouseWheel event".
type wheelParams struct {
	Type   input.DispatchMouseEventType `json:"type"`
	X      float64                      `json:"x"`
	Y      float64                      `json:"y"`
	DeltaX float64                      `json:"deltaX"`
	DeltaY float64                      `json:"deltaY"`
}

var wheelEvent = cdp.Command[wheelParams, cdp.Empty]{Method: input.CommandDispatchMouseEvent}

func (b *chromedpBackend) Scroll(ctx context.Context, dy float64) error {
	info, err := b.Info(ctx)
	if err != nil {
		return err
	}
	x, y := float64(info.Width)/2, float64(info.Height)/2
	return b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, wheelEvent, wheelParams{Type: chromedp.MouseWheel, X: x, Y: y, DeltaX: 0, DeltaY: dy})
		return err
	})
}

func (b *chromedpBackend) WaitLoad(ctx context.Context) error {
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

func (b *chromedpBackend) WaitElement(ctx context.Context, selector string, visible bool) (bool, error) {
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

func (b *chromedpBackend) Screenshot(ctx context.Context, maxDim int) ([]byte, error) {
	var data []byte
	err := b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		params := page.CaptureScreenshotParams{Format: page.CaptureScreenshotFormatJpeg, Quality: ptr[int64](80)}
		if maxDim > 0 {
			metrics, err := cdp.Call(ctx, t, page.GetLayoutMetrics, cdp.Empty{})
			if cssLayout := metrics.CSSLayoutViewport; err == nil && cssLayout != nil {
				w, h := float64(cssLayout.ClientWidth), float64(cssLayout.ClientHeight)
				if max(w, h) > float64(maxDim) {
					scale := float64(maxDim) / max(w, h)
					params.Clip = &page.Viewport{X: 0, Y: 0, Width: w, Height: h, Scale: scale}
				}
			}
		}
		res, err := cdp.Call(ctx, t, page.CaptureScreenshot, params)
		data = res.Data
		return err
	})
	return data, err
}

func (b *chromedpBackend) AXTree(ctx context.Context) (string, error) {
	var nodes []*accessibility.Node
	err := b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		res, err := cdp.Call(ctx, t, accessibility.GetFullAXTree, accessibility.GetFullAXTreeParams{})
		nodes = res.Nodes
		return err
	})
	if err != nil {
		return "", err
	}
	type node struct {
		Role          string `json:"role"`
		Name          string `json:"name"`
		BackendNodeID int    `json:"backendDOMNodeId"`
	}
	out := make([]node, 0, len(nodes))
	for _, n := range nodes {
		if n.Ignored {
			continue
		}
		nn := node{BackendNodeID: int(n.BackendDOMNodeID)}
		if n.Role != nil {
			nn.Role = axString(n.Role.Value)
		}
		if n.Name != nil {
			nn.Name = axString(n.Name.Value)
		}
		if nn.Role == "" && nn.Name == "" {
			continue
		}
		out = append(out, nn)
	}
	data, err := json.Marshal(out)
	return string(data), err
}

// axString renders an AX value the way the rod backend does: a JSON string
// unquoted, anything else as its JSON text. fmt.Sprint of the raw value kept
// the quotes, so roles read "\"button\"" instead of "button".
func axString(v []byte) string {
	var str string
	if json.Unmarshal(v, &str) == nil {
		return str
	}
	return string(v)
}

func (b *chromedpBackend) BoxModel(ctx context.Context, backendNodeID int) (float64, float64, error) {
	var model *dom.BoxModel
	err := b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		res, err := cdp.Call(ctx, t, dom.GetBoxModel, dom.GetBoxModelParams{BackendNodeID: cdp.BackendNodeID(backendNodeID)})
		model = res.Model
		return err
	})
	if err != nil {
		return 0, 0, err
	}
	q := model.Content
	var sx, sy float64
	for i := range 4 {
		sx += q[i*2]
		sy += q[i*2+1]
	}
	return sx / 4, sy / 4, nil
}

func (b *chromedpBackend) Tabs(ctx context.Context) ([]Tab, error) {
	c, off := b.bound(ctx)
	defer off()
	infos, err := chromedp.Targets(c)
	if err != nil {
		return nil, err
	}
	var out []Tab
	for _, ti := range infos {
		if ti.Type != "page" {
			continue
		}
		out = append(out, Tab{TargetID: string(ti.TargetID), Title: ti.Title, URL: ti.URL})
	}
	return out, nil
}

func (b *chromedpBackend) UseTab(ctx context.Context, targetID string) error {
	// A child of browserCtx shares its browser. Deriving from allocCtx (and
	// cancelling the old target context first) closed the browser and then
	// looked the id up in a fresh one: "No target with given id found".
	// Detach on cancel so switching away leaves the previous tab open.
	targetCtx, tabOff := chromedp.NewContext(b.browserCtx, chromedp.WithTargetID(target.ID(targetID)), chromedp.WithDetachOnCancel())
	if err := chromedp.Do(targetCtx); err != nil {
		tabOff()
		return err
	}
	if b.tabOff != nil {
		b.tabOff()
	}
	b.targetCtx, b.tabOff = targetCtx, tabOff
	return nil
}

func (b *chromedpBackend) UploadFiles(ctx context.Context, selector string, paths []string) error {
	var nodeID cdp.NodeID
	err := b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		doc, err := cdp.Call(ctx, t, dom.GetDocument, dom.GetDocumentParams{Depth: ptr[int64](-1)})
		if err != nil {
			return err
		}
		res, err := cdp.Call(ctx, t, dom.QuerySelector, dom.QuerySelectorParams{NodeID: doc.Root.NodeID, Selector: selector})
		nodeID = res.NodeID
		return err
	})
	if err != nil {
		return err
	}
	if nodeID == 0 {
		return fmt.Errorf("upload: element not found: %s", selector)
	}
	return b.run(ctx, func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, dom.SetFileInputFiles, dom.SetFileInputFilesParams{Files: paths, NodeID: nodeID})
		return err
	})
}

// chromedp's ExecAllocator kills Chrome when its context is cancelled —
// there's no detach-only path as there is with rod (see detach.go), and
// whip's default driver is rod. So chromedp dedicated keeps kill-on-Close
// semantics and does NOT reattach: reattach is a rod-driver behavior. This
// preserves chromedp's role as the spike backup without the reflection
// machinery the rod detach requires.
func (b *chromedpBackend) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true
	if b.tabOff != nil {
		b.tabOff()
	}
	if b.browserOff != nil {
		b.browserOff()
	}
	if b.allocStop != nil {
		b.allocStop() // cancels + Allocator.Wait
	}
	// The allocator's Wait returns before the Chrome process has fully
	// exited; give it a beat so profile dirs are releasable (tests clean
	// up TempDirs right after Close; real sessions can relaunch).
	if b.mode != ModeLive {
		time.Sleep(150 * time.Millisecond)
	}
	return nil
}

// Mode returns the backend's mode.
func (b *chromedpBackend) Mode() Mode { return b.mode }

// Obtained reports how this connection was established (Backend).
func (b *chromedpBackend) Obtained() Obtained { return b.obtained }

// HandleDialog answers a pending native dialog. chromedp surfaces dialogs
// through page events; answer the current one if any.
func (b *chromedpBackend) HandleDialog(accept bool, promptText string) error {
	return b.run(context.Background(), func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, page.HandleJavaScriptDialog, page.HandleJavaScriptDialogParams{Accept: accept, PromptText: promptText})
		return err
	})
}
