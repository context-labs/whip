package daemon

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func hostBehaviorCall(t *testing.T, host *recursiveHost, module, operation string, args map[string]any) any {
	t.Helper()
	value, err := host.Call(t.Context(), module, operation, args)
	if err != nil {
		t.Fatalf("%s.%s: %v", module, operation, err)
	}
	return value
}

func TestRecursiveHostArtifactsSchedulesAndRetainedChildren(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	host := runtime.rootNode.host
	text := strings.Repeat("prefix needle suffix;", 2000)
	artifact := hostBehaviorCall(t, host, "artifacts", "put", map[string]any{"text": text}).(map[string]any)
	handle := artifact["handle"].(string)
	if handle == "" || artifact["size"] != int64(len(text)) {
		t.Fatalf("artifact identity=%+v", artifact)
	}
	meta := hostBehaviorCall(t, host, "artifacts", "inspect", map[string]any{"handle": handle}).(map[string]any)
	if meta["source"] != "agent artifact" || meta["size"] != int64(len(text)) {
		t.Fatalf("metadata=%+v", meta)
	}
	chunk := hostBehaviorCall(t, host, "artifacts", "read", map[string]any{"handle": handle, "offset": float64(7), "length": float64(6)}).(map[string]any)
	if chunk["text"] != text[7:13] {
		t.Fatalf("bounded read=%+v", chunk)
	}
	if _, err := host.Call(t.Context(), "context", "search", map[string]any{"handle": handle}); err == nil {
		t.Fatal("empty content search accepted")
	}
	if _, err := host.Call(t.Context(), "context", "history", map[string]any{"handle": handle}); err == nil {
		t.Fatal("content handle accepted as transcript")
	}
	if _, err := host.Call(t.Context(), "artifacts", "read", map[string]any{"handle": "missing"}); err == nil {
		t.Fatal("missing artifact read succeeded")
	}
	schedule := hostBehaviorCall(t, host, "schedules", "create", map[string]any{"schedule": "@every 1h", "prompt": "inspect retained data"}).(map[string]any)
	id := schedule["id"].(int)
	listed := hostBehaviorCall(t, host, "schedules", "list", nil).([]session.Schedule)
	if len(listed) != 1 || listed[0].ID != id || listed[0].Prompt != "inspect retained data" {
		t.Fatalf("schedules=%+v", listed)
	}
	hostBehaviorCall(t, host, "schedules", "cancel", map[string]any{"id": float64(id)})
	if listed := hostBehaviorCall(t, host, "schedules", "list", nil).([]session.Schedule); len(listed) != 0 {
		t.Fatalf("cancelled schedule remains: %+v", listed)
	}
	if _, err := host.Call(t.Context(), "schedules", "create", map[string]any{"schedule": "not a schedule", "prompt": "bad"}); err == nil {
		t.Fatal("invalid schedule persisted")
	}
	if _, err := store.AdmitAgent(t.Context(), session.AgentAdmission{RootID: root.ID(), ParentAgentID: root.ID(), ChildAgentID: "waiting-child", Name: "waiting-child", Prompt: session.RuntimePayload{Data: []byte("wait for work"), MediaType: "text/plain"}, Model: "model", Provider: "provider"}); err != nil {
		t.Fatal(err)
	}
	children, err := root.LoadRetainedAgents(t.Context())
	if err != nil || len(children) != 1 || children[0].ID != "waiting-child" {
		t.Fatalf("retained children=%+v %v", children, err)
	}
	authority, _, err := root.LoadAgentAuthority(t.Context(), "waiting-child")
	if err != nil || authority.AgentID != "waiting-child" {
		t.Fatalf("authority=%+v %v", authority, err)
	}
	history, err := root.LoadAgentTranscript(t.Context(), "waiting-child")
	if err != nil || len(history) != 0 {
		t.Fatalf("new child transcript=%+v %v", history, err)
	}
	args := map[string]any{"ids": []any{"waiting-child"}, "timeout_ms": float64(0)}
	waiting := hostBehaviorCall(t, host, "agents", "wait", args).(map[string]any)
	if waiting["settled"] != false || waiting["timed_out"] != true {
		t.Fatalf("pending input not reflected in wait: %+v", waiting)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := runtime.wait(ctx, runtime.rootNode, []string{"waiting-child"}, 1000); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled wait=%v", err)
	}
	if _, err := runtime.wait(t.Context(), runtime.rootNode, nil, 0); err == nil {
		t.Fatal("empty wait accepted")
	}
	if _, err := host.Call(t.Context(), "agents", "wait", map[string]any{"ids": []any{root.ID()}}); !errors.Is(err, session.ErrAgentAccess) {
		t.Fatalf("wait escaped child scope: %v", err)
	}
	if err := root.TerminalizeSubtree(t.Context(), root.ID(), "waiting-child", "stopped"); err != nil {
		t.Fatal(err)
	}
	settled := hostBehaviorCall(t, host, "agents", "wait", args).(map[string]any)
	if settled["settled"] != true || settled["timed_out"] != false {
		t.Fatalf("terminal child remained busy: %+v", settled)
	}
}

