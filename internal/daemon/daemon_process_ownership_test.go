package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func TestDaemonProcessEnvironmentSnapshotsBeforeConstruction(t *testing.T) {
	const variable = "LC_WHIP_PROCESS_OWNERSHIP"
	t.Setenv(variable, "at-store-open")
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	processes := store.Processes()
	defer store.Close()
	rootID := createRoot(t, store)
	t.Setenv(variable, "before-daemon-new")
	services := tools.NewServices()
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: NewToolRunner(services)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	t.Setenv(variable, "before-root-bind")
	if _, err := owner.Open(rootID); err != nil {
		t.Fatal(err)
	}
	options := services.ProcessOptions()
	if options.Processes != processes || options.RootID != rootID {
		t.Fatalf("borrowed process scope = %p %q, want %p %q", options.Processes, options.RootID, processes, rootID)
	}
	environment, err := options.Processes.ChildEnvironment(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(environment, variable+"=at-store-open") {
		t.Fatal("process environment was not captured before daemon construction")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewRecoveryFailureLeavesResourcesCallerOwned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	defer store.Close()
	processes := store.Processes()
	rootID := createRoot(t, store)
	if err := store.SetBudgetLimit(t.Context(), rootID, "", session.BudgetTokens, 100); err != nil {
		t.Fatal(err)
	}
	fixtureDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer fixtureDB.Close()
	const marker = "fixture process recovery failure"
	if _, err := fixtureDB.Exec(`CREATE TRIGGER reject_process_ownership_recovery BEFORE UPDATE ON budgets BEGIN SELECT RAISE(ABORT,'fixture process recovery failure'); END`); err != nil {
		t.Fatal(err)
	}
	defer fixtureDB.Exec(`DROP TRIGGER IF EXISTS reject_process_ownership_recovery`)
	var stopped atomic.Int32
	if _, err := processes.RegisterStop(rootID, func() error { stopped.Add(1); return nil }); err != nil {
		t.Fatal(err)
	}
	factory := func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	}
	owner, err := New(store, factory)
	if owner != nil || err == nil || !strings.Contains(err.Error(), marker) {
		t.Fatalf("recovery result = %v, %v", owner, err)
	}
	if stopped.Load() != 0 {
		t.Fatal("failed construction closed caller-owned processes")
	}
	if _, err := store.LoadMeta(rootID); err != nil {
		t.Fatalf("failed construction closed caller-owned database: %v", err)
	}
	unregister, err := processes.RegisterStop("after-failed-new", func() error { return nil })
	if err != nil {
		t.Fatalf("failed construction closed caller-owned manager: %v", err)
	}
	unregister()
	if !store.AcquireDaemon() {
		t.Fatal("failed recovery retained the daemon ownership guard")
	}
	store.ReleaseDaemon()
	if _, err := fixtureDB.Exec(`DROP TRIGGER reject_process_ownership_recovery`); err != nil {
		t.Fatal(err)
	}
	owner, err = New(store, factory)
	if err != nil {
		t.Fatalf("retry with the same resources: %v", err)
	}
	defer owner.Close()
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if stopped.Load() != 1 {
		t.Fatalf("successful owner's shutdown callbacks = %d, want 1", stopped.Load())
	}
}

func TestDaemonCloseStopsProcessesBeforeStoreAndCachesErrors(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	defer store.Close()
	processes := store.Processes()
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	sentinels := []error{errors.New("first process stop"), errors.New("second process stop")}
	counts := make([]int, len(sentinels))
	databaseErrors := make([]error, len(sentinels))
	for i, sentinel := range sentinels {
		// Unopened scopes reach global Close rather than an actor's StopRoot.
		if _, err := processes.RegisterStop(fmt.Sprintf("unopened-%d", i), func() error {
			counts[i]++
			_, loadErr := store.LoadMeta(rootID)
			databaseErrors[i] = errors.Join(loadErr, store.SetGoal(rootID, "written during process stop"))
			return sentinel
		}); err != nil {
			t.Fatal(err)
		}
	}
	first := owner.Close()
	for i, sentinel := range sentinels {
		if counts[i] != 1 || databaseErrors[i] != nil || !errors.Is(first, sentinel) {
			t.Fatalf("callback %d: count=%d database=%v close=%v", i, counts[i], databaseErrors[i], first)
		}
	}
	if _, err := processes.RegisterStop("after-close", func() error { return nil }); !errors.Is(err, capability.ErrProcessManagerClosed) {
		t.Fatalf("manager after close = %v", err)
	}
	if _, err := store.LoadMeta(rootID); err == nil {
		t.Fatal("database remained open after daemon shutdown")
	}
	if repeated := owner.Close(); repeated != first {
		t.Fatalf("Close did not return the cached error: first=%v repeated=%v", first, repeated)
	}
	for i, count := range counts {
		if count != 1 {
			t.Fatalf("callback %d repeated: %d", i, count)
		}
	}
	if !store.AcquireDaemon() {
		t.Fatal("Close retained the daemon ownership guard")
	}
	store.ReleaseDaemon()
}

func TestFailedRootBindStopsOnlyItsProcessScope(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	defer store.Close()
	processes := store.Processes()
	healthyID, failingID := createRoot(t, store), createRoot(t, store)
	bindErr := errors.New("fixture root bind failure")
	var healthyStops, failingStops atomic.Int32
	owner, err := New(store, func(_ context.Context, meta session.Meta, _ []llm.Message) (Components, error) {
		services := tools.NewServices()
		return Components{Runner: NewToolRunner(services), Bind: func(context.Context, *Session) error {
			options := services.ProcessOptions()
			if options.Processes != processes || options.RootID != meta.ID {
				return fmt.Errorf("borrowed process scope = %p %q, want %p %q", options.Processes, options.RootID, processes, meta.ID)
			}
			count := &healthyStops
			if meta.ID == failingID {
				count = &failingStops
			}
			if _, err := options.Processes.RegisterStop(meta.ID, func() error { count.Add(1); return nil }); err != nil {
				return err
			}
			if meta.ID == failingID {
				return bindErr
			}
			return nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	healthy, err := owner.Open(healthyID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Open(failingID); !errors.Is(err, bindErr) {
		t.Fatalf("failed root bind = %v", err)
	}
	if failingStops.Load() != 1 || healthyStops.Load() != 0 {
		t.Fatalf("partial-bind callbacks: failing=%d healthy=%d", failingStops.Load(), healthyStops.Load())
	}
	select {
	case <-healthy.Done():
		t.Fatal("failed binding stopped the healthy root")
	default:
	}
	// StopRoot releases only this scope and permits later reactivation.
	unregister, err := processes.RegisterStop(failingID, func() error { return nil })
	if err != nil {
		t.Fatalf("reactivate failed root scope: %v", err)
	}
	unregister()
	healthy.Stop()
	if healthyStops.Load() != 1 || failingStops.Load() != 1 {
		t.Fatalf("root stop callbacks: failing=%d healthy=%d", failingStops.Load(), healthyStops.Load())
	}
	unregister, err = processes.RegisterStop("after-root-stop", func() error { return nil })
	if err != nil {
		t.Fatalf("root stop closed the global manager: %v", err)
	}
	unregister()
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := processes.RegisterStop("after-owner-close", func() error { return nil }); !errors.Is(err, capability.ErrProcessManagerClosed) {
		t.Fatalf("owner shutdown did not close manager: %v", err)
	}
}
