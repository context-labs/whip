package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/session"
)

// End-to-end through the tool: browser_exec drives a real headless Chrome.
func TestBrowserExecE2E(t *testing.T) {
	home, _ := os.UserHomeDir()
	bin := home + "/.cache/ms-playwright/chromium_headless_shell-1248/chrome-headless-shell-linux64/chrome-headless-shell"
	if _, err := os.Stat(bin); err != nil {
		bin = home + "/.cache/ms-playwright/chromium-1248/chrome-linux64/chrome"
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no chromium on this box")
	}
	if _, err := os.Stat("/tmp/chromelibs/usr/lib/x86_64-linux-gnu"); err == nil {
		t.Setenv("LD_LIBRARY_PATH", "/tmp/chromelibs/usr/lib/x86_64-linux-gnu:"+os.Getenv("LD_LIBRARY_PATH"))
	}
	t.Setenv("ROD_BROWSER_BIN", bin)

	ln, _ := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "0.0.0.0:0")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!doctype html><title>e2e-page</title><h1>hello agent</h1>`)
	})
	go http.Serve(ln, mux)
	defer ln.Close()
	ip := "127.0.0.1"
	if conn, err := (&net.Dialer{}).DialContext(context.Background(), "udp", "8.8.8.8:80"); err == nil {
		ip = conn.LocalAddr().(*net.UDPAddr).IP.String()
		conn.Close()
	}
	url := fmt.Sprintf("http://%s:%d", ip, ln.Addr().(*net.TCPAddr).Port)

	t.Setenv("HOME", t.TempDir())
	mgr := browser.NewManager(browser.ModeHeadless)
	defer mgr.CloseAll()
	services := NewServices()
	services.SetBrowser(mgr, true)
	// Host tools run only on services bound to a dispatcher authority; an
	// unbound Services rejects every call before the browser is touched.
	st, err := session.Open(filepath.Join(t.TempDir(), "sessions.db"), capability.NewWorkspaces())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	processes := capability.NewProcessManager()
	t.Cleanup(func() { _ = processes.Close() })
	rootID, err := st.Create(session.SessionKindAgent, t.TempDir(), "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := st.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	services.SetGate(func(context.Context, GateRequest) (GateDecision, string) { return GateAllowOnce, "" })
	if err := services.BindDispatcher(st, st.Workspaces(), processes, authority); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(services.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	code := "# check the test page\ngoto(\"" + url + "\")\nprint(js(\"document.title\"))\nprint(info())"
	args, _ := json.Marshal(map[string]string{"code": code})
	out := Execute(ctx, []Tool{BrowserExec(services)}, "browser_exec", args)
	if strings.HasPrefix(out, "Error") {
		t.Fatalf("tool error: %s", out)
	}
	if !strings.Contains(out, `"e2e-page"`) {
		t.Fatalf("missing title: %s", out)
	}
	if !strings.Contains(out, url) {
		t.Fatalf("missing url in info: %s", out)
	}
}