func TestRecursiveHostRejectsMalformedOperationsBeforeEffects(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	_, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	host := runtime.rootNode.host
	for _, module := range []string{"context", "files", "shell", "browser", "computer", "models", "agents", "messages", "state", "artifacts", "schedules", "permissions", "unknown"} {
		t.Run(module, func(t *testing.T) {
			if _, err := host.Call(t.Context(), module, "nonexistent", map[string]any{"handle": "missing"}); err == nil {
				t.Fatal("unknown operation accepted")
			}
		})
	}
	for _, budget := range []any{[]any{}, map[string]any{"tokens": -1.0}, map[string]any{"tokens": 1.5}, map[string]any{"tokens": math.Inf(1)}, map[string]any{"tokens": math.NaN()}, map[string]any{"tokens": "10"}} {
		if _, err := host.Call(t.Context(), "agents", "spawn", map[string]any{"prompt": "must not run", "budgets": budget}); err == nil {
			t.Fatalf("invalid budget admitted: %+v", budget)
		}
	}
	if _, err := host.Call(t.Context(), "agents", "spawn", map[string]any{"prompt": "must not run", "capabilities": []any{"not-inherited"}}); err == nil {
		t.Fatal("child enlarged capabilities")
	}
	children, err := root.LoadRetainedAgents(t.Context())
	if err != nil || len(children) != 0 {
		t.Fatalf("failed spawn retained children: %+v %v", children, err)
	}
	for _, operation := range []string{"poll", "kill", "wait", "tail"} {
		if _, err := host.Call(t.Context(), "shell", operation, map[string]any{"id": "before-restart"}); err == nil || !strings.Contains(err.Error(), "restart") {
			t.Fatalf("lost job %s: %v", operation, err)
		}
	}
	if _, err := host.Call(t.Context(), "files", "search", nil); err == nil {
		t.Fatal("unbounded empty file search accepted")
	}
	response := hostBehaviorCall(t, host, "permissions", "request", nil).(map[string]any)
	if response["status"] != "invoke_operation" {
		t.Fatalf("permission request=%+v", response)
	}
	if _, err := host.Call(t.Context(), "permissions", "status", map[string]any{"id": "absent"}); err == nil {
		t.Fatal("invented permission ticket")
	}
}

