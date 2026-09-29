package bashrun

import (
	"fmt"
	"os"
	"path/filepath"
)

// Spill writes the full (untruncated) command output to a temp file so the
// model can read/grep the rest when the tool result had to be truncated
// (pi's bash tool does the same). Returns the file path, or "" on failure —
// a spill failure must never break a tool result.
//
// Files live in $TMPDIR/whip-bash-<pid>-* and are left for the OS to reap.
func Spill(output string) string {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("whip-bash-%d", os.Getpid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	f, err := os.CreateTemp(dir, "*.log")
	if err != nil {
		return ""
	}
	if _, err := f.WriteString(output); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name()) // don't leave a partial file claiming to be full
		return ""
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return ""
	}
	return f.Name()
}
