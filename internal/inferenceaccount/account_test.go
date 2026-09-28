package inferenceaccount

import (
	"context"
	"crypto/rand"
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
	"testing"
	"time"

	"github.com/context-labs/whip/internal/inferenceauth"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	t            *testing.T
	service      *Service
	manager      *inferenceauth.Manager
	directory    string
	mu           sync.Mutex
	requests     []string
	teams        []Team
	projects     []Project
	hooks        map[string]http.HandlerFunc
	setupFailure bool
	setups       int
	active       string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, directory: t.TempDir(), teams: []Team{{ID: "team", Name: "Team", Slug: "team"}}, projects: []Project{{ID: "project", Name: "Project"}}, hooks: map[string]http.HandlerFunc{}}
	manager, err := inferenceauth.New(t.Context(), f.directory)
	if err != nil {
		t.Fatal(err)
	}
	f.manager = manager
	t.Cleanup(func() {
		if err := manager.Close(); err != nil && !errors.Is(err, inferenceauth.ErrStoragePending) {
			t.Error(err)
		}
	})
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme+"://"+r.URL.Host != controlURL {
			return nil, errors.New("unexpected control-plane route")
		}
		local := r.Clone(r.Context())
		local.URL.Scheme, local.URL.Host = target.Scheme, target.Host
		return server.Client().Transport.RoundTrip(local)
	})}
	f.service, err = New(t.Context(), manager, client, func(context.Context) error {
		captured, err := manager.Capture(t.Context())
		if err != nil || captured.Key != "private-machine" {
			t.Error("route setup ran before durable machine key")
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.setups++
		if f.setupFailure {
			return errors.New("private setup diagnostic")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.service.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	t.Cleanup(f.service.Close)
	return f
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	hook := f.hooks[r.URL.Path]
	if hook != nil {
		f.mu.Unlock()
		hook(w, r)
		return
	}
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if !strings.HasPrefix(r.URL.Path, "/api/auth/device/") && r.Header.Get("Authorization") != "Bearer private-management" {
		f.t.Error("control-plane request did not use management authorization")
	}
	var body map[string]any
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Error(err)
		}
	}
	switch r.URL.Path {
	case "/api/auth/device/code":
		if body["client_id"] != "whip" {
			f.t.Error("wrong device client")
		}
		_, _ = io.WriteString(w, `{"device_code":"private-device","user_code":"PUBLIC-CODE","expires_in":900,"interval":1}`)
	case "/api/auth/device/token":
		if body["client_id"] != "whip" || body["device_code"] != "private-device" || body["grant_type"] != "urn:ietf:params:oauth:grant-type:device_code" {
			f.t.Error("wrong device exchange body")
		}
		_, _ = io.WriteString(w, `{"access_token":"private-management"}`)
	case "/api/auth/get-session":
		if r.URL.RawQuery != "disableCookieCache=true" {
			f.t.Error("session cache was not disabled")
		}
		_, _ = io.WriteString(w, `{"user":{"id":"user","email":"person@example.test"}}`)
	case "/api/auth/organization/list":
		_ = json.NewEncoder(w).Encode(f.teams)
	case "/api/auth/organization/set-active":
		f.active, _ = body["organizationId"].(string)
		_, _ = io.WriteString(w, `{}`)
	case "/api/rest/projects":
		if f.active != r.Header.Get("X-Inference-Team-Id") {
			f.t.Error("set-active/project request interleaved")
		}
		_ = json.NewEncoder(w).Encode(f.projects)
	case "/api/rest/projects/create":
		if f.active != r.Header.Get("X-Inference-Team-Id") || body["name"] != "Explicit project" {
			f.t.Error("project creation lost explicit scope/name")
		}
		_, _ = io.WriteString(w, `{"id":"created","name":"Explicit project"}`)
	case "/api/rest/api-keys":
		if f.active != r.Header.Get("X-Inference-Team-Id") || body["teamId"] != f.active {
			f.t.Error("key request lost active scope")
		}
		project, _ := body["defaultProjectId"].(string)
		want := []any{map[string]any{"permissions": []any{"read", "write"}, "projectId": project}}
		if !reflect.DeepEqual(body["scopes"], want) {
			f.t.Error("key permissions/body changed")
		}
		_, _ = io.WriteString(w, `{"id":"new-key","key":"private-machine"}`)
	case "/api/auth/sign-out":
		_, _ = io.WriteString(w, `{}`)
	default:
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/rest/api-keys/") {
			if r.URL.Query().Get("teamId") != f.active {
				f.t.Error("archive lost team scope")
			}
			_, _ = io.WriteString(w, `{}`)
		} else {
			f.t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (f *fixture) hook(path string, handler http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hooks[path] = handler
}

func (f *fixture) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, request := range f.requests {
		if strings.Contains(request, " "+path) {
			n++
		}
	}
	return n
}

func (f *fixture) waitState(id string, wanted State) Flow {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view, err := f.service.Get(f.t.Context(), id)
		if err != nil {
			f.t.Fatal(err)
		}
		f.service.mu.Lock()
		busy := f.service.flows[id].busy
		f.service.mu.Unlock()
		if view.State == wanted && !busy {
			return view
		}
		if terminal(view.State) && view.State != wanted {
			f.t.Fatalf("wanted %s, got %+v", wanted, view)
		}
		time.Sleep(time.Millisecond)
	}
	view, _ := f.service.Get(f.t.Context(), id)
	f.t.Fatalf("wanted %s, got %+v", wanted, view)
	return Flow{}
}

func (f *fixture) begin() Flow {
	f.t.Helper()
	view, err := f.service.Begin(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	return view
}

func TestLoginSingletonInstallsBeforeSetupWithoutPublicSecrets(t *testing.T) {
	f := newFixture(t)
	view := f.waitState(f.begin().ID, Succeeded)
	if view.VerificationURL != "" || view.UserCode != "" || view.TeamID != "team" || view.ProjectID != "project" {
		t.Fatalf("unexpected public flow: %+v", view)
	}
	status, err := f.service.Status(t.Context())
	if err != nil || status.ManagementState != "stored" || status.InferenceState != "stored" || status.ExpiresAt != nil {
		t.Fatalf("unexpected safe status: %+v %v", status, err)
	}
	flows, err := f.service.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal([]any{view, status, flows})
	if strings.Contains(string(encoded), "private-") {
		t.Fatal("private credentials/device code entered public DTOs")
	}
	if f.count("/api/rest/api-keys") != 1 || f.count("/api/rest/projects/create") != 0 {
		t.Fatal("singleton flow created extra resources")
	}
	if _, err := f.service.SelectProject(t.Context(), view.ID, "project"); !errors.Is(err, ErrBusy) {
		t.Fatal("terminal selection replayed key provisioning")
	}
	f.service.mu.Lock()
	if f.service.flows[view.ID].credentials.Management.Token != "" {
		t.Error("terminal flow retained credentials")
	}
	f.service.mu.Unlock()
}

func TestExplicitStableChoicesAndProjectCreation(t *testing.T) {
	f := newFixture(t)
	f.teams = []Team{{ID: "team-a", Name: "Same"}, {ID: "team-b", Name: "Same"}}
	f.projects = []Project{{ID: "project-a", Name: "Same"}, {ID: "project-b", Name: "Same"}}
	view := f.waitState(f.begin().ID, ChooseTeam)
	view.Teams[0].ID = "tampered"
	if _, err := f.service.SelectTeam(t.Context(), view.ID, "Same"); !errors.Is(err, ErrInvalid) {
		t.Fatal("team label selected arbitrary identity")
	}
	if _, err := f.service.SelectTeam(t.Context(), view.ID, "team-b"); err != nil {
		t.Fatal(err)
	}
	view = f.waitState(view.ID, ChooseProject)
	if f.count("/api/rest/api-keys") != 0 {
		t.Fatal("ambiguous project auto-selected")
	}
	if _, err := f.service.SelectProject(t.Context(), view.ID, "Same"); !errors.Is(err, ErrInvalid) {
		t.Fatal("project label selected arbitrary identity")
	}
	f.mu.Lock()
	f.projects = nil
	f.mu.Unlock()
	if _, err := f.service.SelectTeam(t.Context(), view.ID, "team-a"); err != nil {
		t.Fatal(err)
	}
	view = f.waitState(view.ID, ChooseProject)
	if len(view.Projects) != 0 || f.count("/api/rest/projects/create") != 0 {
		t.Fatal("missing project invented a default")
	}
	if _, err := f.service.SelectProject(t.Context(), view.ID, "project-b"); !errors.Is(err, ErrInvalid) {
		t.Fatal("stale team project was reused")
	}
	if _, err := f.service.CreateProject(t.Context(), view.ID, "Explicit project"); err != nil {
		t.Fatal(err)
	}
	view = f.waitState(view.ID, Succeeded)
	if view.ProjectID != "created" || view.TeamID != "team-a" {
		t.Fatal("explicit project selection lost")
	}
	if _, err := f.service.CreateProject(t.Context(), view.ID, "Explicit project"); !errors.Is(err, ErrBusy) || f.count("/api/rest/projects/create") != 1 {
		t.Fatal("project creation replayed")
	}
}

func TestBeginAcceptedBeforeNetworkAndCancelJoins(t *testing.T) {
	f := newFixture(t)
	entered, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	f.hook("/api/auth/device/code", func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		<-release
		close(exited)
	})
	ctx, cancel := context.WithCancel(t.Context())
	view, err := f.service.Begin(ctx)
	cancel()
	if err != nil || view.State != Authorizing {
		t.Fatal("begin did not accept before HTTP", err)
	}
	<-entered
	second, err := f.service.Begin(t.Context())
	if err != nil || second.ID != view.ID {
		t.Fatal("lost begin acknowledgement created another login")
	}
	done := make(chan error, 1)
	go func() { _, err := f.service.Cancel(t.Context(), view.ID); done <- err }()
	// Transport cancellation returns before the server handler; joining the
	// client HTTP operation is the host's boundary, not joining a remote server.
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not join client work")
	}
	unblock()
	<-exited
	f.waitState(view.ID, Cancelled)
	if f.count("/api/auth/device/token") != 0 {
		t.Fatal("cancelled device request polled")
	}
}

