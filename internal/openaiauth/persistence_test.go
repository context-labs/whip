package openaiauth

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInstallPersistenceOrdersDiskMemoryAndGeneration(t *testing.T) {
	for _, account := range []string{"same", "different"} {
		for _, stage := range []string{"before_publication", "after_publication"} {
			t.Run(account+"/"+stage, func(t *testing.T) {
				m, old := capturedManager(t, func(http.ResponseWriter, *http.Request) {
					t.Error("persistence retry contacted provider")
				})
				replacement := old.Credentials
				replacement.AccessToken, replacement.RefreshToken = "replacement-access", "replacement-refresh"
				if account == "different" {
					replacement.AccountID = "replacement-account"
				}
				save := m.save
				m.save = func(path string, credentials Credentials) (bool, error) {
					if stage == "before_publication" {
						return false, ErrPersistence
					}
					return saveCredentials(path, credentials, func(*os.File) error { return errors.New("injected sync failure") })
				}
				if err := m.Install(t.Context(), old.Generation, replacement); !errors.Is(err, ErrPersistence) {
					t.Fatal("failed persistence reported success", err)
				}
				want, generation := old.Credentials, old.Generation
				if stage == "after_publication" {
					want, generation = replacement, old.Generation+1
					if err := m.Check(t.Context(), old); !errors.Is(err, ErrLoginChanged) {
						t.Fatal("published replacement retained old authority", err)
					}
					if _, err := m.RefreshCaptured(t.Context(), old); !errors.Is(err, ErrLoginChanged) {
						t.Fatal("old capture refreshed a replacement", err)
					}
					if _, err := m.Capture(t.Context()); !errors.Is(err, ErrPersistence) {
						t.Fatal("unconfirmed replacement became usable", err)
					}
				} else if err := m.Check(t.Context(), old); err != nil {
					t.Fatal("unpublished replacement revoked the old login", err)
				}
				visible, err := readCredentials(m.path, (*os.File).Sync)
				if err != nil || visible.AccessToken != want.AccessToken || visible.AccountID != want.AccountID || m.Generation() != generation {
					t.Fatal("disk, memory and generation diverged", err)
				}
				snapshot, err := m.Snapshot()
				if snapshot != want || errors.Is(err, ErrPersistence) != (stage == "after_publication") {
					t.Fatal("snapshot hid unresolved persistence", err)
				}
				m.save = save
				current, err := m.Capture(t.Context())
				if err != nil || current.Credentials != want || current.Generation != generation {
					t.Fatal("persistence-only retry changed the login", err)
				}
				reopened := New(t.Context(), filepath.Dir(m.path))
				t.Cleanup(reopened.Close)
				durable, err := reopened.Capture(t.Context())
				if err != nil || durable.Credentials.AccessToken != want.AccessToken || durable.Credentials.RefreshToken != want.RefreshToken || durable.Credentials.AccountID != want.AccountID {
					t.Fatal("persisted replacement did not survive restart", err)
				}
			})
		}
	}
}

