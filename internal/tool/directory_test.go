package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"
)

func TestDirectoryListSearchKeepLiteralScopeAndSkipSymlinks(t *testing.T) {
	dir := t.TempDir()
	files := NewFiles()
	for _, path := range []string{"nested", ".git"} {
		if err := os.Mkdir(filepath.Join(dir, path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(t, filepath.Join(dir, "a.txt"), "first\na.*b literal\n")
	writeFixture(t, filepath.Join(dir, "nested", "b.txt"), "a.*b nested\n")
	writeFixture(t, filepath.Join(dir, ".git", "secret"), "a.*b hidden")
	outside := filepath.Join(t.TempDir(), "outside")
	writeFixture(t, outside, "a.*b secret")
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	list := prepareTest(t, files, dir, "files.list", map[string]any{"path": "."})
	if list.Mutating || list.Resource != dir || list.Capability != "files.list" {
		t.Fatal(list)
	}
	value, err := runFileTest(t, list)
	if err != nil || value["output"] != ".git/\na.txt\nfifo\nlinked\nnested/\n" || value["truncated"] != false {
		t.Fatal(value, err)
	}
	search := prepareTest(t, files, dir, "files.search", map[string]any{"path": ".", "query": "a.*b"})
	if search.Mutating {
		t.Fatal("search marked mutating")
	}
	value, err = runFileTest(t, search)
	if err != nil || value["matches"] != 2 || value["truncated"] != false {
		t.Fatal(value, err)
	}
	text := value["output"].(string)
	if !strings.Contains(text, "a.txt:2:a.*b literal") || !strings.Contains(text, "nested/b.txt:1:a.*b nested") || strings.Contains(text, "secret") || strings.Contains(text, "hidden") {
		t.Fatal(text)
	}
	single := prepareTest(t, files, dir, "files.search", map[string]any{"path": "a.txt", "query": "a.*b"})
	value, err = runFileTest(t, single)
	if err != nil || value["output"] != ".:2:a.*b literal\n" {
		t.Fatal(value, err)
	}
	for _, args := range []map[string]any{{"path": "../outside"}, {"path": outside}, {"path": "linked"}} {
		if _, err := files.Prepare(dir, "files.list", args); err == nil {
			t.Fatal("outside path admitted", args)
		}
	}
}

func TestDirectoryBoundsAreExplicitAndResultsStayUTF8(t *testing.T) {
	dir := t.TempDir()
	files := NewFiles()
	for i := range 10001 {
		writeFixture(t, filepath.Join(dir, fmt.Sprintf("file-%04d", i)), "match\n")
	}
	value, err := runFileTest(t, prepareTest(t, files, dir, "files.list", map[string]any{}))
	if err != nil || value["entries"] != 2000 || value["truncated"] != true || value["reason"] == "" || len(value["output"].(string)) > outputBytes {
		t.Fatal(value, err)
	}
	value, err = runFileTest(t, prepareTest(t, files, dir, "files.search", map[string]any{"query": "match"}))
	if err != nil || value["matches"] != 100 || value["truncated"] != true || value["reason"] == "" {
		t.Fatal(value, err)
	}
	writeFixture(t, filepath.Join(dir, "huge"), strings.Repeat("世界", scanBytes/6+1))
	value, err = runFileTest(t, prepareTest(t, files, dir, "files.search", map[string]any{"path": "huge", "query": "世界"}))
	if err != nil || value["matches"] != 1 || value["truncated"] != true || value["scanned_bytes"].(int) > scanBytes || !utf8.ValidString(value["output"].(string)) || len(value["output"].(string)) > outputBytes {
		t.Fatal(value, err)
	}
	for _, test := range []struct {
		op   string
		args map[string]any
	}{
		{"files.list", map[string]any{"limit": 2001}},
		{"files.list", map[string]any{"limit": 0}},
		{"files.list", map[string]any{"query": ""}},
		{"files.list", map[string]any{"limit": "1"}},
		{"files.search", map[string]any{"query": ""}},
		{"files.search", map[string]any{"query": "x", "limit": 101}},
		{"files.search", map[string]any{"query": "x", "unexpected": true}},
	} {
		if _, err := files.Prepare(dir, test.op, test.args); err == nil {
			t.Fatal("invalid arguments admitted", test)
		}
	}
}

type scanCancellation struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *scanCancellation) Err() error {
	c.checks--
	if c.checks == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestDirectoryCancellationDuringTraversalPropagates(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "file"), "match")
	prepared := prepareTest(t, NewFiles(), dir, "files.search", map[string]any{"query": "match"})
	release, err := prepared.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if _, err := prepared.Run(&scanCancellation{Context: ctx, cancel: cancel, checks: 3}, "during-scan"); !errors.Is(err, context.Canceled) {
		t.Fatal("traversal cancellation did not reach caller", err)
	}
}

func TestDirectoryRevalidatesPathAndCancellationBeforeReading(t *testing.T) {
	for _, operation := range []string{"files.list", "files.search"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "scope")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			args := map[string]any{"path": "scope"}
			if operation == "files.search" {
				args["query"] = "secret"
			}
			prepared := prepareTest(t, NewFiles(), dir, operation, args)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), path); err != nil {
				t.Fatal(err)
			}
			if release, err := prepared.Acquire(t.Context()); err == nil {
				release()
				t.Fatal("changed target admitted")
			}
			args["path"] = "."
			prepared = prepareTest(t, NewFiles(), dir, operation, args)
			release, err := prepared.Acquire(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := prepared.Run(ctx, "cancelled"); !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled inspection proceeded", err)
			}
		})
	}
}
