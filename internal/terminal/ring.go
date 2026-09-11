package terminal

import "slices"

// ring retains the newest capacity bytes of output under absolute cursors, so
// a client that reattaches can name the last byte it saw and receive exactly
// what it missed, or the oldest retained byte when it fell further behind.
type ring struct {
	buf   []byte
	start int64 // absolute cursor of buf[0]
	end   int64 // absolute cursor one past the newest byte
}

func newRing(capacity int) *ring { return &ring{buf: make([]byte, 0, capacity)} }

// append records data and forgets the oldest bytes beyond capacity.
func (r *ring) append(data []byte) {
	capacity := cap(r.buf)
	if len(data) >= capacity {
		r.buf = append(r.buf[:0], data[len(data)-capacity:]...)
		r.end += int64(len(data))
		r.start = r.end - int64(capacity)
		return
	}
	if overflow := len(r.buf) + len(data) - capacity; overflow > 0 {
		kept := copy(r.buf, r.buf[overflow:])
		r.buf = r.buf[:kept]
		r.start += int64(overflow)
	}
	r.buf = append(r.buf, data...)
	r.end += int64(len(data))
}

// read copies the retained bytes from cursor onward. A negative cursor asks
// for the tail only; a cursor older than the ring is clamped to its start.
// The second result is the absolute cursor of the first returned byte.
func (r *ring) read(from int64) ([]byte, int64) {
	switch {
	case from < 0, from > r.end:
		from = r.end
	case from < r.start:
		from = r.start
	}
	return slices.Clone(r.buf[from-r.start:]), from
}
