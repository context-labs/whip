package terminal

import (
	"errors"
	"slices"
	"sort"
)

var ErrCursor = errors.New("terminal cursor or page limit is invalid")

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
	if cursor < 0 || cursor > t.ring.end || limit < 1 || limit > ChunkBytes {
		return Page{}, ErrCursor
	}
	from := max(cursor, t.ring.start)
	next := from + min(t.ring.end-from, int64(limit))
	return Page{Status: t.statusLocked(), From: from, Next: next, End: t.ring.end, Truncated: cursor < from, Data: slices.Clone(t.ring.buf[from-t.ring.start : next-t.ring.start])}, nil
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
