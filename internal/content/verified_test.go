package content

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestVerifiedReadIsCompleteBoundedAndDoesNotFollowSpecialFiles(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("complete body"), 10000)
	body, err := store.Put(data)
	if err != nil {
		t.Fatal(err)
	}
	read, err := store.ReadVerified(body, 4<<20)
	if err != nil || !bytes.Equal(read, data) {
		t.Fatal("verified read truncated the body", err)
	}
	if _, err := store.ReadVerified(body, 64<<10); err == nil {
		t.Fatal("read exceeded caller's budget")
	}
	if err := os.WriteFile(store.path(body.Digest), bytes.Repeat([]byte("x"), len(data)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadVerified(body, 4<<20); err == nil {
		t.Fatal("corrupt bytes passed digest check")
	}
	if err := os.Remove(store.path(body.Digest)); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, store.path(body.Digest)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadVerified(body, 4<<20); err == nil {
		t.Fatal("content read followed a symlink")
	}
	if err := os.Remove(store.path(body.Digest)); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(store.path(body.Digest), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadVerified(body, 4<<20); err == nil {
		t.Fatal("content read accepted a FIFO")
	}
}
