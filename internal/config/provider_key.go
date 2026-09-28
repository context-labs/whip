package config

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/context-labs/whip/internal/session"
	"golang.org/x/sys/unix"
)

var (
	ErrKeyConflict       = errors.New("provider key identity already contains different credentials")
	ErrKeyStorage        = errors.New("private provider key publication needs attention")
	ErrKeyStoragePending = fmt.Errorf("%w: published key durability is unconfirmed; retry the same key identity", ErrKeyStorage)
)

// PublishKey saves one immutable private file under a caller-owned identity.
// Exact retries confirm directory durability; different bytes conflict. It
// changes no route or default, and never removes an ambiguously published key.
// The returned canonical file path is an explicit file credential source.
func (a *Authority) PublishKey(ctx context.Context, id, value string) (string, error) {
	if err := session.ValidateID(id); err != nil {
		return "", session.ErrInvalid
	}
	key, err := validateCredential(value)
	if err != nil {
		return "", session.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	canonical, err := filepath.EvalSymlinks(a.directory)
	if err != nil {
		return "", ErrKeyStorage
	}
	directory, err := openCredentialDirectory(canonical)
	if err != nil {
		return "", ErrKeyStorage
	}
	defer directory.Close()
	digest := sha256.Sum256([]byte(id))
	name := "provider-key-" + hex.EncodeToString(digest[:]) + ".txt"
	path := filepath.Join(canonical, name)
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err == nil {
		file := os.NewFile(uintptr(fd), "provider key")
		defer file.Close()
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || !owned(info) || info.Mode().Perm()&0o077 != 0 || info.Size() > maxCredentialBytes {
			return "", ErrKeyStorage
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, maxCredentialBytes+1))
		if readErr != nil || len(raw) > maxCredentialBytes {
			return "", ErrKeyStorage
		}
		after, statErr := file.Stat()
		if statErr != nil || !owned(after) || after.Mode().Perm()&0o077 != 0 || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || int64(len(raw)) != info.Size() {
			return "", ErrKeyStorage
		}
		if string(raw) != key {
			return "", ErrKeyConflict
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if a.syncDirectory(directory) != nil {
			return "", ErrKeyStoragePending
		}
		return path, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", ErrKeyStorage
	}
	entries, err := directory.ReadDir(385)
	if err != nil && !errors.Is(err, io.EOF) || len(entries) > 384 {
		return "", ErrKeyStorage
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "provider-key-") {
			count++
		}
	}
	if count >= 256 {
		return "", ErrKeyStorage
	}
	temporary := ".provider-key-" + rand.Text()
	fd, err = unix.Openat(int(directory.Fd()), temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return "", ErrKeyStorage
	}
	defer func() { _ = unix.Unlinkat(int(directory.Fd()), temporary, 0) }()
	file := os.NewFile(uintptr(fd), "provider key")
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", ErrKeyStorage
	}
	if _, err := file.WriteString(key); err != nil {
		_ = file.Close()
		return "", ErrKeyStorage
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", ErrKeyStorage
	}
	if err := file.Close(); err != nil {
		return "", ErrKeyStorage
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if unix.Linkat(int(directory.Fd()), temporary, int(directory.Fd()), name, 0) != nil {
		return "", ErrKeyStorage
	}
	if a.syncDirectory(directory) != nil {
		return "", ErrKeyStoragePending
	}
	return path, nil
}