func TestLogoutPersistenceKeepsRevocationAndRetryVisible(t *testing.T) {
	for _, stage := range []string{"unlink", "directory_sync"} {
		t.Run(stage, func(t *testing.T) {
			m, old := capturedManager(t, func(http.ResponseWriter, *http.Request) {
				t.Error("logout failure contacted provider")
			})
			fail, syncs := true, 0
			m.remove = func(path string) error {
				return removeCredentials(path, func(path string) error {
					if fail && stage == "unlink" {
						return os.ErrPermission
					}
					return os.Remove(path)
				}, func(directory *os.File) error {
					syncs++
					if fail && stage == "directory_sync" {
						return errors.New("injected sync failure")
					}
					return directory.Sync()
				})
			}
			if err := m.Logout(); !errors.Is(err, ErrPersistence) || m.Generation() != old.Generation+1 {
				t.Fatal("failed logout did not revoke the generation", err)
			}
			if err := m.Check(t.Context(), old); !errors.Is(err, ErrLoginChanged) {
				t.Fatal("failed logout retained captured authority", err)
			}
			if _, err := m.Capture(t.Context()); !errors.Is(err, ErrPersistence) {
				t.Fatal("failed logout reloaded disk credentials", err)
			}
			if err := m.PersistPending(t.Context()); !errors.Is(err, ErrPersistence) {
				t.Fatal("credential persistence repaired a pending logout", err)
			}
			if value, err := m.Snapshot(); value != (Credentials{}) || !errors.Is(err, ErrPersistence) {
				t.Fatal("failed logout appeared clean", err)
			}
			visible, err := readCredentials(m.path, (*os.File).Sync)
			if err != nil || (visible.AccessToken != "") != (stage == "unlink") {
				t.Fatal("unexpected failed removal boundary", err)
			}
			// Before retry, an unlink failure can leave the old account on disk.
			// The live manager must continue reporting that unresolved outcome.
			reopened := New(t.Context(), filepath.Dir(m.path))
			value, err := reopened.Snapshot()
			reopened.Close()
			if err != nil || value.AccessToken != visible.AccessToken {
				t.Fatal("restart disagreed with visible disk state", err)
			}
			fail = false
			previousSyncs := syncs
			if err := m.Logout(); err != nil || syncs != previousSyncs+1 {
				t.Fatal("logout retry did not confirm directory durability", err)
			}
			if value, err := m.Snapshot(); value != (Credentials{}) || err != nil {
				t.Fatal("durable logout remained unresolved", err)
			}
			if _, err := m.Capture(t.Context()); !errors.Is(err, ErrSignInRequired) {
				t.Fatal("durable logout retained a login", err)
			}
			reopened = New(t.Context(), filepath.Dir(m.path))
			t.Cleanup(reopened.Close)
			if value, err := reopened.Snapshot(); value != (Credentials{}) || err != nil {
				t.Fatal("durable logout did not survive restart", err)
			}
		})
	}
}

func TestRotatedTokenDirectorySyncFailureRetriesOnlyPersistence(t *testing.T) {
	var requests atomic.Int32
	m, captured := capturedManager(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if err := r.ParseForm(); err != nil || r.Form.Get("refresh_token") != "old-refresh" {
			t.Error("unexpected refresh token")
		}
		refreshResponse(w)
	})
	save := m.save
	m.save = func(path string, credentials Credentials) (bool, error) {
		return saveCredentials(path, credentials, func(*os.File) error { return errors.New("injected sync failure") })
	}
	if _, err := m.RefreshCaptured(t.Context(), captured); !errors.Is(err, ErrPersistence) {
		t.Fatal("rotation succeeded before directory sync", err)
	}
	if value, err := m.Snapshot(); !errors.Is(err, ErrPersistence) || value.RefreshToken != "new-refresh" {
		t.Fatal("snapshot lost rotated token or hid persistence failure", err)
	}
	if err := m.Check(t.Context(), captured); !errors.Is(err, ErrPersistence) {
		t.Fatal("dirty rotation permitted dispatch", err)
	}
	m.save = save
	value, err := m.RefreshCaptured(t.Context(), captured)
	if err != nil || value.Generation != captured.Generation || value.Credentials.RefreshToken != "new-refresh" || requests.Load() != 1 {
		t.Fatal("retry replayed the rotating refresh token", err)
	}
	if _, err := m.Snapshot(); err != nil {
		t.Fatal("durable rotation retained dirty status", err)
	}
}

func TestCredentialSaveFailureBeforeRenameLeavesExistingBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openai-codex.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "existing")
	if err := os.WriteFile(marker, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if published, err := saveCredentials(path, testCredentials(), (*os.File).Sync); published || !errors.Is(err, ErrPersistence) || strings.Contains(err.Error(), path) {
		t.Fatal("failed rename reported publication or leaked path", err)
	}
	if value, err := os.ReadFile(marker); err != nil || string(value) != "unchanged" {
		t.Fatal("failed rename modified existing storage", err)
	}
}

func TestSuccessfulInstallClearsFailedLogout(t *testing.T) {
	m, old := capturedManager(t, func(http.ResponseWriter, *http.Request) {})
	m.remove = func(string) error { return ErrPersistence }
	if err := m.Logout(); !errors.Is(err, ErrPersistence) {
		t.Fatal(err)
	}
	if err := m.Install(t.Context(), m.Generation(), old.Credentials); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Snapshot(); err != nil {
		t.Fatal("successful replacement retained failed logout state", err)
	}
	if err := m.Check(t.Context(), old); !errors.Is(err, ErrLoginChanged) {
		t.Fatal("same-account reinstall restored old captured authority", err)
	}
}

