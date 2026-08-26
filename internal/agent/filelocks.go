package agent

import (
	"encoding/json"
	"path/filepath"
	"sync"
)

// fileLocks serializes mutations to the same canonical path across parallel
// tool calls. Each path owns a 1-capacity channel used as a semaphore: a tool
// acquires by sending (blocks until free) and releases by receiving. A channel
// per path is the idiomatic Go form of pi's per-path promise-chain queue — no
// explicit unlock bookkeeping.
//
// Only write/edit take a per-path lock; reads don't. Bash takes the global
// lock because a command can touch anything.
//
// The two gates are one lock, not two: a per-path channel alone would only
// serialize mutations against each other, leaving bash free to run beside a
// write it could clobber. So a path mutation holds gate.RLock for as long as it
// holds its path channel, and bash holds gate.Lock, which is the exclusion the
// comment above claims. Mutations still run in parallel with one another,
// because readers do.
type fileLocks struct {
	mu    sync.Mutex
	locks map[string]chan struct{}
	gate  sync.RWMutex // bash write-locks it; every path mutation read-locks it
}

func newFileLocks() *fileLocks {
	return &fileLocks{locks: map[string]chan struct{}{}}
}

// acquirePath blocks until the lock for path is held, returning a release func.
// The 1-capacity channel means the first acquirer succeeds immediately and
// later acquirers block on send until the holder receives.
// The gate is taken before the path channel and released after it. Deadlock is
// not the reason for that order: bash only ever takes the gate and never holds
// a path channel, so no wait cycle exists either way. One fixed order is just
// easier to reason about.
func (f *fileLocks) acquirePath(path string) func() {
	key := canonicalPathKey(path)
	f.mu.Lock()
	ch, ok := f.locks[key]
	if !ok {
		ch = make(chan struct{}, 1)
		f.locks[key] = ch
	}
	f.mu.Unlock()
	f.gate.RLock()
	ch <- struct{}{} // acquire (blocks while held)
	return func() {
		<-ch // release
		f.gate.RUnlock()
	}
}

// acquireGlobal serializes a tool call against every other mutation — used by
// bash, whose side effects can't be attributed to one path.
func (f *fileLocks) acquireGlobal() func() {
	f.gate.Lock()
	return func() { f.gate.Unlock() }
}

// canonicalPathKey normalizes a path so two spellings of the same file share
// one lock (pi resolves through the FS; we settle for absolute + clean).
func canonicalPathKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

// toolMutationPath extracts the path a write/edit tool call will mutate. The
// second return is false for tools whose side effects aren't path-scoped
// (bash), which must take the global lock.
func toolMutationPath(toolName, args string) (string, bool) {
	switch toolName {
	case "write", "edit":
		var a struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(args), &a); err == nil && a.Path != "" {
			return a.Path, true
		}
	}
	return "", false
}
