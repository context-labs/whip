package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// TestBrowserExecReachesModel pins the full loop: the model calls
// browser_exec, whip drives a real headless Chrome, and the page content
// comes back in the tool result the provider receives.
func TestBrowserExecReachesModel(t *testing.T) {
	home, _ := os.UserHomeDir()
	bin := home + "/.cache/ms-playwright/chromium_headless_shell-1234/chrome-headless-shell-linux64/chrome-headless-shell"
	if _, err := os.Stat(bin); err != nil {
		bin = home + "/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome"
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no chromium on this box")
	}
	if _, err := os.Stat("/tmp/chromelibs/usr/lib/x86_64-linux-gnu"); err == nil {
		t.Setenv("LD_LIBRARY_PATH", "/tmp/chromelibs/usr/lib/x86_64-linux-gnu:"+os.Getenv("LD_LIBRARY_PATH"))
	}
	t.Setenv("ROD_BROWSER_BIN", bin)
	t.Setenv("HOME", t.TempDir())

	ln, _ := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "0.0.0.0:0")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!doctype html><title>loop-e2e-page</title><h1>hi</h1>`)
	})
	go http.Serve(ln, mux)
	defer ln.Close()
	ip := "127.0.0.1"
	if conn, err := (&net.Dialer{}).DialContext(context.Background(), "udp", "8.8.8.8:80"); err == nil {
		ip = conn.LocalAddr().(*net.UDPAddr).IP.String()
		conn.Close()
	}
	pageURL := fmt.Sprintf("http://%s:%d", ip, ln.Addr().(*net.TCPAddr).Port)

	mgr := browser.NewManager(browser.ModeHeadless)
	defer mgr.CloseAll()
	services := tools.NewServices()
	services.SetBrowser(mgr, true)
	bindTestServices(t, services, t.TempDir())

	code := fmt.Sprintf("# read the test page\ngoto(%q)\nprint(js(\"document.title\"))", pageURL)
	argsJSON, _ := json.Marshal(map[string]string{"code": code})

	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		call++
		if call == 1 {
			// the model must see browser_exec in the tool list
			found := false
			for _, tool := range req.Tools {
				if tool.Function.Name == "browser_exec" {
					found = true
				}
			}
			if !found {
				t.Errorf("browser_exec not offered to the model")
			}
			fmt.Fprintf(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"b1","type":"function","function":{"name":"browser_exec","arguments":%s}}]}}]}`+"\n\n",
				jsonString(string(argsJSON)))
		} else {
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, `"loop-e2e-page"`) {
				t.Errorf("tool result missing page title: %q", last.Content)
			}
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"the page says loop-e2e-page"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	ag := newTestAgentWithServices(llm.New(srv.URL, "k"), "m", 100, "sys", services)
	final, err := ag.Turn(ctx, "what's the page title?", Events{})
	if err != nil {
		t.Fatal(err)
	}
	if call < 2 {
		t.Fatalf("loop ended after %d calls", call)
	}
	if !strings.Contains(final, "loop-e2e-page") {
		t.Fatalf("final: %q", final)
	}
}
