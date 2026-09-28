package rpc_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runtime"
)

func fixture(t *testing.T) (*runtime.Runtime, *client.Client) {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "whip-v4-") //nolint:usetesting // macOS t.TempDir paths exceed the Unix socket path limit; cleanup is registered below.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	r, err := runtime.Open(t.Context(), directory, model.Scripted{}, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	server, err := rpc.Listen(r)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	c, err := client.Connect(t.Context(), r.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return r, c
}

func call[R any](t *testing.T, c *client.Client, method string, p any) R {
	t.Helper()
	var result R
	if err := c.Call(t.Context(), method, p, &result); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return result
}

func create(t *testing.T, c *client.Client) protocol.CreateTreeResult {
	t.Helper()
	return call[protocol.CreateTreeResult](t, c, "trees.create", protocol.CreateTreeParams{
		Engine: "starlark", Policy: protocol.TreePolicy{MaxDepth: 4, MaxSessions: 100, MaxQueuedInputsPerSession: 100}, Definition: c.Builtins()[0], WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}},
	})
}

func TestGoClientUsesGeneratedContractForUniformSessionOperations(t *testing.T) {
	r, c := fixture(t)
	tree := create(t, c)
	found := call[protocol.Tree](t, c, "trees.get", protocol.TreeParams{TreeID: tree.Tree.ID})
	if found.ID != tree.Tree.ID {
		t.Fatal("wrong tree")
	}
	updated := call[protocol.Tree](t, c, "trees.update", protocol.UpdateTreeParams{TreeID: found.ID, ExpectedRevision: found.Revision, Metadata: protocol.TreeMetadata{Title: new("renamed")}})
	if updated.Revision != found.Revision+1 {
		t.Fatal("metadata revision did not advance")
	}
	configured := call[protocol.Session](t, c, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: tree.Root.ID, ExpectedRevision: tree.Root.ConfigRevision, Patch: protocol.ConfigPatch{Instructions: &protocol.Instructions{Text: "inherited"}}})
	var stale protocol.Session
	if err := c.Call(t.Context(), "sessions.configure", protocol.UpdateConfigurationParams{SessionID: tree.Root.ID, ExpectedRevision: tree.Root.ConfigRevision}, &stale); err == nil {
		t.Fatal("accepted stale configuration")
	}
	spawnParams := protocol.SpawnSessionParams{Identity: protocol.RequestIdentity{ClientID: "go", RequestID: "spawn"}, ParentID: tree.Root.ID, Parts: []protocol.Part{{Type: "text", Text: "initial child work"}}}
	spawned := call[protocol.SpawnSessionResult](t, c, "sessions.spawn", spawnParams)
	retried := call[protocol.SpawnSessionResult](t, c, "sessions.spawn", spawnParams)
	if spawned.Session == nil || spawned.Admission.Input == nil || retried.Session.ID != spawned.Session.ID || retried.Admission.Input.ID != spawned.Admission.Input.ID {
		t.Fatal("spawn did not return a stable child and initial input")
	}
	child := *spawned.Session
	if child.ParentID == nil || *child.ParentID != tree.Root.ID || child.Configuration.Instructions.Text != "inherited" {
		t.Fatalf("child=%+v", child)
	}
	list := call[protocol.ListSessionsResult](t, c, "sessions.list", protocol.ListSessionsParams{TreeID: tree.Tree.ID, Limit: 100})
	if len(list.Items) != 2 {
		t.Fatal("missing sessions")
	}
	definition := call[protocol.Definition](t, c, "definitions.register", protocol.DefinitionDocument{ID: "custom", Name: "custom"})
	loaded := call[protocol.Definition](t, c, "definitions.get", definition.Ref)
	if loaded.Ref != definition.Ref {
		t.Fatal("definition mismatch")
	}
	for _, s := range []protocol.Session{configured, child} {
		identity := protocol.RequestIdentity{ClientID: "go", RequestID: s.ID}
		params := protocol.SubmitParams{Identity: identity, SessionID: s.ID, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "hello"}}}
		first := call[protocol.Admission](t, c, "sessions.submit", params)
		duplicate := call[protocol.Admission](t, c, "sessions.submit", params)
		if first.Input.ID != duplicate.Input.ID || first.Turn != nil {
			t.Fatal("unstable admission")
		}
		history := call[protocol.HistoryResult](t, c, "sessions.history", protocol.HistoryParams{SessionID: s.ID, Limit: 100})
		if len(history.Items) != 0 {
			t.Fatal("history started execution")
		}
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []protocol.ID{tree.Root.ID, child.ID} {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		result, err := c.Wait(ctx, protocol.RequestIdentity{ClientID: "go", RequestID: id})
		if err != nil {
			t.Fatal(err)
		}
		if result.Turn.State != "succeeded" {
			t.Fatalf("outcome=%+v", result.Turn)
		}
		history := call[protocol.HistoryResult](t, c, "sessions.history", protocol.HistoryParams{SessionID: id, Limit: 100})
		expected := 2
		if id == child.ID {
			expected = 4
		}
		if len(history.Items) != expected || history.Items[len(history.Items)-1].Parts[0].Text != "ack: hello" {
			t.Fatalf("history=%+v", history)
		}
	}
	stopped := call[protocol.Session](t, c, "sessions.lifecycle", protocol.LifecycleParams{SessionID: child.ID, Lifecycle: "stopped"})
	if stopped.Lifecycle != "stopped" {
		t.Fatal("lifecycle")
	}
	call[protocol.DeleteResult](t, c, "sessions.delete", protocol.SessionParams{SessionID: child.ID})
	tombstone := call[protocol.Admission](t, c, "receipts.get", protocol.RequestIdentity{ClientID: "go", RequestID: child.ID})
	deletedSpawn := call[protocol.SpawnSessionResult](t, c, "sessions.spawn", spawnParams)
	if deletedSpawn.Session != nil || deletedSpawn.Admission.Receipt.DeletedAt == nil {
		t.Fatal("spawn retry resurrected deleted child")
	}
	if tombstone.Receipt.DeletedAt == nil || tombstone.Input != nil {
		t.Fatal("deleted receipt lost tombstone")
	}
}

