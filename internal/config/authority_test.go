package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/context-labs/whip/internal/session"
)

func configAuthority(t *testing.T, host Host) (*Authority, Snapshot) {
	t.Helper()
	authority, err := NewAuthority(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(authority.directory, host); err != nil {
		t.Fatal(err)
	}
	value, err := authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return authority, value
}

func TestAuthoritySubscriptionPreservesDeclarationsAndSnapshots(t *testing.T) {
	host := Default()
	host.ProjectRoots["project"] = "/project"
	host.SkillRoots["skills"] = "/skills"
	host.StandingInstructionsFile = "/standing/me.md"
	host.Resources = []session.ResourceLimit{{Kind: session.ResourceDepth, Limit: new(int64(3))}}
	host.Engine = session.QuickJS
	host.Providers["api"] = Provider{Kind: "openai-responses", BaseURL: "https://example.test/v1", CredentialEnv: "NAMED_KEY", Models: map[string]Model{
		"model": {ContextWindowTokens: new(int64(9000)), Prices: session.ModelPrices{Input: new(int64(123))}},
	}}
	host.Defaults.Model = session.ModelSelection{Provider: "api", Name: "model", Effort: "high", Temperature: new(0.0)}
	host.Defaults.Compaction.Model = new(host.Defaults.Model.Clone())
	host.Defaults.Instructions = session.Instructions{Text: "keep these instructions", ProjectFiles: []string{"AGENTS.md"}, SkillRoots: []string{"skills"}}
	host.Defaults.OutputSchema = json.RawMessage(`{"type":"integer"}`)
	authority, before := configAuthority(t, host)
	var retained *Host
	installed, err := authority.Update(t.Context(), before.Revision, func(h *Host) error {
		retained = h
		return h.EnsureSubscription()
	})
	if err != nil || installed.Revision == before.Revision {
		t.Fatalf("install revision=%q err=%v", installed.Revision, err)
	}
	if installed.Host.Providers["openai-codex"].Kind != "openai-codex" {
		t.Fatal("subscription route absent")
	}
	delete(installed.Host.Providers, "openai-codex")
	if !reflect.DeepEqual(installed.Host, before.Host) {
		t.Fatalf("installation changed unrelated declarations: got=%+v want=%+v", installed.Host, before.Host)
	}
	// Neither callback-owned values nor returned values alias another snapshot.
	retained.Defaults.Instructions.Text = "mutated callback"
	*installed.Host.Defaults.Model.Temperature = 2
	*installed.Host.Providers["api"].Models["model"].Prices.Input = 999
	current, err := authority.Snapshot(t.Context())
	if err != nil || current.Host.Defaults.Instructions.Text != host.Defaults.Instructions.Text || *current.Host.Defaults.Model.Temperature != 0 || *current.Host.Providers["api"].Models["model"].Prices.Input != 123 {
		t.Fatalf("snapshot aliases caller: %+v %v", current, err)
	}
	if len(current.Host.Resources) != 1 {
		t.Fatal("update expanded omitted resource defaults")
	}
	loaded, err := Load(authority.directory)
	if err != nil || len(loaded.Resources) != len(session.ResourceKinds()) || loaded.Providers["openai-codex"].Kind != "openai-codex" {
		t.Fatalf("existing router read disagrees with publication: %+v %v", loaded, err)
	}
	reopened, err := NewAuthority(authority.directory)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.Snapshot(t.Context())
	if err != nil || !reflect.DeepEqual(after, current) {
		t.Fatalf("reopen lost declaration/revision: %+v %v", after, err)
	}
}

func TestAuthorityNoOpAndExactRevision(t *testing.T) {
	host := Default()
	host.Defaults.OutputSchema = json.RawMessage(`{"type":"integer"}`)
	host.Providers["openai-codex"] = Provider{Kind: "openai-codex", Models: map[string]Model{
		"gpt-6-astra": {TimeoutMillis: 2345, MaxAttempts: 2, ContextWindowTokens: new(int64(200000))},
	}}
	authority, _ := configAuthority(t, host)
	path := filepath.Join(authority.directory, FileName)
	raw, err := json.Marshal(host)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n', '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if before.Revision != hex.EncodeToString(digest[:]) {
		t.Fatal("revision does not describe exact stored bytes")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := authority.Update(t.Context(), before.Revision, (*Host).EnsureSubscription)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("existing valid subscription changed: %+v %v", after, err)
	}
	now, err := os.Stat(path)
	stored, readErr := os.ReadFile(path)
	if err != nil || readErr != nil || !os.SameFile(info, now) || !bytes.Equal(raw, stored) {
		t.Fatal("no-op rewrote the file", err, readErr)
	}
	// Even a formatting-only external edit invalidates an earlier revision.
	if err := os.WriteFile(path, append(raw, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Update(t.Context(), before.Revision, (*Host).EnsureSubscription); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale no-op accepted: %v", err)
	}
}

func TestAuthorityCompetingUpdatesAndFreshReads(t *testing.T) {
	authority, before := configAuthority(t, Default())
	second, err := NewAuthority(authority.directory)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for index, owner := range []*Authority{authority, second} {
		workers.Go(func() {
			<-start
			_, err := owner.Update(t.Context(), before.Revision, func(h *Host) error {
				h.Defaults.Instructions.Text = []string{"first", "second"}[index]
				return nil
			})
			results <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrRevisionConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
	current, err := authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Bootstrap Save uses the same file; an authority never retains stale data.
	host := current.Host
	host.Defaults.Instructions.Text = "external local save"
	if err := Save(authority.directory, host); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Update(t.Context(), current.Revision, (*Host).EnsureSubscription); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("ignored local save: %v", err)
	}
}

func TestAuthorityRejectsInvalidUpdatesWithoutPublication(t *testing.T) {
	for _, tc := range []struct {
		name  string
		patch func(*Host) error
	}{
		{"callback failure", func(*Host) error { return errors.New("rejected patch") }},
		{"invalid host", func(h *Host) error { h.Version--; return nil }},
		{"oversize", func(h *Host) error {
			h.Defaults.Instructions.Text = strings.Repeat("x", session.MaxDocumentBytes)
			return nil
		}},
		{"missing patch", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authority, before := configAuthority(t, Default())
			if _, err := authority.Update(t.Context(), before.Revision, tc.patch); err == nil {
				t.Fatal("invalid update accepted")
			}
			after, err := authority.Snapshot(t.Context())
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatal("rejected update changed configuration", err)
			}
		})
	}
	authority, before := configAuthority(t, Default())
	if _, err := authority.Update(t.Context(), "", (*Host).EnsureSubscription); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("empty revision accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	_, err := authority.Update(ctx, before.Revision, func(h *Host) error { cancel(); return h.EnsureSubscription() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled patch published: %v", err)
	}
	if _, err := authority.Snapshot(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read succeeded: %v", err)
	}
	after, err := authority.Snapshot(t.Context())
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatal("cancelled update changed configuration", err)
	}
}

