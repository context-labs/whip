package inferencenet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stubRelay serves the device-auth + tRPC surface CompleteLogin and key
// minting touch: device/code, device/token, get-session, organization/*,
// project.getProjects, apiKey.create.
func stubRelay(t *testing.T) *httptest.Server {
	t.Helper()
	var tokenPolls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/device/code", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ClientID string `json:"client_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.ClientID != DeviceClientID {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code": "dc-123", "user_code": "ABCD-1234", "expires_in": 600, "interval": 0,
		})
	})
	mux.HandleFunc("/api/auth/device/token", func(w http.ResponseWriter, r *http.Request) {
		// Approve on the second poll to exercise authorization_pending.
		if tokenPolls.Add(1) < 2 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "sess-tok", "token_type": "Bearer"})
	})
	mux.HandleFunc("/api/auth/get-session", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"email": "abe@x.dev", "id": "user-1"}})
	})
	mux.HandleFunc("/api/auth/organization/list", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "user-1", "name": "personal"}})
	})
	mux.HandleFunc("/api/auth/organization/set-active", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	})
	// REST surface (/api/rest): plain JSON, no superjson envelope.
	mux.HandleFunc("/api/rest/projects", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sess-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "auth required"})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "Primary"}})
	})
	mux.HandleFunc("/api/rest/projects/create", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "proj-new", "name": in.Name})
	})
	mux.HandleFunc("/api/rest/api-keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Inference-Team-Id") == "" {
			http.Error(w, "missing team header", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "key-1", "key": "inf-machine-secret"})
	})
	mux.HandleFunc("/api/rest/api-keys/key-1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "key-1", "enabled": false})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// useStub points the package's URLs at the stub relay and restores them after.
func useStub(t *testing.T, srv *httptest.Server) {
	t.Helper()
	oldRelay, oldDash, oldBase := relayURL, dashboardURL, baseURL
	relayURL, dashboardURL, baseURL = srv.URL, srv.URL, srv.URL
	t.Cleanup(func() { relayURL, dashboardURL, baseURL = oldRelay, oldDash, oldBase })
}

func TestCompleteLoginAndMachineKey(t *testing.T) {
	srv := stubRelay(t)
	useStub(t, srv)
	// Collapse the poll sleep so the test doesn't wait out the real interval.
	pollSleepOverride = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { pollSleepOverride = nil })

	var gotURL, gotCode string
	auth, err := CompleteLogin(context.Background(), func(vu, uc string) { gotURL, gotCode = vu, uc }, nil)
	if err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	if !strings.Contains(gotURL, "/device/approve?user_code=ABCD-1234") {
		t.Errorf("verification URL not surfaced: %q", gotURL)
	}
	if gotCode != "ABCD-1234" {
		t.Errorf("user code not surfaced: %q", gotCode)
	}
	if auth.SessionToken != "sess-tok" || auth.UserEmail != "abe@x.dev" || auth.TeamID != "user-1" {
		t.Errorf("unexpected session: %+v", auth)
	}
	if auth.ProjectID != "proj-1" || auth.ProjectName != "Primary" {
		t.Errorf("primary project not selected: %+v", auth)
	}

	key, err := auth.EnsureMachineKey(context.Background())
	if err != nil {
		t.Fatalf("EnsureMachineKey: %v", err)
	}
	if key != "inf-machine-secret" || auth.MachineKeyID != "key-1" {
		t.Errorf("machine key not minted: %+v", auth)
	}
	// A second call returns the stored key without re-minting.
	if k, _ := auth.EnsureMachineKey(context.Background()); k != key {
		t.Errorf("EnsureMachineKey re-minted: %q", k)
	}
}

func TestCompleteLoginCreateProjectOnTheSpot(t *testing.T) {
	srv := stubRelay(t)
	useStub(t, srv)
	pollSleepOverride = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { pollSleepOverride = nil })

	// The chooser picks "create new project" then supplies a name.
	choose := func(kind, title string, options []string) (string, error) {
		if kind == "project" {
			return CreateProjectOption, nil
		}
		return "my-new-project", nil // project-name prompt
	}
	auth, err := CompleteLogin(context.Background(), nil, choose)
	if err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	if auth.ProjectID != "proj-new" || auth.ProjectName != "my-new-project" {
		t.Errorf("created project not selected: %+v", auth)
	}
}

func TestAuthStoreRoundTrip(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if a, _ := LoadAuth(); a != (Auth{}) {
		t.Errorf("missing file should yield zero Auth, got %+v", a)
	}
	want := Auth{SessionToken: "tok", UserEmail: "abe@x.dev", TeamID: "t1", ProjectID: "p1", MachineKey: "mk"}
	if err := SaveAuth(want); err != nil {
		t.Fatalf("SaveAuth: %v", err)
	}
	got, err := LoadAuth()
	if err != nil {
		t.Fatalf("LoadAuth: %v", err)
	}
	if got != want {
		t.Errorf("round trip mismatch: got %+v want %+v", got, want)
	}
	if err := ClearAuth(); err != nil {
		t.Fatalf("ClearAuth: %v", err)
	}
	if a, _ := LoadAuth(); a != (Auth{}) {
		t.Errorf("after clear, got %+v", a)
	}
}

func TestValidateKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer good" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	old := baseURL
	baseURL = srv.URL
	t.Cleanup(func() { baseURL = old })

	if err := ValidateKey(context.Background(), "good"); err != nil {
		t.Errorf("good key rejected: %v", err)
	}
	if err := ValidateKey(context.Background(), "bad"); err == nil {
		t.Error("bad key accepted")
	}
}

func TestRotateAndArchiveMachineKey(t *testing.T) {
	var archived []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer session-token" || r.Header.Get("X-Inference-Team-Id") != "team-1" {
			http.Error(w, "missing authentication", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/api/rest/api-keys/key-old":
			if r.URL.Query().Get("teamId") != "team-1" {
				http.Error(w, "missing team query", http.StatusBadRequest)
				return
			}
			archived = append(archived, "key-old")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/rest/api-keys":
			_, _ = w.Write([]byte(`{"id":"key-new","key":"replacement"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/rest/api-keys/key-new":
			archived = append(archived, "key-new")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	useStub(t, srv)

	auth := Auth{SessionToken: "session-token", TeamID: "team-1", ProjectID: "project-1", MachineKeyID: "key-old", MachineKey: "old"}
	key, err := auth.Rotate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if key != "replacement" || auth.MachineKeyID != "key-new" || auth.MachineKey != "replacement" {
		t.Fatalf("rotated auth = %+v", auth)
	}
	if err := auth.ArchiveMachineKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(archived, ",") != "key-old,key-new" {
		t.Fatalf("archived keys = %v", archived)
	}
}
