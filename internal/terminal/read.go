package terminal

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"
)

var ErrCursor = errors.New("terminal cursor or page limit is invalid")
var ErrReadWait = errors.New("terminal read wait must be within 0..5000 milliseconds")

// MaxReadWait leaves time for a response within the host RPC deadline.
const MaxReadWait = 5 * time.Second

type Page struct {
	Status          Status
	From, Next, End int64
	Truncated       bool
	Data            []byte
}

// Read captures one bounded page and its status under the output ring lock. A
// final exited status means the output end can no longer advance.
func (t *Terminal) Read(cursor int64, limit int) (Page, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.readLocked(cursor, limit)
}

func (t *Terminal) readLocked(cursor int64, limit int) (Page, error) {
	if t.retired {
		return Page{}, ErrNotFound
	}
	if cursor < 0 || cursor > t.ring.end || limit < 1 || limit > ChunkBytes {
		return Page{}, ErrCursor
	}
	from := max(cursor, t.ring.start)
	next := from + min(t.ring.end-from, int64(limit))
	return Page{Status: t.statusLocked(), From: from, Next: next, End: t.ring.end, Truncated: cursor < from, Data: slices.Clone(t.ring.buf[from-t.ring.start : next-t.ring.start])}, nil
}

// ReadWait returns buffered output immediately, or waits for the next output or
// lifecycle change. Capturing the wakeup under the ring lock prevents lost data
// between inspection and waiting. Cancelling a read never stops the shell.
func (t *Terminal) ReadWait(ctx context.Context, cursor int64, limit int, wait time.Duration) (Page, error) {
	if wait < 0 || wait > MaxReadWait {
		return Page{}, ErrReadWait
	}
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	t.mu.Lock()
	page, err := t.readLocked(cursor, limit)
	changed := t.changed
	t.mu.Unlock()
	if err != nil || wait == 0 || page.Next != cursor || page.Status.Exited {
		return page, err
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Page{}, ctx.Err()
	case <-timer.C:
	case <-changed:
	}
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	return t.Read(cursor, limit)
}

func (t *Terminal) notifyLocked() {
	close(t.changed)
	t.changed = make(chan struct{})
}

// List reports at most MaxTerminals current resources, without opening a shell,
// attaching a receiver, or reviving resources from a prior process generation.
func (m *Manager) List() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Status, 0, len(m.terminals))
	for _, t := range m.terminals {
		result = append(result, t.Status())
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
