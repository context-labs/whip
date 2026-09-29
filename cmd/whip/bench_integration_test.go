//go:build integration

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeCompiledBenchmarkSeparatesReadFromInitialization(t *testing.T) {
	directory := t.TempDir()
	binary, home := filepath.Join(directory, "whipcode"), filepath.Join(directory, "product")
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/whip")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = []string{"HOME=" + directory, "WHIPCODE_HOME=" + home, "PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
		if output, err := cmd.CombinedOutput(); err != nil || len(output) != 0 {
			t.Fatalf("benchmark %v: %v\n%s", args, err, output)
		}
	}
	run("--bench")
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("fresh benchmark created product files", err)
	}
	run("--bench-init")
	path := filepath.Join(home, "runtime-v4", "host.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("unsafe publication", info, err)
	}
	run("--bench")
	run("--bench-init")
	if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
		t.Fatal("benchmark changed initialized host file", err)
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 1 || entries[0].Name() != "runtime-v4" {
		t.Fatal("benchmark created non-native files", entries, err)
	}
	if entries, err := os.ReadDir(filepath.Dir(path)); err != nil || len(entries) != 1 || entries[0].Name() != "host.json" {
		t.Fatal("benchmark created database, socket, process lock, or runtime log", entries, err)
	}
}
