package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func prepareTest(t *testing.T, files *Files, directory, operation string, args map[string]any) Prepared {
	t.Helper()
	prepared, err := files.Prepare(directory, operation, args)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func runFileTest(t *testing.T, prepared Prepared) (map[string]any, error) {
	t.Helper()
	release, err := prepared.Acquire(t.Context())
	if err != nil {
		return nil, err
	}
	defer release()
	value, err := prepared.Run(t.Context())
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

func writeFixture(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertBytes(t *testing.T, path, expected string) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != expected {
		t.Fatalf("file %s = %q, error=%v; want %q", path, actual, err, expected)
	}
}

func TestFilesReadWritePatchAndImmutableRequest(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	files := NewFiles()
	args := map[string]any{"path": "nested/file.txt", "content": "first\nsecond\nthird"}
	write := prepareTest(t, files, directory, "files.write", args)
	args["content"] = "mutated request"
	if write.Capability != "files.write" || write.Resource != directory || !write.Mutating {
		t.Fatalf("incorrect prepared authority: %+v", write)
	}
	result, err := runFileTest(t, write)
	if err != nil || result["bytes_written"] != len("first\nsecond\nthird") {
		t.Fatalf("write result=%v error=%v", result, err)
	}
	path := filepath.Join(directory, "nested/file.txt")
	assertBytes(t, path, "first\nsecond\nthird")
	if err := os.Chmod(path, 0o750); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	patch := prepareTest(t, files, directory, "files.patch", map[string]any{
		"path": "nested/file.txt", "old_text": "second", "new_text": "changed",
	})
	result, err = runFileTest(t, patch)
	if err != nil || result["replacements"] != 1 {
		t.Fatalf("patch result=%v error=%v", result, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o750 || os.SameFile(info, before) {
		t.Fatalf("publication did not replace file and preserve mode: %+v error=%v", info, err)
	}
	read := prepareTest(t, files, directory, "files.read", map[string]any{
		"path": "nested/file.txt", "offset": 2, "limit": 1,
	})
	result, err = runFileTest(t, read)
	if err != nil || result["output"] != "2\tchanged\n" || result["next_offset"] != 3 || result["truncated"] != true {
		t.Fatalf("read result=%v error=%v", result, err)
	}
	if _, err := write.Run(t.Context()); err == nil {
		t.Fatal("one-use operation executed again")
	}
}

func TestFilesPatchMismatchAndLimitsDoNotChangeBytes(t *testing.T) {
	directory := t.TempDir()
	files := NewFiles()
	path := filepath.Join(directory, "file")
	original := strings.Join([]string{"same", "same"}, " ")
	writeFixture(t, path, original)
	for _, test := range []struct {
		name, old string
	}{
		{name: "missing", old: "missing"},
		{name: "ambiguous", old: "same"},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared := prepareTest(t, files, directory, "files.patch", map[string]any{
				"path": "file", "old_text": test.old, "new_text": "replacement",
			})
			if _, err := runFileTest(t, prepared); err == nil {
				t.Fatal("invalid patch succeeded")
			}
			assertBytes(t, path, original)
		})
	}
	all := prepareTest(t, files, directory, "files.patch", map[string]any{
		"path": "file", "old_text": "same", "new_text": "new", "replace_all": true,
	})
	result, err := runFileTest(t, all)
	if err != nil || result["replacements"] != 2 {
		t.Fatalf("replace_all=%v error=%v", result, err)
	}
	assertBytes(t, path, strings.Join([]string{"new", "new"}, " "))
	writeFixture(t, path, strings.Repeat("a", fileBytes+1))
	oversized := prepareTest(t, files, directory, "files.patch", map[string]any{
		"path": "file", "old_text": "a", "new_text": "b", "replace_all": true,
	})
	if _, err := runFileTest(t, oversized); err == nil {
		t.Fatal("oversized patch succeeded")
	}
	assertBytes(t, path, strings.Repeat("a", fileBytes+1))
	writeFixture(t, path, strings.Repeat("a", 2000))
	expanded := prepareTest(t, files, directory, "files.patch", map[string]any{
		"path": "file", "old_text": "a", "new_text": strings.Repeat("b", 2000), "replace_all": true,
	})
	if _, err := runFileTest(t, expanded); err == nil {
		t.Fatal("oversized replacement succeeded")
	}
	assertBytes(t, path, strings.Repeat("a", 2000))
}

func TestFilesRejectInvalidRequests(t *testing.T) {
	t.Parallel()
	files := NewFiles()
	directory := t.TempDir()
	for _, test := range []struct {
		name, operation string
		args            map[string]any
	}{
		{name: "absolute", operation: "files.read", args: map[string]any{"path": filepath.Join(directory, "file")}},
		{name: "traversal", operation: "files.read", args: map[string]any{"path": "a/../file"}},
		{name: "escape", operation: "files.read", args: map[string]any{"path": "../file"}},
		{name: "directory", operation: "files.read", args: map[string]any{"path": "."}},
		{name: "nul", operation: "files.read", args: map[string]any{"path": "bad\x00file"}},
		{name: "unknown", operation: "files.read", args: map[string]any{"path": "file", "extra": true}},
		{name: "zero offset", operation: "files.read", args: map[string]any{"path": "file", "offset": 0}},
		{name: "fraction", operation: "files.read", args: map[string]any{"path": "file", "limit": 1.5}},
		{name: "empty old", operation: "files.patch", args: map[string]any{"path": "file", "old_text": "", "new_text": "new"}},
		{name: "missing content", operation: "files.write", args: map[string]any{"path": "file"}},
		{name: "large arguments", operation: "files.write", args: map[string]any{"path": "file", "content": strings.Repeat("a", fileBytes)}},
		{name: "invalid UTF-8", operation: "files.write", args: map[string]any{"path": "file", "content": "\xff"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := files.Prepare(directory, test.operation, test.args); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

func TestFilesMutationLocksCancelAndRevalidate(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	files := NewFiles()
	writeFixture(t, filepath.Join(directory, "target"), "before")
	if err := os.Symlink("target", filepath.Join(directory, "alias")); err != nil {
		t.Fatal(err)
	}
	prepare := func(path string) Prepared {
		return prepareTest(t, files, directory, "files.write", map[string]any{"path": path, "content": "after"})
	}
	first := prepare("target")
	release, err := first.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	cancelled := prepare("alias")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := cancelled.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked acquisition ignored cancellation: %v", err)
	}
	second := prepare("alias")
	acquired := make(chan error, 1)
	go func() {
		unlock, err := second.Acquire(t.Context())
		if unlock != nil {
			unlock()
		}
		acquired <- err
	}()
	select {
	case err := <-acquired:
		t.Fatalf("alias bypassed canonical lock: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if err := os.Remove(filepath.Join(directory, "alias")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(directory, "different"), "different")
	if err := os.Symlink("different", filepath.Join(directory, "alias")); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-acquired:
		if err == nil {
			t.Fatal("retargeted alias kept its old admission")
		}
	case <-time.After(time.Second):
		t.Fatal("mutation lock did not release")
	}
	assertBytes(t, filepath.Join(directory, "target"), "before")
}

func TestFilesConfineSymlinksAndRejectSpecialFiles(t *testing.T) {
	directory, outside := t.TempDir(), t.TempDir()
	files := NewFiles()
	writeFixture(t, filepath.Join(outside, "file"), "outside")
	if err := os.Symlink(outside, filepath.Join(directory, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Prepare(directory, "files.read", map[string]any{"path": "escape/file"}); err == nil {
		t.Fatal("escaping symlink accepted")
	}
	if err := os.Symlink("missing", filepath.Join(directory, "dangling")); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Prepare(directory, "files.write", map[string]any{"path": "dangling", "content": "bad"}); err == nil {
		t.Fatal("dangling symlink accepted")
	}
	if err := syscall.Mkfifo(filepath.Join(directory, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"files.read", "files.write", "files.patch"} {
		t.Run(operation, func(t *testing.T) {
			args := map[string]any{"path": "pipe"}
			switch operation {
			case "files.write":
				args["content"] = "bad"
			case "files.patch":
				args["old_text"], args["new_text"] = "old", "new"
			}
			if _, err := runFileTest(t, prepareTest(t, files, directory, operation, args)); err == nil {
				t.Fatal("special file accepted")
			}
		})
	}
	if err := os.Mkdir(filepath.Join(directory, "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	prepared := prepareTest(t, files, directory, "files.write", map[string]any{"path": "inside/file", "content": "bad"})
	release, err := prepared.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := os.Remove(filepath.Join(directory, "inside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "inside")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Run(t.Context()); err == nil {
		t.Fatal("post-acquisition symlink escaped root")
	}
	assertBytes(t, filepath.Join(outside, "file"), "outside")
}

func TestFilesReadBoundsAndPartialLine(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	files := NewFiles()
	writeFixture(t, filepath.Join(directory, "file"), strings.Repeat("\x01", fileBytes+1))
	result, err := runFileTest(t, prepareTest(t, files, directory, "files.read", map[string]any{"path": "file"}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > fileBytes || result["truncated"] != true || result["next_offset"] != nil {
		t.Fatalf("unbounded or untruthful read: bytes=%d result=%v error=%v", len(raw), result, err)
	}
	writeFixture(t, filepath.Join(directory, "file"), "\xffinvalid")
	if _, err := runFileTest(t, prepareTest(t, files, directory, "files.read", map[string]any{"path": "file"})); err == nil {
		t.Fatal("invalid text silently replaced")
	}
}

func TestFilePublicationFailurePreservesOriginal(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	writeFixture(t, filepath.Join(directory, "file"), "original")
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	injected := errors.New("injected publication failure")
	err = publishFile(t.Context(), root, "file", []byte("replacement"), new(os.FileMode(0o600)), func() error { return injected })
	if !errors.Is(err, injected) {
		t.Fatalf("publication error=%v", err)
	}
	assertBytes(t, filepath.Join(directory, "file"), "original")
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "file" {
		t.Fatalf("temporary publication file leaked: %v error=%v", entries, err)
	}
}
