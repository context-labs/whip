package inferenceauth

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

type storage struct {
	directory     *os.File
	syncFile      func(*os.File) error
	syncDirectory func(*os.File) error
	unlink        func(int, string, int) error
}

type record struct {
	Version     int         `json:"version"`
	Credentials Credentials `json:"credentials"`
}

func openStorage(path string) (*storage, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrStorage
	}
	path = filepath.Clean(path)
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrStorage
	}
	directory := os.NewFile(uintptr(fd), "Inference.net credential directory")
	info, err := directory.Stat()
	if err != nil || !info.IsDir() || !owned(info) || info.Mode().Perm()&0o022 != 0 {
		_ = directory.Close()
		return nil, ErrStorage
	}
	return &storage{directory: directory, syncFile: (*os.File).Sync, syncDirectory: (*os.File).Sync, unlink: unix.Unlinkat}, nil
}

func owned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(stat.Uid) == int64(os.Getuid())
}

func (s *storage) open() (*os.File, os.FileInfo, error) {
	fd, err := unix.Openat(int(s.directory.Fd()), fileName, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), fileName)
	info, err := file.Stat()
	if err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !info.Mode().IsRegular() || !owned(info) || info.Mode().Perm()&0o177 != 0 || !ok || stat.Nlink != 1 {
			err = ErrStorage
		}
	}
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

func (s *storage) read() (Credentials, error) {
	file, before, err := s.open()
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, nil
	}
	if err != nil {
		return Credentials{}, ErrStorage
	}
	defer file.Close()
	if before.Size() > maxRecordBytes {
		return Credentials{}, ErrStorage
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxRecordBytes+1))
	if err != nil || len(raw) > maxRecordBytes {
		return Credentials{}, ErrStorage
	}
	after, err := file.Stat()
	if err != nil || before.Size() != int64(len(raw)) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return Credentials{}, ErrStorage
	}
	if !utf8.Valid(raw) || !uniqueFields(raw) {
		return Credentials{}, ErrStorage
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value record
	if decoder.Decode(&value) != nil || value.Version != 1 || value.Credentials.validate() != nil {
		return Credentials{}, ErrStorage
	}
	return value.Credentials, nil
}

// The record contains objects and scalar fields only. Check duplicate members,
// trailing data, and nesting before decoding so ambiguous credentials fail closed.
func uniqueFields(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value func(int) bool
	value = func(depth int) bool {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return true
		}
		if delimiter != '{' || depth > 3 {
			return false
		}
		names := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || names[name] || name != strings.ToLower(name) {
				return false
			}
			names[name] = true
			if !value(depth + 1) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim('}')
	}
	if !value(0) {
		return false
	}
	_, err := decoder.Token()
	return errors.Is(err, io.EOF)
}

// save distinguishes failure before publication from failure after rename. All
// returned errors are safe categories; temporary filenames and host paths stay
// private. After publication, the caller must block authorization until retry.
func (s *storage) save(credential Credentials, beforePublish func() error) (published bool, err error) {
	if err := credential.validate(); err != nil {
		return false, err
	}
	// #nosec G117 -- This private record is deliberately encoded only for the owned 0600 file below.
	raw, err := json.Marshal(record{Version: 1, Credentials: credential})
	if err != nil || len(raw) > maxRecordBytes {
		return false, ErrInvalid
	}
	if err := s.checkExisting(); err != nil {
		return false, err
	}
	name := ".inference-net-" + rand.Text()
	fd, err := unix.Openat(int(s.directory.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return false, ErrStorage
	}
	file := os.NewFile(uintptr(fd), name)
	defer func() {
		if removeErr := unix.Unlinkat(int(s.directory.Fd()), name, 0); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, ErrStorage)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return false, ErrStorage
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return false, ErrStorage
	}
	if err := s.syncFile(file); err != nil {
		_ = file.Close()
		return false, ErrStorage
	}
	if err := file.Close(); err != nil {
		return false, ErrStorage
	}
	if err := beforePublish(); err != nil {
		return false, err
	}
	if err := unix.Renameat(int(s.directory.Fd()), name, int(s.directory.Fd()), fileName); err != nil {
		return false, ErrStorage
	}
	if err := s.syncDirectory(s.directory); err != nil {
		return true, ErrStoragePending
	}
	return true, nil
}

func (s *storage) checkExisting() error {
	file, _, err := s.open()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return ErrStorage
	}
	if err := file.Close(); err != nil {
		return ErrStorage
	}
	return nil
}

func (s *storage) remove() error {
	if err := s.checkExisting(); err != nil {
		return err
	}
	if err := s.unlink(int(s.directory.Fd()), fileName, 0); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrStoragePending
	}
	// Also sync an already absent name: a prior unlink may have failed to sync.
	if err := s.syncDirectory(s.directory); err != nil {
		return ErrStoragePending
	}
	return nil
}
