//go:build linux

package rlm

import (
	"errors"
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Go reserves virtual address ranges well beyond its resident heap. Keep a
// fixed VM growth allowance above its startup reservations while the parent
// enforces the advertised MemoryBytes limit against RSS every 10ms.
const workerVirtualMemoryOverhead = uint64(4 << 30)

func applyMemoryLimit(bytes uint64) error {
	if bytes > math.MaxInt64-workerVirtualMemoryOverhead {
		return errors.New("RLM memory limit is too large")
	}
	reserved, err := statmBytes(os.Getpid(), 0)
	if err != nil {
		return err
	}
	headroom := bytes + workerVirtualMemoryOverhead
	if reserved > math.MaxInt64-headroom {
		return errors.New("RLM address-space limit is too large")
	}
	addressSpace := reserved + headroom
	debug.SetMemoryLimit(int64(bytes))
	limit := &unix.Rlimit{Cur: addressSpace, Max: addressSpace}
	return unix.Setrlimit(unix.RLIMIT_AS, limit)
}

func residentBytes(pid int) (uint64, error) {
	return statmBytes(pid, 1)
}

// statm starts with virtual and resident page counts, in that order.
func statmBytes(pid, field int) (uint64, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) <= field {
		return 0, syscall.EINVAL
	}
	pages, err := strconv.ParseUint(fields[field], 10, 64)
	pageSize := os.Getpagesize()
	if pageSize < 1 || pages > math.MaxUint64/uint64(pageSize) {
		return 0, syscall.EINVAL
	}
	return pages * uint64(pageSize), err
}
