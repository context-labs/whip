package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderKeyPublicationRetryIdentityAndDurability(t *testing.T) {
	a, before := configAuthority(t, Default())
	original := a.syncDirectory
	a.syncDirectory = func(*os.File) error { return errors.New("private storage diagnostic") }
	if _, err := a.PublishKey(t.Context(), "stable-key", "private-fixture-key"); !errors.Is(err, ErrKeyStoragePending) || strings.Contains(err.Error(), "private storage") {
		t.Fatal("ambiguous durability not safely reported", err)
	}
	files, err := filepath.Glob(filepath.Join(a.directory, "provider-key-*.txt"))
	if err != nil || len(files) != 1 {
		t.Fatal("publication outcome missing", files, err)
	}
	first, err := os.Stat(files[0])
	if err != nil || first.Mode().Perm() != 0o600 {
		t.Fatal("key was not private", err)
	}
	if _, err := a.PublishKey(t.Context(), "stable-key", "different-key"); !errors.Is(err, ErrKeyConflict) {
		t.Fatal("identity overwrite allowed", err)
	}
	a.syncDirectory = original
	path, err := a.PublishKey(t.Context(), "stable-key", "private-fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	again, err := os.Stat(path)
	if err != nil || !first.ModTime().Equal(again.ModTime()) {
		t.Fatal("retry rewrote immutable key", err)
	}
	key, err := (Provider{CredentialSource: "file", CredentialFile: path}).Credential(t.Context(), nil)
	if err != nil || key != "private-fixture-key" {
		t.Fatal("published source is not resolvable", err)
	}
	after, err := a.Snapshot(t.Context())
	if err != nil || before.Revision != after.Revision {
		t.Fatal("key publication changed host declaration", err)
	}
	files, _ = filepath.Glob(filepath.Join(a.directory, "provider-key-*.txt"))
	if len(files) != 1 {
		t.Fatal("retry leaked duplicate key files")
	}
}

func TestProviderKeyCapacityDoesNotPreventExactRetry(t *testing.T) {
	a, _ := configAuthority(t, Default())
	path, err := a.PublishKey(t.Context(), "first", "private-key")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 255 {
		if err := os.WriteFile(filepath.Join(a.directory, fmt.Sprintf("provider-key-fixture-%d.txt", i)), []byte("key"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.PublishKey(t.Context(), "overflow", "new-key"); !errors.Is(err, ErrKeyStorage) {
		t.Fatal("unbounded key files", err)
	}
	if retry, err := a.PublishKey(t.Context(), "first", "private-key"); err != nil || retry != path {
		t.Fatal("capacity prevented confirmation", err)
	}
}

func TestProviderKeyUnsafeExistingFileAndBounds(t *testing.T) {
	a, _ := configAuthority(t, Default())
	path, err := a.PublishKey(t.Context(), "key", "private-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PublishKey(t.Context(), "key", "private-key"); !errors.Is(err, ErrKeyStorage) {
		t.Fatal("public key file accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("host.json", path); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PublishKey(t.Context(), "key", "private-key"); !errors.Is(err, ErrKeyStorage) {
		t.Fatal("symlink followed")
	}
	for _, value := range []string{"", strings.Repeat("k", (64<<10)+1), "line\nbreak"} {
		if _, err := a.PublishKey(t.Context(), "new-key", value); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.PublishKey(ctx, "cancelled", "key"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled publication executed")
	}
}