func TestRecursiveHostShellJobCanTimeoutThenBeKilled(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	_, _, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	host := runtime.rootNode.host
	result := hostBehaviorCall(t, host, "shell", "start", map[string]any{"command": "printf job-output; sleep 30"}).(map[string]any)
	id := result["id"].(string)
	t.Cleanup(func() { _ = runtime.rootNode.agent.Services.KillJob(id) })
	waiting := hostBehaviorCall(t, host, "shell", "wait", map[string]any{"id": id, "timeout_ms": float64(0)}).(map[string]any)
	if waiting["running"] != true || waiting["timed_out"] != true {
		t.Fatalf("live wait=%+v", waiting)
	}
	listed := hostBehaviorCall(t, host, "shell", "list", nil).([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["id"] != id {
		t.Fatalf("job not listed: %+v", listed)
	}
	stopped := hostBehaviorCall(t, host, "shell", "kill", map[string]any{"id": id}).(map[string]any)
	if stopped["running"] != false || stopped["killed"] != true {
		t.Fatalf("kill did not stop job: %+v", stopped)
	}
	final := hostBehaviorCall(t, host, "shell", "wait", map[string]any{"id": id}).(map[string]any)
	if final["timed_out"] != false {
		t.Fatalf("settled job timed out: %+v", final)
	}
	tail := hostBehaviorCall(t, host, "shell", "tail", map[string]any{"id": id, "bytes": float64(4)}).(map[string]any)
	if tail["running"] != false || len(tail["tail"].(string)) > 4 {
		t.Fatalf("tail is not bounded: %+v", tail)
	}
}

func TestRecursiveHostFileOperationsPreserveWorkspaceAndPatchArguments(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	meta, _, err := store.Load(root.ID())
	if err != nil {
		t.Fatal(err)
	}
	host := runtime.rootNode.host
	path := filepath.Join(meta.CWD, "host-file.txt")
	hostBehaviorCall(t, host, "files", "write", map[string]any{"path": path, "content": "before\nneedle\n"})
	args := map[string]any{"path": path, "old": "before", "new": "after"}
	hostBehaviorCall(t, host, "files", "patch", args)
	if len(args) != 3 || args["old_string"] != nil || args["new_string"] != nil {
		t.Fatalf("patch mutated caller arguments: %+v", args)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "after\nneedle\n" {
		t.Fatalf("patch contents=%q %v", content, err)
	}
	read := hostBehaviorCall(t, host, "files", "read", map[string]any{"path": path}).(map[string]any)
	if !strings.Contains(read["output"].(string), "after") {
		t.Fatalf("read=%+v", read)
	}
	listed := hostBehaviorCall(t, host, "files", "list", nil).(map[string]any)
	if !strings.Contains(listed["output"].(string), "host-file.txt") {
		t.Fatalf("default list escaped workspace: %+v", listed)
	}
	search := hostBehaviorCall(t, host, "files", "search", map[string]any{"query": "needle"}).(map[string]any)
	if !strings.Contains(search["output"].(string), "needle") {
		t.Fatalf("default search missed workspace: %+v", search)
	}
	if _, err := host.Call(t.Context(), "files", "patch", map[string]any{"path": path, "old": "absent", "new": "bad"}); err == nil {
		t.Fatal("unmatched patch succeeded")
	}
	if _, err := host.Call(t.Context(), "files", "write", map[string]any{"path": path, "content": math.NaN()}); err == nil {
		t.Fatal("unencodable operation accepted")
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != string(content) {
		t.Fatalf("failed operation changed file: %q %v", unchanged, err)
	}
}

func TestRecursiveHostMCPDiscoveryReportsCurrentAuthority(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	url, effects := localMCPFixture(t, "read instructions before calling tools")
	_, root, runtime := mcpRuntimeFixture(t, url, true)
	host := runtime.rootNode.host
	servers := hostBehaviorCall(t, host, "mcp", "list_servers", nil).([]map[string]any)
	if len(servers) != 1 || servers[0]["name"] != "local" || servers[0]["trusted"] != true || servers[0]["error"] != "" {
		t.Fatalf("servers=%+v", servers)
	}
	listed := hostBehaviorCall(t, host, "mcp", "list_tools", map[string]any{"server": "local"}).([]map[string]any)
	if len(listed) != 4 {
		t.Fatalf("discovery lost tools: %+v", listed)
	}
	for _, tool := range listed {
		if tool["authorized"] != true || tool["generation"] == "" || tool["definition"] == "" {
			t.Fatalf("discovery lacks callable identity: %+v", tool)
		}
	}
	instructions := hostBehaviorCall(t, host, "mcp", "instructions", map[string]any{"server": "local"}).(map[string]any)
	if instructions["server"] != "local" || instructions["instructions"].(map[string]any)["output"] != "read instructions before calling tools" {
		t.Fatalf("instructions=%+v", instructions)
	}
	if _, err := root.RevokeCapability(t.Context(), root.AgentID(), runtime.rootNode.authority.MCP.ID); err != nil {
		t.Fatal(err)
	}
	listed = hostBehaviorCall(t, host, "mcp", "list_tools", map[string]any{"server": "local"}).([]map[string]any)
	for _, tool := range listed {
		if tool["authorized"] != false {
			t.Fatalf("revoked capability advertised as authorized: %+v", tool)
		}
	}
	for _, operation := range []string{"list_tools", "instructions"} {
		if _, err := host.Call(t.Context(), "mcp", operation, map[string]any{"server": "missing"}); err == nil {
			t.Fatalf("unknown server accepted for %s", operation)
		}
	}
	for _, args := range []map[string]any{{"server": "local", "tool": "mutate"}, {"server": "local", "tool": "mutate", "arguments": map[string]any{"bad": math.NaN()}}} {
		if _, err := host.Call(t.Context(), "mcp", "call", args); err == nil {
			t.Fatalf("invalid MCP arguments accepted: %+v", args)
		}
	}
	if _, err := host.Call(t.Context(), "mcp", "nonexistent", nil); err == nil {
		t.Fatal("unknown MCP operation accepted")
	}
	if effects.Load() != 0 {
		t.Fatalf("discovery or malformed calls caused %d effects", effects.Load())
	}
}
