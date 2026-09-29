package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDiscoveryRejectsOversizedAndNonregularSources(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxSourceBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readSource(path); err == nil {
		t.Fatal("oversized source read")
	}
	fifo := filepath.Join(directory, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := readSource(fifo); err == nil {
		t.Fatal("named pipe source read")
	}
	if time.Since(start) > time.Second {
		t.Fatal("discovery waited for named pipe writer")
	}
	for _, parse := range []func([]byte) (map[string]ServerConfig, error){ParseClaude, ParseCodex, ParseOpenCode} {
		if _, err := parse([]byte(strings.Repeat(" ", maxSourceBytes+1))); err == nil {
			t.Fatal("oversized parser input accepted")
		}
	}
}
