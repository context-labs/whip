package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestBothEnginesFilesUseScopedAuthorityAndOneUsePermissions(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{
				"edit":         "files.write(path=\"note.txt\", content=\"alpha\")\nfiles.patch(path=\"note.txt\", old_text=\"alpha\", new_text=\"beta\")\nprint(files.read(path=\"note.txt\")[\"output\"])",
				"write-again":  "files.write(path=\"note.txt\", content=\"unapproved\")",
				"read-revoked": "print(files.read(path=\"note.txt\")[\"output\"])",
			}
			if engine == session.QuickJS {
				codes = map[string]string{
					"edit":         "await files.write({path: 'note.txt', content: 'alpha'}); await files.patch({path: 'note.txt', old_text: 'alpha', new_text: 'beta'}); console.log((await files.read({path: 'note.txt'})).output)",
					"write-again":  "await files.write({path: 'note.txt', content: 'unapproved'})",
					"read-revoked": "console.log((await files.read({path: 'note.txt'})).output)",
				}
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(codes))
			current := createEngineSession(t, r, engine)
			grant, err := r.CreateGrant(t.Context(), session.Grant{ID: "read-workspace", SessionID: current.ID, Capability: "files.read", Resource: current.WorkingDirectory})
			if err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, current.ID, "edit")
			write := awaitRuntimeFilePermission(t, r, current.ID, "edit", "files.write")
			path := filepath.Join(current.WorkingDirectory, "note.txt")
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("write ran before approval: %v", err)
			}
			if _, err := r.ResolvePermission(t.Context(), write.ID, true); err != nil {
				t.Fatal(err)
			}
			patch := awaitRuntimeFilePermission(t, r, current.ID, "edit", "files.patch")
			assertRuntimeFileBytes(t, path, "alpha")
			if _, err := r.ResolvePermission(t.Context(), patch.ID, true); err != nil {
				t.Fatal(err)
			}
			finished := waitTestWithin(t, r, "edit", terminal, 30*time.Second)
			if finished.Turn.State != session.Succeeded {
				t.Fatalf("approved files cell: %+v runtime=%v", finished.Turn, r.Err())
			}
			assertRuntimeFileBytes(t, path, "beta")
			operations, err := r.Operations(t.Context(), finished.Turn.ID, "", 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(operations) != 3 {
				t.Fatalf("missing host effect evidence: %+v", operations)
			}
			for _, operation := range operations {
				if operation.SessionID != current.ID || operation.TurnID != finished.Turn.ID || operation.CellID != write.CellID || operation.Resource != current.WorkingDirectory || operation.State != session.OperationSucceeded || operation.DispatchedAt == nil || operation.FinishedAt == nil || operation.Result == nil || operation.GrantID == nil {
					t.Fatalf("incorrect operation ownership or settlement: %+v", operation)
				}
				if operation.Capability == "files.read" && *operation.GrantID != grant.ID {
					t.Fatalf("read did not use standing authority: %+v", operation)
				}
			}
			grants, err := r.Grants(t.Context(), current.ID, "", 100)
			if err != nil {
				t.Fatal(err)
			}
			approved := map[session.OperationID]bool{write.ID: false, patch.ID: false}
			for _, permissionGrant := range grants {
				if permissionGrant.OperationID != nil {
					if _, ok := approved[*permissionGrant.OperationID]; !ok {
						t.Fatalf("approval widened to unrelated operation: %+v", permissionGrant)
					}
					approved[*permissionGrant.OperationID] = true
				}
			}
			if len(grants) != 3 || !approved[write.ID] || !approved[patch.ID] {
				t.Fatalf("approvals did not create distinct one-use grants: %+v", grants)
			}
			cell, err := r.Cell(t.Context(), write.CellID)
			if err != nil {
				t.Fatal(err)
			}
			if cell.State != session.CellSucceeded || cell.Checkpoint == nil || cell.ResultMessageID == nil {
				t.Fatalf("host operations missing settled cell boundary: %+v", cell)
			}
			history, err := r.History(t.Context(), current.ID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			var output struct {
				Result process.Result `json:"result"`
			}
			if err := json.Unmarshal([]byte(history[len(history)-1].Parts[0].Text), &output); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.Result.Output, "1\tbeta") {
				t.Fatalf("read did not return patched bytes to the model: %+v", output.Result)
			}

			submitTest(t, r, current.ID, "write-again")
			nextWrite := awaitRuntimeFilePermission(t, r, current.ID, "write-again", "files.write")
			if nextWrite.ID == write.ID || nextWrite.GrantID != nil {
				t.Fatalf("later write reused one-use authority: %+v", nextWrite)
			}
			denyRuntimeFilePermission(t, r, nextWrite, "write-again")
			assertRuntimeFileBytes(t, path, "beta")
			revoked, err := r.RevokeGrant(t.Context(), grant.ID)
			if err != nil {
				t.Fatal(err)
			}
			if revoked.RevokedAt == nil {
				t.Fatal("standing grant revocation was not recorded")
			}
			submitTest(t, r, current.ID, "read-revoked")
			read := awaitRuntimeFilePermission(t, r, current.ID, "read-revoked", "files.read")
			if read.GrantID != nil {
				t.Fatalf("read reused revoked standing authority: %+v", read)
			}
			denyRuntimeFilePermission(t, r, read, "read-revoked")
		})
	}
}

