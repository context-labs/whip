package codexauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSourceAvailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		auth string
		want error
	}{
		{
			name: "valid Codex login",
			auth: `{"tokens":{"access_token":"access","refresh_token":"refresh","account_id":"account"}}`,
		},
		{name: "absent login", want: ErrLoginRequired},
		{name: "malformed saved credentials", auth: `{not JSON`, want: ErrLoginRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.auth != "" {
				writeAuth(t, filepath.Join(home, ".codex", "auth.json"), tc.auth)
			}

			err := (&Source{HomeDir: home}).Available()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Available() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCredentialsReadsCodex(t *testing.T) {
	home := t.TempDir()
	writeAuth(t, filepath.Join(home, ".codex", "auth.json"), `{
		"tokens":{"access_token":"codex-access","refresh_token":"codex-refresh","account_id":"codex-account"}
	}`)

	source := &Source{HomeDir: home}
	got, err := source.Credentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "codex-access" || got.AccountID != "codex-account" {
		t.Fatalf("credentials = %+v, want Codex credentials", got)
	}
}

func TestCredentialsReadsCodexJWTClaims(t *testing.T) {
	home := t.TempDir()
	expires := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	writeAuth(t, filepath.Join(home, ".codex", "auth.json"), `{
		"auth_mode":"chatgpt",
		"tokens":{"access_token":"access","refresh_token":"refresh","id_token":"`+jwt(t, expires, "jwt-account")+`"}
	}`)

	source := &Source{HomeDir: home}
	got, err := source.Credentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "jwt-account" {
		t.Fatalf("credentials = %+v", got)
	}
	loaded, err := source.load()
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.expiresAt.Equal(expires) {
		t.Fatalf("expires at = %v, want %v", loaded.expiresAt, expires)
	}
}

func TestCredentialsRefreshesCodex(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	path := filepath.Join(home, ".codex", "auth.json")
	writeAuth(t, path, `{
		"other":{"keep":true},
		"tokens":{"access_token":"old-access","refresh_token":"old-refresh","id_token":"`+jwt(t, now.Add(-time.Hour), "account")+`","custom":"preserve"}
	}`)

	var refreshToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		refreshToken = r.PostForm.Get("refresh_token")
		w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","id_token":"` + jwt(t, expires, "account") + `"}`))
	}))
	defer srv.Close()

	source := Source{HomeDir: home, HTTP: srv.Client(), TokenURL: srv.URL + "/oauth/token", now: func() time.Time { return now }}
	got, err := source.Credentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "new-access" || got.AccountID != "account" || refreshToken != "old-refresh" {
		t.Fatalf("credentials = %+v, refresh token = %q", got, refreshToken)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	var other map[string]bool
	if err := json.Unmarshal(stored["other"], &other); err != nil || !other["keep"] {
		t.Fatalf("unrelated root fields were lost: %s", data)
	}
	var tokens map[string]json.RawMessage
	if err := json.Unmarshal(stored["tokens"], &tokens); err != nil {
		t.Fatal(err)
	}
	if string(tokens["custom"]) != `"preserve"` || string(tokens["access_token"]) != `"new-access"` || string(tokens["refresh_token"]) != `"new-refresh"` {
		t.Fatalf("stored tokens = %s", stored["tokens"])
	}
}

func TestCredentialsRejectsInvalidRefreshResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "non-success response", status: http.StatusUnauthorized, body: "sign in again"},
		{name: "malformed JSON", status: http.StatusOK, body: "not JSON"},
		{name: "missing access token", status: http.StatusOK, body: `{"refresh_token":"new-refresh"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
			writeAuth(t, filepath.Join(home, ".codex", "auth.json"), `{"tokens":{"access_token":"expired-access","refresh_token":"old-refresh","id_token":"`+jwt(t, now.Add(-time.Hour), "account")+`"}}`)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/oauth/token" {
					http.NotFound(w, r)
					return
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") != "old-refresh" {
					t.Fatalf("refresh form = %v", r.PostForm)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			source := Source{HomeDir: home, HTTP: srv.Client(), TokenURL: srv.URL + "/oauth/token", now: func() time.Time { return now }}
			_, err := source.Credentials(context.Background())
			if err == nil || !strings.Contains(err.Error(), "could not refresh codex login; run whip auth codex") {
				t.Fatalf("Credentials() error = %v, want actionable refresh error", err)
			}
			if strings.Contains(err.Error(), "expired-access") || strings.Contains(err.Error(), "old-refresh") {
				t.Fatalf("Credentials() leaked saved credential in error: %v", err)
			}
		})
	}
}

func TestCredentialsReportsRefreshPersistenceFailure(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(home, ".codex")
	path := filepath.Join(dir, "auth.json")
	writeAuth(t, path, `{"tokens":{"access_token":"expired-access","refresh_token":"old-refresh","id_token":"`+jwt(t, now.Add(-time.Hour), "account")+`"}}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		// Credentials has already read auth.json by the time refresh reaches the
		// server. Replace its parent directory with a file so the subsequent
		// atomic write must fail, independent of platform permission semantics.
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
	}))
	defer srv.Close()

	source := Source{HomeDir: home, HTTP: srv.Client(), TokenURL: srv.URL + "/oauth/token", now: func() time.Time { return now }}
	_, err := source.Credentials(context.Background())
	if err == nil || !strings.Contains(err.Error(), "save refreshed codex login") {
		t.Fatalf("Credentials() error = %v, want persistence failure", err)
	}
}

