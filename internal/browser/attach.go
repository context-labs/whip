package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// parseDevToolsActivePort reads the explicitly selected profile's two-line port file.
func parseDevToolsActivePort(data []byte) (port int, wsPath string, err error) {
	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		return 0, "", fmt.Errorf("DevToolsActivePort: want 2 lines, got %d", len(lines))
	}
	port, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, "", fmt.Errorf("DevToolsActivePort port: %w", err)
	}
	wsPath = strings.TrimSpace(lines[1])
	if wsPath == "" {
		return 0, "", errors.New("DevToolsActivePort: empty ws path")
	}
	return port, wsPath, nil
}

// browserRunningForProfile is a passive lock check; native launch refuses
// a live holder instead of closing or quarantining another browser.
func browserRunningForProfile(base string) bool {
	if runtime.GOOS == "windows" {
		return true // Chromium on Windows uses a named mutex; assume running.
	}
	target, err := os.Readlink(filepath.Join(base, "SingletonLock"))
	if err != nil {
		return false
	}
	pidStr := target[strings.LastIndex(target, "-")+1:]
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 = existence probe (os.kill(pid, 0) in the Python).
	return proc.Signal(os.Signal(sigzero())) == nil
}
