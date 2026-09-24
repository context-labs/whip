package browser

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browser/extrelay"
	"github.com/go-rod/rod"
)

// TestExtensionRealChrome is the goal's acceptance path: a real Chrome with
// the whip extension loaded (autoAttach pins the active tab — no human
// click), driven through the chrome.debugger tunnel via the relay, using
// whip's real *Browser (the Backend browser_exec calls) against a live page.
//
// Branded Google Chrome ignores --load-extension ("not allowed in Google
// Chrome" in its logs), so this uses Chrome for Testing (Playwright's full
// build, which allows it). Dev-machine only (headed window + extension);
// skips in CI and when CfT is absent.
func TestExtensionRealChrome(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("headed extension test is dev-machine only")
	}
	bin := chromeForTestingPath()
	if bin == "" {
		t.Skip("Chrome for Testing not installed (npx playwright install chromium --no-shell)")
	}

	rel, err := extrelay.NewRelay()
	if err != nil {
		t.Fatal(err)
	}
	defer rel.Close()

	// Materialize the extension with autoAttach + this relay's token so the
	// service worker pins the active tab on startup without a click.
	dir := t.TempDir()
	if _, err := extrelay.WriteExtension(dir); err != nil {
		t.Fatal(err)
	}
	relayJSON := fmt.Sprintf("{\"addr\":%q,\"token\":%q,\"autoAttach\":true}\n", rel.Addr(), rel.Token())
	if err := os.WriteFile(filepath.Join(dir, "relay.json"), []byte(relayJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	url := testPage(t)
	prof := t.TempDir()
	portLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := portLn.Addr().(*net.TCPAddr).Port
	portLn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+prof,
		"--load-extension="+dir,
		"--disable-extensions-except="+dir,
		"--no-first-run", "--no-default-browser-check",
		"--app="+url+"/set-cookie",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("launch chrome: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// Wait for the extension to pin a tab and connect to the relay.
	if err := rel.WaitAttached(ctx); err != nil {
		for _, l := range rel.SWLogs() {
			t.Logf("swlog: %s", l)
		}
		t.Fatalf("extension never attached: %v", err)
	}
	t.Log("extension attached to relay")

	// Drive whip's real Backend through the relay (same wiring openExtension
	// uses, against this test relay).
	b := &Browser{mode: ModeExtension, obtained: ObtainedLive}
	b.browser = rod.New().ControlURL(rel.CDPURL())
	if err := b.browser.Connect(); err != nil {
		t.Fatalf("connect through relay: %v", err)
	}
	b.browser = b.browser.Context(ctx)
	defer b.Close()

	// attachPage may run before the pinned tab reports its URL; retry until
	// the pinned tab is the test page.
	deadline := time.Now().Add(25 * time.Second)
	var ok bool
	for time.Now().Before(deadline) {
		if err := b.attachPage(); err == nil && b.page != nil {
			if info, ierr := b.Info(ctx); ierr == nil && strings.HasPrefix(info.URL, url) {
				ok = true
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ok {
		t.Fatal("extension did not pin the test page tab")
	}
	t.Log("pinned tab is the test page")

	// The goal's action set, against the real page through the tunnel.
	if err := b.Navigate(ctx, url+"/set-cookie"); err != nil {
		t.Errorf("Navigate: %v", err)
	}
	cookie, err := b.Eval(ctx, `document.getElementById("cookie") && document.getElementById("cookie").textContent`)
	if err != nil {
		t.Errorf("Eval: %v", err)
	}
	if cookie != `"real-session-42"` {
		t.Errorf("cookie through tunnel: %s", cookie)
	}
	if err := b.ClickAt(ctx, 20, 20); err != nil {
		t.Errorf("ClickAt: %v", err)
	}
	if err := b.TypeText(ctx, "hi"); err != nil {
		t.Errorf("TypeText: %v", err)
	}
	jpeg, err := b.Screenshot(ctx, 1568)
	if err != nil || len(jpeg) < 100 {
		t.Errorf("Screenshot: %v len=%d", err, len(jpeg))
	}
	ax, err := b.AXTree(ctx)
	if err != nil || !strings.Contains(ax, "hello") {
		t.Errorf("AXTree: %v (%.100s)", err, ax)
	}
}
