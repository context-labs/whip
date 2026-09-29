package browser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/context-labs/whip/internal/capability"
)

// NativeUploads captures file identities, not contents or filesystem authority.
// Its separate resource names the exact workspace and canonical path set. Bytes
// are read only after the corresponding upload operation is durably dispatched.
type NativeUploads struct {
	workspace string
	identity  os.FileInfo
	files     map[string]nativeUploadFile
	paths     []string
	size      int64
}
type nativeUploadFile struct {
	canonical, relative string
	identity            os.FileInfo
}

func CaptureNativeUploads(cwd string, paths []string) (*NativeUploads, error) {
	if len(paths) == 0 {
		return nil, errors.New("browser upload capture requires files")
	}
	if len(paths) > 16 {
		return nil, errors.New("browser upload batch exceeds 16 files")
	}
	workspace, err := capability.NewWorkspaces().Open(cwd)
	if err != nil {
		return nil, err
	}
	identity, err := os.Stat(workspace.Root())
	if err != nil {
		return nil, err
	}
	captured := &NativeUploads{workspace: workspace.Root(), identity: identity, files: map[string]nativeUploadFile{}}
	for _, path := range paths {
		if _, ok := captured.files[path]; ok {
			continue
		}
		canonical, err := workspace.Resolve(path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(canonical)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 4<<20 {
			return nil, errors.New("browser uploads require regular files of at most 4 MiB")
		}
		relative, err := filepath.Rel(workspace.Root(), canonical)
		if err != nil {
			return nil, err
		}
		captured.files[path] = nativeUploadFile{canonical, relative, info}
		captured.size += info.Size()
		captured.paths = append(captured.paths, canonical)
	}
	if captured.size > 16<<20 {
		return nil, errors.New("browser upload batch exceeds 16 MiB")
	}
	slices.Sort(captured.paths)
	captured.paths = slices.Compact(captured.paths)
	return captured, nil
}
func (u *NativeUploads) Paths() []string { return slices.Clone(u.paths) }
func (u *NativeUploads) Resource(browserResource string) string {
	raw, _ := json.Marshal([]any{browserResource, u.workspace, u.paths})
	sum := sha256.Sum256(raw)
	return "browser-upload:" + hex.EncodeToString(sum[:])
}

func (u *NativeUploads) open() (*os.Root, error) {
	root, err := os.OpenRoot(u.workspace)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(".")
	if err != nil || !os.SameFile(u.identity, info) {
		_ = root.Close()
		return nil, errors.New("browser upload workspace identity changed")
	}
	return root, nil
}

// Snapshot retains copies until the acquired generation ends, because Chrome
// may defer reading selected files until a later batch submits a form.
func (u *NativeUploads) Snapshot(ctx context.Context, lease *NativeLease, directory string) (map[string]string, error) {
	if err := lease.reserveUploads(len(u.files), u.size); err != nil {
		return nil, err
	}
	root, err := u.open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	temp, err := os.MkdirTemp(directory, "browser-upload-")
	if err != nil {
		return nil, err
	}
	adopted := false
	defer func() {
		if !adopted {
			_ = os.RemoveAll(temp)
		}
	}()
	result := map[string]string{}
	for path, file := range u.files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source, err := root.OpenFile(file.relative, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		data, readErr := readNativeUpload(source, file.identity)
		closeErr := source.Close()
		if err = errors.Join(readErr, closeErr); err != nil {
			return nil, err
		}
		sub, err := os.MkdirTemp(temp, "file-")
		if err != nil {
			return nil, err
		}
		target := filepath.Join(sub, filepath.Base(file.canonical))
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return nil, err
		}
		result[path] = target
	}
	if err := lease.keepUploadDirectory(temp); err != nil {
		return nil, err
	}
	adopted = true
	return result, nil
}

func readNativeUpload(file *os.File, expected os.FileInfo) ([]byte, error) {
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || !os.SameFile(before, expected) || before.Size() != expected.Size() || !before.ModTime().Equal(expected.ModTime()) {
		return nil, errors.New("browser upload file changed after preparation")
	}
	data, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != expected.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return nil, errors.New("browser upload file changed while reading")
	}
	return data, nil
}

// CollectNativeUploads removes only this runtime's reserved temporary upload
// namespace after exclusive runtime ownership is acquired. Crash leftovers are
// never rebound to a new browser generation or treated as authorization.
func CollectNativeUploads(ctx context.Context, directory string) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := dir.ReadDir(4097)
	closeErr := dir.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return errors.Join(readErr, closeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if len(entries) > 4096 {
		return errors.New("runtime directory exceeds browser upload cleanup bound")
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		suffix, ok := strings.CutPrefix(entry.Name(), "browser-upload-")
		if !ok || len(suffix) < 1 || len(suffix) > 10 {
			continue
		}
		valid := true
		for _, c := range suffix {
			if c < '0' || c > '9' {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		if err := root.RemoveAll(entry.Name()); err != nil {
			return err
		}
	}
	return nil
}
