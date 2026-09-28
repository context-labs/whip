package content

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

func TestVerifiedRangeChecksBytesOutsideReturnedWindow(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw := bytes.Repeat([]byte("🌏"), MaxReadSize)
	body, err := s.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		offset int64
		length int
	}{{0, 1}, {1, MaxReadSize}, {body.Size - 2, 10}, {body.Size, 1}} {
		got, err := s.ReadVerifiedRange(body, test.offset, test.length)
		end := min(test.offset+int64(test.length), body.Size)
		if err != nil || !bytes.Equal(got, raw[test.offset:end]) {
			t.Fatalf("range %+v: %x %v", test, got, err)
		}
	}
	for _, position := range []int{0, len(raw) - 1} {
		changed := bytes.Clone(raw)
		changed[position] = '!'
		if err := os.WriteFile(s.path(body.Digest), changed, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := s.ReadVerifiedRange(body, 100, 10)
		if !errors.Is(err, errContentMismatch) || got != nil {
			t.Fatalf("corrupt range returned data: %x %v", got, err)
		}
	}
}