func TestHandshakeRejectsWrongIdentityAndVersionBeforeOperations(t *testing.T) {
	r, c := fixture(t)
	if second, err := rpc.Listen(r); err == nil {
		_ = second.Close()
		t.Fatal("replaced live listener")
	}
	wrong := protocol.ID("other")
	_, err := client.Connect(t.Context(), r.SocketPath(), &wrong)
	var remote *client.Error
	if !errors.As(err, &remote) || remote.Kind != "IDENTITY" {
		t.Fatalf("wrong identity: %v", err)
	}
	for _, request := range []string{
		`{"jsonrpc":"2.0","id":"x","method":"initialize","params":{"major":3}}`,
		`{"jsonrpc":"2.0","id":"x","method":"trees.get","params":{"tree_id":"missing"}}`,
	} {
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", r.SocketPath())
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write([]byte(request + "\n")); err != nil {
			t.Fatal(err)
		}
		var response protocol.Response
		if err := json.NewDecoder(conn).Decode(&response); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		if response.Error == nil || response.Error.Kind != "INVALID" {
			t.Fatalf("bad handshake accepted: %+v", response)
		}
	}
	if c.Identity() != protocol.ID(r.Identity()) {
		t.Fatal("wrong identity")
	}
}

func TestDisconnectAfterSubmissionDoesNotCancelExecution(t *testing.T) {
	r, c := fixture(t)
	tree := create(t, c)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", r.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(map[string]any{"jsonrpc": "2.0", "id": "init", "method": "initialize", "params": map[string]int{"major": 4}}); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(conn).ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	identity := protocol.RequestIdentity{ClientID: "go", RequestID: "lost-ack"}
	params := protocol.SubmitParams{Identity: identity, SessionID: tree.Root.ID, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "detached"}}}
	if err := json.NewEncoder(conn).Encode(map[string]any{"jsonrpc": "2.0", "id": "submit", "method": "sessions.submit", "params": params}); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		var value protocol.Admission
		err := c.Call(ctx, "receipts.get", identity, &value)
		if err == nil {
			break
		}
		var remote *client.Error
		if !errors.As(err, &remote) || remote.Kind != "NOT_FOUND" {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	result, err := c.Wait(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	if result.Turn.State != "succeeded" {
		t.Fatalf("outcome=%+v", result.Turn)
	}
	duplicate := call[protocol.Admission](t, c, "sessions.submit", params)
	if duplicate.Input.ID != result.Input.ID {
		t.Fatal("retry duplicated input")
	}
}

func TestSocketPathDoesNotReplaceFiles(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "whip-v4-") //nolint:usetesting // macOS t.TempDir paths exceed the Unix socket path limit; cleanup is registered below.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	r, err := runtime.Open(t.Context(), directory, model.Scripted{}, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	path := filepath.Join(directory, "runtime.sock")
	if err := os.WriteFile(path, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	if server, err := rpc.Listen(r); err == nil {
		_ = server.Close()
		t.Fatal("replaced ordinary file")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "retained" {
		t.Fatalf("file changed: %q %v", raw, err)
	}
}
