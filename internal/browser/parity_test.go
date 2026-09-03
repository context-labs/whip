package browser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestDriverParity runs the core Backend ops against BOTH drivers on one
// page and records works/latency per op — the spike's decision data.
// Runs whichever drivers are available; rod is always present, chromedp
// joins when its import built (always, post-spike).
func TestDriverParity(t *testing.T) {
	home, _ := os.UserHomeDir()
	bin := home + "/.cache/ms-playwright/chromium_headless_shell-1234/chrome-headless-shell-linux64/chrome-headless-shell"
	if _, err := os.Stat(bin); err != nil {
		bin = home + "/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome"
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no chromium")
	}
	t.Setenv("ROD_BROWSER_BIN", bin)
	if _, err := os.Stat("/tmp/chromelibs/usr/lib/x86_64-linux-gnu"); err == nil {
		t.Setenv("LD_LIBRARY_PATH", "/tmp/chromelibs/usr/lib/x86_64-linux-gnu:"+os.Getenv("LD_LIBRARY_PATH"))
	}

	ln, _ := net.Listen("tcp", "0.0.0.0:0")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!doctype html><title>parity-page</title><h1 id="h">hello</h1><div id="q" contenteditable="true"></div><div id="b" onclick="document.title='clicked'" style="padding:8px">go</div>`)
	})
	go http.Serve(ln, mux)
	defer ln.Close()
	ip := "127.0.0.1"
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		ip = conn.LocalAddr().(*net.UDPAddr).IP.String()
		conn.Close()
	}
	url := fmt.Sprintf("http://%s:%d", ip, ln.Addr().(*net.TCPAddr).Port)

	drivers := []string{"rod", "chromedp"}
	type result struct {
		ok  bool
		ms  int64
		err string
	}
	table := map[string]map[string]result{}

	for _, drv := range drivers {
		table[drv] = map[string]result{}
		t.Setenv("HOME", t.TempDir())
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)

		b, err := openNamedWithOptions(ctx, ModeHeadless, "default", drv, nil, nil, "")
		if err != nil {
			t.Fatalf("%s open: %v", drv, err)
		}

		op := func(name string, fn func() error) {
			start := time.Now()
			err := fn()
			table[drv][name] = result{ok: err == nil, ms: time.Since(start).Milliseconds(), err: errStr(err)}
		}

		op("Navigate", func() error { return b.Navigate(ctx, url) })
		op("Eval", func() error {
			r, err := b.Eval(ctx, "document.title")
			if err == nil && r != `"parity-page"` {
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
			if err := b.Fill(ctx, "#q", "x"); err != nil {
				return err
			}
			v, err := b.Eval(ctx, "document.activeElement.id")
			if err == nil && v != `"q"` {
				return fmt.Errorf("focus %q", v)
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
		b.Close()
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
