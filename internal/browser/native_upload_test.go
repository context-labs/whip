package browser

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeUploadsExactPrivateCopiesAndGenerationCleanup(t *testing.T) {
	cwd := t.TempDir()
	original := filepath.Join(cwd, "fixture.txt")
	if err := os.WriteFile(original, []byte("captured bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	uploads, err := CaptureNativeUploads(cwd, []string{"fixture.txt"})
	if err != nil {
		t.Fatal(err)
	}
	h := nativeHostFixture(t)
	c := nativeCapture(t, h, "root", "root", "default")
	lease := nativeLease(t, c)
	resource, err := c.UploadResource(uploads)
	if err != nil {
		t.Fatal(err)
	}
	if resource == c.Description().Resource {
		t.Fatal("upload inherited generic browser grant")
	}
	paths, err := uploads.Snapshot(t.Context(), lease, h.options.Directory)
	if err != nil {
		t.Fatal(err)
	}
	private := paths["fixture.txt"]
	if private == original || filepath.Base(private) != "fixture.txt" {
		t.Fatal(private)
	}
	if err := os.WriteFile(original, []byte("later workspace edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(private)
	if err != nil || string(data) != "captured bytes" {
		t.Fatal(string(data), err)
	}
	lease.Close()
	if _, err := os.Stat(private); err != nil {
		t.Fatal("copy deleted before a later form submission", err)
	}
	h.RevokeResource(resource)
	if _, err := os.Stat(private); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("revoked snapshot retained", err)
	}
}

func TestNativeUploadsRejectReplacementEscapeAndBounds(t *testing.T) {
	for _, kind := range []string{"file", "workspace", "outside", "size"} {
		t.Run(kind, func(t *testing.T) {
			cwd := filepath.Join(t.TempDir(), "workspace")
			if err := os.Mkdir(cwd, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cwd, "fixture")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			if kind == "outside" {
				if _, err := CaptureNativeUploads(cwd, []string{"../elsewhere"}); err == nil {
					t.Fatal("escaped")
				}
				return
			}
			if kind == "size" {
				if err := os.Truncate(path, (4<<20)+1); err != nil {
					t.Fatal(err)
				}
				if _, err := CaptureNativeUploads(cwd, []string{"fixture"}); err == nil {
					t.Fatal("over-limit file accepted")
				}
				return
			}
			captured, err := CaptureNativeUploads(cwd, []string{"fixture"})
			if err != nil {
				t.Fatal(err)
			}
			if kind == "file" {
				if err := os.Rename(path, path+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Rename(cwd, cwd+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(cwd, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			h := nativeHostFixture(t)
			lease := nativeLease(t, nativeCapture(t, h, "root", "root", "default"))
			if _, err := captured.Snapshot(t.Context(), lease, h.options.Directory); err == nil {
				t.Fatal("replacement accepted")
			}
		})
	}
}

func TestNativeProgramExternalUploadsAndPrivateNetworkPolicy(t *testing.T) {
	if _, err := CompileProgram(`upload("input", "file")`); err == nil {
		t.Fatal("desktop accepted host paths")
	}
	program, err := CompileNativeProgram(`upload("input", ["one", "two"])`, false, false)
	if err != nil || len(program.UploadPaths()) != 2 {
		t.Fatal(program, err)
	}
	if err := program.checkURL(t.Context(), "http://127.0.0.1:8080/"); err == nil {
		t.Fatal("private destination accepted")
	}
	live, err := CompileNativeProgram(`info()`, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := live.checkURL(t.Context(), "http://127.0.0.1:8080/"); err != nil {
		t.Fatal(err)
	}
	if err := live.checkURL(t.Context(), "http://169.254.169.254/latest/meta-data"); err == nil {
		t.Fatal("metadata floor bypassed")
	}
}

func TestNativeUploadsCrashCollectionCannotFollowForeignSymlink(t *testing.T) {
	runtimeDirectory, outside := t.TempDir(), t.TempDir()
	foreign := filepath.Join(outside, "private.txt")
	if err := os.WriteFile(foreign, []byte("human file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(runtimeDirectory, "browser-upload-123")); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(runtimeDirectory, "browser-upload-user-named")
	if err := os.Mkdir(retained, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CollectNativeUploads(t.Context(), runtimeDirectory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("cleanup escaped runtime", err)
	}
	if _, err := os.Lstat(filepath.Join(runtimeDirectory, "browser-upload-123")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if _, err := os.Stat(retained); err != nil {
		t.Fatal("cleanup removed unowned name", err)
	}
}