func TestAuthoritySubscriptionConflictsAndMalformedFile(t *testing.T) {
	host := Default()
	host.Providers["openai-codex"] = Provider{Kind: "openai-chat", BaseURL: "https://custom.test/v1", CredentialEnv: "CUSTOM"}
	authority, before := configAuthority(t, host)
	if _, err := authority.Update(t.Context(), before.Revision, (*Host).EnsureSubscription); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("custom route overwritten: %v", err)
	}
	after, err := authority.Snapshot(t.Context())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("conflict changed configuration", err)
	}
	for _, invalid := range []Provider{
		{Kind: "openai-codex", BaseURL: "https://custom.test"},
		{Kind: "openai-codex", CredentialEnv: "CUSTOM"},
		{Kind: "openai-codex", Models: map[string]Model{"model": {MaxAttempts: session.MaxModelAttempts + 1}}},
	} {
		host.Providers["openai-codex"] = invalid
		if err := host.EnsureSubscription(); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid fixed profile accepted: %v", err)
		}
	}
	path := filepath.Join(authority.directory, FileName)
	for _, raw := range []string{"{", "{\"version\":9,\"defaults\":{\"instructions\":{\"text\":\"\xff\"}}}", `{"version":8}`, `{"unknown":"value"}`, strings.Repeat("x", session.MaxDocumentBytes+1)} {
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		called := false
		_, err := authority.Update(t.Context(), before.Revision, func(*Host) error { called = true; return nil })
		if err == nil || called {
			t.Fatal("malformed file reached patch", err)
		}
		stored, err := os.ReadFile(path)
		if err != nil || string(stored) != raw {
			t.Fatal("malformed file was repaired implicitly", err)
		}
	}
}

