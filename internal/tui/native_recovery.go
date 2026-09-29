package tui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

const (
	nativeRecoveryCount = 64
	nativeRecoveryBytes = 32 << 20
)

// Native recovery is a private client journal, never session state. One frozen
// unresolved input per runtime/owner prevents another terminal overwriting an
// unknown admission. Every replay still requires the native receipt protocol.
type nativeRecovery struct {
	root    *os.Root
	runtime protocol.ID
}

func openNativeRecovery(directory string, runtime protocol.ID) (*nativeRecovery, error) {
	if !filepath.IsAbs(directory) {
		return nil, errors.New("absolute client directory required")
	}
	parent, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer func() { _ = parent.Close() }()
	const name = "tui-inputs"
	if err := parent.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := parent.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || !nativeRecoveryOwned(info) {
		return nil, errors.New("input recovery directory must be an owned private directory")
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		_ = root.Close()
		return nil, errors.New("input recovery directory changed")
	}
	// The journal directory itself must survive before any saved input can
	// authorize a send; syncing only a file in a new directory is insufficient.
	if err := nativeSyncRoot(parent); err != nil {
		_ = root.Close()
		return nil, err
	}
	return &nativeRecovery{root: root, runtime: runtime}, nil
}

func nativeRecoveryOwned(info os.FileInfo) bool {
	value, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(value.Uid) == int64(os.Getuid())
}

func (r *nativeRecovery) name(owner protocol.ID) string {
	digest := sha256.Sum256([]byte(string(r.runtime) + "\x00" + string(owner)))
	return hex.EncodeToString(digest[:]) + ".json"
}

func (r *nativeRecovery) locked(operation func() error) error {
	const name = ".write.lock"
	// Elect one creator: Darwin can return ENOENT for concurrent O_CREATE.
	lock, err := r.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if errors.Is(err, os.ErrExist) {
		lock, err = r.root.OpenFile(name, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	}
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	info, err := lock.Stat()
	if err != nil {
		return err
	}
	named, err := r.root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !nativeRecoveryOwned(info) || !os.SameFile(info, named) {
		return errors.New("invalid input recovery lock")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another terminal is updating input recovery; inspect and try again: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	return operation()
}

func (r *nativeRecovery) read(owner protocol.ID) ([]byte, error) {
	file, err := r.root.OpenFile(r.name(owner), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !nativeRecoveryOwned(info) || info.Size() > client.MaxInputRecordBytes {
		return nil, errors.New("input recovery requires an owned private regular file of at most 4 MiB")
	}
	raw, err := io.ReadAll(io.LimitReader(file, client.MaxInputRecordBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > client.MaxInputRecordBytes {
		return nil, errors.New("input recovery exceeds 4 MiB")
	}
	return raw, nil
}

func (r *nativeRecovery) restore(connection *client.Client, owner protocol.ID) (command *client.InputCommand, err error) {
	err = r.locked(func() error {
		raw, err := r.read(owner)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		value, err := connection.RestoreInput(raw)
		if err != nil {
			return err
		}
		record := value.Record()
		var scope struct {
			SessionID protocol.ID `json:"session_id"`
		}
		if err := json.Unmarshal(record.Params, &scope); err != nil {
			return err
		}
		if scope.SessionID != owner || record.RuntimeID != r.runtime {
			return errors.New("input recovery belongs to another owner")
		}
		command = value
		return nil
	})
	return command, err
}

func (r *nativeRecovery) record(command *client.InputCommand) (protocol.ID, []byte, error) {
	record := command.Record()
	if record.RuntimeID != r.runtime {
		return "", nil, errors.New("input record belongs to another runtime")
	}
	// Only immutable intent is journalled. Check always reads canonical acceptance.
	record.Accepted = false
	var scope struct {
		SessionID protocol.ID `json:"session_id"`
	}
	if err := json.Unmarshal(record.Params, &scope); err != nil {
		return "", nil, err
	}
	if scope.SessionID == "" {
		return "", nil, errors.New("input record has no owner")
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return "", nil, err
	}
	if len(raw) > client.MaxInputRecordBytes {
		return "", nil, errors.New("input recovery exceeds 4 MiB")
	}
	return scope.SessionID, raw, nil
}

// save confirms file and directory durability before any admission is sent.
// An exact retained record may be re-synced; a different input is never replaced.
func (r *nativeRecovery) save(command *client.InputCommand) error {
	owner, raw, err := r.record(command)
	if err != nil {
		return err
	}
	return r.locked(func() error {
		existing, err := r.read(owner)
		if err == nil {
			if !bytes.Equal(existing, raw) {
				return errors.New("another unresolved input is retained for this session; inspect it before submitting")
			}
			file, err := r.root.OpenFile(r.name(owner), os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
			if err != nil {
				return err
			}
			again, readErr := io.ReadAll(io.LimitReader(file, client.MaxInputRecordBytes+1))
			if readErr != nil || !bytes.Equal(again, raw) {
				_ = file.Close()
				return errors.New("input recovery changed before durable publication")
			}
			if err := errors.Join(file.Sync(), file.Close()); err != nil {
				return err
			}
			return r.sync()
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := r.capacity(len(raw)); err != nil {
			return err
		}
		file, err := r.root.OpenFile(r.name(owner), os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(raw)
		if writeErr == nil {
			writeErr = file.Sync()
		}
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return err
		}
		return r.sync()
	})
}

func (r *nativeRecovery) capacity(added int) error {
	directory, err := r.root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	entries, err := directory.ReadDir(nativeRecoveryCount + 2)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	count, size := 0, int64(added)
	for _, entry := range entries {
		if entry.Name() == ".write.lock" {
			continue
		}
		count++
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > client.MaxInputRecordBytes {
			return errors.New("unexpected file in input recovery directory")
		}
		size += info.Size()
	}
	if count >= nativeRecoveryCount || size > nativeRecoveryBytes {
		return errors.New("terminal recovery holds 64 pending inputs or 32 MiB; inspect retained requests before submitting more")
	}
	return nil
}

func (r *nativeRecovery) clear(command *client.InputCommand) error {
	owner, raw, err := r.record(command)
	if err != nil {
		return err
	}
	return r.locked(func() error {
		existing, err := r.read(owner)
		if errors.Is(err, os.ErrNotExist) {
			return r.sync()
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(existing, raw) {
			return errors.New("input recovery changed; a different request has been preserved")
		}
		if err := r.root.Remove(r.name(owner)); err != nil {
			return err
		}
		return r.sync()
	})
}

func (r *nativeRecovery) sync() error { return nativeSyncRoot(r.root) }

func nativeSyncRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
