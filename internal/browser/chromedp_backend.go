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
)

// chromedpBackend holds a chromedp allocator (browser-level) plus a
// target-bound context for the controlled tab.
type chromedpBackend struct {
	mode      Mode
	allocCtx  context.Context
	allocStop context.CancelFunc
	targetCtx context.Context
	targetOff context.CancelFunc
	closed    bool
	obtained  Obtained
}

// openChromedp connects per mode: live attaches via the discovered WS URL
// (remote allocator), falling back hermes-style to a launched dedicated
// instance when none is debuggable; dedicated/headless reattach to a
// still-running whip Chrome for the profile, else launch via the default
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
		allocCtx, cancel = chromedp.NewRemoteAllocator(ctx, ws)
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

	targetCtx, targetOff := chromedp.NewContext(allocCtx)
	b.targetCtx, b.targetOff = targetCtx, targetOff
	// Ensure the browser + first tab exist.
	if err := chromedp.Run(targetCtx); err != nil {
		_ = b.Close()
		return nil, fmt.Errorf("chromedp connect: %w", err)
	}
	return b, nil
}

func (b *chromedpBackend) run(ctx context.Context, actions ...chromedp.Action) error {
	// chromedp.Run executes on the target-bound context; the caller's ctx
	// bounds it via a deadline wrap when present.
	c := b.targetCtx
	if d, ok := ctx.Deadline(); ok {
		var off context.CancelFunc
		c, off = context.WithDeadline(b.targetCtx, d)
		defer off()
	}
	return chromedp.Run(c, actions...)
}

func (b *chromedpBackend) Info(ctx context.Context) (PageInfo, error) {
	var rawJSON string
	err := b.run(ctx, chromedp.Evaluate(`JSON.stringify({url:location.href,title:document.title,w:innerWidth,h:innerHeight,sx:scrollX,sy:scrollY,pw:document.documentElement.scrollWidth,ph:document.documentElement.scrollHeight})`, &rawJSON))
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
	if err := b.run(ctx, chromedp.Navigate(url)); err != nil {
		return err
	}
	return b.WaitLoad(ctx)
}

func (b *chromedpBackend) Back(ctx context.Context) error {
	var entries []*page.NavigationEntry
	var current int
	err := b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		cur, ents, err := page.GetNavigationHistory().Do(ctx)
		if err != nil {
			return err
		}
		current, entries = int(cur), ents
		return nil
	}))
	if err != nil {
		return err
	}
	if current <= 0 {
		return nil
	}
	prev := entries[current-1]
	return b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return page.NavigateToHistoryEntry(prev.ID).Do(ctx)
	}))
}

func (b *chromedpBackend) Eval(ctx context.Context, expression string) (string, error) {
	var res *runtime.RemoteObject
	var exp *runtime.ExceptionDetails
	err := b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		res, exp, err = runtime.Evaluate(expression).
			WithReturnByValue(true).
			WithAwaitPromise(true).
			Do(ctx)
		return err
	}))
	if err != nil {
		// browser-harness's js() retries illegal top-level return in an IIFE.
		if strings.Contains(err.Error(), "Illegal return statement") {
			return b.Eval(ctx, "(function(){"+expression+"})()")
		}
		return "", err
	}
	if exp != nil {
		desc := exp.Text
		if exp.Exception != nil && exp.Exception.Description != "" {
			desc = exp.Exception.Description
		}
		return "", fmt.Errorf("JavaScript evaluation failed: %s; expression: %.160s", desc, expression)
	}
	if res == nil || res.Type == "undefined" {
		return "null", nil
	}
	return string(res.Value), nil
}

func (b *chromedpBackend) ClickAt(ctx context.Context, x, y float64) error {
	press := input.DispatchMouseEvent(input.MousePressed, x, y).
		WithButton(input.Left).WithClickCount(1)
	release := input.DispatchMouseEvent(input.MouseReleased, x, y).
		WithButton(input.Left).WithClickCount(1)
	return b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := press.Do(ctx); err != nil {
			return err
		}
		return release.Do(ctx)
	}))
}

func (b *chromedpBackend) TypeText(ctx context.Context, text string) error {
	return b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.InsertText(text).Do(ctx)
	}))
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

