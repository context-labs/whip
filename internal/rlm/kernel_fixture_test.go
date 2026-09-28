package rlm

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/engine/process"
)

func TestWorkerProcess(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		return
	}
	if err := process.WorkerMain(os.Args[separator+1:], os.Stdin, os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func retainedKernel(t *testing.T, options process.KernelOptions) *process.Kernel {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	options.Command = []string{executable, "-test.run=TestWorkerProcess", "--"}
	kernel, err := process.NewKernel(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kernel.Close)
	return kernel
}

type failingScratch struct{ saveErr error }

func (store *failingScratch) Load(context.Context) (string, process.SnapshotManifest, error) {
	return "", process.SnapshotManifest{}, nil
}

func (store *failingScratch) Save(context.Context, string, process.SnapshotManifest) error {
	return store.saveErr
}

type memoryCheckpoints struct {
	mu    sync.Mutex
	value *process.Checkpoint
}

func (store *memoryCheckpoints) Load(context.Context) (*process.Checkpoint, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.value == nil {
		return nil, nil //nolint:nilnil // CheckpointStore uses nil to report that no checkpoint exists.
	}
	checkpoint := *store.value
	checkpoint.Data = append([]byte{}, checkpoint.Data...)
	return &checkpoint, nil
}

func (store *memoryCheckpoints) Save(_ context.Context, value process.Checkpoint) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	value.Data = append([]byte{}, value.Data...)
	store.value = &value
	return nil
}
