package inferenceauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func recordFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(record{Version: 1, Credentials: fixtureCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestStrictBoundedRecord(t *testing.T) {
	t.Parallel()
	raw := recordFixture(t)
	for _, test := range []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"valid", raw, true},
		{"exact byte bound", append(bytes.Clone(raw), bytes.Repeat([]byte(" "), maxRecordBytes-len(raw))...), true},
		{"oversized", append(bytes.Clone(raw), bytes.Repeat([]byte(" "), maxRecordBytes-len(raw)+1)...), false},
		{"unknown version", bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":2`), 1), false},
		{"unknown member", bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"secret":"private-value"`), 1), false},
		{"duplicate", bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), false},
		{"duplicate nested", bytes.Replace(raw, []byte(`"token":"private-management"`), []byte(`"token":"private-management","token":"different"`), 1), false},
		{"escaped duplicate", bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"\u0076ersion":1`), 1), false},
		{"case alias", bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"Version":1`), 1), false},
		{"trailing value", append(bytes.Clone(raw), []byte(` {}`)...), false},
		{"invalid utf8", bytes.Replace(raw, []byte("private-management"), []byte{0xff}, 1), false},
		{"null record", []byte(`null`), false},
		{"array record", []byte(`[]`), false},
		{"null credentials", []byte(`{"version":1,"credentials":null}`), false},
		{"invalid token", bytes.Replace(raw, []byte("private-management"), []byte(`private\nmanagement`), 1), false},
		{"empty", nil, false},
		{"truncated", raw[:len(raw)-1], false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, fileName), test.data, 0o600); err != nil {
				t.Fatal(err)
			}
			manager, err := New(t.Context(), directory)
			if test.valid {
				if err != nil {
					t.Fatal(err)
				}
				if err := manager.Close(); err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrStorage) || manager != nil {
				t.Fatalf("invalid private record accepted: %v", err)
			}
			if err != nil && (strings.Contains(err.Error(), directory) || strings.Contains(err.Error(), "private-value")) {
				t.Fatal("storage diagnostic leaked private input")
			}
		})
	}
}

func TestCredentialValidationAndEncodedLimit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*Credentials)
	}{
		{"empty", func(c *Credentials) { *c = Credentials{} }},
		{"management identity missing", func(c *Credentials) { c.Management.UserID = "" }},
		{"expiry without management", func(c *Credentials) { c.Management.Token = "" }},
		{"partial scope", func(c *Credentials) { c.Scope.ProjectID = "" }},
		{"key without scope", func(c *Credentials) { c.Scope = Scope{} }},
		{"key identity missing", func(c *Credentials) { c.MachineKey.ID = "" }},
		{"key metadata without value", func(c *Credentials) { c.MachineKey.Value = "" }},
		{"oversized token", func(c *Credentials) { c.MachineKey.Value = strings.Repeat("k", maxTokenBytes+1) }},
		{"oversized identity", func(c *Credentials) { c.Management.UserID = strings.Repeat("u", 257) }},
		{"invalid utf8 name", func(c *Credentials) { c.Scope.TeamName = string([]byte{0xff}) }},
		{"header injection", func(c *Credentials) { c.MachineKey.Value = "key\r\nsecret" }},
		{"zero expiry", func(c *Credentials) { *c.Management.ExpiresAt = c.Management.ExpiresAt.AddDate(-2019, 0, 0) }},
		{"escaped record overflow", func(c *Credentials) {
			c.Management.Token = strings.Repeat(`"`, maxTokenBytes)
			c.MachineKey.Value = c.Management.Token
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manager, directory := managerFixture(t)
			value := fixtureCredentials()
			test.change(&value)
			if err := manager.Install(t.Context(), 0, value); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid record installed: %v", err)
			}
			if manager.Generation() != 0 {
				t.Fatal("invalid input changed generation")
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 {
				t.Fatal("invalid input created a record")
			}
		})
	}
}

func TestUnsafeCredentialFilesAndDirectories(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"public file", "executable file", "directory file", "fifo file", "inside symlink", "outside symlink", "dangling symlink", "hardlink", "writable directory", "directory symlink", "directory symlink trailing slash"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			path := filepath.Join(directory, fileName)
			root := directory
			raw := recordFixture(t)
			switch kind {
			case "public file", "executable file":
				mode := os.FileMode(0o644)
				if kind == "executable file" {
					mode = 0o700
				}
				if err := os.WriteFile(path, raw, mode); err != nil {
					t.Fatal(err)
				}
			case "directory file":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "fifo file":
				if err := unix.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			case "inside symlink", "outside symlink", "dangling symlink", "hardlink":
				target := filepath.Join(directory, "target")
				if kind == "outside symlink" {
					target = filepath.Join(t.TempDir(), "target")
				}
				if kind != "dangling symlink" {
					if err := os.WriteFile(target, raw, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				if kind == "hardlink" {
					err = os.Link(target, path)
				} else {
					err = os.Symlink(target, path)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "writable directory":
				if err := os.Chmod(directory, 0o770); err != nil {
					t.Fatal(err)
				}
			case "directory symlink", "directory symlink trailing slash":
				root = filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(directory, root); err != nil {
					t.Fatal(err)
				}
				if kind == "directory symlink trailing slash" {
					root += "/"
				}
			}
			if manager, err := New(t.Context(), root); !errors.Is(err, ErrStorage) || manager != nil {
				t.Fatalf("unsafe storage accepted: %v", err)
			}
		})
	}
	for _, path := range []string{"", "relative"} {
		if _, err := New(t.Context(), path); !errors.Is(err, ErrStorage) {
			t.Fatalf("implicit directory accepted: %v", err)
		}
	}
}

func TestPublicationUsesAnchoredDirectoryAndPrivateMode(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	directory := filepath.Join(parent, "account")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	manager, err := New(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	moved := filepath.Join(parent, "original")
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	installFixture(t, manager)
	info, err := os.Stat(filepath.Join(moved, fileName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("publication changed root or permissions: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, fileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("publication followed replaced directory path")
	}
	if _, err := manager.Logout(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(moved, fileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("logout did not use anchor")
	}
}

func TestUnsafeReplacementDoesNotAuthorizeOrDeleteTarget(t *testing.T) {
	t.Parallel()
	manager, directory := managerFixture(t)
	old := installFixture(t, manager)
	target := filepath.Join(t.TempDir(), "private")
	raw := recordFixture(t)
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, fileName)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := manager.Install(t.Context(), old.Generation, fixtureCredentials()); !errors.Is(err, ErrStorage) {
		t.Fatal("overwrote unsafe record")
	}
	if _, err := manager.Logout(); !errors.Is(err, ErrStoragePending) {
		t.Fatal("unsafe logout not reported")
	}
	if _, err := manager.Capture(t.Context()); !errors.Is(err, ErrStoragePending) {
		t.Fatal("unsafe logout retained authority")
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("symlink target changed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Logout(); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledNewDoesNotOpenStorage(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := New(ctx, "missing-private-directory"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled initialization did filesystem work: %v", err)
	}
}
