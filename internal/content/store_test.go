package content

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutDeduplicatesAndReadsBoundedSlices(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("0123456789abcdef"), 8*1024)

	a, err := st.Put(body)
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.Put(body)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("equal content should reuse one body: %+v != %+v", a, b)
	}
	bodies, err := st.Bodies()
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 1 || bodies[0] != a {
		t.Fatalf("bodies = %+v, want only %+v", bodies, a)
	}

	tests := []struct {
		name    string
		offset  int64
		length  int
		want    []byte
		wantErr bool
	}{
		{name: "valid", offset: 3, length: 7, want: body[3:10]},
		{name: "empty", offset: 4, length: 0, want: []byte{}},
		{name: "end", offset: int64(len(body)), length: 12, want: []byte{}},
		{name: "capped", offset: 0, length: MaxReadSize + 1, want: body[:MaxReadSize]},
		{name: "past end", offset: int64(len(body)) + 1, length: 1, wantErr: true},
		{name: "negative", offset: -1, length: 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := st.Read(a.Digest, tt.offset, tt.length)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Read error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !bytes.Equal(got, tt.want) {
				t.Fatalf("Read returned %d bytes, want %d", len(got), len(tt.want))
			}
		})
	}
}

func TestPublishFailuresLeaveOnlyDiagnosableBodies(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(strings.Repeat("durable", 2048))
	stop := errors.New("simulated crash")

	if _, err := st.put(body, func(stage string) error {
		if stage == "before_file_sync" {
			return stop
		}
		return nil
	}); !errors.Is(err, stop) {
		t.Fatalf("before-sync failure = %v", err)
	}
	if bodies, err := st.Bodies(); err != nil || len(bodies) != 0 {
		t.Fatalf("partial write should publish no body: %+v, %v", bodies, err)
	}

	published, err := st.put(body, func(stage string) error {
		if stage == "after_rename" {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("after-rename failure = %v", err)
	}
	orphans, err := st.Orphans(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[0].Digest != published.Digest {
		t.Fatalf("renamed body should be diagnosed as orphan: %+v", orphans)
	}
	if _, err := os.Stat(st.path(published.Digest)); err != nil {
		t.Fatalf("diagnosis must not delete the orphan: %v", err)
	}
	if got, err := st.Put(body); err != nil || got != published {
		t.Fatalf("retry should adopt the immutable body: %+v, %v", got, err)
	}
	if orphans, err = st.Orphans(map[string]struct{}{published.Digest: {}}); err != nil || len(orphans) != 0 {
		t.Fatalf("referenced body reported orphan: %+v, %v", orphans, err)
	}
}

func TestStoreRejectsMalformedFilesystemState(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "home")
	if err := os.WriteFile(homeFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(homeFile); err == nil {
		t.Fatal("New accepted a file as its home directory")
	}

	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.dir, "not-a-digest"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(st.dir, strings.Repeat("0", 64)), 0o700); err != nil {
		t.Fatal(err)
	}
	if bodies, err := st.Bodies(); err != nil || len(bodies) != 0 {
		t.Fatalf("malformed directory entries became bodies: %+v, %v", bodies, err)
	}

	payload := []byte("immutable")
	body, err := st.Put(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.path(body.Digest), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Put(payload); err == nil || !strings.Contains(err.Error(), "does not match its digest") {
		t.Fatalf("Put accepted a corrupt existing body: %v", err)
	}
}

func TestStoreValidationAndFilesystemErrors(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Read("bad", 0, 1); err == nil {
		t.Fatal("Read accepted an invalid digest")
	}
	if _, err := st.Read(strings.Repeat("0", 64), 0, 1); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing body error = %v", err)
	}

	dirDigest := strings.Repeat("a", 64)
	if err := os.Mkdir(st.path(dirDigest), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := st.verify(Body{Digest: dirDigest}); err == nil {
		t.Fatal("verify accepted a directory as a body")
	}

	payload := []byte("rename collision")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	if _, err := st.put(payload, func(stage string) error {
		if stage == "before_file_sync" {
			return os.Mkdir(st.path(digest), 0o700)
		}
		return nil
	}); err == nil {
		t.Fatal("Put accepted a directory at the final body path")
	}

	missing := filepath.Join(t.TempDir(), "missing")
	if err := syncDir(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("syncDir missing path error = %v", err)
	}
	broken, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(broken.dir); err != nil {
		t.Fatal(err)
	}
	if _, err := broken.Bodies(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Bodies missing directory error = %v", err)
	}
	if _, err := broken.Orphans(nil); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Orphans missing directory error = %v", err)
	}
}
