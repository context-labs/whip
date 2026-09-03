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
// fixed VM allowance for the runtime while the parent process enforces the
// advertised MemoryBytes limit against RSS every 10ms.
const workerVirtualMemoryOverhead = uint64(4 << 30)

func applyMemoryLimit(bytes uint64) error {
	if bytes > math.MaxInt64 {
		return errors.New("RLM memory limit is too large")
	}
	debug.SetMemoryLimit(int64(bytes))
	addressSpace := bytes + workerVirtualMemoryOverhead
	if addressSpace < bytes || addressSpace > math.MaxInt64 {
		return errors.New("RLM address-space limit is too large")
	}
	limit := &unix.Rlimit{Cur: addressSpace, Max: addressSpace}
	return unix.Setrlimit(unix.RLIMIT_AS, limit)
}

func residentBytes(pid int) (uint64, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, syscall.EINVAL
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	pageSize := os.Getpagesize()
	if pageSize < 1 {
		return 0, syscall.EINVAL
	}
	return pages * uint64(pageSize), err
}