func TestAuthorityPersistenceFailureAndPublishedErrorRecovery(t *testing.T) {
	authority, before := configAuthority(t, Default())
	if os.Getuid() != 0 { // Root bypasses Unix permission denial.
		if err := os.Chmod(authority.directory, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(authority.directory, 0o700) })
		if _, err := authority.Update(t.Context(), before.Revision, (*Host).EnsureSubscription); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("unwritable directory update: %v", err)
		}
		after, err := authority.Snapshot(t.Context())
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Fatal("failed publication changed file", err)
		}
		if err := os.Chmod(authority.directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	failure := errors.New("injected directory sync failure")
	authority.syncDirectory = func(*os.File) error { return failure }
	if _, err := authority.Update(t.Context(), before.Revision, (*Host).EnsureSubscription); !errors.Is(err, failure) || !strings.Contains(err.Error(), "published") {
		t.Fatalf("post-publication failure was hidden: %v", err)
	}
	current, err := authority.Snapshot(t.Context())
	if err != nil || current.Revision == before.Revision || current.Host.Providers["openai-codex"].Kind != "openai-codex" {
		t.Fatalf("caller cannot recover published outcome: %+v %v", current, err)
	}
	if _, err := authority.Update(t.Context(), before.Revision, (*Host).EnsureSubscription); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("old revision survived publication: %v", err)
	}
	if _, err := authority.Update(t.Context(), current.Revision, (*Host).EnsureSubscription); !errors.Is(err, failure) {
		t.Fatalf("unchanged retry claimed unconfirmed durability: %v", err)
	}
	authority.syncDirectory = (*os.File).Sync
	recovered, err := authority.Update(t.Context(), current.Revision, (*Host).EnsureSubscription)
	if err != nil || !reflect.DeepEqual(recovered, current) {
		t.Fatalf("setup retry was not idempotent: %+v %v", recovered, err)
	}
	entries, err := os.ReadDir(authority.directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != FileName {
		t.Fatal("publication leaked temporary files", err)
	}
}

func TestAuthorityPublicationRemainsAnchoredDuringDirectoryRetarget(t *testing.T) {
	authority, before := configAuthority(t, Default())
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	finished := make(chan struct{})
	var updateErr error
	go func() {
		_, updateErr = authority.Update(t.Context(), before.Revision, func(h *Host) error {
			close(entered)
			<-release
			return h.EnsureSubscription()
		})
		close(finished)
	}()
	// Join before temp-directory cleanup, including failed assertions.
	defer func() { unblock(); <-finished }()
	<-entered
	original := authority.directory + "-original"
	if err := os.Rename(authority.directory, original); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(original) })
	if err := os.Mkdir(authority.directory, 0o700); err != nil {
		t.Fatal(err)
	}
	other := Default()
	other.Defaults.Instructions.Text = "replacement directory"
	raw, err := encodeHost(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(authority.directory, FileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	unblock()
	<-finished
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	old, oldErr := Load(original)
	current, currentErr := Load(authority.directory)
	if oldErr != nil || currentErr != nil || old.Providers["openai-codex"].Kind != "openai-codex" || current.Defaults.Instructions.Text != other.Defaults.Instructions.Text || len(current.Providers) != 0 {
		t.Fatalf("retarget crossed directory ownership: old=%+v current=%+v errors=%v %v", old, current, oldErr, currentErr)
	}
}

func TestConfigurationPathAndModeBoundaries(t *testing.T) {
	for _, mode := range []os.FileMode{0o400, 0o600, 0o644} {
		authority, before := configAuthority(t, Default())
		if err := os.Chmod(filepath.Join(authority.directory, FileName), mode); err != nil {
			t.Fatal(err)
		}
		if _, err := authority.Update(t.Context(), before.Revision, (*Host).EnsureSubscription); err != nil {
			t.Fatalf("existing readable mode %o rejected: %v", mode, err)
		}
		info, err := os.Stat(filepath.Join(authority.directory, FileName))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("published permissions: %v %v", info, err)
		}
	}
	for _, kind := range []string{"symlink inside", "symlink outside", "dangling symlink", "directory", "fifo", "writable file", "writable directory"} {
		t.Run(kind, func(t *testing.T) {
			authority, before := configAuthority(t, Default())
			path := filepath.Join(authority.directory, FileName)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink inside", "symlink outside", "dangling symlink":
				target := filepath.Join(authority.directory, "target")
				if kind == "symlink outside" {
					target = filepath.Join(t.TempDir(), "target")
				}
				if kind != "dangling symlink" {
					if err := os.WriteFile(target, []byte("unrelated file"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			case "writable file":
				if err := os.WriteFile(path, []byte("unrelated file"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o622); err != nil {
					t.Fatal(err)
				}
			case "writable directory":
				if err := os.Chmod(authority.directory, 0o722); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := authority.Snapshot(t.Context()); err == nil {
				t.Fatal("unsafe read accepted")
			}
			if _, err := authority.Update(t.Context(), before.Revision, (*Host).EnsureSubscription); err == nil {
				t.Fatal("unsafe update accepted")
			}
			if err := Save(authority.directory, Default()); err == nil {
				t.Fatal("unsafe bootstrap replacement accepted")
			}
		})
	}
	owner, _ := configAuthority(t, Default())
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(owner.directory, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{alias, alias + string(os.PathSeparator)} {
		if _, err := Load(path); err == nil {
			t.Fatal("symlink directory accepted")
		}
	}
}