func (b *chromedpBackend) PressKey(ctx context.Context, key string) error {
	def, ok := cdKeys[key]
	if !ok && len(key) == 1 {
		def = struct {
			code string
			key  int64
			text string
		}{key, int64(key[0]), key}
		ok = true
	}
	if !ok {
		return fmt.Errorf("unknown key %q", key)
	}
	return b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		down := &input.DispatchKeyEventParams{
			Type:                  input.KeyDown,
			Key:                   key,
			Code:                  def.code,
			WindowsVirtualKeyCode: def.key,
			NativeVirtualKeyCode:  def.key,
		}
		if err := down.Do(ctx); err != nil {
			return err
		}
		if def.text != "" {
			ch := &input.DispatchKeyEventParams{Type: input.KeyChar, Text: def.text, Key: key, Code: def.code}
			if err := ch.Do(ctx); err != nil {
				return err
			}
		}
		up := &input.DispatchKeyEventParams{Type: input.KeyUp, Key: key, Code: def.code, WindowsVirtualKeyCode: def.key, NativeVirtualKeyCode: def.key}
		return up.Do(ctx)
	}))
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

func (b *chromedpBackend) Scroll(ctx context.Context, dy float64) error {
	info, err := b.Info(ctx)
	if err != nil {
		return err
	}
	x, y := float64(info.Width)/2, float64(info.Height)/2
	return b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		p := &input.DispatchMouseEventParams{Type: input.MouseWheel, X: x, Y: y, DeltaX: 0, DeltaY: dy}
		return p.Do(ctx)
	}))
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
	err := b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		params := page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatJpeg).WithQuality(80)
		if maxDim > 0 {
			_, _, _, cssLayout, _, _, err := page.GetLayoutMetrics().Do(ctx)
			if err == nil && cssLayout != nil {
				w, h := float64(cssLayout.ClientWidth), float64(cssLayout.ClientHeight)
				if max(w, h) > float64(maxDim) {
					scale := float64(maxDim) / max(w, h)
					params = params.WithClip(&page.Viewport{X: 0, Y: 0, Width: w, Height: h, Scale: scale})
				}
			}
		}
		var err error
		data, err = params.Do(ctx)
		return err
	}))
	return data, err
}

func (b *chromedpBackend) AXTree(ctx context.Context) (string, error) {
	var nodes []*accessibility.Node
	err := b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		nodes, err = accessibility.GetFullAXTree().Do(ctx)
		return err
	}))
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
			nn.Role = fmt.Sprint(n.Role.Value)
		}
		if n.Name != nil {
			nn.Name = fmt.Sprint(n.Name.Value)
		}
		if nn.Role == "" && nn.Name == "" {
			continue
		}
		out = append(out, nn)
	}
	data, err := json.Marshal(out)
	return string(data), err
}

func (b *chromedpBackend) BoxModel(ctx context.Context, backendNodeID int) (float64, float64, error) {
	var model *dom.BoxModel
	err := b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		model, err = dom.GetBoxModel().WithBackendNodeID(cdp.BackendNodeID(backendNodeID)).Do(ctx)
		return err
	}))
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
	var infos []*target.Info
	err := b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		infos, err = target.GetTargets().Do(ctx)
		return err
	}))
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
	b.targetOff()
	targetCtx, targetOff := chromedp.NewContext(b.allocCtx, chromedp.WithTargetID(target.ID(targetID)))
	b.targetCtx, b.targetOff = targetCtx, targetOff
	return chromedp.Run(targetCtx)
}

func (b *chromedpBackend) UploadFiles(ctx context.Context, selector string, paths []string) error {
	var nodeID cdp.NodeID
	err := b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		doc, err := dom.GetDocument().WithDepth(-1).Do(ctx)
		if err != nil {
			return err
		}
		nodeID, err = dom.QuerySelector(doc.NodeID, selector).Do(ctx)
		return err
	}))
	if err != nil {
		return err
	}
	if nodeID == 0 {
		return fmt.Errorf("upload: element not found: %s", selector)
	}
	return b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return dom.SetFileInputFiles(paths).WithNodeID(nodeID).Do(ctx)
	}))
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
	if b.targetOff != nil {
		b.targetOff()
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
	return b.run(context.Background(), chromedp.ActionFunc(func(ctx context.Context) error {
		return page.HandleJavaScriptDialog(accept).WithPromptText(promptText).Do(ctx)
	}))
}
