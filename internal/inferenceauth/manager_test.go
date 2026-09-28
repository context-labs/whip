package inferenceauth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func fixtureCredentials() Credentials {
	return Credentials{
		Management: Management{Token: "private-management", UserID: "user", Email: "user@example.test", ExpiresAt: new(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))},
		Scope:      Scope{TeamID: "team", TeamName: "Team", ProjectID: "project", ProjectName: "Project"},
		MachineKey: MachineKey{ID: "key", Value: "private-inference", Name: "whip"},
	}
}

func managerFixture(t *testing.T) (*Manager, string) {
	t.Helper()
	directory := t.TempDir()
	manager, err := New(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil && !errors.Is(err, ErrStoragePending) {
			t.Error(err)
		}
	})
	return manager, directory
}

func installFixture(t *testing.T, manager *Manager) CapturedMachineKey {
	t.Helper()
	if err := manager.Install(t.Context(), manager.Generation(), fixtureCredentials()); err != nil {
		t.Fatal(err)
	}
	captured, err := manager.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return captured
}

func TestCredentialKindsExpiryAndOwnership(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"management only", "key only", "expired management and key", "unknown expiry"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			manager, directory := managerFixture(t)
			if _, err := manager.Capture(t.Context()); !errors.Is(err, ErrKeyRequired) {
				t.Fatalf("empty capture: %v", err)
			}
			value := fixtureCredentials()
			switch kind {
			case "management only":
				value.MachineKey = MachineKey{}
				value.Scope = Scope{}
			case "key only":
				value.Management = Management{}
			case "unknown expiry":
				value.Management.ExpiresAt = nil
			}
			expected := value.clone()
			if err := manager.Install(t.Context(), 0, value); err != nil {
				t.Fatal(err)
			}
			if value.Management.ExpiresAt != nil {
				*value.Management.ExpiresAt = time.Time{}
			}
			snapshot, err := manager.Snapshot()
			if err != nil || !reflect.DeepEqual(snapshot, expected) {
				t.Fatalf("install did not own credentials: %v", err)
			}
			if snapshot.Management.ExpiresAt != nil {
				*snapshot.Management.ExpiresAt = time.Time{}
			}
			again, err := manager.Snapshot()
			if err != nil || !reflect.DeepEqual(again, expected) {
				t.Fatalf("snapshot did not own credentials: %v", err)
			}
			capture, err := manager.Capture(t.Context())
			if kind == "management only" {
				if !errors.Is(err, ErrKeyRequired) {
					t.Fatalf("management was used for inference: %v", err)
				}
			} else if err != nil || capture.Key != expected.MachineKey.Value || capture.TeamID != expected.Scope.TeamID || capture.ProjectID != expected.Scope.ProjectID {
				t.Fatalf("independent key capture failed: %v", err)
			}
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := New(t.Context(), directory)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			saved, err := reopened.Snapshot()
			if err != nil || !reflect.DeepEqual(saved, expected) {
				t.Fatalf("reopen changed saved record: %v", err)
			}
			if kind != "management only" && !errors.Is(reopened.Check(t.Context(), capture), ErrChanged) {
				t.Fatal("capture crossed manager lifetime")
			}
		})
	}
}

