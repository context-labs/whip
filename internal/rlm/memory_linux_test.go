//go:build linux

package rlm

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestMemoryLimitPreservesRuntimeReservations(t *testing.T) {
	if raceEnabled || strconv.IntSize < 64 {
		t.Skip("production address-space limit requires a non-race 64-bit worker")
	}
	if os.Getenv("WHIP_MEMORY_LIMIT_HELPER") != "1" {
		command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMemoryLimitPreservesRuntimeReservations$")
		command.Env = append(os.Environ(), "WHIP_MEMORY_LIMIT_HELPER=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("worker with a large startup reservation: %v\n%s", err, output)
		}
		return
	}

	// Reserve address space without allocating RAM, as the Go runtime does.
	reservationBytes := uint64(5 << 30)
	reservation, err := unix.Mmap(-1, 0, int(reservationBytes), unix.PROT_NONE, unix.MAP_PRIVATE|unix.MAP_ANON|unix.MAP_NORESERVE)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Munmap(reservation) }()
	if err := applyMemoryLimit(defaultMemoryBytes); err != nil {
		t.Fatal(err)
	}
	page, err := unix.Mmap(-1, 0, os.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		t.Fatalf("a small allocation below the RAM budget failed: %v", err)
	}
	page[0] = 42
	if err := unix.Munmap(page); err != nil {
		t.Fatal(err)
	}
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_AS, &limit); err != nil {
		t.Fatal(err)
	}
	// A mapping as large as the entire limit must still fail: existing
	// reservations consume part of that space, and the ceiling remains finite.
	oversized, err := unix.Mmap(-1, 0, int(limit.Cur), unix.PROT_NONE, unix.MAP_PRIVATE|unix.MAP_ANON|unix.MAP_NORESERVE)
	if err == nil {
		_ = unix.Munmap(oversized)
		t.Fatal("address-space ceiling was not enforced")
	}
	if !errors.Is(err, unix.ENOMEM) {
		t.Fatalf("oversized mapping: %v", err)
	}
}
