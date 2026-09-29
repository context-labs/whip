package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browserconfig"
)

// Every real browser test needs an explicitly supplied private testing binary.
// No installed browser, user profile, conventional port or executable PATH is scanned.
func chromiumPath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("WHIP_BROWSER_NATIVE_TEST_BINARY")
	if path == "" {
		t.Skip("set WHIP_BROWSER_NATIVE_TEST_BINARY to private test Chromium")
	}
	if !filepath.IsAbs(path) {
		t.Fatal("test Chromium path must be absolute")
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		t.Fatal("test Chromium unavailable", err)
	}
	return path
}

func chromeForTestingPath() string {
	if os.Getenv("WHIP_BROWSER_NATIVE_TEST_EXTENSION") != "1" {
		return ""
	}
	return os.Getenv("WHIP_BROWSER_NATIVE_TEST_BINARY")
}

func testPage(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if marker, ok := strings.CutPrefix(r.URL.Path, "/marker/"); ok {
			fmt.Fprintf(w, `<!doctype html><title>marker-%s</title><h1>%s</h1>`, marker, marker)
			return
		}
		switch r.URL.Path {
		case "/set-cookie":
			http.SetCookie(w, &http.Cookie{Name: "whip-e2e", Value: "real-session-42", Path: "/"})
			http.Redirect(w, r, "/", http.StatusFound)
		case "/":
			value := "none"
			if cookie, err := r.Cookie("whip-e2e"); err == nil {
				value = cookie.Value
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!doctype html><title>whipcode e2e</title><h1 id="h">hello</h1><div id="q" contenteditable="true"></div><div id="b" onclick="document.title='clicked'" style="padding:8px">go</div><div id="cookie">%s</div>`, value)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

func TestE2EHeadless(t *testing.T) {
	for _, driver := range []string{DriverRod, DriverChromedp} {
		t.Run(driver, func(t *testing.T) { testNativeHelpers(t, driver) })
	}
}

func testNativeHelpers(t *testing.T, driver string) {
	t.Helper()
	url := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	b, err := OpenNative(ctx, t.Context(), nativeOptions(t, browserconfig.Config{Mode: "headless", Executable: chromiumPath(t)}, driver))
	if err != nil {
		t.Fatalf("open headless: %v", err)
	}
	defer b.Close()
	if b.Mode() != ModeHeadless {
		t.Fatalf("mode: %v", b.Mode())
	}

	if err := b.Navigate(ctx, url+"/set-cookie"); err != nil {
		t.Fatal(err)
	}
	info, err := b.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.URL, url) {
		t.Fatalf("url: %q", info.URL)
	}
	// Real-cookie check: the page renders the cookie value the server set.
	cookie, err := b.Eval(ctx, `document.getElementById("cookie").textContent`)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != `"real-session-42"` {
		t.Fatalf("cookie round-trip failed: %s", cookie)
	}
	// AX tree → box → click workflow (the agent's primary path).
	tree, err := b.AXTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tree, "hello") {
		t.Fatalf("ax tree missing heading: %.200s", tree)
	}
	h, err := b.Eval(ctx, `(()=>{const r=document.getElementById("b").getBoundingClientRect();return [r.x+r.width/2,r.y+r.height/2]})()`)
	if err != nil {
		t.Fatal(err)
	}
	var xy [2]float64
	if err := jsonUnmarshal(h, &xy); err != nil {
		t.Fatal(err)
	}
	if err := b.ClickAt(ctx, xy[0], xy[1]); err != nil {
		t.Fatal(err)
	}
	title, err := b.Eval(ctx, `document.title`)
	if err != nil || title != `"clicked"` {
		t.Fatalf("click didn't land: title=%s err=%v", title, err)
	}
	// Screenshot produces a real JPEG.
	jpeg, err := b.Screenshot(ctx, 1568)
	if err != nil {
		t.Fatal(err)
	}
	if len(jpeg) < 500 || jpeg[0] != 0xFF || jpeg[1] != 0xD8 {
		t.Fatalf("not a jpeg: %d bytes, magic %x", len(jpeg), jpeg[:2])
	}
}

// This visible-window check remains explicitly opt-in, separate from required
// headless CI. It uses only a private profile and the selected testing binary.
func TestE2EDedicated(t *testing.T) {
	if os.Getenv("WHIP_BROWSER_NATIVE_TEST_HEADED") != "1" {
		t.Skip("explicit private headed Chrome fixture required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	options := nativeOptions(t, browserconfig.Config{Mode: "dedicated", Executable: chromiumPath(t)}, DriverRod)
	connection, err := OpenNative(ctx, t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.Navigate(ctx, testPage(t)); err != nil {
		t.Fatal(err)
	}
	if err := connection.Fill(ctx, "#q", "paper towels"); err != nil {
		t.Fatal(err)
	}
	if value, err := connection.Eval(ctx, "document.activeElement.id"); err != nil || value != `"q"` {
		t.Fatal(value, err)
	}
	if _, err := os.Stat(filepath.Join(options.Directory, "browser", "profiles", options.Profile)); err != nil {
		t.Fatal(err)
	}
}

// The separately owned private Chrome stands in for a human browser. Both live
// drivers must detach without closing that process, changing modes or replaying.
func TestE2ELiveAttach(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	options := nativeOptions(t, browserconfig.Config{Mode: "headless", Executable: chromiumPath(t)}, DriverRod)
	human, err := OpenNative(ctx, t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer human.Close()
	page := testPage(t)
	if err := human.Navigate(ctx, page+"/set-cookie"); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(options.Directory, "browser", "profiles", options.Profile)
	for _, driver := range []string{DriverRod, DriverChromedp} {
		liveOptions := nativeOptions(t, browserconfig.Config{Mode: "live", LiveProfile: profile}, driver)
		live, err := OpenNative(ctx, t.Context(), liveOptions)
		if err != nil {
			t.Fatal(err)
		}
		if live.Mode() != ModeLive {
			t.Fatal("live mode changed")
		}
		if cookie, err := live.Eval(ctx, `document.getElementById("cookie").textContent`); err != nil || cookie != `"real-session-42"` {
			t.Fatal(cookie, err)
		}
		tabs, err := live.Tabs(ctx)
		if err != nil || len(tabs) == 0 {
			t.Fatal(tabs, err)
		}
		if err := live.UseTab(ctx, tabs[0].TargetID); err != nil {
			t.Fatal(err)
		}
		if err := live.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := human.Info(ctx); err != nil {
			t.Fatal("live detach killed independent Chrome", err)
		}
	}
	// Native launch refuses a live profile holder; no takeover or quarantine.
	if duplicate, err := OpenNative(ctx, t.Context(), options); err == nil {
		_ = duplicate.Close()
		t.Fatal("live profile holder was replaced")
	}
	if _, err := human.Info(ctx); err != nil {
		t.Fatal("refused launch changed holder", err)
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatal("profile moved", err)
	}
}

func TestEvalImmediatelyAfterAttach(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	options := nativeOptions(t, browserconfig.Config{Mode: "headless", Executable: chromiumPath(t)}, DriverRod)
	for i := range 5 {
		browser, err := OpenNative(ctx, t.Context(), options)
		if err != nil {
			t.Fatal(i, err)
		}
		value, err := browser.Eval(ctx, "1+1")
		if err != nil || value != "2" {
			t.Fatal(i, value, err)
		}
		if err := browser.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := browser.Eval(ctx, "1+1"); err == nil {
			t.Fatal("closed connection reopened")
		}
	}
}
