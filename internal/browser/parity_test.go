package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browserconfig"
)

// TestDriverParity runs the core Backend ops against BOTH drivers on one
// private page. Any failed operation fails the test; logged durations are
// diagnostic only. Both drivers use the explicit native transport and owner.
func TestDriverParity(t *testing.T) {
	bin := chromiumPath(t)
	url := testPage(t)

	drivers := []string{"rod", "chromedp"}
	type result struct {
		ok  bool
		ms  int64
		err string
	}
	table := map[string]map[string]result{}

	for _, drv := range drivers {
		table[drv] = map[string]result{}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		t.Cleanup(cancel)

		b, err := OpenNative(ctx, t.Context(), nativeOptions(t, browserconfig.Config{Mode: "headless", Executable: bin}, drv))
		if err != nil {
			t.Fatalf("%s open: %v", drv, err)
		}

		t.Cleanup(func() { _ = b.Close() })
		op := func(name string, fn func() error) {
			start := time.Now()
			err := fn()
			if err != nil {
				t.Errorf("%s %s: %v", drv, name, err)
			}
			table[drv][name] = result{ok: err == nil, ms: time.Since(start).Milliseconds(), err: errStr(err)}
		}

		op("Navigate", func() error { return b.Navigate(ctx, url) })
		op("Eval", func() error {
			r, err := b.Eval(ctx, "document.title")
			if err == nil && r != `"whipcode e2e"` {
				return fmt.Errorf("title %q", r)
			}
			return err
		})
		op("Info", func() error {
			i, err := b.Info(ctx)
			if err == nil && !strings.HasPrefix(i.URL, url) {
				return fmt.Errorf("url %q", i.URL)
			}
			return err
		})
		op("AXTree", func() error {
			tree, err := b.AXTree(ctx)
			if err == nil && !strings.Contains(tree, "hello") {
				return errors.New("ax missing heading")
			}
			return err
		})
		op("ClickAt", func() error {
			h, err := b.Eval(ctx, `(()=>{const r=document.getElementById("b").getBoundingClientRect();return [r.x+r.width/2,r.y+r.height/2]})()`)
			if err != nil {
				return err
			}
			var xy [2]float64
			if err := jsonUnmarshal(h, &xy); err != nil {
				return err
			}
			if err := b.ClickAt(ctx, xy[0], xy[1]); err != nil {
				return err
			}
			title, err := b.Eval(ctx, "document.title")
			if err == nil && title != `"clicked"` {
				return fmt.Errorf("click didn't land: title=%s", title)
			}
			return err
		})
		op("Screenshot", func() error {
			j, err := b.Screenshot(ctx, 1568)
			if err == nil && (len(j) < 500 || j[0] != 0xFF || j[1] != 0xD8) {
				return fmt.Errorf("not a jpeg: %d bytes", len(j))
			}
			return err
		})
		op("Fill-focus", func() error {
			if err := b.Fill(ctx, "#q", "x hé🌿"); err != nil {
				return err
			}
			v, err := b.Eval(ctx, "document.activeElement.id")
			if err == nil && v != `"q"` {
				return fmt.Errorf("focus %q", v)
			}
			if err != nil {
				return err
			}
			value, err := b.Eval(ctx, `document.getElementById("q").textContent`)
			if err != nil {
				return err
			}
			var actual string
			if err = json.Unmarshal([]byte(value), &actual); err != nil {
				return err
			}
			if actual != "x hé🌿" {
				return fmt.Errorf("fill text %s", value)
			}
			if err = b.Fill(ctx, "#q", ""); err != nil {
				return err
			}
			if err = b.PressKey(ctx, "x"); err != nil {
				return err
			}
			if err = b.PressKey(ctx, "🌿"); err != nil {
				return err
			}
			value, err = b.Eval(ctx, `document.getElementById("q").textContent`)
			if err != nil {
				return err
			}
			if err = json.Unmarshal([]byte(value), &actual); err != nil {
				return err
			}
			if actual != "x🌿" {
				return fmt.Errorf("key text %s", value)
			}
			return err
		})
		op("Scroll", func() error { return b.Scroll(ctx, -300) })
		op("WaitElement", func() error {
			ok, err := b.WaitElement(ctx, "#h", false)
			if err == nil && !ok {
				return errors.New("not found")
			}
			return err
		})
		op("Tabs", func() error {
			tabs, err := b.Tabs(ctx)
			if err == nil && len(tabs) == 0 {
				return errors.New("no tabs")
			}
			return err
		})
		_ = b.Close()
		cancel()
	}
	// Emit the decision table.
	ops := []string{"Navigate", "Eval", "Info", "AXTree", "ClickAt", "Screenshot", "Fill-focus", "Scroll", "WaitElement", "Tabs"}
	fmt.Println("\n=== DRIVER PARITY (headless, warm) ===")
	fmt.Printf("%-12s | %-18s | %-18s\n", "op", "rod", "chromedp")
	for _, op := range ops {
		r, c := table["rod"][op], table["chromedp"][op]
		fmt.Printf("%-12s | %-18s | %-18s\n", op, cell(r.ok, r.ms, r.err), cell(c.ok, c.ms, c.err))
	}
}

func cell(ok bool, ms int64, err string) string {
	if !ok {
		return "FAIL " + err
	}
	return fmt.Sprintf("ok %dms", ms)
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
