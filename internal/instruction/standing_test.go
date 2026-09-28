package instruction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestStandingInstructionsFilterTextAndAuditRawBytes(t *testing.T) {
	dir, root := workspace(t)
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"filtered", "  # heading\r\n\n  Prefer concise answers.  \r\n\tUse π and 🌍.\t\n  ## another heading\n Keep inline # text \n", "Prefer concise answers.\nUse π and 🌍.\nKeep inline # text"},
		{"empty", "", ""},
		{"comments", "  # ignored\n\t\n\u2003# also ignored\n", ""},
		{"unicode whitespace", "\u2003first\u2003\n\n\tsecond", "first\nsecond"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFile(t, dir, "me.md", tc.raw)
			snapshot, err := LoadStanding(t.Context(), root, "me.md")
			if err != nil || snapshot.Text != tc.want || len(snapshot.Sources) != 1 || len(snapshot.Selected) != 0 {
				t.Fatalf("snapshot=%+v err=%v", snapshot, err)
			}
			source := snapshot.Sources[0]
			if err := source.Validate(); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(tc.raw))
			if source.Kind != "standing_instructions" || source.Scope != "host" || source.RootID == nil || *source.RootID != "standing" || source.Path != "me.md" || source.Bytes != int64(len(tc.raw)) || source.SHA256 != hex.EncodeToString(digest[:]) {
				t.Fatalf("incorrect raw-file audit: %+v", source)
			}
			if strings.Contains(snapshot.Text, dir) || strings.Contains(source.Path, dir) {
				t.Fatal("snapshot exposed parent directory")
			}
		})
	}
}

func TestStandingInstructionsBoundsAndInvalidText(t *testing.T) {
	dir, root := workspace(t)
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"exact limit", strings.Repeat("x", session.MaxInstructionSourceBytes), true},
		{"long comment", "#" + strings.Repeat("x", session.MaxInstructionSourceBytes-1), true},
		{"oversized", strings.Repeat("x", session.MaxInstructionSourceBytes+1), false},
		{"oversized comments", "#" + strings.Repeat("x", session.MaxInstructionSourceBytes), false},
		{"invalid UTF-8", "bad\xff", false},
		{"invalid comment UTF-8", "# bad\xff", false},
		{"NUL", "bad\x00", false},
		{"comment NUL", "# bad\x00", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFile(t, dir, "me.md", tc.text)
			snapshot, err := LoadStanding(t.Context(), root, "me.md")
			if tc.valid != (err == nil) {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if !tc.valid && (snapshot.Text != "" || len(snapshot.Sources) != 0) {
				t.Fatalf("invalid source returned partial snapshot: %+v", snapshot)
			}
		})
	}
}

func TestStandingInstructionsRejectMissingNonregularAndSymlinkSources(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "FIFO", "inside symlink", "outside symlink", "dangling symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir, root := workspace(t)
			path := filepath.Join(dir, "me.md")
			var err error
			switch kind {
			case "missing":
			case "directory":
				err = os.Mkdir(path, 0o700)
			case "FIFO":
				err = syscall.Mkfifo(path, 0o600)
			case "inside symlink":
				writeFile(t, dir, "target", "must not follow even inside root")
				err = os.Symlink("target", path)
			case "outside symlink":
				outside := t.TempDir()
				writeFile(t, outside, "target", "private")
				err = os.Symlink(filepath.Join(outside, "target"), path)
			case "dangling symlink":
				err = os.Symlink("missing", path)
			}
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := LoadStanding(t.Context(), root, "me.md")
				result <- err
			}()
			select {
			case err := <-result:
				if err == nil || strings.Contains(err.Error(), dir) {
					t.Fatalf("unsafe source accepted or parent path leaked: %v", err)
				}
				if kind == "missing" && !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("missing source lost underlying cause: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("nonregular source blocked the reader")
			}
			if kind == "missing" {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("reader created the missing file", err)
				}
			}
		})
	}
}

func TestStandingInstructionsValidateAuthorityBasenameAndCancellation(t *testing.T) {
	dir, root := workspace(t)
	writeFile(t, dir, "me.md", "valid")
	if _, err := LoadStanding(t.Context(), nil, "me.md"); err == nil {
		t.Fatal("nil authority accepted")
	}
	for _, name := range []string{"", ".", "..", "../me.md", "nested/me.md", `nested\me.md`, filepath.Join(dir, "me.md"), "bad\x00", "bad\xff"} {
		if _, err := LoadStanding(t.Context(), root, name); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid basename accepted: %q %v", name, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := LoadStanding(ctx, root, "me.md"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read=%v", err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStanding(t.Context(), root, "me.md"); err == nil || strings.Contains(err.Error(), dir) {
		t.Fatalf("closed root accepted or parent path exposed: %v", err)
	}
}

func TestStandingInstructionsKeepParentDescriptorAcrossRetarget(t *testing.T) {
	parent := t.TempDir()
	original := filepath.Join(parent, "original")
	if err := os.Mkdir(original, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, original, "me.md", "ORIGINAL")
	root, err := os.OpenRoot(original)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	if err := os.Rename(original, filepath.Join(parent, "moved")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	writeFile(t, outside, "me.md", "RETARGETED")
	if err := os.Symlink(outside, original); err != nil {
		t.Fatal(err)
	}
	snapshot, err := LoadStanding(t.Context(), root, "me.md")
	if err != nil || snapshot.Text != "ORIGINAL" {
		t.Fatalf("read escaped anchored parent: %+v %v", snapshot, err)
	}
}

func TestStandingInstructionsCloseReadDescriptors(t *testing.T) {
	dir, root := workspace(t)
	writeFile(t, dir, "valid", "instructions")
	writeFile(t, dir, "invalid", "bad\x00")
	if err := os.Mkdir(filepath.Join(dir, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("valid", filepath.Join(dir, "symlink")); err != nil {
		t.Fatal(err)
	}
	readAll := func() {
		t.Helper()
		for _, name := range []string{"valid", "invalid", "directory", "symlink", "missing"} {
			_, err := LoadStanding(t.Context(), root, name)
			if (name == "valid") != (err == nil) {
				t.Fatalf("read %s: %v", name, err)
			}
		}
	}
	readAll()
	before, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skip("descriptor inventory unavailable", err)
	}
	for range 32 {
		readAll()
	}
	after, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) > len(before) {
		t.Fatalf("reads leaked descriptors: before=%d after=%d", len(before), len(after))
	}
}
