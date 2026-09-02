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

func applyMemoryLimit(bytes uint64) error {
	if bytes > math.MaxInt64 {
		return errors.New("RLM memory limit is too large")
	}
	debug.SetMemoryLimit(int64(bytes))
	addressSpace := max(bytes*4, uint64(1<<30))
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
	return pages * uint64(os.Getpagesize()), err
}
