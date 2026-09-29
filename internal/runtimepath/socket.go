// Package runtimepath defines the shared native Unix socket location.
package runtimepath

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// Socket is read-only. The execution owner creates and validates its private
// socket directory under the durable runtime lock before any listener starts.
func Socket(directory string) string {
	path := filepath.Join(directory, "runtime.sock")
	if len(path) <= 100 {
		return path
	}
	// /tmp is deliberately independent of the caller's TMPDIR: startup and
	// discovery must agree, and a long user temp directory cannot solve this limit.
	digest := sha256.Sum256([]byte(directory))
	return fmt.Sprintf("/tmp/whip-%d-%x/runtime.sock", os.Getuid(), digest)
}
