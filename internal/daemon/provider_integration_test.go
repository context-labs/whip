package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/inferencenet"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
)

type providerIntegrationTransport func(*http.Request) (*http.Response, error)

func (f providerIntegrationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// Install before constructing the daemon so its workers close before restoring
// the transport. Only the named upstream hosts and local test servers may run.
func providerHTTPFixture(t *testing.T, hosts []string, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	endpoint := httptest.NewServer(handler)
	previous := http.DefaultTransport
	http.DefaultTransport = providerIntegrationTransport(func(r *http.Request) (*http.Response, error) {
		if slices.Contains(hosts, r.URL.Hostname()) {
			request := r.Clone(r.Context())
			request.URL.Scheme, request.URL.Host = "http", endpoint.Listener.Addr().String()
			return previous.RoundTrip(request)
		}
		if r.URL.Scheme == "http" && net.ParseIP(r.URL.Hostname()).IsLoopback() {
			return previous.RoundTrip(r)
		}
		t.Errorf("unexpected provider fixture request to %s", r.URL.Hostname())
		return nil, fmt.Errorf("unexpected provider fixture request to %s", r.URL.Hostname())
	})
	t.Cleanup(func() {
		http.DefaultTransport = previous
		endpoint.Close()
	})
	return endpoint
}

func providerModelEndpoint(t *testing.T, key string, models []llm.ModelInfo) string {
	t.Helper()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("incorrect model discovery route or credentials")
			http.Error(w, "invalid discovery request", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": models})
	}))
	t.Cleanup(endpoint.Close)
	return endpoint.URL + "/v1"
}

func providerOnboardingHTTPFixture(t *testing.T) {
	t.Helper()
	endpoint := providerHTTPFixture(t, []string{"openrouter.ai"}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			if r.Header.Get("Authorization") != "Bearer private-api-key" {
				t.Error("OpenRouter discovery received an untrimmed or incorrect key")
				http.Error(w, "bad key", http.StatusUnauthorized)
				return
			}
		} else if !strings.HasPrefix(r.URL.Path, "/api/auth/device/") && r.Header.Get("Authorization") != "Bearer private-session-token" {
			t.Error("incorrect Inference.net session identity")
			http.Error(w, "bad session", http.StatusUnauthorized)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/rest/") && r.Header.Get("X-Inference-Team-Id") != "team" {
			t.Error("incorrect Inference.net workspace identity")
		}
		var body map[string]any
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("invalid provider request body: %v", err)
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/key":
			_, _ = w.Write([]byte(`{"data":{}}`))
		case "GET /api/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"fixture-model","supports_tools":true,"output_modalities":["text"]},{"id":"compact-model"}]}`))
		case "POST /api/auth/device/code":
			if body["client_id"] != inferencenet.DeviceClientID {
				t.Error("incorrect device client identity")
			}
			_, _ = w.Write([]byte(`{"device_code":"private-device-code","user_code":"display-code","expires_in":60,"interval":1}`))
		case "POST /api/auth/device/token":
			if body["client_id"] != inferencenet.DeviceClientID || body["device_code"] != "private-device-code" || body["grant_type"] != "urn:ietf:params:oauth:grant-type:device_code" {
				t.Error("incorrect device token request")
			}
			_, _ = w.Write([]byte(`{"access_token":"private-session-token"}`))
		case "GET /api/auth/get-session":
			_, _ = w.Write([]byte(`{"user":{"id":"person","email":"person@example.test"}}`))
		case "GET /api/auth/organization/list":
			_, _ = w.Write([]byte(`[{"id":"team","name":"Team"},{"id":"other","name":"Other"}]`))
		case "POST /api/auth/organization/set-active":
			if body["organizationId"] != "team" {
				t.Error("incorrect active workspace")
			}
			_, _ = w.Write([]byte(`{}`))
		case "GET /api/rest/projects":
			_, _ = w.Write([]byte(`[{"id":"existing","name":"Existing"},{"id":"other","name":"Other"}]`))
		case "POST /api/rest/projects/create":
			if body["name"] != "Created" {
				t.Error("project creation lost its trimmed name")
			}
			_, _ = w.Write([]byte(`{"id":"created","name":"Created"}`))
		case "POST /api/rest/api-keys":
			project, _ := body["defaultProjectId"].(string)
			name, _ := body["name"].(string)
			if !slices.Contains([]string{"existing", "created"}, project) || body["teamId"] != "team" || !strings.HasPrefix(name, "whip-") {
				t.Error("incorrect key provisioning identity")
			}
			wantScopes := []any{map[string]any{"permissions": []any{"read", "write"}, "projectId": project}}
			if !reflect.DeepEqual(body["scopes"], wantScopes) {
				t.Error("incorrect key provisioning scope")
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "key-" + project, "key": "private-machine-" + project})
		case "DELETE /api/rest/api-keys/key-created":
			if r.URL.Query().Get("teamId") != "team" {
				t.Error("logout archived a key under the wrong workspace")
			}
			_, _ = w.Write([]byte(`{}`))
		case "POST /api/auth/sign-out":
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected provider fixture route: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	})
	t.Cleanup(inferencenet.SetURLsForTest(endpoint.URL, endpoint.URL, endpoint.URL+"/v1"))
}

func waitProviderClientState(t *testing.T, client providerBehaviorClient, id, state string) protocol.ProviderLoginStatus {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	var result protocol.ProviderLoginStatus
	for {
		var err error
		result, err = client.LoginStatus(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if result.State == state {
			return result
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wanted %s, got %s", state, result.State)
		case <-ticker.C:
		}
	}
}
