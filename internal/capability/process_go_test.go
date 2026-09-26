//go:build darwin || linux

package capability

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessGoUsesPreloadedModuleCacheOffline(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("Go executable is not on PATH")
	}
	work := t.TempDir()
	cache := t.TempDir()
	const dependency = "example.com/whip-cache-test"
	// Listing the module graph needs cached metadata, so this fixture exercises
	// a real Go command without downloads or toolchain compilation.
	mod := filepath.Join(cache, "cache", "download", dependency, "@v", "v1.0.0.mod")
	if err := os.MkdirAll(filepath.Dir(mod), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mod, []byte("module "+dependency+"\n\ngo 1.20\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(filepath.Dir(mod), "v1.0.0.info")
	if err := os.WriteFile(info, []byte(`{"Version":"v1.0.0","Time":"2020-01-01T00:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte("module example.com/app\n\ngo 1.20\n\nrequire "+dependency+" v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOMODCACHE", cache)
	t.Setenv("GOCACHE", t.TempDir())
	t.Setenv("GOPATH", t.TempDir())
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOFLAGS", "-mod=mod")
	m := NewProcessManager()
	t.Cleanup(func() { _ = m.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	p, err := m.Start(ctx, "root", goBin, []string{"list", "-m", "all"}, ProcessOptions{
		Cwd: work,
		// Explicitly prohibit downloads and ignore host-specific Go settings.
		Env:   map[string]string{"GOPROXY": "off", "GOSUMDB": "off", "GOENV": "off", "GOWORK": "off"},
		Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("offline module lookup failed: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), dependency+" v1.0.0\n") {
		t.Fatalf("cached dependency missing from module graph: %s", stdout.String())
	}
}
