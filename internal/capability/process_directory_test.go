//go:build darwin || linux

package capability

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessRevalidatesCapturedDirectoryIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := NewProcessManager()
	defer m.Close()
	var output strings.Builder
	opts := ProcessOptions{Cwd: dir, CwdIdentity: identity, Stdin: strings.NewReader(""), Stdout: &output, Stderr: io.Discard}
	process, err := m.Start(context.Background(), "owner", "/bin/sh", []string{"-c", "cat marker"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	process.Stop()
	if output.String() != "original" {
		t.Fatal(output.String())
	}
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(context.Background(), "owner", "/bin/sh", []string{"-c", "touch forbidden"}, opts); err == nil {
		t.Fatal("replaced path accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "forbidden")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
