package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// whip.log is an append-only event log for harness operations that touch
// on-disk state: config loads/saves (with a before/after fingerprint),
// catalog refreshes, session store activity. It exists so that when state
// gets corrupted there is a record of which process did what, when, and
// with what result — "did the harness misbehave?" becomes answerable.
//
// Logging never fails the caller: every write is best-effort.

const (
	logFileName = "whip.log"
	// logMaxBytes caps the file; past it the log is rotated to whip.log.1
	// (single generation — enough history to debug, never grows unbounded).
	logMaxBytes = 1 << 20 // 1 MiB
)

var logMu sync.Mutex

// LogEvent appends one timestamped line to ~/.whipcode/whip.log. op is a short
// verb ("config.save", "config.load", "catalog.fetch", ...); detail is
// free-form context. Best-effort: errors are swallowed by design.
func LogEvent(op, detail string) {
	logMu.Lock()
	defer logMu.Unlock()
	dir, err := Dir()
	if err != nil {
		return
	}
	p := filepath.Join(dir, logFileName)
	if st, err := os.Stat(p); err == nil && st.Size() > logMaxBytes {
		_ = os.Rename(p, p+".1") // single-generation rotation
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(f, "%s %-16s pid=%d %s\n", time.Now().UTC().Format(time.RFC3339), op, os.Getpid(), detail)
	_ = f.Close() // best-effort log; close error acknowledged, not actionable
}

// logf is the printf-style convenience form.
func logf(op, format string, args ...any) {
	LogEvent(op, fmt.Sprintf(format, args...))
}
