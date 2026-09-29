package rpc_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestHostOperationRPCModelFreeSchemasPermissionAndExactRecovery(t *testing.T) {
	r, c := fixture(t)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	tree := call[protocol.CreateTreeResult](t, c, "trees.create", protocol.CreateTreeParams{CreationID: "model-free", Engine: "quickjs", Definition: c.Builtins()[0], WorkingDirectory: t.TempDir()})
	if tree.Root.Configuration.Model.Provider != "" {
		t.Fatal(tree)
	}
	if err := os.WriteFile(filepath.Join(tree.Root.WorkingDirectory, "file"), []byte("wire direct result"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := call[protocol.HostToolSchemasResult](t, c, "tool.schemas", protocol.SessionParams{SessionID: tree.Root.ID})
	if len(catalog.Items) != 14 {
		t.Fatal(catalog)
	}
	request := protocol.CallHostToolParams{Identity: protocol.RequestIdentity{ClientID: "human", RequestID: "stable"}, SessionID: tree.Root.ID, Operation: protocol.DirectHostInput{Module: "files", Name: "read", ArgumentsBase64: base64.StdEncoding.EncodeToString([]byte(`{"path":"file"}`))}}
	accepted := call[protocol.Admission](t, c, "tool.call", request)
	if accepted.Input.HostOperation == nil || accepted.Input.Kind != "host_operation" || len(accepted.Input.Parts) != 0 {
		t.Fatal(accepted)
	}
	var op protocol.HostOperation
	deadline := time.Now().Add(5 * time.Second)
	for {
		permissions := call[protocol.PermissionsResult](t, c, "permissions.list", protocol.PermissionsParams{SessionID: tree.Root.ID, Limit: 10})
		if len(permissions.Items) > 0 {
			op = call[protocol.HostOperation](t, c, "operations.get", protocol.HostOperationParams{OperationID: permissions.Items[0].OperationID})
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("permission missing")
		}
		time.Sleep(time.Millisecond)
	}
	if op.CellID != nil || op.Origin != "host_operation" || op.State != "waiting" {
		t.Fatal(op)
	}
	call[protocol.Permission](t, c, "permissions.resolve", protocol.ResolvePermissionParams{OperationID: op.ID, Approved: true})
	var finished protocol.Admission
	for {
		finished = call[protocol.Admission](t, c, "receipts.get", request.Identity)
		if finished.Turn != nil && finished.Turn.FinishedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("direct completion missing")
		}
		time.Sleep(time.Millisecond)
	}
	if finished.Turn.State != "succeeded" {
		t.Fatal(finished)
	}
	operations := call[protocol.HostOperationsResult](t, c, "turns.operations", protocol.HostOperationsParams{TurnID: finished.Turn.ID, Limit: 10})
	if len(operations.Items) != 1 || !strings.Contains(string(operations.Items[0].Result.Value), "wire direct result") {
		t.Fatal(operations)
	}
	if again := call[protocol.Admission](t, c, "tool.call", request); again.Input.ID != accepted.Input.ID {
		t.Fatal("duplicate admission")
	}
	request.Operation.Name = "write"
	var response json.RawMessage
	var wire *client.Error
	if err := c.Call(t.Context(), "tool.call", request, &response); !errors.As(err, &wire) || wire.Kind != "CONFLICT" {
		t.Fatal(err)
	}
	request.Identity.RequestID = "forged"
	request.Operation.Module = "agents"
	request.Operation.Name = "spawn"
	if err := c.Call(t.Context(), "tool.call", request, &response); err == nil {
		t.Fatal("arbitrary coordination exposed")
	}
}
