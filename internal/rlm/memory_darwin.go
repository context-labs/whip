//go:build darwin

package rlm

import (
	"context"
	"errors"
	"math"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func applyMemoryLimit(bytes uint64) error {
	if bytes > math.MaxInt64 {
		return errors.New("RLM memory limit is too large")
	}
	debug.SetMemoryLimit(int64(bytes))
	limit := &unix.Rlimit{Cur: bytes, Max: bytes}
	if err := unix.Setrlimit(unix.RLIMIT_RSS, limit); err != nil && !errors.Is(err, unix.EINVAL) {
		return err
	}
	return nil
}

func residentBytes(pid int) (uint64, error) {
	command := exec.CommandContext(context.Background(), "/bin/ps", "-o", "rss=", "-p", strconv.Itoa(pid))
	command.Env = []string{}
	output, err := command.Output()
	if err != nil {
		return 0, err
	}
	kib, err := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
	return kib << 10, err
}