func TestKnownKeyPersistenceRetryDoesNotMintAgain(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.directory, "inference-net.json")
	f.hook("/api/rest/api-keys", func(w http.ResponseWriter, _ *http.Request) {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"id":"new-key","key":"private-machine"}`)
	})
	view := f.waitState(f.begin().ID, PersistenceRequired)
	if f.count("/api/rest/api-keys") != 1 {
		t.Fatal("mint did not run exactly once")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Retry(t.Context(), view.ID); err != nil {
		t.Fatal(err)
	}
	f.waitState(view.ID, Succeeded)
	if f.count("/api/rest/api-keys") != 1 || f.count("/api/auth/device/token") != 1 {
		t.Fatal("persistence retry repeated remote work")
	}
}

func TestKnownManagementPersistenceRetryDoesNotExchangeAgain(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.directory, "inference-net.json")
	f.hook("/api/auth/get-session", func(w http.ResponseWriter, _ *http.Request) {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"user":{"id":"user","email":"person@example.test"}}`)
	})
	view := f.waitState(f.begin().ID, PersistenceRequired)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Retry(t.Context(), view.ID); err != nil {
		t.Fatal(err)
	}
	f.waitState(view.ID, Succeeded)
	if f.count("/api/auth/device/token") != 1 {
		t.Fatal("management save retry exchanged again")
	}
}

