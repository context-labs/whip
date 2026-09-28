package inferenceauth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCancellationDuringFirstLoadDoesNotAuthorizeOrCache(t *testing.T) {
	manager, directory := managerFixture(t)
	if err := os.WriteFile(filepath.Join(directory, fileName), recordFixture(t), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	manager.storage.syncDirectory = func(*os.File) error { cancel(); return nil }
	if _, err := manager.Capture(ctx); !errors.Is(err, context.Canceled) || manager.loaded {
		t.Fatalf("cancelled load authorized or cached credential: %v", err)
	}
	manager.storage.syncDirectory = (*os.File).Sync
	if _, err := manager.Capture(t.Context()); err != nil {
		t.Fatalf("cancelled load blocked later fresh capture: %v", err)
	}
}

func TestFirstUseConfirmsSavedCredentialsAndAbsenceBeforeAuthorization(t *testing.T) {
	for _, saved := range []bool{false, true} {
		name := "absent"
		if saved {
			name = "saved"
		}
		t.Run(name, func(t *testing.T) {
			manager, directory := managerFixture(t)
			if saved {
				if err := os.WriteFile(filepath.Join(directory, fileName), recordFixture(t), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			confirms := 0
			manager.storage.syncDirectory = func(*os.File) error { confirms++; return errors.New("fixture") }
			if manager.loaded || manager.Generation() != 0 || confirms != 0 {
				t.Fatal("constructing/inspecting generation read private credentials")
			}
			if _, err := manager.Capture(t.Context()); !errors.Is(err, ErrStorage) || manager.loaded {
				t.Fatalf("failed first confirmation authorized credentials: %v", err)
			}
			if _, err := manager.Snapshot(); !errors.Is(err, ErrStorage) || confirms != 2 {
				t.Fatalf("failed first confirmation became cached state: %v", err)
			}
			manager.storage.syncDirectory = (*os.File).Sync
			captured, err := manager.Capture(t.Context())
			if saved {
				if err != nil || captured.Key != fixtureCredentials().MachineKey.Value {
					t.Fatalf("saved key did not recover after confirmation: %v", err)
				}
			} else if !errors.Is(err, ErrKeyRequired) {
				t.Fatalf("absent key did not recover after confirmation: %v", err)
			}
		})
	}
}

func TestFirstLogoutLoadsCleanupRecordAndRevokesIt(t *testing.T) {
	manager, directory := managerFixture(t)
	if err := os.WriteFile(filepath.Join(directory, fileName), recordFixture(t), 0o600); err != nil {
		t.Fatal(err)
	}
	prior, err := manager.Logout()
	if err != nil || prior.Management.Token != fixtureCredentials().Management.Token || prior.MachineKey.Value != fixtureCredentials().MachineKey.Value {
		t.Fatalf("first logout lost private cleanup record: %v", err)
	}
	if _, err := manager.Capture(t.Context()); !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("logout authorized saved key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, fileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("first logout did not remove saved credentials")
	}
}
