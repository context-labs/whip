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
