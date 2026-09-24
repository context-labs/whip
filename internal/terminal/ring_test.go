package terminal

import (
	"bytes"
	"testing"
)

func TestRingKeepsNewestBytesUnderAbsoluteCursors(t *testing.T) {
	r := newRing(8)
	r.append([]byte("abcde"))
	if data, from := r.read(0); string(data) != "abcde" || from != 0 {
		t.Fatalf("read(0) = %q, %d", data, from)
	}
	r.append([]byte("fghij")) // ten bytes written, eight retained
	if r.start != 2 || r.end != 10 {
		t.Fatalf("cursors = %d..%d, want 2..10", r.start, r.end)
	}
	if data, from := r.read(0); string(data) != "cdefghij" || from != 2 {
		t.Fatalf("read(0) after overflow = %q, %d", data, from)
	}
	if data, from := r.read(7); string(data) != "hij" || from != 7 {
		t.Fatalf("read(7) = %q, %d", data, from)
	}
	if data, from := r.read(-1); len(data) != 0 || from != 10 {
		t.Fatalf("tail read = %q, %d", data, from)
	}
	if data, from := r.read(99); len(data) != 0 || from != 10 {
		t.Fatalf("future read = %q, %d", data, from)
	}
}

func TestRingAcceptsWritesLargerThanCapacity(t *testing.T) {
	r := newRing(4)
	r.append(bytes.Repeat([]byte("x"), 3))
	r.append([]byte("0123456789"))
	if data, from := r.read(0); string(data) != "6789" || from != 9 || r.end != 13 {
		t.Fatalf("read = %q, %d, end %d", data, from, r.end)
	}
	// Returned bytes are copies: later appends must not mutate them.
	data, _ := r.read(0)
	r.append([]byte("zz"))
	if string(data) != "6789" {
		t.Fatalf("read result aliased the ring: %q", data)
	}
}