func TestSetupFailureRetriesOnlySavedKey(t *testing.T) {
	f := newFixture(t)
	f.setupFailure = true
	view := f.waitState(f.begin().ID, SetupRequired)
	if _, err := f.manager.Capture(t.Context()); err != nil {
		t.Fatal("setup failure discarded installed key", err)
	}
	f.mu.Lock()
	f.setupFailure = false
	f.mu.Unlock()
	if _, err := f.service.Retry(t.Context(), view.ID); err != nil {
		t.Fatal(err)
	}
	f.waitState(view.ID, Succeeded)
	if f.count("/api/rest/api-keys") != 1 {
		t.Fatal("setup retry minted key")
	}
}

func TestUncertainCreationCannotBeReplayed(t *testing.T) {
	for _, path := range []string{"/api/rest/projects/create", "/api/rest/api-keys"} {
		t.Run(path, func(t *testing.T) {
			f := newFixture(t)
			if strings.Contains(path, "projects") {
				f.projects = nil
			}
			f.hook(path, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"private-error":`) })
			view := f.begin()
			if strings.Contains(path, "projects") {
				f.waitState(view.ID, ChooseProject)
				if _, err := f.service.CreateProject(t.Context(), view.ID, "Explicit project"); err != nil {
					t.Fatal(err)
				}
			}
			view = f.waitState(view.ID, Uncertain)
			if _, err := f.service.Retry(t.Context(), view.ID); !errors.Is(err, ErrInvalid) || f.count(path) != 1 {
				t.Fatal("uncertain creation replayed")
			}
			if strings.Contains(view.Failure, "private-error") {
				t.Fatal("provider body leaked")
			}
		})
	}
}

func TestRotationInstallsBeforeArchiveAndCleanupRetryDoesNotMint(t *testing.T) {
	f := newFixture(t)
	prior := inferenceauth.Credentials{Management: inferenceauth.Management{Token: "private-management", UserID: "user"}, Scope: inferenceauth.Scope{TeamID: "team", ProjectID: "project"}, MachineKey: inferenceauth.MachineKey{ID: "old-key", Value: "old-private-machine"}}
	if err := f.manager.Install(t.Context(), f.manager.Generation(), prior); err != nil {
		t.Fatal(err)
	}
	old, err := f.manager.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f.hook("/api/rest/api-keys/old-key", func(w http.ResponseWriter, _ *http.Request) {
		if err := f.manager.Check(t.Context(), old); !errors.Is(err, inferenceauth.ErrChanged) {
			t.Error("archive preceded durable replacement")
		}
		if current, err := f.manager.Capture(t.Context()); err != nil || current.Key != "private-machine" {
			t.Error("replacement not usable during old archive")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{}`)
	})
	view, err := f.service.Rotate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f.waitState(view.ID, CleanupRequired)
	f.hook("/api/rest/api-keys/old-key", nil)
	if _, err := f.service.Retry(t.Context(), view.ID); err != nil {
		t.Fatal(err)
	}
	f.waitState(view.ID, Succeeded)
	if f.count("/api/rest/api-keys/") != 2 || f.count("/api/rest/api-keys") != 3 {
		t.Fatal("cleanup retry minted or missed archive")
	}
}

func TestLogoutLocalFailureCannotBeClearedBySetup(t *testing.T) {
	f := newFixture(t)
	f.waitState(f.begin().ID, Succeeded)
	path := filepath.Join(f.directory, "inference-net.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Logout(t.Context())
	if err != nil || result.LocalFailure == "" || result.Status.InferenceState != "unavailable" {
		t.Fatalf("logout failure hidden: %+v %v", result, err)
	}
	if _, err := f.service.Setup(t.Context()); !errors.Is(err, ErrCredentials) {
		t.Fatal("setup cleared pending logout")
	}
	if _, err := f.manager.Capture(t.Context()); !errors.Is(err, inferenceauth.ErrStoragePending) {
		t.Fatal("failed local logout retained authorization")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	// Pending local deletion must remember already completed remote work even
	// if its ordinary public retention interval has passed.
	f.service.mu.Lock()
	for i := range f.service.cleanup {
		f.service.cleanup[i].expires = time.Now().Add(-time.Second)
	}
	f.service.mu.Unlock()
	result, err = f.service.Logout(t.Context())
	if err != nil || result.LocalFailure != "" || result.Status.InferenceState != "absent" {
		t.Fatalf("explicit logout retry failed: %+v %v", result, err)
	}
	if f.count("/api/auth/sign-out") != 1 || f.count("/api/rest/api-keys/") != 1 {
		t.Fatal("local persistence retry repeated completed remote cleanup")
	}
}

func TestCancelAndCloseJoinCreationAndPreserveUncertainty(t *testing.T) {
	for _, action := range []string{"cancel", "close"} {
		t.Run(action, func(t *testing.T) {
			f := newFixture(t)
			original := f.service.http.Transport
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			f.service.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/api/rest/api-keys" {
					return original.RoundTrip(r)
				}
				close(entered)
				<-r.Context().Done()
				<-release
				return nil, r.Context().Err()
			})
			view := f.begin()
			<-entered
			done := make(chan struct{})
			go func() {
				defer close(done)
				if action == "close" {
					f.service.Close()
				} else {
					_, _ = f.service.Cancel(t.Context(), view.ID)
				}
			}()
			select {
			case <-done:
				t.Fatal("cancellation did not join owned request")
			case <-time.After(10 * time.Millisecond):
			}
			unblock()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("cancellation failed to join released request")
			}
			f.service.mu.Lock()
			state := f.service.flows[view.ID].view.State
			f.service.mu.Unlock()
			if state != Uncertain {
				t.Fatalf("remote creation cancellation lost uncertainty: %s", state)
			}
			if _, err := f.manager.Capture(t.Context()); !errors.Is(err, inferenceauth.ErrKeyRequired) {
				t.Fatal("cancelled creation installed authorization")
			}
		})
	}
}

