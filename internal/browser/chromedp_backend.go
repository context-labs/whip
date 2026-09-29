// ChromeDP operations execute over the same captured, bounded native transport
// as Rod. The native owner controls acquisition, revocation and process lifetime.

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// chromedpBackend executes exclusively through a captured native transport.
type chromedpBackend struct {
	executor cdp.Executor
	mode     Mode
}

func (b *chromedpBackend) run(ctx context.Context, actions ...chromedp.Action) error {
	return chromedp.Tasks(actions).Do(cdp.WithExecutor(ctx, b.executor))
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
	if err := b.navigate(ctx, url); err != nil {
		return err
	}
	return b.WaitLoad(ctx)
}

// navigate uses the captured executor directly. chromedp.Navigate requires an
// allocator-owned target context, which neither native nor Desktop borrows.
func (b *chromedpBackend) navigate(ctx context.Context, url string) error {
	return b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, failure, _, err := page.Navigate(url).Do(ctx)
		if err != nil {
			return err
		}
		if failure != "" {
			return errors.New("browser navigation failed")
		}
		return nil
	}))
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
	if current >= len(entries) || entries[current-1] == nil {
		return errors.New("invalid browser navigation history")
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

func (b *chromedpBackend) PressKey(ctx context.Context, key string) error {
	def, named := keyDefs[key]
	if !named && utf8.RuneCountInString(key) != 1 {
		return fmt.Errorf("unknown key %q", key)
	}
	down := input.DispatchKeyEvent(input.KeyDown).WithKey(key)
	if named {
		down = down.WithCode(def.Code).WithWindowsVirtualKeyCode(int64(def.Key))
	}
	if err := b.run(ctx, down); err != nil {
		return err
	}
	if !named || key == " " {
		if err := b.TypeText(ctx, key); err != nil {
			return err
		}
	} else if def.Text != "" {
		if err := b.run(ctx, input.DispatchKeyEvent(input.KeyChar).WithKey(key).WithCode(def.Code).WithText(def.Text)); err != nil {
			return err
		}
	}
	down.Type = input.KeyUp
	return b.run(ctx, down)
}

func (b *chromedpBackend) Fill(ctx context.Context, selector, text string) error {
	return fillInput(ctx, b, selector, text)
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
		if n == nil {
			return "", errors.New("invalid browser accessibility node")
		}
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
	if model == nil || len(model.Content) != 8 {
		return 0, 0, errors.New("invalid browser box model")
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
		if ti == nil {
			return nil, errors.New("invalid browser target")
		}
		if ti.Type != "page" {
			continue
		}
		out = append(out, Tab{TargetID: string(ti.TargetID), Title: ti.Title, URL: ti.URL})
	}
	return out, nil
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

// Mode returns the backend's mode.
func (b *chromedpBackend) Mode() Mode { return b.mode }

// HandleDialog answers a pending native dialog. chromedp surfaces dialogs
// through page events; answer the current one if any.
func (b *chromedpBackend) HandleDialog(accept bool, promptText string) error {
	return b.run(context.Background(), chromedp.ActionFunc(func(ctx context.Context) error {
		return page.HandleJavaScriptDialog(accept).WithPromptText(promptText).Do(ctx)
	}))
}