func TestPersistPendingFirstLoginRequiresNoProviderOrReplacement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("local persistence contacted provider")
	}))
	t.Cleanup(server.Close)
	m := New(t.Context(), t.TempDir())
	m.issuer = server.URL
	t.Cleanup(m.Close)
	save := m.save
	m.save = func(path string, credentials Credentials) (bool, error) {
		return saveCredentials(path, credentials, func(*os.File) error { return errors.New("injected sync failure") })
	}
	credentials := testCredentials() // Expired credentials must not trigger refresh.
	if err := m.Install(t.Context(), m.Generation(), credentials); !errors.Is(err, ErrPersistence) {
		t.Fatal("first login reported success before directory sync", err)
	}
	generation := m.Generation()
	if err := m.PersistPending(t.Context()); !errors.Is(err, ErrPersistence) {
		t.Fatal("unrepaired storage was accepted", err)
	}
	m.save = save
	if err := m.PersistPending(t.Context()); err != nil || m.Generation() != generation {
		t.Fatal("persistence retry required a replacement login", err)
	}
	if value, err := m.Snapshot(); err != nil || value != credentials {
		t.Fatal("local persistence changed credentials", err)
	}
	reopened := New(t.Context(), filepath.Dir(m.path))
	t.Cleanup(reopened.Close)
	if value, err := reopened.Snapshot(); err != nil || value.AccessToken != credentials.AccessToken || value.RefreshToken != credentials.RefreshToken {
		t.Fatal("first-login persistence did not survive restart", err)
	}
}

func TestLoadConfirmsPublishedCredentialsAndAbsenceBeforeAuthorization(t *testing.T) {
	for _, outcome := range []string{"credentials", "absence"} {
		t.Run(outcome, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "openai-codex.json")
			credentials := testCredentials()
			credentials.ExpiresAt = time.Now().Add(time.Hour)
			if _, err := saveCredentials(path, credentials, (*os.File).Sync); err != nil {
				t.Fatal(err)
			}
			failedSync := func(*os.File) error { return errors.New("injected sync failure") }
			if outcome == "credentials" {
				credentials.AccessToken, credentials.RefreshToken = "published-access", "published-refresh"
				if published, err := saveCredentials(path, credentials, failedSync); !published || !errors.Is(err, ErrPersistence) {
					t.Fatal("missing post-publication failure", err)
				}
			} else if err := removeCredentials(path, os.Remove, failedSync); !errors.Is(err, ErrPersistence) {
				t.Fatal("missing post-unlink failure", err)
			}
			before, readErr := os.ReadFile(path)
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				t.Fatal(readErr)
			}
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("first-load confirmation contacted provider")
			}))
			t.Cleanup(server.Close)
			m := New(t.Context(), directory)
			t.Cleanup(m.Close)
			m.issuer = server.URL
			m.save = func(string, Credentials) (bool, error) {
				t.Error("first-load confirmation rewrote credentials")
				return false, ErrPersistence
			}
			repaired, confirmations := false, 0
			m.read = func(path string) (Credentials, error) {
				return readCredentials(path, func(directory *os.File) error {
					confirmations++
					if !repaired {
						return failedSync(directory)
					}
					return directory.Sync()
				})
			}
			if value, err := m.Snapshot(); value != (Credentials{}) || !errors.Is(err, ErrPersistence) || strings.Contains(err.Error(), directory) {
				t.Fatal("unconfirmed saved state was published", err)
			}
			if _, err := m.Capture(t.Context()); !errors.Is(err, ErrPersistence) {
				t.Fatal("unconfirmed saved state authorized capture", err)
			}
			if err := m.PersistPending(t.Context()); !errors.Is(err, ErrPersistence) || confirmations != 3 {
				t.Fatal("failed confirmation was cached as clean state", err)
			}
			repaired = true
			value, err := m.Snapshot()
			if err != nil || confirmations != 4 || m.Generation() != 0 {
				t.Fatal("local confirmation did not recover", err)
			}
			if outcome == "credentials" {
				captured, err := m.Capture(t.Context())
				if err != nil || captured.Credentials.AccessToken != credentials.AccessToken || value.RefreshToken != credentials.RefreshToken {
					t.Fatal("confirmed saved credentials changed", err)
				}
			} else {
				if value != (Credentials{}) {
					t.Fatal("confirmed absence restored credentials")
				}
				if _, err := m.Capture(t.Context()); !errors.Is(err, ErrSignInRequired) {
					t.Fatal("confirmed absence was not signed out", err)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) || !bytes.Equal(before, after) || confirmations != 4 {
				t.Fatal("confirmation rewrote state or repeated completed loading", err)
			}
		})
	}
}
