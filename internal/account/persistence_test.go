package account

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/openaiauth"
)

func TestUnresolvedCredentialStorageBlocksStatusSetupAndLogin(t *testing.T) {
	for _, operation := range []string{"rotation", "logout"} {
		t.Run(operation, func(t *testing.T) {
			f := newFixture(t, nil)
			if err := f.manager.Install(t.Context(), f.manager.Generation(), savedCredentials("account")); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(f.directory, "openai-codex.json")
			if err := os.Rename(path, path+".saved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			blocker := filepath.Join(path, "blocker")
			if err := os.WriteFile(blocker, []byte("blocked storage"), 0o600); err != nil {
				t.Fatal(err)
			}
			wantRequests := int32(0)
			if operation == "rotation" {
				wantRequests = 1
				if _, err := f.manager.Capture(t.Context()); !errors.Is(err, openaiauth.ErrPersistence) {
					t.Fatal("rotated credentials became usable before persistence", err)
				}
			} else if status, err := f.service.Logout(t.Context()); !errors.Is(err, ErrLogout) || status.AuthState != "unavailable" {
				t.Fatal("failed logout appeared clean", status.AuthState, err)
			}
			for range 2 {
				status, err := f.service.Status(t.Context())
				if err != nil || status.AuthState != "unavailable" || status.Failure == "" || strings.Contains(status.Failure, f.directory) {
					t.Fatal("status hid unresolved storage", status.AuthState, err)
				}
				assertPublic(t, status)
				if status, err := f.service.Setup(t.Context()); !errors.Is(err, ErrCredentials) || status.AuthState != "unavailable" || status.RouteState != "missing" {
					t.Fatal("setup published from unresolved credentials", status.AuthState, err)
				}
				if _, err := f.service.Begin(t.Context()); !errors.Is(err, ErrCredentials) {
					t.Fatal("login started while storage was unresolved", err)
				}
			}
			if f.requests.Load() != wantRequests {
				t.Fatal("inspection or setup contacted provider", f.requests.Load())
			}
			if err := os.Remove(blocker); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			wantState := "signed_out"
			if operation == "rotation" {
				wantState = "stored"
				if _, err := f.service.Setup(t.Context()); err != nil {
					t.Fatal("setup failed after persistence recovered", err)
				}
			} else if _, err := f.service.Logout(t.Context()); err != nil {
				t.Fatal("logout retry failed", err)
			}
			if status, err := f.service.Status(t.Context()); err != nil || status.AuthState != wantState || f.requests.Load() != wantRequests {
				t.Fatal("local retry did not recover truthful status", status.AuthState, err)
			}
			manager := openaiauth.New(t.Context(), f.directory)
			t.Cleanup(manager.Close)
			service, err := New(t.Context(), manager, f.authority)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(service.Close)
			if status, err := service.Status(t.Context()); err != nil || status.AuthState != wantState || f.requests.Load() != wantRequests {
				t.Fatal("recovered credential state did not survive restart", status.AuthState, err)
			}
		})
	}
}

func TestStatusDistinguishesTerminalAuthRejectionFromStorageFailure(t *testing.T) {
	f := newFixture(t, func(_ *fixture, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_grant","message":"private-refresh"}}`))
	})
	if err := f.manager.Install(t.Context(), f.manager.Generation(), savedCredentials("account")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.Capture(t.Context()); err == nil {
		t.Fatal("rejected refresh succeeded")
	}
	status, err := f.service.Status(t.Context())
	if err != nil || status.AuthState != "sign_in_required" || f.requests.Load() != 1 {
		t.Fatal("terminal rejection misclassified or retried", status.AuthState, err)
	}
	assertPublic(t, status)
	if _, err := f.service.Setup(t.Context()); !errors.Is(err, ErrCredentials) || f.requests.Load() != 1 {
		t.Fatal("setup retried rejected credentials", err)
	}
}
