package daemon

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/openaiauth"
)

func openAITestCredentials() openaiauth.Credentials {
	return openaiauth.Credentials{
		AccessToken: "private-access-token", RefreshToken: "private-refresh-token", AccountID: "account-id",
		ExpiresAt: time.Now().Add(time.Hour), Email: "account@example.com", Plan: "pro",
	}
}

func TestOpenAILoginAcrossUnixAndWebSocketWithoutJournalingSecrets(t *testing.T) {
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	ready, release := make(chan struct{}), make(chan struct{})
	providerHTTPFixture(t, []string{"auth.openai.com", "chatgpt.com"}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /api/accounts/deviceauth/usercode":
			_, _ = w.Write([]byte(`{"device_auth_id":"private-device-id","user_code":"private-device-code","interval":1}`))
		case "POST /api/accounts/deviceauth/token":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["device_auth_id"] != "private-device-id" || body["user_code"] != "private-device-code" {
				t.Error("device polling lost its authorization identity")
			}
			close(ready)
			select {
			case <-r.Context().Done():
				return
			case <-release:
			}
			_, _ = w.Write([]byte(`{"authorization_code":"private-authorization-code","code_verifier":"private-code-verifier"}`))
		case "POST /oauth/token":
			if err := r.ParseForm(); err != nil || r.Form.Get("code") != "private-authorization-code" || r.Form.Get("code_verifier") != "private-code-verifier" || r.Form.Get("grant_type") != "authorization_code" {
				t.Error("token exchange lost its authorization identity")
			}
			idToken := "fixture." + base64.RawURLEncoding.EncodeToString([]byte(`{"chatgpt_account_id":"account-id","email":"account@example.com"}`)) + ".fixture"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "private-access-token", "refresh_token": "private-refresh-token", "id_token": idToken, "expires_in": 3600,
			})
		case "GET /backend-api/codex/models":
			if r.Header.Get("Authorization") != "Bearer private-access-token" || r.Header.Get("Chatgpt-Account-Id") != "account-id" {
				t.Error("subscription discovery lost its account identity")
			}
			_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-5.5","visibility":"list","context_window":400000}]}`))
		default:
			t.Errorf("unexpected OpenAI fixture route: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	})
	fixture := newV2Fixture(t, &fakeRunner{})
	local := fixture.dial("unix", "local-login")
	flow, err := local.BeginProviderLogin(t.Context(), openaiauth.Provider)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("device authorization did not reach polling")
	}
	_ = local.Close()
	remote := fixture.dial("websocket", "remote-login")
	recovered, err := remote.BeginProviderLogin(t.Context(), openaiauth.Provider)
	if err != nil || recovered.FlowID != flow.FlowID || recovered.UserCode != "private-device-code" {
		t.Fatalf("cross-transport login recovery: %v", err)
	}
	close(release)
	waitProviderClientState(t, remote, flow.FlowID, "succeeded")
	status, err := remote.ProviderStatus(t.Context(), openaiauth.Provider)
	if err != nil || status.AuthState != "connected" {
		t.Fatalf("remote account status: %+v %v", status, err)
	}
	if _, err := remote.RotateProviderKey(t.Context(), openaiauth.Provider); err == nil {
		t.Fatal("subscription incorrectly exposed API-key rotation")
	}
	local = fixture.dial("unix", "local-logout")
	if _, err := local.LogoutProvider(t.Context(), openaiauth.Provider); err != nil {
		t.Fatal(err)
	}
	status, err = remote.ProviderStatus(t.Context(), openaiauth.Provider)
	if err != nil || status.AuthState != "signed_out" {
		t.Fatalf("logout did not propagate to remote client: %+v %v", status, err)
	}
	replay, err := remote.Replay(t.Context(), ReplayParams{RootID: fixture.rootID, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(replay)
	if err != nil || strings.Contains(string(data), "private-") {
		t.Fatal("authentication secrets reached the durable event log")
	}
}
