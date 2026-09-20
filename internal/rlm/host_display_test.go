package rlm

import (
	"strings"
	"testing"
)

func TestHostDisplayUsesOnlyTypedBoundedIdentities(t *testing.T) {
	read := hostDisplay("files", "read", map[string]any{"path": "source/a.ts", "secret": "do not display"})
	if read == nil || read.Target != "source/a.ts" || read.Query != "" {
		t.Fatalf("file display: %+v", read)
	}
	if display := hostDisplay("files", "patch", map[string]any{"path": strings.Repeat("a", 4097), "content": "do not display"}); display != nil {
		t.Fatalf("invented a truncated file identity: %+v", display)
	}
	spawn := HostCall{Module: "agents", Operation: "spawn", Display: hostDisplay("agents", "spawn", map[string]any{"name": "Review", "prompt": "private task"})}
	finished := hostResultDisplay(spawn, map[string]any{"id": "child", "content": "not a label"})
	if finished.ChildID != "child" || finished.Label != "Review" || spawn.Display.ChildID != "" {
		t.Fatalf("spawn identity or immutable start: %+v", finished)
	}
	if hostResultDisplay(spawn, map[string]any{"id": strings.Repeat("x", 257)}).ChildID != "" {
		t.Fatal("truncated child IDs must not become links")
	}
}
