package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type failingScratch struct {
	memoryScratch
	failures sync.Mutex
	loadErr  error
	saveErr  error
}

func (s *failingScratch) Load(ctx context.Context) (string, SnapshotManifest, error) {
	s.failures.Lock()
	err := s.loadErr
	s.failures.Unlock()
	if err != nil {
		return "", SnapshotManifest{}, err
	}
	return s.memoryScratch.Load(ctx)
}

func (s *failingScratch) Save(ctx context.Context, snapshot string, manifest SnapshotManifest) error {
	s.failures.Lock()
	err := s.saveErr
	s.failures.Unlock()
	if err != nil {
		return err
	}
	return s.memoryScratch.Save(ctx, snapshot, manifest)
}

func lifecycleKernel(t *testing.T, store ScratchStore) *Kernel {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := NewKernel(KernelOptions{Command: []string{executable, "-test.run=TestWorkerProcess", "--"}, Scratch: store, Manager: NewManager(1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kernel.Close)
	return kernel
}

func TestScratchLoadFailureStopsWorkerAndRetries(t *testing.T) {
	store := &failingScratch{}
	kernel := lifecycleKernel(t, store)
	if _, err := kernel.Exec(t.Context(), Cell{Code: "saved = 42"}); err != nil {
		t.Fatal(err)
	}
	if err := kernel.Suspend(); err != nil {
		t.Fatal(err)
	}
	store.loadErr = errors.New("temporary load failure")
	for range 3 {
		if _, _, _, err := kernel.AcquireTurn(t.Context()); err == nil {
			t.Fatal("load failure accepted")
		}
		if kernel.Started() || kernel.manager.Active() != 0 {
			t.Fatal("failed acquisition leaked resident worker")
		}
	}
	store.loadErr = nil
	result, err := kernel.Exec(t.Context(), Cell{Code: "saved"})
	if err != nil || result.Value != float64(42) || result.Restored == nil {
		t.Fatalf("retry = %+v, %v", result, err)
	}
}

func TestScratchSaveFailureKeepsCellAndPreviousCheckpoint(t *testing.T) {
	store := &failingScratch{}
	kernel := lifecycleKernel(t, store)
	if _, err := kernel.Exec(t.Context(), Cell{Code: "saved = 1"}); err != nil {
		t.Fatal(err)
	}
	before, _, _ := store.Load(t.Context())
	store.saveErr = errors.New("temporary save failure")
	result, err := kernel.Exec(t.Context(), Cell{Code: "saved = 2\nsaved"})
	if err != nil || result.Value != float64(2) || result.Scratch == nil || !strings.Contains(result.Scratch.Warning, "Do not replay") {
		t.Fatalf("cell = %+v, %v", result, err)
	}
	after, _, _ := store.Load(t.Context())
	if before != after {
		t.Fatal("failed save replaced checkpoint")
	}
	store.saveErr = nil
	if _, err := kernel.Exec(t.Context(), Cell{Code: "saved"}); err != nil {
		t.Fatal(err)
	}
	if err := kernel.Suspend(); err != nil {
		t.Fatal(err)
	}
	result, err = kernel.Exec(t.Context(), Cell{Code: "saved"})
	if err != nil || result.Value != float64(2) {
		t.Fatalf("saved retry = %+v, %v", result, err)
	}
}

func TestScratchCorruptionNeverOverwritesCheckpoint(t *testing.T) {
	store := &memoryScratch{program: "not a structured snapshot"}
	kernel := lifecycleKernel(t, store)
	for range 2 {
		if _, err := kernel.Exec(t.Context(), Cell{Code: "saved = 2"}); err == nil {
			t.Fatal("corrupt restore accepted")
		}
		if kernel.Started() || kernel.manager.Active() != 0 {
			t.Fatal("corrupt restore leaked worker")
		}
		if store.count() != 0 {
			t.Fatal("corrupt checkpoint overwritten")
		}
	}
}

func TestScratchManifestChangesAreCheckpointed(t *testing.T) {
	store := &memoryScratch{}
	kernel := lifecycleKernel(t, store)
	if _, err := kernel.Exec(t.Context(), Cell{Code: "saved = 2"}); err != nil {
		t.Fatal(err)
	}
	count := store.count()
	result, err := kernel.Exec(t.Context(), Cell{Code: "unsupported = files.read"})
	if err != nil || result.Scratch == nil || len(result.Scratch.Skipped) != 1 {
		t.Fatalf("skip = %+v, %v", result, err)
	}
	if store.count() != count+1 {
		t.Fatal("manifest-only change not saved")
	}
	result, err = kernel.Exec(t.Context(), Cell{Code: "saved"})
	if err != nil || result.Scratch != nil || store.count() != count+1 {
		t.Fatalf("unchanged report repeated %+v %v", result, err)
	}
}

func TestScratchOversizedFrameKeepsWorkerAndCheckpoint(t *testing.T) {
	store := &memoryScratch{}
	kernel := lifecycleKernel(t, store)
	kernel.limits.FrameBytes = 4096
	if _, err := kernel.Exec(t.Context(), Cell{Code: "saved = 1"}); err != nil {
		t.Fatal(err)
	}
	before, _, _ := store.Load(t.Context())
	process := kernel.worker
	result, err := kernel.Exec(t.Context(), Cell{Code: "saved = '\\x00' * 4000\nlen(saved)"})
	if err != nil || result.Value != float64(4000) || result.Scratch == nil || result.Scratch.Warning == "" {
		t.Fatalf("overflow = %+v %v", result, err)
	}
	if !kernel.Started() || kernel.worker != process || kernel.manager.Active() != 1 {
		t.Fatal("checkpoint overflow killed worker")
	}
	after, _, _ := store.Load(t.Context())
	if before != after {
		t.Fatal("overflow overwrote prior checkpoint")
	}
	result, err = kernel.Exec(t.Context(), Cell{Code: "saved = 2\nsaved"})
	if err != nil || result.Value != float64(2) || result.Scratch != nil {
		t.Fatalf("retry = %+v %v", result, err)
	}
}

func TestScratchMidTurnLoadFailureLeavesNoReplacementWorker(t *testing.T) {
	store := &failingScratch{}
	kernel := lifecycleKernel(t, store)
	ctx, _, release, err := kernel.AcquireTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := kernel.Exec(ctx, Cell{Code: "saved = 42"}); err != nil {
		t.Fatal(err)
	}
	// Simulate a process lost between cells in the same lease.
	kernel.mu.Lock()
	kernel.stop()
	kernel.mu.Unlock()
	store.loadErr = errors.New("offline")
	if _, err := kernel.Exec(ctx, Cell{Code: "saved"}); err == nil {
		t.Fatal("replacement load succeeded")
	}
	if kernel.Started() || kernel.manager.Active() != 0 {
		t.Fatal("replacement worker leaked")
	}
	store.loadErr = nil
	result, err := kernel.Exec(ctx, Cell{Code: "saved"})
	if err != nil || result.Value != float64(42) || result.Restored == nil {
		t.Fatalf("replacement retry = %+v %v", result, err)
	}
}

func TestScratchDeadProcessReplacementRetainsPoolReservation(t *testing.T) {
	store := &memoryScratch{}
	kernel := lifecycleKernel(t, store)
	ctx, _, release, err := kernel.AcquireTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := kernel.Exec(ctx, Cell{Code: "saved = 42"}); err != nil {
		t.Fatal(err)
	}
	kernel.mu.Lock()
	process := kernel.worker
	if err := killProcessGroup(process.command.Process.Pid); err != nil {
		t.Fatal(err)
	}
	<-process.done
	// Force startProcess to encounter the dead resident before its watcher can
	// take the kernel lock. This must not release the running pool grant.
	if err := kernel.startProcess(); err != nil {
		t.Fatal(err)
	}
	if _, err := kernel.restoreLocked(ctx); err != nil {
		t.Fatal(err)
	}
	kernel.mu.Unlock()
	if kernel.manager.Active() != 1 || !kernel.manager.running(kernel) {
		t.Fatal("replacement escaped pool accounting")
	}
	result, err := kernel.Exec(ctx, Cell{Code: "saved"})
	if err != nil || result.Value != float64(42) {
		t.Fatalf("replacement = %+v %v", result, err)
	}
}

func TestScratchCancelledAcquisitionStopsExistingResident(t *testing.T) {
	kernel := lifecycleKernel(t, &memoryScratch{})
	if err := kernel.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, _, err := kernel.AcquireTurn(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	if kernel.Started() || kernel.manager.Active() != 0 {
		t.Fatal("cancelled grant left an unaccounted worker")
	}
}

func TestScratchReplacementStaysPinnedUntilTurnRelease(t *testing.T) {
	kernel := lifecycleKernel(t, &memoryScratch{})
	other := lifecycleKernel(t, &memoryScratch{})
	other.manager = kernel.manager
	ctx, _, release, err := kernel.AcquireTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := kernel.Exec(ctx, Cell{Code: "saved = 41"}); err != nil {
		t.Fatal(err)
	}
	kernel.mu.Lock()
	kernel.stop()
	kernel.mu.Unlock()
	for _, code := range []string{"saved + 1", "saved + 1"} {
		result, err := kernel.Exec(ctx, Cell{Code: code})
		if err != nil || result.Value != float64(42) {
			t.Fatalf("replacement = %+v %v", result, err)
		}
		if !kernel.manager.running(kernel) {
			t.Fatal("replacement was unpinned at cell boundary")
		}
		competing, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		_, _, done, err := other.AcquireTurn(competing)
		done()
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("competing worker acquired pinned slot: %v", err)
		}
	}
	release()
	_, _, done, err := other.AcquireTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	done()
	// Reusing an ended lease context behaves as a single cell, never a new pin.
	if _, err := kernel.Exec(ctx, Cell{Code: "saved"}); err != nil {
		t.Fatal(err)
	}
	if kernel.manager.State(kernel) != KernelResident {
		t.Fatal("ended turn context leaked replacement pin")
	}
}

func TestScratchWhitespaceCheckpointIsNotAbsent(t *testing.T) {
	for _, snapshot := range []string{" ", "\n\t", ""} {
		t.Run(fmt.Sprintf("snapshot_%q", snapshot), func(t *testing.T) {
			store := &memoryScratch{program: snapshot, manifest: SnapshotManifest{Saved: []string{}}}
			kernel := lifecycleKernel(t, store)
			for range 2 {
				if _, err := kernel.Exec(t.Context(), Cell{Code: "saved = 2"}); err == nil {
					t.Fatal("empty checkpoint accepted")
				}
				if kernel.Started() || kernel.manager.Active() != 0 {
					t.Fatal("failed restore leaked worker")
				}
				if store.count() != 0 {
					t.Fatal("corrupt checkpoint overwritten")
				}
				stored, _, err := store.Load(t.Context())
				if err != nil || stored != snapshot {
					t.Fatal("checkpoint changed")
				}
			}
		})
	}
	store := &memoryScratch{program: " \t"}
	kernel := lifecycleKernel(t, store)
	if _, err := kernel.Exec(t.Context(), Cell{Code: "saved = 2"}); err == nil {
		t.Fatal("whitespace without manifest accepted")
	}
}
