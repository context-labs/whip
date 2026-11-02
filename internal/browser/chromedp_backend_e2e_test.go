package browser

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

// TestChromedpBackendE2E covers the chromedp ops TestDriverParity does not:
// history, key and text input, box model, file upload, dialogs and tab
// switching. It exists because chromedp's major API rewrites (0.16 → 0.21)
// change every one of these call paths, and only a real browser proves them.
func TestChromedpBackendE2E(t *testing.T) {
	chromiumPath(t)
	t.Setenv("HOME", t.TempDir())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/one", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>one</title>`)
	})
	mux.HandleFunc("/two", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>two</title>
<input id="t" autofocus onkeydown="if(event.key==='Enter')document.title='entered'">
<button id="b" style="position:absolute;left:100px;top:200px;width:40px;height:20px">b</button>
<input id="f" type="file">`)
	})
	go http.Serve(ln, mux)
	t.Cleanup(func() { ln.Close() })
	base := "http://" + ln.Addr().String()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	b, err := openChromedp(ctx, ModeHeadless, "default", nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer b.Close()

	eval := func(expr string) string {
		t.Helper()
		v, err := b.Eval(ctx, expr)
		if err != nil {
			t.Fatalf("eval %s: %v", expr, err)
		}
		return v
	}

	if err := b.Navigate(ctx, base+"/one"); err != nil {
		t.Fatalf("navigate one: %v", err)
	}
	if err := b.Navigate(ctx, base+"/two"); err != nil {
		t.Fatalf("navigate two: %v", err)
	}

	t.Run("TypeTextAndPressKey", func(t *testing.T) {
		eval(`document.getElementById("t").focus()`)
		if err := b.TypeText(ctx, "hi"); err != nil {
			t.Fatal(err)
		}
		if err := b.PressKey(ctx, "!"); err != nil {
			t.Fatal(err)
		}
		if v := eval(`document.getElementById("t").value`); v != `"hi!"` {
			t.Fatalf("value = %s", v)
		}
		if err := b.PressKey(ctx, "Enter"); err != nil {
			t.Fatal(err)
		}
		if v := eval(`document.title`); v != `"entered"` {
			t.Fatalf("Enter keydown did not fire: title = %s", v)
		}
		if err := b.PressKey(ctx, "NoSuchKey"); err == nil {
			t.Fatal("unknown key accepted")
		}
	})

	t.Run("EvalErrors", func(t *testing.T) {
		if v := eval(`return 7`); v != "7" {
			t.Fatalf("top-level return retry = %s", v)
		}
		if v := eval(`undefined`); v != "null" {
			t.Fatalf("undefined = %s", v)
		}
		if _, err := b.Eval(ctx, `throw new Error("boom")`); err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("exception err = %v", err)
		}
		if v := eval(`Promise.resolve(3)`); v != "3" {
			t.Fatalf("awaited promise = %s", v)
		}
	})

	t.Run("BoxModel", func(t *testing.T) {
		tree, err := b.AXTree(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var nodes []struct {
			Role          string `json:"role"`
			Name          string `json:"name"`
			BackendNodeID int    `json:"backendDOMNodeId"`
		}
		if err := jsonUnmarshal(tree, &nodes); err != nil {
			t.Fatal(err)
		}
		id := 0
		for _, n := range nodes {
			if n.Role == "button" && n.Name == "b" {
				id = n.BackendNodeID
			}
		}
		if id == 0 {
			t.Fatalf("button not in AX tree: %s", tree)
		}
		x, y, err := b.BoxModel(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if x < 110 || x > 130 || y < 200 || y > 220 {
			t.Fatalf("center = (%v, %v), want ~(120, 210)", x, y)
		}
	})

	t.Run("UploadFiles", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "up.txt")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := b.UploadFiles(ctx, "#f", []string{path}); err != nil {
			t.Fatal(err)
		}
		if v := eval(`document.getElementById("f").files[0].name`); v != `"up.txt"` {
			t.Fatalf("file = %s", v)
		}
		if err := b.UploadFiles(ctx, "#missing", []string{path}); err == nil {
			t.Fatal("missing selector accepted")
		}
	})

	t.Run("HandleDialog", func(t *testing.T) {
		eval(`setTimeout(()=>{document.title=String(prompt("q"))},0)`)
		deadline := time.Now().Add(5 * time.Second)
		for {
			err := b.HandleDialog(true, "answer")
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("dialog never answerable: %v", err)
			}
			time.Sleep(50 * time.Millisecond)
		}
		if v := eval(`document.title`); v != `"answer"` {
			t.Fatalf("prompt result = %s", v)
		}
	})

	t.Run("Back", func(t *testing.T) {
		if err := b.Back(ctx); err != nil {
			t.Fatal(err)
		}
		if err := b.WaitLoad(ctx); err != nil {
			t.Fatal(err)
		}
		info, err := b.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if info.Title != "one" {
			t.Fatalf("after Back title = %q", info.Title)
		}
	})

	t.Run("UseTab", func(t *testing.T) {
		eval(`void window.open("` + base + `/two", "_blank")`)
		var other string
		deadline := time.Now().Add(5 * time.Second)
		for other == "" && time.Now().Before(deadline) {
			tabs, err := b.Tabs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, tab := range tabs {
				if strings.HasSuffix(tab.URL, "/two") {
					other = tab.TargetID
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		if other == "" {
			t.Fatal("second tab not listed")
		}
		if err := b.UseTab(ctx, other); err != nil {
			t.Fatal(err)
		}
		if err := b.WaitLoad(ctx); err != nil {
			t.Fatal(err)
		}
		if v := eval(`location.pathname`); v != `"/two"` {
			t.Fatalf("UseTab drives %s", v)
		}
	})
}

// TestChromedpDedicatedAndLive covers chromedp's two other connection paths:
// a headed dedicated launch over a debugging port (websocket transport, which
// chromedp >= 0.21 no longer picks by default) and a live attach to that
// browser through the remote allocator.
func TestChromedpDedicatedAndLive(t *testing.T) {
	chromiumPath(t)
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" && goruntime.GOOS != "darwin" {
		t.Skip("headed launch needs a display")
	}
	t.Setenv("HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	launched, err := openChromedp(ctx, ModeDedicated, "default", nil)
	if err != nil {
		t.Fatalf("dedicated open: %v", err)
	}
	defer launched.Close()
	if launched.Obtained() != ObtainedLaunched {
		t.Fatalf("obtained = %v", launched.Obtained())
	}
	if v, err := launched.Eval(ctx, "1+1"); err != nil || v != "2" {
		t.Fatalf("dedicated eval = %q, %v", v, err)
	}

	// The dedicated launch opened a debugging port; Chrome advertises it in
	// the profile's DevToolsActivePort. Attach to it as a live browser.
	home, _ := os.UserHomeDir()
	raw, err := os.ReadFile(filepath.Join(dedicatedProfileDir(home, "default"), "DevToolsActivePort"))
	if err != nil {
		t.Fatalf("dedicated Chrome opened no debugging port: %v", err)
	}
	port, path, err := parseDevToolsActivePort(raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIP_CDP_WS", fmt.Sprintf("ws://127.0.0.1:%d%s", port, path))
	live, err := openChromedp(ctx, ModeLive, "default", nil)
	if err != nil {
		t.Fatalf("live attach: %v", err)
	}
	if live.Obtained() != ObtainedLive {
		t.Fatalf("live obtained = %v", live.Obtained())
	}
	if v, err := live.Eval(ctx, "2+2"); err != nil || v != "4" {
		t.Fatalf("live eval = %q, %v", v, err)
	}
	live.Close()
	// Closing a live attachment must leave the user's browser running.
	if v, err := launched.Eval(ctx, "3+3"); err != nil || v != "6" {
		t.Fatalf("browser gone after live Close: %q, %v", v, err)
	}
}
