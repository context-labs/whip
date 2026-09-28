// Package content stores immutable runtime values outside SQLite.
package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const MaxReadSize = 64 << 10

var errContentMismatch = errors.New("content body does not match its digest")

type Body struct {
	Digest string
	Size   int64
}

type Store struct{ dir string }

func New(home string) (*Store, error) {
	dir := filepath.Join(home, "artifacts", "sha256")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // this path is a directory, not a regular file
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Put(data []byte) (Body, error) { return s.put(data, nil) }

func (s *Store) put(data []byte, hook func(string) error) (Body, error) {
	sum := sha256.Sum256(data)
	body := Body{Digest: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	if ok, err := s.verify(body); ok {
		if err == nil {
			err = syncDir(s.dir)
		}
		return body, err
	} else if err != nil && !errors.Is(err, errContentMismatch) {
		return body, err
	}

	f, err := os.CreateTemp(s.dir, ".publish-*")
	if err != nil {
		return Body{}, err
	}
	tmp := f.Name()
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
		_ = os.Remove(tmp)
	}()
	if err := f.Chmod(0o600); err != nil {
		return Body{}, err
	}
	if _, err := f.Write(data); err != nil {
		return Body{}, err
	}
	if hook != nil {
		if err := hook("before_file_sync"); err != nil {
			return body, err
		}
	}
	if err := f.Sync(); err != nil {
		return Body{}, err
	}
	if err := f.Close(); err != nil {
		return Body{}, err
	}
	closed = true

	final := s.path(body.Digest)
	if err := os.Rename(tmp, final); err != nil {
		if ok, verifyErr := s.verify(body); ok && verifyErr == nil {
			return body, syncDir(s.dir)
		}
		return Body{}, err
	}
	if hook != nil {
		if err := hook("after_rename"); err != nil {
			return body, err
		}
	}
	if err := syncDir(s.dir); err != nil {
		return body, err
	}
	return body, nil
}

func (s *Store) Read(digest string, offset int64, length int) ([]byte, error) {
	if !validDigest(digest) {
		return nil, errors.New("invalid content digest")
	}
	if offset < 0 || length < 0 {
		return nil, errors.New("invalid content range")
	}
	f, err := os.Open(s.path(digest))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, &os.PathError{Op: "read", Path: f.Name(), Err: syscall.EISDIR}
	}
	if offset > info.Size() {
		return nil, fmt.Errorf("content offset %d exceeds size %d", offset, info.Size())
	}
	if length > MaxReadSize {
		length = MaxReadSize
	}
	if remaining := info.Size() - offset; int64(length) > remaining {
		length = int(remaining)
	}
	buf := make([]byte, length)
	if length == 0 {
		return buf, nil
	}
	n, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

func (s *Store) Bodies() ([]Body, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	bodies := make([]Body, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !validDigest(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			bodies = append(bodies, Body{Digest: entry.Name(), Size: info.Size()})
		}
	}
	sort.Slice(bodies, func(i, j int) bool { return bodies[i].Digest < bodies[j].Digest })
	return bodies, nil
}

func (s *Store) Orphans(referenced map[string]struct{}) ([]Body, error) {
	bodies, err := s.Bodies()
	if err != nil {
		return nil, err
	}
	orphans := bodies[:0]
	for _, body := range bodies {
		if _, ok := referenced[body.Digest]; !ok {
			orphans = append(orphans, body)
		}
	}
	return orphans, nil
}

func (s *Store) verify(want Body) (bool, error) {
	f, err := openRegular(s.path(want.Digest))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() != want.Size {
		return false, errContentMismatch
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, want.Size+1))
	if err != nil {
		return false, err
	}
	if n != want.Size || hex.EncodeToString(h.Sum(nil)) != want.Digest {
		return false, fmt.Errorf("%w: %s", errContentMismatch, want.Digest)
	}
	return true, nil
}

func openRegular(path string) (*os.File, error) {
	// Nonblocking prevents a replaced FIFO from hanging before its type is
	// checked; NOFOLLOW prevents a digest name from becoming a file capability.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // Store-owned digest path, never a caller-supplied filesystem path.
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("content body is not a regular file")
	}
	return f, nil
}

// ReadVerified reads a complete bounded body and checks its digest. The caller
// must resolve an authorized reference before requesting these immutable bytes.
func (s *Store) ReadVerified(body Body, maxBytes int64) ([]byte, error) {
	if !validDigest(body.Digest) || body.Size < 0 || body.Size > maxBytes || maxBytes > 64<<20 {
		return nil, errors.New("invalid or oversized content body")
	}
	f, err := openRegular(s.path(body.Digest))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() != body.Size {
		return nil, errContentMismatch
	}
	data, err := io.ReadAll(io.LimitReader(f, body.Size+1))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	if int64(len(data)) != body.Size || hex.EncodeToString(digest[:]) != body.Digest {
		return nil, errContentMismatch
	}
	return data, nil
}

// Collect removes unreferenced bodies in bounded directory batches. Only call
// before serving, under exclusive runtime ownership; uploads publish before SQL.
func (s *Store) Collect(ctx context.Context, referenced func(context.Context, string) (bool, error)) error {
	dir, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := dir.ReadDir(100)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".publish-") {
				info, err := entry.Info()
				if err != nil {
					return err
				}
				// A killed publisher cannot run its deferred temporary-file cleanup.
				// DirEntry.Info describes the link itself, so special files stay untouched.
				if info.Mode().IsRegular() {
					if err := os.Remove(filepath.Join(s.dir, entry.Name())); err != nil {
						return err
					}
				}
				continue
			}
			if !validDigest(entry.Name()) {
				continue
			}
			keep, err := referenced(ctx, entry.Name())
			if err != nil {
				return err
			}
			if keep {
				continue
			}
			// Remove the directory entry itself, never follow a symlink or recurse.
			if err := os.Remove(s.path(entry.Name())); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return syncDir(s.dir)
		}
	}
}

func (s *Store) path(digest string) string { return filepath.Join(s.dir, digest) }

func validDigest(digest string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(digest)
	return err == nil && hex.EncodeToString(decoded) == digest
}

func syncDir(path string) error {
	dir, err := os.Open(path) //nolint:gosec // callers pass only store-owned directories
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
