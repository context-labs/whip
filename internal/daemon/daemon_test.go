package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestResumeActiveIsolatesFailuresAndAllowsRetry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIPCODE_HOME", home)
	store := openStore(t, filepath.Join(home, "sessions.db"))
	ids := []string{createRoot(t, store), createRoot(t, store), createRoot(t, store)}
	slices.Sort(ids)
	for _, id := range ids {
		if _, err := store.AddSchedule(id, "@every 1h", "wake", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	before, err := store.LoadMeta(ids[1])
	if err != nil {
		t.Fatal(err)
	}
	schedules := store.Schedules(ids[1])
	failure := &config.UnknownModelError{Model: "model"}
	repaired := false
	var attempts []string
	processes := newTestProcesses(t)
	value, err := New(store, processes, func(_ context.Context, meta session.Meta, _ []llm.Message) (Components, error) {
		attempts = append(attempts, meta.ID)
		if meta.ID == ids[1] && !repaired {
			return Components{}, failure
		}
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	if err := value.ResumeActive(t.Context()); err != nil {
		t.Fatalf("session failure prevented startup: %v", err)
	}
	if !reflect.DeepEqual(attempts, ids) {
		t.Fatalf("restore attempts = %v, want %v", attempts, ids)
	}
	log, err := os.ReadFile(filepath.Join(home, "whip.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"session.resume", ids[1], `model="model"`, `provider="provider"`, "unknown model"} {
		if !strings.Contains(string(log), field) {
			t.Errorf("restore log missing %q: %s", field, log)
		}
	}
	after, err := store.LoadMeta(ids[1])
	if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(schedules, store.Schedules(ids[1])) {
		t.Fatalf("failed restore changed saved session: %v", err)
	}
	if _, err := value.Open(ids[1]); !errors.Is(err, failure) {
		t.Fatalf("opening broken session = %v", err)
	}
	repaired = true
	if _, err := value.Open(ids[1]); err != nil {
		t.Fatalf("repaired session could not retry: %v", err)
	}
}

func TestResumeActiveCancellation(t *testing.T) {
	for _, when := range []string{"before restore", "during restore", "daemon shutdown"} {
		t.Run(when, func(t *testing.T) {
			store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
			for range 2 {
				if _, err := store.AddSchedule(createRoot(t, store), "@every 1h", "wake", time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			attempts := 0
			processes := newTestProcesses(t)
			value, err := New(store, processes, func(context.Context, session.Meta, []llm.Message) (Components, error) {
				attempts++
				cancel()
				return Components{}, errors.New("session initialization failed")
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = value.Close() })
			wantAttempts := 0
			switch when {
			case "before restore":
				cancel()
			case "during restore":
				wantAttempts = 1
			case "daemon shutdown":
				value.cancel()
			}
			if err := value.ResumeActive(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("restore cancellation = %v", err)
			}
			if attempts != wantAttempts {
				t.Fatalf("attempts = %d, want %d", attempts, wantAttempts)
			}
		})
	}
}