func TestGenerationChangeRejectsStaleDiscoveryWithoutProvisioning(t *testing.T) {
	f := newFixture(t)
	f.hook("/api/rest/projects", func(w http.ResponseWriter, _ *http.Request) {
		value := inferenceauth.Credentials{Management: inferenceauth.Management{Token: "replacement-management", UserID: "other-user"}}
		if err := f.manager.Install(t.Context(), f.manager.Generation(), value); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `[{"id":"project","name":"Project"}]`)
	})
	view := f.waitState(f.begin().ID, Interrupted)
	if len(view.Projects) != 0 || f.count("/api/rest/api-keys") != 0 {
		t.Fatal("stale project discovery provisioned against replaced authorization")
	}
}

func TestLogoutReportsCleanupFailureSeparatelyAndRetriesIt(t *testing.T) {
	f := newFixture(t)
	f.waitState(f.begin().ID, Succeeded)
	f.hook("/api/rest/api-keys/new-key", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	result, err := f.service.Logout(t.Context())
	if err != nil || result.LocalFailure != "" || result.CleanupFailure == "" || result.Status.InferenceState != "absent" {
		t.Fatalf("cleanup/local failure conflated: %+v %v", result, err)
	}
	if f.count("/api/auth/sign-out") != 0 {
		t.Fatal("failed archive discarded management authority")
	}
	f.hook("/api/rest/api-keys/new-key", nil)
	cleanup, err := f.service.RetryCleanup(t.Context())
	if err != nil || len(cleanup) != 1 || cleanup[0].KeyState != "archived" || cleanup[0].SessionState != "signed_out" {
		t.Fatalf("cleanup retry failed: %+v %v", cleanup, err)
	}
	if f.count("/api/rest/api-keys/") != 2 || f.count("/api/rest/api-keys") != 3 {
		t.Fatal("cleanup retry created another key")
	}
}

func TestDevicePendingSlowDownAndExpiry(t *testing.T) {
	f := newFixture(t)
	var intervals []time.Duration
	f.service.wait = func(ctx context.Context, d time.Duration) error { intervals = append(intervals, d); return ctx.Err() }
	n := 0
	f.hook("/api/auth/device/token", func(w http.ResponseWriter, _ *http.Request) {
		n++
		w.WriteHeader(http.StatusBadRequest)
		switch n {
		case 1:
			_, _ = io.WriteString(w, `{"error":"authorization_pending"}`)
		case 2:
			_, _ = io.WriteString(w, `{"error":"slow_down"}`)
		default:
			_, _ = io.WriteString(w, `{"error":"expired_token"}`)
		}
	})
	f.waitState(f.begin().ID, Expired)
	if !reflect.DeepEqual(intervals, []time.Duration{time.Second, time.Second, 6 * time.Second}) {
		t.Fatalf("poll intervals: %v", intervals)
	}
	if f.count("/api/auth/get-session") != 0 {
		t.Fatal("expired device token became management credentials")
	}
}

func TestFlowBoundsEpochExpiryAndPublicErrorIsolation(t *testing.T) {
	f := newFixture(t)
	f.hook("/api/auth/device/code", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"secret":"private-response"}`)
	})
	var first Flow
	for i := range maxFlows {
		view := f.waitState(f.begin().ID, Failed)
		if i == 0 {
			first = view
		}
	}
	if _, err := f.service.Begin(t.Context()); !errors.Is(err, ErrLimit) {
		t.Fatal("retained flow bound missing")
	}
	list, err := f.service.List(t.Context())
	if err != nil || len(list) != maxFlows {
		t.Fatal("retained flows lost")
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "private-response") {
		t.Fatal("remote diagnostic leaked")
	}
	f.service.mu.Lock()
	f.service.flows[first.ID].retainUntil = time.Now().Add(-time.Second)
	f.service.mu.Unlock()
	f.waitState(f.begin().ID, Failed)
	next, err := New(t.Context(), f.manager, nil, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	old, err := next.Get(t.Context(), first.ID)
	if err != nil || old.State != Interrupted {
		t.Fatal("old process flow was not interrupted")
	}
	if _, err := next.Get(t.Context(), "garbage"); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid process identity accepted")
	}
}

func TestManagementExpiryNeverInvalidatesIndependentInferenceKey(t *testing.T) {
	f := newFixture(t)
	credentials := inferenceauth.Credentials{Management: inferenceauth.Management{Token: "private-management", UserID: "user", ExpiresAt: new(time.Now().Add(-time.Hour))}, Scope: inferenceauth.Scope{TeamID: "team", ProjectID: "project"}, MachineKey: inferenceauth.MachineKey{ID: "key", Value: "private-machine"}}
	if err := f.manager.Install(t.Context(), f.manager.Generation(), credentials); err != nil {
		t.Fatal(err)
	}
	status, err := f.service.Status(t.Context())
	if err != nil || status.ManagementState != "expired" || status.InferenceState != "stored" {
		t.Fatalf("independent auth states collapsed: %+v %v", status, err)
	}
	if _, err := f.service.Rotate(t.Context()); !errors.Is(err, ErrManagement) {
		t.Fatal("expired management minted a key")
	}
	if f.count("/api/rest") != 0 {
		t.Fatal("local status made management requests")
	}
}

func TestKeyOnlyLogoutAllowsNewLoginWithoutBorrowingNewAuthority(t *testing.T) {
	f := newFixture(t)
	prior := inferenceauth.Credentials{Scope: inferenceauth.Scope{TeamID: "old-team", ProjectID: "old-project"}, MachineKey: inferenceauth.MachineKey{ID: "old-key", Value: "private-old-machine"}}
	if err := f.manager.Install(t.Context(), f.manager.Generation(), prior); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Logout(t.Context())
	if err != nil || result.LocalFailure != "" || result.CleanupFailure == "" || !result.Status.CleanupPending || len(result.Cleanup) != 1 {
		t.Fatalf("key-only remote cleanup falsely succeeded: %+v %v", result, err)
	}
	cleanupID := result.Cleanup[0].ID
	f.waitState(f.begin().ID, Succeeded)
	current, err := f.manager.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := f.service.RetryCleanup(t.Context())
	if err == nil || len(cleanup) != 1 || cleanup[0].ID != cleanupID || cleanup[0].KeyState != "pending" || cleanup[0].Failure == "" {
		t.Fatalf("prior failure lost on independent retry: %+v %v", cleanup, err)
	}
	if err := f.manager.Check(t.Context(), current); err != nil || f.count("/api/rest/api-keys/old-key") != 0 || f.count("/api/auth/sign-out") != 0 {
		t.Fatal("retry borrowed new authority or revoked current authorization")
	}
	result, err = f.service.Logout(t.Context())
	if err != nil || result.LocalFailure != "" || result.CleanupFailure == "" || result.Status.InferenceState != "absent" {
		t.Fatalf("old cleanup prevented current local revoke: %+v %v", result, err)
	}
	if f.count("/api/rest/api-keys/new-key") != 1 || f.count("/api/auth/sign-out") != 1 {
		t.Fatal("old failure prevented independent current cleanup")
	}
	_, _ = f.service.RetryCleanup(t.Context())
	if f.count("/api/rest/api-keys/new-key") != 1 || f.count("/api/auth/sign-out") != 1 {
		t.Fatal("cleanup retry repeated completed remote steps")
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "private-") {
		t.Fatal("cleanup exposed authority")
	}
}

func TestCleanupCapacityRejectsCreationBeforeHTTPButNeverLocalLogout(t *testing.T) {
	f := newFixture(t)
	f.service.mu.Lock()
	for range maxFlows {
		value := inferenceauth.Credentials{MachineKey: inferenceauth.MachineKey{ID: "key-" + rand.Text()}}
		if !f.service.addCleanup(value) {
			t.Fatal("unexpected cleanup capacity")
		}
	}
	f.service.mu.Unlock()
	if _, err := f.service.Begin(t.Context()); !errors.Is(err, ErrLimit) || f.count("/api/auth/device/code") != 0 {
		t.Fatal("capacity failure performed credential creation")
	}
	value := inferenceauth.Credentials{Scope: inferenceauth.Scope{TeamID: "team", ProjectID: "project"}, MachineKey: inferenceauth.MachineKey{ID: "external-key", Value: "private-machine"}}
	if err := f.manager.Install(t.Context(), f.manager.Generation(), value); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Logout(t.Context())
	if err != nil || result.LocalFailure != "" || result.CleanupFailure == "" || result.Status.InferenceState != "absent" || len(result.Cleanup) != maxFlows {
		t.Fatalf("capacity compromised bounded local revoke: %+v %v", result, err)
	}
}

func TestApprovedReplacementCannotPairNewManagementWithOldKeyAfterRestart(t *testing.T) {
	f := newFixture(t)
	prior := inferenceauth.Credentials{Management: inferenceauth.Management{Token: "old-management", UserID: "old-user"}, Scope: inferenceauth.Scope{TeamID: "old-team", ProjectID: "old-project"}, MachineKey: inferenceauth.MachineKey{ID: "old-key", Value: "private-old-machine"}}
	if err := f.manager.Install(t.Context(), f.manager.Generation(), prior); err != nil {
		t.Fatal(err)
	}
	old, err := f.manager.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f.projects = []Project{{ID: "project-a"}, {ID: "project-b"}}
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	f.hook("/api/auth/device/code", func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, `{"device_code":"private-device","user_code":"PUBLIC-CODE","expires_in":900,"interval":1}`)
	})
	view := f.begin()
	<-entered
	if err := f.manager.Check(t.Context(), old); err != nil {
		t.Fatal("Begin cleared authorization before approval")
	}
	unblock()
	f.waitState(view.ID, ChooseProject)
	status, err := f.service.Status(t.Context())
	if err != nil || status.ManagementState != "stored" || status.InferenceState != "absent" || !status.CleanupPending {
		t.Fatalf("partial replacement status is not truthful: %+v %v", status, err)
	}
	if err := f.manager.Check(t.Context(), old); !errors.Is(err, inferenceauth.ErrChanged) {
		t.Fatal("approved account replacement retained old capture")
	}
	if f.count("/api/rest/api-keys") != 0 {
		t.Fatal("old key archived before replacement or arbitrary project chosen")
	}
	f.service.Close()
	if err := f.manager.Close(); err != nil {
		t.Fatal(err)
	}
	nextManager, err := inferenceauth.New(t.Context(), f.directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = nextManager.Close() }()
	stored, err := nextManager.Snapshot()
	if err != nil || stored.Management.Token != "private-management" || stored.MachineKey.ID != "" || stored.Scope.TeamID != "" {
		t.Fatal("restart paired new management with old key/scope")
	}
	next, err := New(t.Context(), nextManager, f.service.http, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	interrupted, err := next.Get(t.Context(), view.ID)
	if err != nil || interrupted.State != Interrupted || f.count("/api/rest/api-keys") != 0 {
		t.Fatal("restart replayed abandoned flow or claimed completion")
	}
}

func TestReplacementArchivesWithPriorAuthorityOnlyAfterNewKeyInstallation(t *testing.T) {
	f := newFixture(t)
	prior := inferenceauth.Credentials{Management: inferenceauth.Management{Token: "old-management", UserID: "old-user"}, Scope: inferenceauth.Scope{TeamID: "old-team", ProjectID: "old-project"}, MachineKey: inferenceauth.MachineKey{ID: "old-key", Value: "private-old-machine"}}
	if err := f.manager.Install(t.Context(), f.manager.Generation(), prior); err != nil {
		t.Fatal(err)
	}
	f.hook("/api/auth/organization/set-active", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID string `json:"organizationId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.ID == "old-team" && r.Header.Get("Authorization") != "Bearer old-management" {
			t.Error("old scope borrowed new account authority")
		}
		f.mu.Lock()
		f.active = body.ID
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{}`)
	})
	f.hook("/api/rest/api-keys/old-key", func(w http.ResponseWriter, r *http.Request) {
		current, err := f.manager.Capture(t.Context())
		if err != nil || current.Key != "private-machine" || r.Header.Get("Authorization") != "Bearer old-management" || r.URL.Query().Get("teamId") != "old-team" {
			t.Error("archive preceded install or used unrelated authority")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	f.hook("/api/auth/sign-out", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer old-management" {
			t.Error("replacement signed out new account")
		}
		_, _ = io.WriteString(w, `{}`)
	})
	f.waitState(f.begin().ID, Succeeded)
	cleanup, err := f.service.ListCleanup(t.Context())
	if err != nil || len(cleanup) != 1 || cleanup[0].KeyState != "archived" || cleanup[0].SessionState != "signed_out" {
		t.Fatalf("prior cleanup did not complete: %+v %v", cleanup, err)
	}
}

func TestControlPlaneRefusesRedirectsAndUnsafeKeyPaths(t *testing.T) {
	f := newFixture(t)
	f.hook("/api/auth/organization/list", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://example.invalid/steal-management")
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	f.waitState(f.begin().ID, Failed)
	if f.count("/api/auth/organization/list") != 1 || f.count("/steal-management") != 0 {
		t.Fatal("management redirect followed")
	}
	for _, key := range []string{".", "..", "../other", "a/b", `a\b`} {
		if err := f.service.archive(t.Context(), "private-management", "team", key); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted unsafe remote key path %q", key)
		}
	}
	if f.count("/api/auth/organization/set-active") != 0 {
		t.Fatal("invalid key path reached HTTP")
	}
}
