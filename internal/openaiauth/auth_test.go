package openaiauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testCredentials() Credentials {
	return Credentials{
		AccessToken: "old-access", RefreshToken: "old-refresh", AccountID: "account",
		ExpiresAt: time.Now().Add(-time.Minute), Email: "test@example.com",
	}
}

func testManager(t *testing.T, handler http.HandlerFunc) *Manager {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	m := New(t.Context(), t.TempDir())
	m.issuer = server.URL
	t.Cleanup(m.Close)
	if err := m.Install(t.Context(), m.Generation(), testCredentials()); err != nil {
		t.Fatal(err)
	}
	return m
}

func refreshResponse(w http.ResponseWriter) {
	_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
}

func TestRefreshAllowsOmittedExpiryAndKeepsRotatingToken(t *testing.T) {
	m := testManager(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh"}`))
	})
	before := time.Now()
	credentials, err := m.Credentials(t.Context())
	if err != nil || credentials.RefreshToken != "new-refresh" || credentials.ExpiresAt.Before(before.Add(59*time.Minute)) ||
		credentials.ExpiresAt.After(time.Now().Add(time.Hour)) {
		t.Fatalf("optional token expiry did not use the upstream fallback: %v", err)
	}
}

func TestRefreshCoalescesAndSurvivesCancelledWaiter(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	m := testManager(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("refresh_token") != "old-refresh" {
			t.Error("incorrect refresh form")
		}
		<-release
		refreshResponse(w)
	})
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() { _, err := m.Credentials(ctx); first <- err }()
	<-started
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			credentials, err := m.Credentials(t.Context())
			if err != nil || credentials.RefreshToken != "new-refresh" {
				t.Errorf("refresh waiter failed: %v", err)
			}
		})
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller: %v", err)
	}
	close(release)
	wg.Wait()
	if requests.Load() != 1 {
		t.Fatalf("got %d refreshes", requests.Load())
	}
	restarted := New(t.Context(), filepath.Dir(m.path))
	t.Cleanup(restarted.Close)
	credentials, err := restarted.Credentials(t.Context())
	if err != nil || credentials.RefreshToken != "new-refresh" {
		t.Fatalf("rotation did not survive restart: %v", err)
	}
}

func TestRefreshCannotUndoLogoutOrReplacement(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "logout", true: "replacement"}[replace], func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			m := testManager(t, func(w http.ResponseWriter, _ *http.Request) {
				close(started)
				<-release
				refreshResponse(w)
			})
			result := make(chan error, 1)
			go func() { _, err := m.Credentials(t.Context()); result <- err }()
			<-started
			generation := m.Generation()
			if err := m.Logout(); err != nil {
				t.Fatal(err)
			}
			if err := m.Install(t.Context(), generation, testCredentials()); !errors.Is(err, ErrLoginChanged) {
				t.Fatalf("late login was accepted: %v", err)
			}
			if replace {
				credentials := testCredentials()
				credentials.AccessToken, credentials.AccountID = "replacement", "other-account"
				credentials.ExpiresAt = time.Now().Add(time.Hour)
				if err := m.Install(t.Context(), m.Generation(), credentials); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			if err := <-result; !errors.Is(err, ErrLoginChanged) {
				t.Fatalf("late refresh was accepted: %v", err)
			}
			credentials, err := m.Credentials(t.Context())
			if replace && (err != nil || credentials.AccessToken != "replacement") {
				t.Fatalf("replacement account was overwritten: %v", err)
			}
			if !replace && !errors.Is(err, ErrSignInRequired) {
				t.Fatalf("logout was undone: %v", err)
			}
		})
	}
}

func TestRotatedTokenSaveFailureDoesNotReuseOldRefreshToken(t *testing.T) {
	var requests atomic.Int32
	m := testManager(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		refreshResponse(w)
	})
	m.save = func(string, Credentials) error { return errors.New("disk unavailable") }
	if _, err := m.Credentials(t.Context()); err == nil {
		t.Fatal("reported success before persisting rotated token")
	}
	m.mu.Lock()
	m.save = saveCredentials
	m.mu.Unlock()
	credentials, err := m.Credentials(t.Context())
	if err != nil || credentials.RefreshToken != "new-refresh" || requests.Load() != 1 {
		t.Fatalf("did not retry persistence of rotated token: requests=%d err=%v", requests.Load(), err)
	}
}

