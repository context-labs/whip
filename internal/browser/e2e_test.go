// e2e_test.go exercises the real browser path against Playwright's
// Chromium (present on this machine; tests skip cleanly without it).
// Three modes: headless, dedicated, and live-attach via an explicit CDP
// endpoint (the user's everyday-Chrome flow, minus the profile scan,
// which browser_test.go covers against fake profile dirs).

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
)

var chromiumCandidates = []string{
	"~/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome",
	"~/.cache/ms-playwright/chromium_headless_shell-1234/chrome-headless-shell-linux64/chrome-headless-shell",
	// macOS dev boxes: rod launches these headed/headless itself.
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
}

// chromeForTestingPath returns Playwright's full Chrome-for-Testing build —
// the only locally-available build that honors --load-extension (branded
// Google Chrome ignores it). Used by the extension E2E; "" when absent.
func chromeForTestingPath() string {
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, "Library/Caches/ms-playwright/chromium-1234/chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func chromiumPath(t *testing.T) string {
	t.Helper()
	// Unpacked Ubuntu debs for Chrome's shared libs (no sudo on this box).
	libs := "/tmp/chromelibs/usr/lib/x86_64-linux-gnu"
	if _, err := os.Stat(libs); err == nil {
		t.Setenv("LD_LIBRARY_PATH", libs+":"+os.Getenv("LD_LIBRARY_PATH"))
	}
	home, _ := os.UserHomeDir()
	for _, c := range chromiumCandidates {
		p := strings.Replace(c, "~", home, 1)
		if _, err := os.Stat(p); err == nil {
			t.Setenv("ROD_BROWSER_BIN", p)
			return p
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Skip("no chromium-family binary found")
	return ""
}

// testPage serves a page with a cookie check + a known element to click.
func testPage(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if after, ok := strings.CutPrefix(r.URL.Path, "/marker/"); ok {
			marker := after
			fmt.Fprintf(w, `<!doctype html><title>marker-%s</title><h1>%s</h1>`, marker, marker)
			return
		}
		switch r.URL.Path {
		case "/set-cookie":
			http.SetCookie(w, &http.Cookie{Name: "whip-e2e", Value: "real-session-42", Path: "/"})
			http.Redirect(w, r, "/", http.StatusFound)
		case "/":
			c, err := r.Cookie("whip-e2e")
			cookie := "none"
			if err == nil {
				cookie = c.Value
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!doctype html><html><head><title>whip e2e</title></head><body>
<h1 id="h">hello</h1><div id="q" contenteditable="true"></div><div id="b" onclick="document.title='clicked'" style="padding:8px">go</div>
<div id="cookie">%s</div></body></html>`, cookie)
		default:
			http.NotFound(w, r)
		}
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	// Chrome in this sandboxed env can't reach the test server's 127.0.0.1;
	// give the URL on the box's LAN IP instead (bound on 0.0.0.0 above).
	ip := "127.0.0.1"
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		ip = conn.LocalAddr().(*net.UDPAddr).IP.String()
		conn.Close()
	}
	return fmt.Sprintf("http://%s:%d", ip, ln.Addr().(*net.TCPAddr).Port)
}

func TestE2EHeadless(t *testing.T) {
	_ = chromiumPath(t)           // rod's launcher finds the playwright cache itself
	t.Setenv("HOME", t.TempDir()) // isolated profile: a reused profile dir
	// from a crashed run poisons the launch (renderer wedges on first nav)
	url := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	b, err := Open(ctx, ModeHeadless)
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

func TestE2EDedicated(t *testing.T) {
	_ = chromiumPath(t)
	url := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Dedicated uses the whip-owned profile dir.
	home := t.TempDir() // don't touch the real ~/.whip during tests
	t.Setenv("HOME", home)

	b, err := Open(ctx, ModeDedicated)
	if err != nil {
		t.Fatalf("open dedicated: %v", err)
	}
	defer closeFixtureBrowser(t, b)
	if err := b.Navigate(ctx, url); err != nil {
		t.Fatal(err)
	}
	// Fill dispatches real key events; in this sandboxed headless build the
	// text doesn't land on form controls (renderer quirk — see the doc's
	// gotchas), so verify the call succeeds and focuses, not the payload.
	if err := b.Fill(ctx, "#q", "paper towels"); err != nil {
		t.Fatal(err)
	}
	v, err := b.Eval(ctx, `document.activeElement.id`)
	if err != nil || v != `"q"` {
		t.Fatalf("fill focus: %s %v", v, err)
	}
	// The whip-owned profile dir must exist (separate from the user's).
	if _, err := os.Stat(filepath.Join(home, ".whip", "browser", "dedicated-profile")); err != nil {
		t.Fatalf("dedicated profile dir missing: %v", err)
	}
}

// TestE2ELiveAttach covers the user's-running-Chrome flow: a separately
// launched Chrome with a debug port (what the profile scan resolves to),
// attached via WHIP_CDP_URL. Real cookies, and Close must NOT kill it.
func TestE2ELiveAttach(t *testing.T) {
	bin := chromiumPath(t)
	url := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Launch a "user's Chrome" with remote debugging on a fixed port.
	portLn, _ := net.Listen("tcp", "127.0.0.1:0")
	port := portLn.Addr().(*net.TCPAddr).Port
	portLn.Close()
	profile := t.TempDir()
	cmd := exec.Command(bin,
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+profile,
		"--no-first-run", "--no-default-browser-check", "--headless=new", // CI box has no display; live-mode discovery is display-agnostic
		"about:blank",
	)
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	// Seed a cookie INSIDE that browser (simulating the user's session):
	// attach, navigate to /set-cookie, detach — then the test browser must
	// see the cookie.
	t.Setenv("WHIP_CDP_URL", fmt.Sprintf("http://127.0.0.1:%d", port))
	deadline := time.Now().Add(30 * time.Second)
	var b Backend
	var err error
	for time.Now().Before(deadline) {
		b, err = Open(ctx, ModeLive)
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("attach to live chrome: %v", err)
	}
	if err := b.Navigate(ctx, url+"/set-cookie"); err != nil {
		t.Fatal(err)
	}
	cookie, err := b.Eval(ctx, `document.getElementById("cookie").textContent`)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != `"real-session-42"` {
		t.Fatalf("live cookie: %s", cookie)
	}
	// Tabs + tab switch flow.
	tabs, err := b.Tabs(ctx)
	if err != nil || len(tabs) == 0 {
		t.Fatalf("tabs: %v %v", tabs, err)
	}
	// Close detaches only — the "user's Chrome" must survive.
	b.Close()
	ws, err := DiscoverLiveWS(ctx)
	if err != nil {
		t.Fatalf("browser died with our Close: %v", err)
	}
	if !strings.Contains(ws, "devtools/browser") {
		t.Fatalf("ws url: %q", ws)
	}
}

func jsonUnmarshal(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}

// Regression: eval right after attach must not hang (a Page.enable settle
// race was reported against an earlier build). No settle sleeps — a race
// here shows up as a hang. 5 iterations to catch flakiness.
func TestEvalImmediatelyAfterAttach(t *testing.T) {
	_ = chromiumPath(t)
	t.Setenv("HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for i := range 5 {
		b, err := Open(ctx, ModeHeadless)
		if err != nil {
			t.Fatalf("iter %d open: %v", i, err)
		}
		// zero settle: eval the instant we're attached
		res, err := b.Eval(ctx, "1+1")
		if err != nil {
			t.Fatalf("iter %d eval: %v", i, err)
		}
		if res != "2" {
			t.Fatalf("iter %d: got %s", i, res)
		}
		b.Close()
	}
}

// TestE2ELiveFallsBackToLaunched exercises the hermes-style fallback: live
// discovery finds nothing debuggable (HOME is a bare temp dir, no explicit
// endpoint) and Open(ModeLive) transparently lands on a launched dedicated
// Chrome instead of erroring.
func TestE2ELiveFallsBackToLaunched(t *testing.T) {
	if os.Getenv("WHIP_CDP_WS") != "" || os.Getenv("WHIP_CDP_URL") != "" {
		t.Skip("explicit CDP endpoint set — fallback bypassed")
	}
	// Hermeticity: DiscoverLiveWS's last-resort probe of 9222/9223 could hit
	// a real debug browser on this machine even with an empty HOME, which
	// would attach live instead of falling back. Skip when one answers.
	for _, p := range []int{9222, 9223} {
		if portLive(p) {
			t.Skipf("ambient browser on %d — fallback would not trigger", p)
		}
	}
	_ = chromiumPath(t)
	t.Setenv("HOME", t.TempDir()) // no profiles at all → live discovery fails
	url := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	b, err := Open(ctx, ModeLive)
	if err != nil {
		t.Fatalf("live fallback must not error: %v", err)
	}
	defer closeFixtureBrowser(t, b)
	if b.Obtained() != ObtainedLaunched {
		t.Fatalf("fallback should have launched, got obtained=%v", b.Obtained())
	}
	if err := b.Navigate(ctx, url); err != nil {
		t.Fatal(err)
	}
	info, err := b.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.URL, url) {
		t.Fatalf("url: %q", info.URL)
	}
}

// TestE2EDedicatedReattach verifies a still-running whip Chrome is reused:
// close the backend's CDP connection (simulating a dead/stale backend)
// while keeping the browser process alive, then Open again — it must
// reattach to the SAME browser rather than spawn a duplicate, and the
// profile's cookies survive.
func TestE2EDedicatedReattach(t *testing.T) {
	_ = chromiumPath(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	url := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	b1, err := Open(ctx, ModeDedicated)
	if err != nil {
		t.Fatalf("launch dedicated: %v", err)
	}
	// Teardown first: any t.Fatal below must not leak a (headed) Chrome.
	// Fresh context — the test's ctx may be expired by cleanup time.
	prof := dedicatedProfileDir(home, "default")
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		killProfileChrome(cctx, t, prof)
	})
	if b1.Obtained() != ObtainedLaunched {
		t.Fatalf("first open should launch, got %v", b1.Obtained())
	}
	if err := b1.Navigate(ctx, url+"/set-cookie"); err != nil {
		t.Fatal(err)
	}

	// Detach: with the new Close semantics, b1.Close() severs our CDP
	// connection without killing Chrome (b1 owns the launcher, but a
	// detached live/reattached backend leaves the process alive — that's
	// the point of reattach).
	b1.Close()
	if _, ok := DiscoverWSForProfile(ctx, prof); !ok {
		t.Fatal("Close must leave the dedicated Chrome alive for reattach")
	}

	// Second open must reattach: same profile, cookie intact, no new launch.
	b2, err := Open(ctx, ModeDedicated)
	if err != nil {
		t.Fatalf("reattach: %v", err)
	}
	if b2.Obtained() != ObtainedReattached {
		t.Fatalf("second open should reattach, got %v", b2.Obtained())
	}
	if b2.(*Browser).launcher != nil {
		t.Fatal("reattach must not own a launcher (that would be a new process)")
	}
	if err := b2.Navigate(ctx, url+"/"); err != nil {
		t.Fatal(err)
	}
	cookie, err := b2.Eval(ctx, `document.getElementById("cookie").textContent`)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != `"real-session-42"` {
		t.Fatalf("reattached browser lost profile cookies: %s", cookie)
	}

	// Closing the reattached backend detaches (Chrome survives); a third
	// open reattaches again rather than stacking a second process.
	b2.Close()
	if _, ok := DiscoverWSForProfile(ctx, prof); !ok {
		t.Fatal("reattached Close must leave Chrome alive")
	}
	b3, err := Open(ctx, ModeDedicated)
	if err != nil {
		t.Fatalf("third open: %v", err)
	}
	if b3.Obtained() != ObtainedReattached {
		t.Fatalf("third open should reattach the surviving Chrome, got %v", b3.Obtained())
	}
	b3.Close()
	// Chrome is killed by the t.Cleanup registered after the first Open.
}

// killProfileChrome terminates the Chrome behind a profile dir by closing
// the launcher-owned process — used at test teardown after detach-style
// Closes left it running.
func killProfileChrome(ctx context.Context, t *testing.T, prof string) {
	t.Helper()
	ws, ok := DiscoverWSForProfile(ctx, prof)
	if !ok {
		return
	}
	// proto.BrowserClose over the WS shuts Chrome down cleanly.
	if b := rod.New().ControlURL(ws); b.Connect() == nil {
		_ = b.Close() // this one kills — intended at teardown
	}
	time.Sleep(500 * time.Millisecond)
}

// Persistent dedicated browsers outlive Close; fixture launches must stop before
// testing removes their profiles, including a live-mode fallback launch.
func closeFixtureBrowser(t *testing.T, b Backend) {
	t.Helper()
	_ = b.Close()
	if browser, ok := b.(*Browser); ok && browser.launcher != nil {
		if err := stopLauncher(browser.launcher); err != nil {
			t.Error(err)
		}
	}
}
