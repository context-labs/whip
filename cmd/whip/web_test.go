package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/localruntime"
	"github.com/context-labs/whip/internal/protocol"
)

func TestWebEndpointValidation(t *testing.T) {
	for _, test := range []struct{ name, value, want, errorText string }{
		{name: "loopback", value: "http://127.0.0.1:43210", want: "http://127.0.0.1:43210"},
		{name: "proxy", value: "https://whip.example/", want: "https://whip.example"},
		{name: "script rejected", value: "javascript:alert(1)", errorText: "HTTP or HTTPS"},
		{name: "credentials rejected", value: "http://user:secret@localhost:8080", errorText: "without credentials"},
		{name: "path rejected", value: "http://localhost:8080/session", errorText: "without credentials"},
		{name: "query rejected", value: "http://localhost:8080?", errorText: "without credentials"},
		{name: "wildcard", value: "http://[::]:8080", errorText: "wildcard"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateWebEndpoint(test.value)
			if test.errorText != "" {
				if err == nil || !strings.Contains(err.Error(), test.errorText) {
					t.Fatalf("error %v", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("endpoint %q %v", got, err)
			}
		})
	}
}

func TestWebCLIExplicitURLNeverStartsRuntime(t *testing.T) {
	home := filepath.Join(t.TempDir(), "missing-home")
	t.Setenv("WHIPCODE_HOME", home)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/web" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprintf(w, `{"runtime_id":"fixture","process_epoch":"boot","max_content_bytes":4194304,"available":true,"major":%d,"websocket_path":"/api/v4/ws","content_path":"/api/v4/content/"}`, protocol.Major)
	}))
	defer server.Close()
	oldOpen, oldLaunch := openWebBrowser, launchNativeRuntime
	t.Cleanup(func() { openWebBrowser, launchNativeRuntime = oldOpen, oldLaunch })
	opens := 0
	openWebBrowser = func(value string) bool {
		opens++
		if value != server.URL+"/" {
			t.Errorf("opened %s", value)
		}
		return true
	}
	launchNativeRuntime = func(context.Context, localruntime.Paths, localruntime.Launch) (localruntime.Status, error) {
		t.Fatal("web command must never launch or replace runtime")
		return localruntime.Status{}, nil
	}
	output := captureDaemonOutput(t, func() error { return webCLI([]string{"--url", server.URL, "--no-open"}) })
	if strings.TrimSpace(output) != server.URL+"/" || opens != 0 {
		t.Fatalf("print URL %q opens=%d", output, opens)
	}
	captureDaemonOutput(t, func() error { return webCLI([]string{"--url", server.URL}) })
	if opens != 1 {
		t.Fatal("browser was not opened")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("explicit URL touched home: %v", err)
	}
}

func TestWebDiscoveryRejectsMissingAndIncompatibleAssets(t *testing.T) {
	for _, test := range []struct {
		name            string
		code            int
		body, errorText string
	}{
		{name: "not packaged", code: 200, body: fmt.Sprintf(`{"runtime_id":"fixture","process_epoch":"boot","max_content_bytes":4194304,"available":false,"major":%d,"websocket_path":"/api/v4/ws","content_path":"/api/v4/content/"}`, protocol.Major), errorText: "without web assets"},
		{name: "old endpoint", code: 404, errorText: "check the URL"},
		{name: "host rejected", code: 403, errorText: "WHIPCODE_ALLOWED_HOSTS"},
		{name: "major mismatch", code: 200, body: `{"runtime_id":"fixture","process_epoch":"boot","max_content_bytes":4194304,"available":true,"major":1}`, errorText: "invalid gateway web discovery"},
		{name: "malformed", code: 200, body: `<html>`, errorText: "invalid gateway web discovery"},
		{name: "oversized", code: 200, body: strings.Repeat(" ", 4097), errorText: "oversized gateway web discovery"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.code); fmt.Fprint(w, test.body) }))
			defer server.Close()
			if err := checkWebAssets(t.Context(), server.URL); err == nil || !strings.Contains(err.Error(), test.errorText) {
				t.Fatalf("error %v", err)
			}
		})
	}
}

func TestWebStoppedDaemonIsNotStarted(t *testing.T) {
	home := filepath.Join(nativeDaemonHome(t), "missing")
	t.Setenv("WHIPCODE_HOME", home)
	err := runWeb(t.Context(), []string{"--no-open"})
	if err == nil || !strings.Contains(err.Error(), "whipcode daemon start") {
		t.Fatalf("error %v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("web created home: %v", err)
	}
}