func TestRefreshRejectionAndShutdown(t *testing.T) {
	t.Run("terminal rejection is sanitized and stops repeated refreshes", func(t *testing.T) {
		var requests atomic.Int32
		m := testManager(t, func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"refresh_token_reused","message":"secret-token"}}`))
		})
		for range 2 {
			_, err := m.Credentials(t.Context())
			if err == nil || strings.Contains(err.Error(), "secret-token") || !strings.Contains(err.Error(), "sign in again") {
				t.Fatalf("incorrect public error: %v", err)
			}
		}
		if requests.Load() != 1 {
			t.Fatal("retried terminal rejection")
		}
	})
	t.Run("close joins refresh", func(t *testing.T) {
		started := make(chan struct{})
		m := testManager(t, func(_ http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			close(started)
			<-r.Context().Done()
		})
		result := make(chan error, 1)
		go func() { _, err := m.Credentials(t.Context()); result <- err }()
		<-started
		m.Close()
		if err := <-result; err == nil {
			t.Fatal("refresh succeeded after shutdown")
		}
	})
}

func TestCredentialStorageProtectsExistingData(t *testing.T) {
	directory := t.TempDir()
	m := New(t.Context(), directory)
	t.Cleanup(m.Close)
	if err := m.Install(t.Context(), m.Generation(), testCredentials()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(m.path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials are not owner-only: %v", err)
	}
	for _, data := range []string{"broken-json", strings.Repeat("x", maxBytes+1), `{}`} {
		if err := os.WriteFile(m.path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		reopened := New(t.Context(), directory)
		if err := reopened.Install(t.Context(), reopened.Generation(), testCredentials()); err == nil {
			t.Error("silently overwrote malformed credentials")
		}
		reopened.Close()
		actual, err := os.ReadFile(m.path)
		if err != nil || string(actual) != data {
			t.Fatal("existing data was changed")
		}
	}
}

func TestDeviceFlow(t *testing.T) {
	var polls atomic.Int32
	claims := base64.RawURLEncoding.EncodeToString([]byte(
		`{"https://api.openai.com/auth":{"chatgpt_account_id":"account","chatgpt_compute_residency":"eu","chatgpt_plan_type":"pro"}}`,
	))
	m := testManager(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			_, _ = w.Write([]byte(`{"device_auth_id":"private","user_code":"ABCD-1234","interval":"1"}`))
		case "/api/accounts/deviceauth/token":
			if polls.Add(1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"authorization_code":"authorization","code_verifier":"verifier"}`))
		case "/oauth/token":
			if err := r.ParseForm(); err != nil || r.Form.Get("code_verifier") != "verifier" ||
				r.Form.Get("redirect_uri") != issuer+"/deviceauth/callback" {
				t.Error("incorrect authorization-code exchange")
			}
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "header." + claims + ".signature", RefreshToken: "refresh", ExpiresIn: 3600,
			})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	code, err := m.StartDevice(t.Context())
	if err != nil || code.UserCode != "ABCD-1234" || code.VerificationURL != issuer+"/codex/device" {
		t.Fatalf("start failed: %v", err)
	}
	code.interval = time.Millisecond
	credentials, err := m.CompleteDevice(t.Context(), code)
	if err != nil || credentials.AccountID != "account" || credentials.ComputeResidency != "eu" || credentials.Plan != "pro" {
		t.Fatalf("exchange failed: %v", err)
	}
	if polls.Load() != 2 {
		t.Fatal("did not wait through pending response")
	}
}

func TestDeviceFlowFailureBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "disabled initial request", status: 403, body: `{}`},
		{name: "missing endpoint", status: 404, body: `{}`},
		{name: "oversized", status: 200, body: strings.Repeat("x", maxBytes+1)},
		{name: "malformed", status: 200, body: `not json`},
		{name: "missing code", status: 200, body: `{}`},
		{name: "bad interval", status: 200, body: `{"device_auth_id":"id","user_code":"code","interval":0}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := testManager(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			})
			if _, err := m.StartDevice(t.Context()); err == nil {
				t.Fatal("accepted invalid initial device response")
			}
		})
	}
	m := testManager(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) })
	for _, expires := range []time.Time{time.Now().Add(-time.Second), time.Now().Add(time.Hour)} {
		ctx, cancel := context.WithCancel(t.Context())
		if expires.After(time.Now()) {
			cancel()
		}
		_, err := m.CompleteDevice(ctx, DeviceCode{deviceID: "id", interval: time.Millisecond, ExpiresAt: expires})
		cancel()
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("flow did not stop: %v", err)
		}
	}
}
