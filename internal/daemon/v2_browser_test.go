package daemon

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestV2BrowserBridge is an opt-in real-browser harness. All state is temporary;
// ordinary test runs do not start browsers or touch the user's daemon.
func TestV2BrowserBridge(t *testing.T) {
	directory := os.Getenv("WHIP_BROWSER_SMOKE_DIR")
	if directory == "" {
		t.Skip("set WHIP_BROWSER_SMOKE_DIR for real-browser smoke")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	frontendURL := "http://" + listener.Addr().String()
	fixture := newV2Fixture(t, &fakeRunner{}, frontendURL)
	info := map[string]string{"frontend": frontendURL, "endpoint": fixture.endpoint, "root_id": fixture.rootID}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var once sync.Once
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/safari-result" && r.Method == http.MethodPost {
			data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
			if err != nil {
				http.Error(w, "invalid result", 400)
				return
			}
			if err := os.WriteFile(filepath.Join(directory, "safari-result.json"), data, 0o600); err != nil {
				http.Error(w, "cannot save result", 500)
			}
			return
		}
		if r.URL.Path == "/safari-smoke" {
			data, err := os.ReadFile(filepath.Join(directory, "safari-smoke.html"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(data)
			return
		}
		if r.URL.Path == "/done" {
			once.Do(func() { close(done) })
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>WHIP protocol smoke</title><p>Temporary protocol test fixture.</p>"))
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()
	if err := os.WriteFile(filepath.Join(directory, "bridge.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Minute):
		t.Fatal("browser harness timed out")
	}
}
