package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/daemon"
)

func TestWebEndpointRequiresExplicitRuntimeChanges(t *testing.T) {
	for _, test := range []struct {
		name                      string
		status                    daemonStatus
		override, want, errorText string
	}{
		{name: "stopped", status: daemonStatus{State: "stopped"}, errorText: "WHIP_NETWORK=1 whip daemon start"},
		{name: "network disabled", status: daemonStatus{State: "running"}, errorText: "WHIP_NETWORK=1 whip daemon restart"},
		{name: "unhealthy", status: daemonStatus{State: "unhealthy", Error: "broken"}, errorText: "broken"},
		{name: "discovered", status: daemonStatus{State: "running", NetworkEndpoint: "http://127.0.0.1:43210"}, want: "http://127.0.0.1:43210"},
		{name: "proxy", status: daemonStatus{State: "running", NetworkEndpoint: "http://127.0.0.1:43210"}, override: "https://whip.example/", want: "https://whip.example"},
		{name: "script rejected", status: daemonStatus{State: "running", NetworkEndpoint: "javascript:alert(1)"}, errorText: "HTTP or HTTPS"},
		{name: "credentials rejected", status: daemonStatus{State: "running", NetworkEndpoint: "http://user:secret@localhost:8080"}, errorText: "without credentials"},
		{name: "path rejected", status: daemonStatus{State: "running", NetworkEndpoint: "http://localhost:8080/session"}, errorText: "without credentials"},
		{name: "wildcard", status: daemonStatus{State: "running", NetworkEndpoint: "http://[::]:8080"}, errorText: "wildcard"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := webEndpoint(test.status, test.override)
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

func TestWebCLIUsesDiscoveredURLWithoutStartingRuntime(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/web" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"available":true,"protocol_major":3,"websocket_path":"/api/v3/ws","content_path":"/api/v3/content/"}`)
	}))
	defer server.Close()
	oldProbe, oldOpen, oldLaunch := probeWebDaemon, openWebBrowser, launchManagedDaemon
	t.Cleanup(func() { probeWebDaemon, openWebBrowser, launchManagedDaemon = oldProbe, oldOpen, oldLaunch })
	probeWebDaemon = func(daemon.RuntimePaths, time.Duration) (daemonStatus, *daemon.Client) {
		return daemonStatus{State: "running", NetworkEndpoint: server.URL}, nil
	}
	opens := 0
	openWebBrowser = func(value string) bool {
		opens++
		if value != server.URL+"/" {
			t.Errorf("opened %s", value)
		}
		return true
	}
	launchManagedDaemon = func(daemon.RuntimePaths) error {
		t.Fatal("web command must never launch or replace runtime")
		return nil
	}
	output := captureDaemonOutput(t, func() error { return webCLI([]string{"--no-open"}) })
	if strings.TrimSpace(output) != server.URL+"/" || opens != 0 {
		t.Fatalf("print URL %q opens=%d", output, opens)
	}
	captureDaemonOutput(t, func() error { return webCLI(nil) })
	if opens != 1 {
		t.Fatal("browser was not opened")
	}
}

func TestWebDiscoveryRejectsMissingAndIncompatibleAssets(t *testing.T) {
	for _, test := range []struct {
		name            string
		code            int
		body, errorText string
	}{
		{name: "not packaged", code: 200, body: `{"available":false,"protocol_major":3,"websocket_path":"/api/v3/ws","content_path":"/api/v3/content/"}`, errorText: "without web assets"},
		{name: "old daemon", code: 404, errorText: "explicitly restart"},
		{name: "host rejected", code: 403, errorText: "WHIP_ALLOWED_HOSTS"},
		{name: "major mismatch", code: 200, body: `{"available":true,"protocol_major":1}`, errorText: "incompatible"},
		{name: "malformed", code: 200, body: `<html>`, errorText: "invalid daemon web discovery"},
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