func TestInstallGenerationAndCapturedScope(t *testing.T) {
	t.Parallel()
	manager, _ := managerFixture(t)
	initial := installFixture(t, manager)
	for _, change := range []string{"same account", "rotation", "different account"} {
		value := fixtureCredentials()
		if change == "rotation" {
			value.MachineKey.Value = "rotated"
		}
		if change == "different account" {
			value.Management.UserID = "other"
			value.Scope.TeamID = "other-team"
		}
		before, err := manager.Capture(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := manager.Install(t.Context(), before.Generation, value); err != nil {
			t.Fatal(err)
		}
		if err := manager.Check(t.Context(), before); !errors.Is(err, ErrChanged) {
			t.Fatalf("%s retained old capture: %v", change, err)
		}
		if err := manager.Install(t.Context(), before.Generation, value); !errors.Is(err, ErrChanged) {
			t.Fatalf("stale install: %v", err)
		}
	}
	current, err := manager.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tamper := range []func(*CapturedMachineKey){func(c *CapturedMachineKey) { c.Key = "other" }, func(c *CapturedMachineKey) { c.KeyID = "other" }, func(c *CapturedMachineKey) { c.TeamID = "other" }, func(c *CapturedMachineKey) { c.ProjectID = "other" }, func(c *CapturedMachineKey) { c.owner = nil }} {
		changed := current
		tamper(&changed)
		if err := manager.Check(t.Context(), changed); !errors.Is(err, ErrChanged) {
			t.Fatalf("modified capture accepted: %v", err)
		}
	}
	if err := manager.Check(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	if err := manager.Check(t.Context(), initial); !errors.Is(err, ErrChanged) {
		t.Fatal("original capture survived replacements")
	}
}

func TestInstallPublicationFailureAndLocalRetry(t *testing.T) {
	t.Parallel()
	for _, afterRename := range []bool{false, true} {
		name := "before publication"
		if afterRename {
			name = "published but unsynced"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			manager, directory := managerFixture(t)
			old := installFixture(t, manager)
			replacement := fixtureCredentials()
			replacement.MachineKey.Value = "replacement"
			calls := 0
			fail := func(*os.File) error { calls++; return errors.New("private filesystem diagnostic") }
			if afterRename {
				manager.storage.syncDirectory = fail
			} else {
				manager.storage.syncFile = fail
			}
			err := manager.Install(t.Context(), old.Generation, replacement)
			expectedError := ErrStorage
			if afterRename {
				expectedError = ErrStoragePending
			}
			if !errors.Is(err, expectedError) || calls != 1 {
				t.Fatalf("publication failure not reported: %v (%d)", err, calls)
			}
			snapshot, err := manager.Snapshot()
			if afterRename {
				if !errors.Is(err, ErrStoragePending) || snapshot.MachineKey.Value != replacement.MachineKey.Value || manager.Generation() != old.Generation+1 {
					t.Fatal("published replacement is not blocked current state")
				}
				if !errors.Is(manager.Check(t.Context(), old), ErrChanged) {
					t.Fatal("old capture survived rename")
				}
				if _, err := manager.Capture(t.Context()); !errors.Is(err, ErrStoragePending) {
					t.Fatalf("failed local retry authorized: %v", err)
				}
				if err := manager.Install(t.Context(), manager.Generation(), fixtureCredentials()); !errors.Is(err, ErrStoragePending) {
					t.Fatalf("pending record overwritten: %v", err)
				}
			} else {
				if err != nil || snapshot.MachineKey.Value != old.Key || manager.Generation() != old.Generation {
					t.Fatal("failed temporary write changed authorization")
				}
				if err := manager.Check(t.Context(), old); err != nil {
					t.Fatal(err)
				}
			}
			// Inspection did not retry, and a new owner can authorize only actual bytes
			// surviving on disk after its own directory synchronization.
			disk, err := openStorage(directory)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := disk.read()
			if err != nil {
				t.Fatal(err)
			}
			if err := disk.directory.Close(); err != nil {
				t.Fatal(err)
			}
			if afterRename && saved.MachineKey.Value != replacement.MachineKey.Value || !afterRename && saved.MachineKey.Value != old.Key {
				t.Fatal("memory and publication boundary disagree")
			}
			manager.storage.syncFile = (*os.File).Sync
			manager.storage.syncDirectory = (*os.File).Sync
			current, err := manager.Capture(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if afterRename && (current.Key != replacement.MachineKey.Value || current.Generation != old.Generation+1) {
				t.Fatal("retry changed generation or lost replacement")
			}
			if snapshot, err := manager.Snapshot(); err != nil || snapshot.MachineKey.Value != current.Key {
				t.Fatal("persistence did not resolve")
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 1 {
				t.Fatalf("temporary file leak: %v %d", err, len(entries))
			}
		})
	}
}

func TestUnresolvedPublicationReopen(t *testing.T) {
	t.Parallel()
	manager, directory := managerFixture(t)
	old := installFixture(t, manager)
	manager.storage.syncDirectory = func(*os.File) error { return errors.New("fixture") }
	replacement := fixtureCredentials()
	replacement.MachineKey.Value = "replacement"
	if err := manager.Install(t.Context(), old.Generation, replacement); !errors.Is(err, ErrStoragePending) {
		t.Fatal(err)
	}
	if err := manager.Close(); !errors.Is(err, ErrStoragePending) {
		t.Fatal("close concealed unresolved write")
	}
	reopened, err := New(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Capture(t.Context())
	if err != nil || got.Key != replacement.MachineKey.Value {
		t.Fatalf("surviving bytes not confirmed on reopen: %v", err)
	}
}

func TestLogoutFailureAndExplicitRetry(t *testing.T) {
	t.Parallel()
	for _, afterUnlink := range []bool{false, true} {
		name := "unlink failed"
		if afterUnlink {
			name = "unlinked but unsynced"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			manager, directory := managerFixture(t)
			old := installFixture(t, manager)
			calls := 0
			if afterUnlink {
				manager.storage.syncDirectory = func(*os.File) error { calls++; return errors.New("fixture") }
			} else {
				manager.storage.unlink = func(int, string, int) error { calls++; return unix.EACCES }
			}
			prior, err := manager.Logout()
			if !errors.Is(err, ErrStoragePending) || prior.MachineKey.Value != old.Key {
				t.Fatalf("logout outcome lost: %v", err)
			}
			if err := manager.Check(t.Context(), old); !errors.Is(err, ErrChanged) {
				t.Fatalf("logout did not immediately revoke: %v", err)
			}
			if snapshot, err := manager.Snapshot(); !errors.Is(err, ErrStoragePending) || !reflect.DeepEqual(snapshot, Credentials{}) {
				t.Fatal("pending logout appeared signed in or resolved")
			}
			if _, err := manager.Capture(t.Context()); !errors.Is(err, ErrStoragePending) || calls != 1 {
				t.Fatal("capture retried deletion or authorized")
			}
			if err := manager.Install(t.Context(), manager.Generation(), fixtureCredentials()); !errors.Is(err, ErrStoragePending) {
				t.Fatal("unresolved logout accepted login")
			}
			_, statErr := os.Stat(filepath.Join(directory, fileName))
			if afterUnlink && !errors.Is(statErr, os.ErrNotExist) || !afterUnlink && statErr != nil {
				t.Fatal("unexpected unlink publication")
			}
			manager.storage.syncDirectory = (*os.File).Sync
			manager.storage.unlink = unix.Unlinkat
			retry, err := manager.Logout()
			if err != nil || !reflect.DeepEqual(retry, prior) {
				t.Fatalf("logout retry lost cleanup state: %v", err)
			}
			if _, err := manager.Capture(t.Context()); !errors.Is(err, ErrKeyRequired) {
				t.Fatal("logout retained key")
			}
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := New(t.Context(), directory)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if snapshot, err := reopened.Snapshot(); err != nil || !reflect.DeepEqual(snapshot, Credentials{}) {
				t.Fatal("logout did not survive restart")
			}
		})
	}
}

func TestUnsyncedLogoutReopenReportsFilesystemState(t *testing.T) {
	t.Parallel()
	manager, directory := managerFixture(t)
	installFixture(t, manager)
	manager.storage.syncDirectory = func(*os.File) error { return errors.New("fixture") }
	if _, err := manager.Logout(); !errors.Is(err, ErrStoragePending) {
		t.Fatal(err)
	}
	if err := manager.Close(); !errors.Is(err, ErrStoragePending) {
		t.Fatal(err)
	}
	reopened, err := New(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Capture(t.Context()); !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("reopen invented deleted credentials: %v", err)
	}
}

func TestConcurrentInstallAndCloseBeforePublication(t *testing.T) {
	t.Run("one generation winner", func(t *testing.T) {
		manager, _ := managerFixture(t)
		var winners atomic.Int32
		var group sync.WaitGroup
		for range 16 {
			group.Go(func() {
				err := manager.Install(t.Context(), 0, fixtureCredentials())
				if err == nil {
					winners.Add(1)
				} else if !errors.Is(err, ErrChanged) {
					t.Error(err)
				}
			})
		}
		group.Wait()
		if winners.Load() != 1 || manager.Generation() != 1 {
			t.Fatal("CAS admitted multiple replacements")
		}
	})
	t.Run("close joins and cancels unpublished install", func(t *testing.T) {
		manager, directory := managerFixture(t)
		entered, release := make(chan struct{}), make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })
		defer unblock()
		manager.storage.syncFile = func(file *os.File) error { close(entered); <-release; return file.Sync() }
		installed := make(chan error, 1)
		go func() { installed <- manager.Install(t.Context(), 0, fixtureCredentials()) }()
		<-entered
		closed := make(chan error, 1)
		go func() { closed <- manager.Close() }()
		<-manager.ctx.Done()
		select {
		case <-closed:
			t.Fatal("close returned before publication exited")
		default:
		}
		unblock()
		if err := <-installed; !errors.Is(err, context.Canceled) {
			t.Fatalf("close allowed unpublished write: %v", err)
		}
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(directory, fileName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("cancelled publication reached disk")
		}
		if _, err := manager.Capture(t.Context()); !errors.Is(err, ErrClosed) {
			t.Fatal("closed manager authorized")
		}
	})
}

func TestConcurrentLogoutAndInstall(t *testing.T) {
	t.Parallel()
	manager, _ := managerFixture(t)
	old := installFixture(t, manager)
	var group sync.WaitGroup
	group.Go(func() {
		err := manager.Install(t.Context(), old.Generation, fixtureCredentials())
		if err != nil && !errors.Is(err, ErrChanged) {
			t.Error(err)
		}
	})
	group.Go(func() {
		if _, err := manager.Logout(); err != nil {
			t.Error(err)
		}
	})
	group.Wait()
	if _, err := manager.Capture(t.Context()); !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("old generation resurrected after logout: %v", err)
	}
	if err := manager.Check(t.Context(), old); !errors.Is(err, ErrChanged) {
		t.Fatal("old capture retained")
	}
}

