// Package clientnotes stores user-edited, client-local checkbox notes. These
// files are never host state, model instructions, or execution authority.
package clientnotes

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const MaxBytes = 256 << 10

var ErrChanged = errors.New("notes changed; list them again before marking an entry done")

type Store struct {
	root      *os.Root
	directory string
	mu        sync.Mutex
	closed    bool
	calls     sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

type (
	Scope struct {
		store      *Store
		file, Name string
	}
	Entry struct {
		Number int
		Text   string
		Done   bool
	}
	Snapshot struct {
		Path, Revision string
		Entries        []Entry
	}
)

// Open selects only the fresh client namespace under the caller's explicit home.
// It does not read, copy or update retired memory files or runtime directories.
func Open(home string) (*Store, error) {
	if home == "" {
		return nil, errors.New("explicit client home is required")
	}
	absolute, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"client-v4", "memory"} {
		if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			_ = root.Close()
			return nil, err
		}
		info, err := root.Lstat(name)
		if err != nil || !info.IsDir() {
			_ = root.Close()
			return nil, errors.New("client notes directory must be an owned directory, not a symbolic link")
		}
		next, err := root.OpenRoot(name)
		_ = root.Close()
		if err != nil {
			return nil, err
		}
		actual, err := next.Stat(".")
		if err != nil || !os.SameFile(info, actual) {
			_ = next.Close()
			return nil, ErrChanged
		}
		root = next
	}
	return &Store{root: root, directory: filepath.Join(absolute, "client-v4", "memory")}, nil
}

func (s *Store) Close() error {
	s.closeOnce.Do(func() { s.mu.Lock(); s.closed = true; s.mu.Unlock(); s.calls.Wait(); s.closeErr = s.root.Close() })
	return s.closeErr
}

func (s *Store) begin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	s.calls.Add(1)
	return nil
}

func (s *Store) Installation() Scope {
	return Scope{store: s, file: "installation.md", Name: "installation"}
}

// Session hashes both identities; neither a remote path nor an untrusted ID can
// become a client filesystem path. The same ID on two hosts has separate notes.
func (s *Store) Session(runtimeID, sessionID string) (Scope, error) {
	for _, id := range []string{runtimeID, sessionID} {
		if id == "" || len(id) > 256 || strings.ContainsRune(id, 0) || !utf8.ValidString(id) {
			return Scope{}, errors.New("bounded runtime and session identities are required")
		}
	}
	digest := sha256.Sum256([]byte(runtimeID + "\x00" + sessionID))
	return Scope{store: s, file: fmt.Sprintf("session-%x.md", digest), Name: "session"}, nil
}

func (scope Scope) Read(ctx context.Context) (Snapshot, error) {
	if scope.store == nil {
		return Snapshot{}, errors.New("no local note scope")
	}
	if err := scope.store.begin(); err != nil {
		return Snapshot{}, err
	}
	defer scope.store.calls.Done()
	raw, err := scope.read(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	return scope.snapshot(raw), nil
}

func (scope Scope) read(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := scope.store.root.Lstat(scope.file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("notes must be a regular file, not a symbolic link")
	}
	file, err := scope.store.root.Open(scope.file)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, actual) || !actual.Mode().IsRegular() {
		return nil, ErrChanged
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxBytes {
		return nil, errors.New("local notes exceed 256 KiB; shorten the file before reading or editing")
	}
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return nil, errors.New("local notes must contain UTF-8 text without NUL")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}

func revision(raw []byte) string { return fmt.Sprintf("%x", sha256.Sum256(raw)) }

func entries(raw []byte) []Entry {
	result := []Entry{}
	for line := range strings.Lines(string(raw)) {
		text := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		value, open := strings.CutPrefix(text, "- [ ] ")
		if open {
			result = append(result, Entry{Number: len(result) + 1, Text: value})
			continue
		}
		if value, done := strings.CutPrefix(text, "- [x] "); done {
			result = append(result, Entry{Number: len(result) + 1, Text: value, Done: true})
		}
	}
	return result
}

func (scope Scope) snapshot(raw []byte) Snapshot {
	return Snapshot{Path: filepath.Join(scope.store.directory, scope.file), Revision: revision(raw), Entries: entries(raw)}
}

// Forget changes exactly one checkbox after comparing the last listed bytes.
// Our writers share an advisory file lock; arbitrary external editors do not
// participate. Atomic rename preserves the rest of the markdown byte-for-byte.
func (scope Scope) Forget(parent context.Context, number int, expected string) (Snapshot, error) {
	if scope.store == nil {
		return Snapshot{}, errors.New("no local note scope")
	}
	if number < 1 || len(expected) != 64 {
		return Snapshot{}, errors.New("list local notes before selecting an entry number")
	}
	if err := scope.store.begin(); err != nil {
		return Snapshot{}, err
	}
	defer scope.store.calls.Done()
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	// Concurrent non-exclusive O_CREATE can return ENOENT on Darwin while
	// another opener creates the same name. Elect one creator; other writers
	// open its existing inode. NONBLOCK also prevents a substituted FIFO from
	// blocking before the regular-file and identity checks below.
	lock, err := scope.store.root.OpenFile(".write.lock", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrExist) {
		lock, err = scope.store.root.OpenFile(".write.lock", os.O_RDWR|syscall.O_NONBLOCK, 0)
	}
	if err != nil {
		return Snapshot{}, err
	}
	defer lock.Close()
	info, err := scope.store.root.Lstat(".write.lock")
	if err != nil || !info.Mode().IsRegular() {
		return Snapshot{}, errors.New("invalid local notes lock")
	}
	actual, err := lock.Stat()
	if err != nil {
		return Snapshot{}, err
	}
	if !os.SameFile(info, actual) {
		return Snapshot{}, ErrChanged
	}
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return Snapshot{}, err
		}
		select {
		case <-ctx.Done():
			return Snapshot{}, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	raw, err := scope.read(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if revision(raw) != expected {
		return Snapshot{}, ErrChanged
	}
	count, offset := 0, 0
	for line := range strings.Lines(string(raw)) {
		open := strings.HasPrefix(line, "- [ ] ")
		done := strings.HasPrefix(line, "- [x] ")
		if open || done {
			count++
			if count == number {
				if done {
					return Snapshot{}, fmt.Errorf("entry %d is already marked done", number)
				}
				raw[offset+3] = 'x'
				if err := scope.write(ctx, raw); err != nil {
					return Snapshot{}, err
				}
				return scope.snapshot(raw), nil
			}
		}
		offset += len(line)
	}
	return Snapshot{}, fmt.Errorf("no memory entry %d", number)
}

func (scope Scope) write(ctx context.Context, raw []byte) (err error) {
	name := ".notes-" + rand.Text()
	file, err := scope.store.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close(); _ = scope.store.root.Remove(name) }()
	if _, err = file.Write(raw); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = scope.store.root.Rename(name, scope.file); err != nil {
		return err
	}
	directory, err := scope.store.root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
