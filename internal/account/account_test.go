package account

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/session"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type fixture struct {
	service   *Service
	manager   *openaiauth.Manager
	authority *config.Authority
	directory string
	requests  atomic.Int32
}

func newFixture(t *testing.T, handler func(*fixture, http.ResponseWriter, *http.Request)) *fixture {
	t.Helper()
	f := &fixture{directory: t.TempDir()}
	if _, err := config.Initialize(f.directory); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if handler != nil {
			handler(f, w, r)
			return
		}
		deviceResponse(t, w, r)
	}))
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	local := server.Client().Transport
	http.DefaultTransport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "auth.openai.com" {
			return nil, errors.New("unexpected auth destination")
		}
		request := r.Clone(r.Context())
		request.URL.Scheme, request.URL.Host = endpoint.Scheme, endpoint.Host
		return local.RoundTrip(request)
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	f.manager = openaiauth.New(t.Context(), f.directory)
	t.Cleanup(f.manager.Close)
	f.authority, err = config.NewAuthority(f.directory)
	if err != nil {
		t.Fatal(err)
	}
	f.service, err = New(t.Context(), f.manager, f.authority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.service.Close)
	return f
}

func deviceResponse(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	switch r.URL.Path {
	case "/api/accounts/deviceauth/usercode":
		_, _ = w.Write([]byte(`{"device_auth_id":"private-device-id","user_code":"PUBLIC-CODE","interval":1}`))
	case "/api/accounts/deviceauth/token":
		_, _ = w.Write([]byte(`{"authorization_code":"private-authorization-code","code_verifier":"private-verifier"}`))
	case "/oauth/token":
		claims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"account","chatgpt_plan_type":"pro"}}`))
		identity := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"member@example.test"}`))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "private-access." + claims + ".signature", "refresh_token": "private-refresh",
			"id_token": "private-identity." + identity + ".signature", "expires_in": 3600,
		})
	default:
		t.Errorf("unexpected auth path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func finishFlow(t *testing.T, s *Service, id string) Flow {
	t.Helper()
	s.mu.Lock()
	flow := s.flows[id]
	s.mu.Unlock()
	select {
	case <-flow.done:
	case <-time.After(5 * time.Second):
		t.Fatal("flow did not finish")
	}
	result, err := s.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func waitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("expected boundary was not reached")
	}
}

func assertPublic(t *testing.T, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-", "device_auth_id", "access_token", "refresh_token", "code_verifier"} {
		if bytes.Contains(raw, []byte(private)) {
			t.Fatalf("private authentication data reached public projection: %s", private)
		}
	}
}