func TestCancellationDoesNotChangeCredentials(t *testing.T) {
	t.Parallel()
	manager, _ := managerFixture(t)
	old := installFixture(t, manager)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := manager.Install(cancelled, old.Generation, fixtureCredentials()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := manager.Capture(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := manager.Check(cancelled, old); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := manager.Check(t.Context(), old); err != nil {
		t.Fatal(err)
	}
}

func TestCloseWaitsForPublishedOutcome(t *testing.T) {
	t.Parallel()
	manager, directory := managerFixture(t)
	if _, err := manager.Snapshot(); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	manager.storage.syncDirectory = func(*os.File) error { close(entered); <-release; return errors.New("fixture") }
	installed := make(chan error, 1)
	go func() { installed <- manager.Install(t.Context(), 0, fixtureCredentials()) }()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- manager.Close() }()
	<-manager.ctx.Done()
	select {
	case <-closed:
		t.Fatal("close returned during directory sync")
	default:
	}
	unblock()
	if err := <-installed; !errors.Is(err, ErrStoragePending) {
		t.Fatalf("published outcome mislabeled cancelled: %v", err)
	}
	if err := <-closed; !errors.Is(err, ErrStoragePending) {
		t.Fatal("close concealed published uncertainty")
	}
	reopened, err := New(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if captured, err := reopened.Capture(t.Context()); err != nil || captured.Key != fixtureCredentials().MachineKey.Value {
		t.Fatalf("published record lost: %v", err)
	}
}

func TestGenerationExhaustionDoesNotWrap(t *testing.T) {
	t.Parallel()
	manager, _ := managerFixture(t)
	installFixture(t, manager)
	manager.generation = ^uint64(0)
	old, err := manager.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Install(t.Context(), old.Generation, fixtureCredentials()); !errors.Is(err, ErrChanged) {
		t.Fatal("generation wrapped")
	}
	if _, err := manager.Logout(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Check(t.Context(), old); !errors.Is(err, ErrChanged) {
		t.Fatal("logout retained exhausted-generation capture")
	}
}
