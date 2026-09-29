package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"golang.org/x/sys/unix"
)

func prepareRunRecordDirectory(directory string) error {
	root, err := os.OpenRoot(filepath.Dir(filepath.Dir(directory)))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for _, path := range []string{"client-v4", "client-v4/runs"} {
		if err := root.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := root.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !runRecordOwned(info) {
			return errors.New("run recovery directory must be an owned private directory")
		}
	}
	dir, err := root.Open("client-v4/runs")
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.ReadDir(257)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) >= 256 {
		return errors.New("256 unresolved CLI run records retained; inspect or explicitly remove finished records before another run")
	}
	return nil
}

func saveRunRecord(path string, record client.InputRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(raw) > client.MaxInputRecordBytes {
		return errors.New("run recovery record exceeds 4 MiB")
	}
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("create run recovery file: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	_, writeErr := file.Write(raw)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	err = errors.Join(writeErr, file.Close())
	if err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func readRunRecord(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !runRecordOwned(info) {
		return nil, errors.New("run recovery requires an owned private regular file")
	}
	if info.Size() > client.MaxInputRecordBytes {
		return nil, errors.New("run recovery record exceeds 4 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, client.MaxInputRecordBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > client.MaxInputRecordBytes {
		return nil, errors.New("run recovery record exceeds 4 MiB")
	}
	return data, nil
}

func readRunSystemFile(path string) ([]byte, error) {
	// An explicitly selected symlink to a regular file is allowed; opening with
	// NONBLOCK prevents FIFOs from hanging before we can inspect the descriptor.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("system instructions require a regular file")
	}
	if info.Size() > 512<<10 {
		return nil, errors.New("system instructions exceed 512 KiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, (512<<10)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 512<<10 {
		return nil, errors.New("system instructions exceed 512 KiB")
	}
	return data, nil
}

func runRecordOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(stat.Uid) == int64(os.Getuid())
}

// The explicit -no-session action first stops its subtree. Deletion can still
// fail for retained workspace pins or external concurrent admissions; that error
// preserves the session and recovery evidence rather than claiming cleanup.
func deleteNativeRun(ctx context.Context, c *client.Client, id protocol.ID) error {
	s, err := c.Session(id)
	if err != nil {
		return err
	}
	owner, err := s.Get(ctx)
	if err != nil {
		return err
	}
	var stopped protocol.Session
	if err := c.Call(ctx, "sessions.lifecycle", protocol.LifecycleParams{SessionID: id, Lifecycle: "stopped"}, &stopped); err != nil {
		return err
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		members := map[protocol.ID]protocol.Session{id: owner}
		var after *protocol.ID
		for range 100 {
			var page protocol.ListSessionsResult
			if err := c.Call(ctx, "sessions.list", protocol.ListSessionsParams{TreeID: owner.TreeID, After: after, Limit: 100}, &page); err != nil {
				return err
			}
			for _, member := range page.Items {
				members[member.ID] = member
			}
			if len(page.Items) < 100 {
				after = nil
				break
			}
			after = new(page.Items[len(page.Items)-1].ID)
		}
		if after != nil {
			return errors.New("session cleanup exceeds 10000 tree members; stop and delete explicitly")
		}
		for _, member := range members {
			current := member
			for range len(members) {
				if current.ID == id {
					if member.ID != id && member.Lifecycle != "stopped" {
						if err := c.Call(ctx, "sessions.lifecycle", protocol.LifecycleParams{SessionID: member.ID, Lifecycle: "stopped"}, &stopped); err != nil {
							return err
						}
					}
					break
				}
				if current.ParentID == nil {
					break
				}
				parent, ok := members[*current.ParentID]
				if !ok {
					break
				}
				current = parent
			}
		}
		var deleted protocol.DeleteResult
		err := c.Call(ctx, "sessions.delete", protocol.SessionParams{SessionID: id}, &deleted)
		if err == nil {
			return nil
		}
		var remote *client.Error
		if !errors.As(err, &remote) || remote.Kind != "BUSY" {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("session %s cleanup did not finish: %w", id, ctx.Err())
		case <-ticker.C:
		}
	}
}