func TestBothEnginesCancelPendingFilePermissionReleasesWorker(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{"blocked": "files.write(path=\"cancelled.txt\", content=\"unapproved\")", "next-session": "print(7)"}
			if engine == session.QuickJS {
				codes = map[string]string{"blocked": "await files.write({path: 'cancelled.txt', content: 'unapproved'})", "next-session": "console.log(7)"}
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(codes))
			current := createEngineSession(t, r, engine)
			submitTest(t, r, current.ID, "blocked")
			pending := awaitRuntimeFilePermission(t, r, current.ID, "blocked", "files.write")
			if _, err := r.CancelTurn(t.Context(), pending.TurnID); err != nil {
				t.Fatal(err)
			}
			finished := waitTestWithin(t, r, "blocked", terminal, 30*time.Second)
			if finished.Turn.State != session.Cancelled {
				t.Fatalf("pending operation prevented turn cancellation: %+v", finished.Turn)
			}
			operation, err := r.Operation(t.Context(), pending.ID)
			if err != nil {
				t.Fatal(err)
			}
			if operation.State != session.OperationCancelled || operation.DispatchedAt != nil || operation.Result == nil {
				t.Fatalf("cancelled permission dispatched or lost evidence: %+v", operation)
			}
			permissions, err := r.Permissions(t.Context(), current.ID, "", 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(permissions) != 1 || permissions[0].State != session.PermissionCancelled || permissions[0].ResolvedAt == nil {
				t.Fatalf("cancelled turn left pending permission: %+v", permissions)
			}
			if _, err := r.ResolvePermission(t.Context(), pending.ID, true); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("late permission revived cancelled operation: %v", err)
			}
			if _, err := os.Stat(filepath.Join(current.WorkingDirectory, "cancelled.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("cancelled operation wrote bytes: %v", err)
			}
			cell, err := r.Cell(t.Context(), pending.CellID)
			if err != nil {
				t.Fatal(err)
			}
			if cell.ResultMessageID == nil || (cell.State != session.CellUncertain && cell.State != session.CellFailed) {
				t.Fatalf("cancelled cell lost its failed execution boundary: %+v", cell)
			}
			if cell.State == session.CellUncertain && cell.Checkpoint != nil {
				t.Fatalf("uncertain cell exposed a reusable checkpoint: %+v", cell)
			}
			// Host cancellation can reach the guest as a language error before
			// process cancellation wins. A correlated final result may retain its
			// exact checkpoint; a terminated worker must never retain one.
			if cell.Checkpoint != nil {
				entry, err := r.kernel(t.Context(), current.ID)
				if err != nil {
					t.Fatal(err)
				}
				restored, err := entry.checkpoints.Load(t.Context())
				if err != nil || restored == nil {
					t.Fatalf("settled cancellation checkpoint cannot be restored: %v", err)
				}
			}
			// There is only one kernel slot; a new session proves cancellation
			// released the worker lease as well as the pending permission.
			next := createEngineSession(t, r, engine)
			runCellTurn(t, r, next.ID, "next-session", "7\n")
			if err := r.Err(); err != nil {
				t.Fatalf("permission cancellation faulted the runtime: %v", err)
			}
		})
	}
}

func awaitRuntimeFilePermission(t *testing.T, r *Runtime, id session.SessionID, key, capability string) session.Operation {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		permissions, err := r.Permissions(ctx, id, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, permission := range permissions {
			if permission.State != session.PermissionPending {
				continue
			}
			operation, err := r.Operation(ctx, permission.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			if operation.Capability != capability || operation.State != session.OperationWaiting || operation.DispatchedAt != nil {
				t.Fatalf("unexpected pending host operation: %+v", operation)
			}
			return operation
		}
		a, err := r.Admission(ctx, session.RequestIdentity{ClientID: "test", RequestID: key})
		if err != nil {
			t.Fatal(err)
		}
		if terminal(a) {
			t.Fatalf("turn settled before %s permission: %+v runtime=%v", capability, a.Turn, r.Err())
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for %s permission: %+v runtime=%v", capability, permissions, r.Err())
		case <-ticker.C:
		}
	}
}

func denyRuntimeFilePermission(t *testing.T, r *Runtime, operation session.Operation, key string) {
	t.Helper()
	permission, err := r.ResolvePermission(t.Context(), operation.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if permission.State != session.PermissionDenied || permission.ResolvedAt == nil {
		t.Fatalf("denial was not durably resolved: %+v", permission)
	}
	waitTestWithin(t, r, key, terminal, 30*time.Second)
	denied, err := r.Operation(t.Context(), operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if denied.State != session.OperationDenied || denied.DispatchedAt != nil || denied.Result == nil || denied.Result.Failure == nil {
		t.Fatalf("denial executed an operation or lost its reason: %+v", denied)
	}
	cell, err := r.Cell(t.Context(), operation.CellID)
	if err != nil {
		t.Fatal(err)
	}
	if cell.State != session.CellFailed || cell.Checkpoint == nil || cell.ResultMessageID == nil {
		t.Fatalf("denied host operation did not settle as a guest error: %+v", cell)
	}
}

func assertRuntimeFileBytes(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("file bytes=%q want=%q", data, want)
	}
}
