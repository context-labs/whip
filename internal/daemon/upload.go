package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"sync"

	"github.com/context-labs/whip/internal/session"
)

type uploadKey struct{ clientID, uploadID string }

type uploadState struct {
	begin    UploadBeginParams
	file     *os.File
	hash     hash.Hash
	received int64
}

type uploadManager struct {
	store *session.Store
	dir   string
	mu    sync.Mutex
	live  map[uploadKey]*uploadState
}

func newUploadManager(store *session.Store, dir string) *uploadManager {
	return &uploadManager{store: store, dir: dir, live: make(map[uploadKey]*uploadState)}
}

func (m *uploadManager) begin(clientID string, begin UploadBeginParams) error {
	if clientID == "" || begin.UploadID == "" || begin.RootID == "" || begin.Size < 0 || begin.Size > MaxUploadSize {
		return errors.New("upload requires client, upload ID, root, and bounded size")
	}
	if !validSHA256(begin.ExpectedDigest) {
		return errors.New("upload requires a lowercase SHA-256 digest")
	}
	key := uploadKey{clientID, begin.UploadID}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.live[key]; existing != nil {
		if existing.begin != begin {
			return errors.New("upload ID was reused with different metadata")
		}
		return nil
	}
	file, err := os.CreateTemp(m.dir, ".whip-upload-*")
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name()) //nolint:gosec // CreateTemp chose this path inside the daemon-owned upload directory.
		return err
	}
	m.live[key] = &uploadState{begin: begin, file: file, hash: sha256.New()}
	return nil
}

func (m *uploadManager) chunk(clientID string, chunk UploadChunkParams) error {
	if len(chunk.Data) > MaxSnapshotChunk {
		return errors.New("upload chunk exceeds 256 KiB")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.live[uploadKey{clientID, chunk.UploadID}]
	if state == nil {
		return errors.New("upload is not active")
	}
	if chunk.Offset != state.received || state.received+int64(len(chunk.Data)) > state.begin.Size {
		return errors.New("upload chunk is out of sequence")
	}
	if _, err := state.file.Write(chunk.Data); err != nil {
		return err
	}
	if _, err := state.hash.Write(chunk.Data); err != nil {
		return err
	}
	state.received += int64(len(chunk.Data))
	return nil
}

func (m *uploadManager) finish(ctx context.Context, clientID, uploadID string) (ContentHandle, error) {
	key := uploadKey{clientID, uploadID}
	m.mu.Lock()
	state := m.live[key]
	delete(m.live, key)
	m.mu.Unlock()
	if state == nil {
		return ContentHandle{}, errors.New("upload is not active")
	}
	defer func() {
		_ = os.Remove(state.file.Name()) //nolint:gosec // The live state contains only the file returned by CreateTemp.
	}()
	if state.received != state.begin.Size {
		_ = state.file.Close()
		return ContentHandle{}, errors.New("upload is incomplete")
	}
	if got := hex.EncodeToString(state.hash.Sum(nil)); got != state.begin.ExpectedDigest {
		_ = state.file.Close()
		return ContentHandle{}, fmt.Errorf("upload digest mismatch: got %s", got)
	}
	if err := state.file.Sync(); err != nil {
		_ = state.file.Close()
		return ContentHandle{}, err
	}
	if err := state.file.Close(); err != nil {
		return ContentHandle{}, err
	}
	data, err := os.ReadFile(state.file.Name())
	if err != nil {
		return ContentHandle{}, err
	}
	value, err := m.store.StoreContent(ctx, session.ContentGrant{RootID: state.begin.RootID, Scope: session.ContentGrantRoot}, session.RuntimePayload{
		Data: data, MediaType: state.begin.MediaType, Source: state.begin.Source,
	})
	if err != nil {
		return ContentHandle{}, err
	}
	return ContentHandle{
		ReferenceID: value.ReferenceID, Digest: value.Digest, Size: value.Size,
		MediaType: value.MediaType, Source: value.Source,
	}, nil
}

func (m *uploadManager) abortClient(clientID string) {
	m.mu.Lock()
	var aborted []*uploadState
	for key, state := range m.live {
		if clientID == "" || key.clientID == clientID {
			delete(m.live, key)
			aborted = append(aborted, state)
		}
	}
	m.mu.Unlock()
	for _, state := range aborted {
		_ = state.file.Close()
		_ = os.Remove(state.file.Name())
	}
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}