func TestCredentialsIgnoresPi(t *testing.T) {
	home := t.TempDir()
	writeAuth(t, filepath.Join(home, ".pi", "agent", "auth.json"), `{
		"openai-codex":{"access":"secret-access","refresh":"secret-refresh","expires":4102444800,"accountId":"account"}
	}`)
	_, err := (&Source{HomeDir: home}).Credentials(context.Background())
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("error = %v, want login hint", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("token leaked in error: %v", err)
	}
}

func TestDeviceLoginPersistsCodexCredentials(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "auth.json")
	writeAuth(t, path, `{"other":{"keep":true}}`)

	expires := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	var userCode, poll, exchange bool
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			userCode = true
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["client_id"] != clientID {
				t.Fatalf("client id = %q", body["client_id"])
			}
			w.Write([]byte(`{"device_auth_id":"device-id","user_code":"ABCD-1234","interval":"1"}`))
		case "/api/accounts/deviceauth/token":
			poll = true
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["device_auth_id"] != "device-id" || body["user_code"] != "ABCD-1234" {
				t.Fatalf("poll = %#v", body)
			}
			w.Write([]byte(`{"authorization_code":"authorization-code","code_verifier":"verifier"}`))
		case "/oauth/token":
			exchange = true
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("code") != "authorization-code" || r.PostForm.Get("code_verifier") != "verifier" || r.PostForm.Get("client_id") != clientID || r.PostForm.Get("redirect_uri") != srv.URL+"/deviceauth/callback" {
				t.Fatalf("exchange form = %v", r.PostForm)
			}
			w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","id_token":"` + jwt(t, expires, "new-account") + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var shown DeviceCode
	source := Source{HomeDir: home, HTTP: srv.Client(), IssuerURL: srv.URL}
	if err := source.DeviceLogin(context.Background(), func(code DeviceCode) { shown = code }); err != nil {
		t.Fatal(err)
	}
	if !userCode || !poll || !exchange {
		t.Fatalf("steps usercode=%t poll=%t exchange=%t", userCode, poll, exchange)
	}
	if shown.VerificationURL != srv.URL+"/codex/device" || shown.UserCode != "ABCD-1234" {
		t.Fatalf("shown code = %+v", shown)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("auth permissions = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	var other map[string]bool
	if err := json.Unmarshal(stored["other"], &other); err != nil || !other["keep"] {
		t.Fatalf("stored root = %s", data)
	}
	if string(stored["auth_mode"]) != `"chatgpt"` {
		t.Fatalf("stored root = %s", data)
	}
	got, err := source.Credentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "new-access" || got.AccountID != "new-account" {
		t.Fatalf("credentials = %+v", got)
	}
}

func TestDeviceLoginProtocolHelpers(t *testing.T) {
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		raw  json.RawMessage
		want time.Time
	}{
		{name: "number", raw: json.RawMessage(`60`), want: now.Add(time.Minute)},
		{name: "string", raw: json.RawMessage(`"120"`), want: now.Add(2 * time.Minute)},
		{name: "zero", raw: json.RawMessage(`0`)},
		{name: "invalid", raw: json.RawMessage(`"nope"`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := expiryFromDuration(tc.raw, now); !got.Equal(tc.want) {
				t.Fatalf("expiryFromDuration(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name string
		raw  json.RawMessage
		want time.Duration
	}{
		{name: "string", raw: json.RawMessage(`"2"`), want: 2 * time.Second},
		{name: "zero", raw: json.RawMessage(`0`), want: time.Second},
		{name: "invalid", raw: json.RawMessage(`"bad"`), want: time.Second},
		{name: "capped", raw: json.RawMessage(`99999`), want: deviceLoginTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pollInterval(tc.raw); got != tc.want {
				t.Fatalf("pollInterval(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
	if _, ok := jwtClaims("not-a-jwt"); ok {
		t.Fatal("invalid JWT was accepted")
	}
	if claims, ok := jwtClaims(jwt(t, now.Add(time.Hour), "account")); !ok || claims.Auth.ChatGPTAccountID != "account" {
		t.Fatalf("JWT claims = %+v, ok=%t", claims, ok)
	}
}

func TestDeviceLoginProtocolErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			_, _ = w.Write([]byte(`{"device_auth_id":""}`))
		case "/oauth/token":
			_, _ = w.Write([]byte(`{"access_token":"only-access"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	source := Source{HTTP: srv.Client(), IssuerURL: srv.URL, TokenURL: srv.URL + "/oauth/token"}
	if _, err := source.requestDeviceCode(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid server response") {
		t.Fatalf("invalid device code error = %v", err)
	}
	if _, err := source.exchangeDeviceCode(context.Background(), "code", "verifier"); err == nil || !strings.Contains(err.Error(), "invalid server response") {
		t.Fatalf("invalid token exchange error = %v", err)
	}
}

func TestDeviceLoginUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	err := (&Source{HomeDir: t.TempDir(), HTTP: srv.Client(), IssuerURL: srv.URL}).DeviceLogin(context.Background(), nil)
	if !errors.Is(err, ErrDeviceLoginUnsupported) {
		t.Fatalf("error = %v", err)
	}
}

func TestDeviceLoginCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			w.Write([]byte(`{"device_auth_id":"device-id","user_code":"ABCD-1234","interval":"1"}`))
		case "/api/accounts/deviceauth/token":
			cancel()
			w.WriteHeader(http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err := (&Source{HomeDir: t.TempDir(), HTTP: srv.Client(), IssuerURL: srv.URL}).DeviceLogin(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
}

func TestDeviceLoginReportsPollingFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			_, _ = w.Write([]byte(`{"device_auth_id":"device-id","user_code":"ABCD-1234","interval":"1"}`))
		case "/api/accounts/deviceauth/token":
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err := (&Source{HomeDir: t.TempDir(), HTTP: srv.Client(), IssuerURL: srv.URL}).DeviceLogin(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "poll device login: server returned 503 Service Unavailable") {
		t.Fatalf("error = %v, want polling failure", err)
	}
}

func TestDeviceLoginRequiresAccountClaim(t *testing.T) {
	expires := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	idToken := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":`+strconv.FormatInt(expires.Unix(), 10)+`}`)) + ".sig"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			_, _ = w.Write([]byte(`{"device_auth_id":"device-id","user_code":"ABCD-1234","interval":"1"}`))
		case "/api/accounts/deviceauth/token":
			_, _ = w.Write([]byte(`{"authorization_code":"authorization-code","code_verifier":"verifier"}`))
		case "/oauth/token":
			_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","id_token":"` + idToken + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err := (&Source{HomeDir: t.TempDir(), HTTP: srv.Client(), IssuerURL: srv.URL}).DeviceLogin(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "could not determine Codex account") {
		t.Fatalf("error = %v, want missing account claim", err)
	}
}

func TestDeviceLoginEndpointStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	source := Source{HTTP: srv.Client(), IssuerURL: srv.URL, TokenURL: srv.URL + "/oauth/token"}
	if _, err := source.requestDeviceCode(context.Background()); err == nil || !strings.Contains(err.Error(), "start device login: server returned 503 Service Unavailable") {
		t.Fatalf("device-code error = %v", err)
	}
	if _, err := source.exchangeDeviceCode(context.Background(), "code", "verifier"); err == nil || !strings.Contains(err.Error(), "exchange device login: server returned 503 Service Unavailable") {
		t.Fatalf("token-exchange error = %v", err)
	}
}

func jwt(t *testing.T, expires time.Time, account string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(map[string]any{
		"exp": expires.Unix(),
		"https://api.openai.com/auth": map[string]string{
			"chatgpt_account_id": account,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func writeAuth(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
