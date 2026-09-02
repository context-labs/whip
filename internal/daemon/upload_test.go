package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadsAcceptEmptyAndMultipleChunks(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	manager := newUploadManager(store, t.TempDir())
	for _, test := range []struct {
		id   string
		data []byte
	}{
		{id: "empty"},
		{id: "multi", data: []byte(strings.Repeat("content", 80_000))},
	} {
		digest := sha256.Sum256(test.data)
		begin := UploadBeginParams{
			UploadID: test.id, RootID: rootID, ExpectedDigest: hex.EncodeToString(digest[:]),
			Size: int64(len(test.data)), MediaType: "application/octet-stream", Source: "test",
		}
		if err := manager.begin("client", begin); err != nil {
			t.Fatal(err)
		}
		for offset := 0; offset < len(test.data); offset += MaxSnapshotChunk {
			end := min(offset+MaxSnapshotChunk, len(test.data))
			if err := manager.chunk("client", UploadChunkParams{UploadID: test.id, Offset: int64(offset), Data: test.data[offset:end]}); err != nil {
				t.Fatal(err)
			}
		}
		handle, err := manager.finish(context.Background(), "client", test.id)
		if err != nil || handle.Digest != begin.ExpectedDigest || handle.Size != begin.Size || handle.ReferenceID == "" {
			t.Fatalf("finish = %+v, %v", handle, err)
		}
		var got []byte
		for offset := int64(0); offset < handle.Size; {
			chunk, _, err := store.ReadContent(context.Background(), handle.ReferenceID, rootID, "", offset, 64<<10)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, chunk...)
			offset += int64(len(chunk))
		}
		if string(got) != string(test.data) {
			t.Fatalf("uploaded %s content changed", test.id)
		}
	}
}

func TestUploadRejectsMismatchAndRemovesInterruptedState(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	manager := newUploadManager(store, t.TempDir())
	data := []byte("partial")
	wrong := sha256.Sum256([]byte("different"))
	begin := UploadBeginParams{UploadID: "bad", RootID: rootID, ExpectedDigest: hex.EncodeToString(wrong[:]), Size: int64(len(data))}
	if err := manager.begin("client", begin); err != nil {
		t.Fatal(err)
	}
	if err := manager.chunk("client", UploadChunkParams{UploadID: "bad", Data: data}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.finish(context.Background(), "client", "bad"); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("mismatch error = %v", err)
	}

	digest := sha256.Sum256(data)
	begin = UploadBeginParams{UploadID: "interrupted", RootID: rootID, ExpectedDigest: hex.EncodeToString(digest[:]), Size: int64(len(data))}
	if err := manager.begin("client", begin); err != nil {
		t.Fatal(err)
	}
	path := manager.live[uploadKey{"client", "interrupted"}].file.Name()
	manager.abortClient("client")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("interrupted temporary file remains: %v", err)
	}
	if _, err := manager.finish(context.Background(), "client", "interrupted"); err == nil {
		t.Fatal("interrupted upload remained active")
	}
}

func TestUploadAdmissionAndChunkBounds(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	manager := newUploadManager(store, t.TempDir())
	if err := manager.begin("", UploadBeginParams{}); err == nil {
		t.Fatal("empty upload metadata was accepted")
	}
	if err := manager.begin("client", UploadBeginParams{UploadID: "bad", RootID: rootID, ExpectedDigest: strings.Repeat("A", 64), Size: 1}); err == nil {
		t.Fatal("non-canonical digest was accepted")
	}
	data := []byte("data")
	digest := sha256.Sum256(data)
	begin := UploadBeginParams{UploadID: "bounded", RootID: rootID, ExpectedDigest: hex.EncodeToString(digest[:]), Size: int64(len(data))}
	if err := manager.begin("client", begin); err != nil {
		t.Fatal(err)
	}
	if err := manager.begin("client", begin); err != nil {
		t.Fatalf("matching begin retry = %v", err)
	}
	changed := begin
	changed.Size++
	if err := manager.begin("client", changed); err == nil {
		t.Fatal("upload ID metadata conflict was accepted")
	}
	if err := manager.chunk("missing", UploadChunkParams{UploadID: "bounded"}); err == nil {
		t.Fatal("missing upload accepted a chunk")
	}
	if err := manager.chunk("client", UploadChunkParams{UploadID: "bounded", Offset: 1, Data: data}); err == nil {
		t.Fatal("out-of-sequence upload chunk was accepted")
	}
	if err := manager.chunk("client", UploadChunkParams{UploadID: "bounded", Data: make([]byte, MaxSnapshotChunk+1)}); err == nil {
		t.Fatal("oversized upload chunk was accepted")
	}
	if _, err := manager.finish(context.Background(), "client", "bounded"); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete upload = %v", err)
	}
	if validSHA256("short") {
		t.Fatal("short digest was accepted")
	}
	missingDir := filepath.Join(t.TempDir(), "missing", "uploads")
	if err := newUploadManager(store, missingDir).begin("client", begin); err == nil {
		t.Fatal("upload started in a missing runtime directory")
	}
}