func TestBeginRecoverySetupStatusAndRestart(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	f := newFixture(t, func(_ *fixture, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/accounts/deviceauth/usercode" {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		deviceResponse(t, w, r)
	})
	defer unblock()
	before, err := f.authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.authority.Update(t.Context(), before.Revision, func(h *config.Host) error {
		h.Providers["existing"] = config.Provider{Kind: "openai-chat", BaseURL: "https://existing.test/v1"}
		h.Defaults.Model = session.ModelSelection{Provider: "existing", Name: "model", Effort: "high"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	observer, cancel := context.WithCancel(t.Context())
	first, err := f.service.Begin(observer)
	if err != nil {
		t.Fatal(err)
	}
	cancel() // Losing the initiating observer does not stop accepted work.
	waitSignal(t, started)
	var callers sync.WaitGroup
	for range 12 {
		callers.Go(func() {
			flow, err := f.service.Begin(t.Context())
			if err != nil || flow.ID != first.ID {
				t.Errorf("concurrent begin did not reuse active identity: %v", err)
			}
		})
	}
	callers.Wait()
	list, err := f.service.List(t.Context())
	if err != nil || len(list) != 1 || list[0].ID != first.ID || f.requests.Load() != 1 {
		t.Fatalf("lost acknowledgement cannot recover flow: %+v %v requests=%d", list, err, f.requests.Load())
	}
	unblock()
	completed := finishFlow(t, f.service, first.ID)
	if completed.State != Succeeded || completed.UserCode != "" || completed.VerificationURL != "" {
		t.Fatalf("login did not settle safely: %+v", completed)
	}
	status, err := f.service.Status(t.Context())
	if err != nil || status.AuthState != "stored" || status.RouteState != "configured" || status.Email != "member@example.test" || status.AccountID != "account" || status.Plan != "pro" || f.requests.Load() != 3 {
		t.Fatalf("saved account status=%+v err=%v requests=%d", status, err, f.requests.Load())
	}
	assertPublic(t, []any{completed, status, list})
	after, err := f.authority.Snapshot(t.Context())
	if err != nil || after.Host.Defaults.Model.Provider != "existing" || len(after.Host.Providers) != 2 {
		t.Fatal("login replaced defaults or existing routes", err)
	}
	f.service.Close()
	f.manager.Close()
	reopened := openaiauth.New(t.Context(), f.directory)
	t.Cleanup(reopened.Close)
	service, err := New(t.Context(), reopened, f.authority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	old, err := service.Get(t.Context(), first.ID)
	if err != nil || old.State != Interrupted {
		t.Fatalf("old process flow was not visibly interrupted: %+v %v", old, err)
	}
	status, err = service.Status(t.Context())
	if err != nil || status.AuthState != "stored" || f.requests.Load() != 3 {
		t.Fatal("restart status refreshed or lost credentials", err)
	}
	status, err = service.Logout(t.Context())
	if err != nil || status.AuthState != "signed_out" || status.RouteState != "configured" {
		t.Fatalf("logout changed route or retained credentials: %+v %v", status, err)
	}
	if _, err := os.Stat(filepath.Join(f.directory, "openai-codex.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("logout did not remove credentials: %v", err)
	}
}

func TestSavedCredentialsSurviveSetupConflictAndRetry(t *testing.T) {
	f := newFixture(t, func(f *fixture, w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			current, err := f.authority.Snapshot(r.Context())
			if err != nil {
				t.Error(err)
				return
			}
			_, err = f.authority.Update(r.Context(), current.Revision, func(h *config.Host) error {
				h.Providers[openaiauth.Provider] = config.Provider{Kind: "openai-chat", BaseURL: "https://custom.test/v1"}
				return nil
			})
			if err != nil {
				t.Error(err)
				return
			}
		}
		deviceResponse(t, w, r)
	})
	flow, err := f.service.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	completed := finishFlow(t, f.service, flow.ID)
	status, err := f.service.Status(t.Context())
	if err != nil || completed.State != SetupRequired || status.AuthState != "stored" || status.RouteState != "conflict" {
		t.Fatalf("credential success was conflated with setup failure: %+v %+v %v", completed, status, err)
	}
	if _, err := f.service.Setup(t.Context()); !errors.Is(err, ErrSetupRequired) {
		t.Fatalf("conflicting route overwritten: %v", err)
	}
	if _, err := f.service.Begin(t.Context()); !errors.Is(err, ErrConfiguration) || f.requests.Load() != 3 {
		t.Fatalf("conflicting setup made another device request: %v", err)
	}
	current, err := f.authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.authority.Update(t.Context(), current.Revision, func(h *config.Host) error { delete(h.Providers, openaiauth.Provider); return nil })
	if err != nil {
		t.Fatal(err)
	}
	generation := f.manager.Generation()
	status, err = f.service.Setup(t.Context())
	if err != nil || status.RouteState != "configured" || f.requests.Load() != 3 || f.manager.Generation() != generation {
		t.Fatalf("setup retried authorization/reinstalled credentials: %+v %v", status, err)
	}
	assertPublic(t, []any{completed, status})
}

func TestReplacementDuringAuthorizationCannotInstallStaleLogin(t *testing.T) {
	for _, accountID := range []string{"account", "different-account", "logout"} {
		t.Run(accountID, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			f := newFixture(t, func(_ *fixture, w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth/token" {
					close(started)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				deviceResponse(t, w, r)
			})
			defer unblock()
			flow, err := f.service.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			waitSignal(t, started)
			if accountID == "logout" {
				err = f.manager.Logout()
			} else {
				err = f.manager.Install(t.Context(), f.manager.Generation(), savedCredentials(accountID))
			}
			if err != nil {
				t.Fatal(err)
			}
			unblock()
			result := finishFlow(t, f.service, flow.ID)
			credentials, err := f.manager.Snapshot()
			if err != nil || result.State != Failed || (accountID == "logout" && credentials.AccessToken != "") || (accountID != "logout" && credentials.AccessToken != "replacement-private-access") {
				t.Fatal("late authorization undid replacement/logout", result.State, err)
			}
			assertPublic(t, result)
		})
	}
}

func savedCredentials(accountID string) openaiauth.Credentials {
	return openaiauth.Credentials{AccessToken: "replacement-private-access", RefreshToken: "replacement-private-refresh", AccountID: accountID, Email: "saved@example.test", ExpiresAt: time.Now().Add(-time.Hour)}
}

func TestStatusAndSetupNeverRefreshAndMalformedStorageFailsClosed(t *testing.T) {
	f := newFixture(t, func(_ *fixture, w http.ResponseWriter, _ *http.Request) {
		t.Error("unexpected network call")
		w.WriteHeader(http.StatusBadGateway)
	})
	if err := f.manager.Install(t.Context(), f.manager.Generation(), savedCredentials("saved-account")); err != nil {
		t.Fatal(err)
	}
	status, err := f.service.Status(t.Context())
	if err != nil || status.AuthState != "stored" || status.ExpiresAt == nil || !status.ExpiresAt.Before(time.Now()) || status.RouteState != "missing" {
		t.Fatalf("expired stored credentials misrepresented: %+v %v", status, err)
	}
	status, err = f.service.Setup(t.Context())
	if err != nil || status.AuthState != "stored" || status.RouteState != "configured" || f.requests.Load() != 0 {
		t.Fatalf("setup contacted provider: %+v %v", status, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.service.Logout(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled logout changed credentials", err)
	}
	current, err := f.service.Status(t.Context())
	if err != nil || !reflect.DeepEqual(status, current) {
		t.Fatal("cancelled logout changed status", err)
	}
	f.service.Close()
	f.manager.Close()
	if err := os.WriteFile(filepath.Join(f.directory, "openai-codex.json"), []byte(`{"accessToken":"private-invalid"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := openaiauth.New(t.Context(), f.directory)
	t.Cleanup(manager.Close)
	service, err := New(t.Context(), manager, f.authority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	if _, err := service.Begin(t.Context()); !errors.Is(err, ErrCredentials) {
		t.Fatal("malformed storage allowed new authorization", err)
	}
	status, err = service.Status(t.Context())
	if err != nil || status.AuthState != "unavailable" || f.requests.Load() != 0 {
		t.Fatalf("malformed status contacted provider: %+v %v", status, err)
	}
	assertPublic(t, status)
}

func TestCancelAndCloseJoinStartingAndPollingRequests(t *testing.T) {
	for _, path := range []string{"/api/accounts/deviceauth/usercode", "/api/accounts/deviceauth/token"} {
		for _, action := range []string{"cancel", "close", "logout"} {
			t.Run(action+path, func(t *testing.T) {
				started := make(chan struct{})
				cancelled, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
				unblock := sync.OnceFunc(func() { close(release) })
				defer unblock()
				f := newFixture(t, func(_ *fixture, w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == path {
						_, _ = io.Copy(io.Discard, r.Body)
						close(started)
						select {
						case <-r.Context().Done():
						case <-release:
						}
						return
					}
					deviceResponse(t, w, r)
				})
				local := http.DefaultTransport
				http.DefaultTransport = transport(func(r *http.Request) (*http.Response, error) {
					response, err := local.RoundTrip(r)
					if r.URL.Path == path {
						close(cancelled)
						<-release
						close(exited)
					}
					return response, err
				})
				flow, err := f.service.Begin(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				waitSignal(t, started)
				result := make(chan error, 1)
				done := make(chan struct{})
				defer func() {
					unblock()
					f.service.cancel()
					waitSignal(t, done)
				}()
				go func() {
					defer close(done)
					switch action {
					case "cancel":
						_, err := f.service.Cancel(t.Context(), flow.ID)
						result <- err
					case "logout":
						_, err := f.service.Logout(t.Context())
						result <- err
					default:
						f.service.Close()
						result <- nil
					}
				}()
				waitSignal(t, cancelled)
				select {
				case err := <-result:
					t.Fatal("returned without joining device request", err)
				default:
				}
				unblock()
				if err := <-result; err != nil {
					t.Fatal(err)
				}
				waitSignal(t, exited)
				if action != "close" {
					settled, err := f.service.Get(t.Context(), flow.ID)
					if err != nil || settled.UserCode != "" || settled.State == Authorizing || settled.State == Succeeded {
						t.Fatalf("cancelled request published: %+v %v", settled, err)
					}
				}
				// Service.Close never closes its borrowed credential manager.
				if err := f.manager.Install(t.Context(), f.manager.Generation(), savedCredentials("still-owned")); err != nil {
					t.Fatal("service closed borrowed manager", err)
				}
			})
		}
	}
}

func TestFlowBoundsExpiryAndSafeFailures(t *testing.T) {
	f := newFixture(t, func(_ *fixture, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"private-provider-body"}}`))
	})
	var first Flow
	for index := range maxFlows {
		flow, err := f.service.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		finished := finishFlow(t, f.service, flow.ID)
		if index == 0 {
			first = finished
		}
		if finished.State != Failed || !strings.Contains(finished.Failure, "enable device code authorization") {
			t.Fatalf("device denial lost safe guidance: %+v", finished)
		}
		assertPublic(t, finished)
	}
	if _, err := f.service.Begin(t.Context()); !errors.Is(err, ErrLimit) {
		t.Fatal("flow bound ignored", err)
	}
	f.service.mu.Lock()
	f.service.flows[first.ID].retainUntil = time.Now().Add(-time.Second)
	f.service.mu.Unlock()
	list, err := f.service.List(t.Context())
	if err != nil || len(list) != maxFlows-1 {
		t.Fatalf("expired flow did not free retention: %d %v", len(list), err)
	}
	if _, err := f.service.Get(t.Context(), first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired current-process record remained", err)
	}
	if _, err := f.service.Get(t.Context(), "invalid"); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid flow identity accepted", err)
	}
	flow, err := f.service.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	finishFlow(t, f.service, flow.ID)
}

func TestExpirationAndServiceContextStopAuthorization(t *testing.T) {
	for _, state := range []FlowState{Expired, Interrupted} {
		t.Run(string(state), func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			f := newFixture(t, func(_ *fixture, _ http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(started)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			})
			if state == Expired {
				f.service.lifetime = 100 * time.Millisecond
			}
			flow, err := f.service.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			waitSignal(t, started)
			if state == Interrupted {
				f.service.cancel()
			}
			f.service.mu.Lock()
			current := f.service.flows[flow.ID]
			f.service.mu.Unlock()
			waitSignal(t, current.done)
			f.service.mu.Lock()
			finished := current.view
			f.service.mu.Unlock()
			credentials, err := f.manager.Snapshot()
			if err != nil || finished.State != state || finished.UserCode != "" || credentials.AccessToken != "" || f.requests.Load() != 1 {
				t.Fatalf("expired/interrupted authorization progressed: state=%s requests=%d err=%v", finished.State, f.requests.Load(), err)
			}
		})
	}
}

func TestInstallAndSetupPublicationOrderWithLogoutAndCancel(t *testing.T) {
	for _, action := range []string{"logout", "cancel", "close"} {
		t.Run(action, func(t *testing.T) {
			exchange, response := make(chan struct{}), make(chan struct{})
			releaseResponse := sync.OnceFunc(func() { close(response) })
			defer releaseResponse()
			f := newFixture(t, func(_ *fixture, w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth/token" {
					close(exchange)
					select {
					case <-response:
					case <-r.Context().Done():
						return
					}
				}
				deviceResponse(t, w, r)
			})
			flow, err := f.service.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			waitSignal(t, exchange)
			active, err := f.service.Get(t.Context(), flow.ID)
			if err != nil || active.State != Authorizing || active.UserCode != "PUBLIC-CODE" || active.VerificationURL != "https://auth.openai.com/codex/device" {
				t.Fatalf("missing safe approval details: %+v %v", active, err)
			}
			assertPublic(t, active)

			// Hold the real config publication lock while the token response is
			// installed. The service must order later mutations after this commit.
			snapshot, err := f.authority.Snapshot(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			locked, publish := make(chan struct{}), make(chan struct{})
			releasePublish := sync.OnceFunc(func() { close(publish) })
			writerDone := make(chan struct{})
			defer func() { releasePublish(); waitSignal(t, writerDone) }()
			go func() {
				defer close(writerDone)
				_, err := f.authority.Update(t.Context(), snapshot.Revision, func(*config.Host) error {
					close(locked)
					<-publish
					return nil
				})
				if err != nil {
					t.Error(err)
				}
			}()
			waitSignal(t, locked)
			generation := f.manager.Generation()
			releaseResponse()
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			timeout := time.After(5 * time.Second)
			for f.manager.Generation() == generation {
				select {
				case <-ticker.C:
				case <-timeout:
					t.Fatal("credential installation was not reached")
				}
			}
			completed := make(chan struct{})
			defer func() { releasePublish(); waitSignal(t, completed) }()
			go func() {
				defer close(completed)
				switch action {
				case "logout":
					_, err := f.service.Logout(t.Context())
					if err != nil {
						t.Error(err)
					}
				case "cancel":
					result, err := f.service.Cancel(t.Context(), flow.ID)
					if err != nil || result.State != Succeeded {
						t.Errorf("cancellation relabeled completed login: %s %v", result.State, err)
					}
				case "close":
					f.service.Close()
				}
			}()
			if action == "close" {
				waitSignal(t, f.service.ctx.Done())
			}
			releasePublish()
			waitSignal(t, completed)
			credentials, err := f.manager.Snapshot()
			if err != nil || (credentials.AccessToken == "") != (action == "logout") {
				t.Fatal("mutation order lost saved credentials or reinstalled after logout", err)
			}
			if action == "close" {
				f.service.mu.Lock()
				result := f.service.flows[flow.ID].view
				f.service.mu.Unlock()
				if result.State != SetupRequired {
					t.Fatalf("shutdown lost saved-login/setup boundary: %+v", result)
				}
			}
		})
	}
}
