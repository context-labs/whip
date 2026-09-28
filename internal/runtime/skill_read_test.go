package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func skillReadFixture(t *testing.T) (*Runtime, session.Session, session.Cell, string) {
	t.Helper()
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner := createTest(t, r)
	hostRoot := t.TempDir()
	r.host.SkillRoots = map[string]string{"shared": hostRoot, "other": t.TempDir()}
	policy := session.Instructions{SkillRoots: []string{"shared", "unregistered"}}
	owner, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy})
	if err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, owner.ID, "skill-read")
	claimed, err := r.store.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	message, err := r.store.AppendMessage(t.Context(), claimed.Turn.ID, session.MessageDraft{ID: "skill_call", Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{"code":"1"}`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	cell, dispatch, err := r.store.BeginCell(t.Context(), session.CellSpec{ID: "skill_cell", TurnID: claimed.Turn.ID, CallMessageID: message.ID, CallID: "call"})
	if err != nil || !dispatch {
		t.Fatalf("begin skill cell: %v %v", dispatch, err)
	}
	return r, owner, cell, hostRoot
}

func writeHostSkill(t *testing.T, root, directory, text string) string {
	t.Helper()
	path := filepath.Join(root, directory, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeInstructionFile(t, path, text)
	return path
}

func skillInvocation(owner session.Session, cell session.Cell, id string, args map[string]any) tool.Invocation {
	return tool.Invocation{SessionID: owner.ID, CellID: cell.ID, RequestID: id, Module: "skills", Name: "read", Arguments: args}
}

func skillGrant(t *testing.T, r *Runtime, owner session.Session, host bool) session.Grant {
	t.Helper()
	capability, resource := "files.read", owner.WorkingDirectory
	if host {
		capability, resource = "skills.read", "shared"
	}
	grant, err := r.CreateGrant(t.Context(), session.Grant{ID: "skill_grant", SessionID: owner.ID, Capability: capability, Resource: resource})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func TestSkillReadCapturedHostPolicyAndInvalidRequests(t *testing.T) {
	r, owner, cell, root := skillReadFixture(t)
	writeHostSkill(t, root, "one", "---\nname: exact\ndescription: host\n---\nHOST BODY")
	policy := session.Instructions{SkillRoots: []string{"other"}}
	current, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := r.prepareSkillRead(t.Context(), current, skillInvocation(current, cell, "old-policy", map[string]any{"root_id": "shared", "name": "exact"}))
	if err != nil || prepared.Capability != "skills.read" || prepared.Resource != "shared" || prepared.Mutating || prepared.Apply != nil {
		t.Fatalf("captured policy lost: %+v %v", prepared, err)
	}
	if strings.Contains(string(prepared.Arguments), root) {
		t.Fatal("operation intent leaked absolute host path")
	}
	for _, args := range []map[string]any{
		{"root_id": "other", "name": "exact"},
		{"root_id": "unregistered", "name": "exact"},
		{"root_id": "../../escape", "name": "exact"},
		{"root_id": "", "name": "exact"},
		{"root_id": "shared", "name": "exact", "path": "../../escape"},
		{"name": ""},
		{"name": "../escape"},
		{"name": strings.Repeat("a", 65)},
		{"name": "exact", "offset": "1"},
		{"name": "exact", "offset": -1},
		{"name": "exact", "offset": "-1"},
		{"name": "exact", "offset": "262145", "sha256": strings.Repeat("a", 64)},
		{"name": "exact", "length": 0},
		{"name": "exact", "length": 65537},
		{"name": "exact", "sha256": strings.Repeat("A", 64)},
		{"name": "exact", "sha256": "abcd"},
	} {
		if _, err := r.prepareSkillRead(t.Context(), current, skillInvocation(current, cell, "invalid", args)); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid request accepted: %#v %v", args, err)
		}
	}
	other := createTest(t, r)
	if _, err := r.prepareSkillRead(t.Context(), other, skillInvocation(other, cell, "foreign", map[string]any{"name": "exact"})); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("foreign cell accepted: %v", err)
	}
	operations, err := r.Operations(t.Context(), cell.TurnID, "", 100)
	if err != nil || len(operations) != 0 {
		t.Fatalf("preparation admitted an operation: %+v %v", operations, err)
	}
	skillGrant(t, r, owner, true)
	value, _, err := tool.NewDispatcher(r.store, r.store, r).Call(t.Context(), skillInvocation(owner, cell, "read-old-policy", map[string]any{"root_id": "shared", "name": "exact"}))
	if err != nil || value.(skillReadPage).Name != "exact" {
		t.Fatalf("old captured root cannot dispatch after config edit: %+v %v", value, err)
	}
}

func TestSkillReadWorkspaceBytePagesAndDigestMutation(t *testing.T) {
	r, owner, cell, _ := skillReadFixture(t)
	header := "---\nname: exact\ndescription: workspace\n---\n"
	text := header + strings.Repeat("🦊é", 13000)
	path := writeRuntimeSkill(t, owner.WorkingDirectory, "z", header, strings.TrimPrefix(text, header))
	writeRuntimeSkill(t, owner.WorkingDirectory, "a", "---\nname: exact\ndescription: loser\n---\n", "LOSER")
	skillGrant(t, r, owner, false)
	dispatcher := tool.NewDispatcher(r.store, r.store, r)
	read := func(id string, args map[string]any) (skillReadPage, session.OperationID, error) {
		value, operation, err := dispatcher.Call(t.Context(), skillInvocation(owner, cell, id, args))
		if err != nil {
			return skillReadPage{}, operation, err
		}
		return value.(skillReadPage), operation, nil
	}
	first, operation, err := read("first", map[string]any{"root_id": nil, "name": "exact"})
	if err != nil || first.Offset != 0 || first.TotalBytes != int64(len(text)) || first.NextOffset == nil || *first.NextOffset != 65536 || first.Source.Path != ".agents/skills/z/SKILL.md" || first.Source.Scope != "workspace" {
		t.Fatalf("first page=%+v err=%v", first, err)
	}
	digest := sha256.Sum256([]byte(text))
	if first.SHA256 != hex.EncodeToString(digest[:]) || first.Source.SHA256 != first.SHA256 {
		t.Fatal("page digest did not cover the complete file")
	}
	a, err := base64.StdEncoding.DecodeString(first.DataBase64)
	if err != nil || string(a) != text[:65536] {
		t.Fatal("first page did not preserve raw UTF-8 bytes", err)
	}
	second, _, err := read("second", map[string]any{"name": "exact", "offset": strconv.FormatInt(*first.NextOffset, 10), "sha256": first.SHA256})
	if err != nil || second.NextOffset != nil || second.SHA256 != first.SHA256 {
		t.Fatalf("second page=%+v err=%v", second, err)
	}
	b, err := base64.StdEncoding.DecodeString(second.DataBase64)
	if err != nil || string(append(a, b...)) != text {
		t.Fatal("pages did not reconstruct exact Unicode file", err)
	}
	last, _, err := read("empty-end", map[string]any{"name": "exact", "offset": strconv.Itoa(len(text)), "sha256": first.SHA256})
	if err != nil || last.DataBase64 != "" || last.NextOffset != nil {
		t.Fatalf("offset at end failed: %+v %v", last, err)
	}
	if _, _, err := read("past-end", map[string]any{"name": "exact", "offset": strconv.Itoa(len(text) + 1), "sha256": first.SHA256}); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("past-end offset accepted: %v", err)
	}
	writeInstructionFile(t, path, text+"changed after the first page")
	if value, _, err := dispatcher.Call(t.Context(), skillInvocation(owner, cell, "changed", map[string]any{"name": "exact", "offset": "1", "length": 1, "sha256": first.SHA256})); err == nil || value != nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed full file returned partial page: %+v %v", value, err)
	}
	stored, err := r.Operation(t.Context(), operation)
	if err != nil || stored.State != session.OperationSucceeded || stored.Result == nil || strings.Contains(string(stored.Result.Value), "🦊") || strings.Contains(string(stored.Result.Value), owner.WorkingDirectory) {
		t.Fatalf("operation lost bounded base64 evidence: %+v %v", stored, err)
	}
	writeInstructionFile(t, path, header+strings.Repeat("x", session.MaxInvokedSkillBytes))
	if value, _, err := dispatcher.Call(t.Context(), skillInvocation(owner, cell, "oversized", map[string]any{"name": "exact"})); err == nil || value != nil {
		t.Fatalf("oversized skill returned data: %+v %v", value, err)
	}
}

type skillReadLedger struct {
	*store.Store
	admitted chan session.Operation
	dispatch func(context.Context, session.OperationID) error
}

func (l skillReadLedger) AdmitOperation(ctx context.Context, spec session.OperationSpec) (session.Operation, error) {
	operation, err := l.Store.AdmitOperation(ctx, spec)
	if err == nil && l.admitted != nil {
		l.admitted <- operation
	}
	return operation, err
}

func (l skillReadLedger) DispatchOperation(ctx context.Context, id session.OperationID) (bool, error) {
	if l.dispatch != nil {
		if err := l.dispatch(ctx, id); err != nil {
			return false, err
		}
	}
	return l.Store.DispatchOperation(ctx, id)
}

func TestSkillReadStandingAuthorityRechecksRevocationAndCancellation(t *testing.T) {
	for _, mode := range []string{"standing", "revoked", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			r, owner, cell, root := skillReadFixture(t)
			writeHostSkill(t, root, "exact", "---\nname: exact\ndescription: host\n---\nbody")
			grant := skillGrant(t, r, owner, true)
			ledger := skillReadLedger{Store: r.store, dispatch: func(ctx context.Context, _ session.OperationID) error {
				switch mode {
				case "revoked":
					_, err := r.store.RevokeGrant(ctx, grant.ID)
					return err
				case "cancelled":
					_, err := r.store.CancelTurn(ctx, cell.TurnID)
					return err
				default:
					return nil
				}
			}}
			value, id, err := tool.NewDispatcher(ledger, r.store, r).Call(t.Context(), skillInvocation(owner, cell, mode, map[string]any{"root_id": "shared", "name": "exact"}))
			operation, readErr := r.Operation(t.Context(), id)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "standing" {
				if err != nil || value == nil || operation.State != session.OperationSucceeded || operation.DispatchedAt == nil {
					t.Fatalf("authorized read failed: %+v %v", operation, err)
				}
			} else {
				want := session.OperationCancelled
				if mode == "revoked" {
					want = session.OperationDenied
				}
				if err == nil || value != nil || operation.DispatchedAt != nil || operation.State != want {
					t.Fatalf("stopped authority still dispatched: %+v %v", operation, err)
				}
			}
		})
	}
}

func TestSkillReadOneUsePermissionAndPendingCancellation(t *testing.T) {
	for _, mode := range []string{"approve", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			r, owner, cell, root := skillReadFixture(t)
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			admitted := make(chan session.Operation, 2)
			dispatcher := tool.NewDispatcher(skillReadLedger{Store: r.store, admitted: admitted}, r.store, r)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, _, err := dispatcher.Call(ctx, skillInvocation(owner, cell, "permission", map[string]any{"root_id": "shared", "name": "exact"}))
				result <- err
			}()
			var operation session.Operation
			select {
			case operation = <-admitted:
			case <-ctx.Done():
				t.Fatal("operation was not admitted", ctx.Err())
			}
			if operation.State != session.OperationWaiting || operation.GrantID != nil {
				t.Fatal("missing grant bypassed permission", operation)
			}
			if mode == "cancel" {
				cancel()
			} else {
				// Opening the root before permission would already have failed.
				writeHostSkill(t, root, "exact", "---\nname: exact\ndescription: host\n---\nbody")
				if _, err := r.ResolvePermission(ctx, operation.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			err := <-result
			if (mode == "approve") != (err == nil) {
				t.Fatalf("permission result: %v", err)
			}
			stored, err := r.Operation(t.Context(), operation.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "cancel" {
				if stored.State != session.OperationCancelled || stored.DispatchedAt != nil {
					t.Fatal("pending cancellation read a skill", stored)
				}
				return
			}
			if stored.State != session.OperationSucceeded || stored.GrantID == nil {
				t.Fatal("one-use approval did not dispatch", stored)
			}
			go func() {
				_, _, err := dispatcher.Call(ctx, skillInvocation(owner, cell, "second-permission", map[string]any{"root_id": "shared", "name": "exact"}))
				result <- err
			}()
			select {
			case operation = <-admitted:
			case <-ctx.Done():
				t.Fatal("second operation was not admitted", ctx.Err())
			}
			if operation.State != session.OperationWaiting || operation.GrantID != nil {
				t.Fatal("one-use approval became standing authority", operation)
			}
			if _, err := r.ResolvePermission(ctx, operation.ID, false); err != nil {
				t.Fatal(err)
			}
			if err := <-result; err == nil {
				t.Fatal("denied second read succeeded")
			}
		})
	}
}

type skillReadPrepared tool.Prepared

func (p skillReadPrepared) PrepareCoordination(context.Context, session.Session, tool.Invocation) (tool.Prepared, error) {
	return tool.Prepared(p), nil
}

func TestSkillReadAcquiredRootClosesOnEveryExit(t *testing.T) {
	for _, mode := range []string{"success", "missing", "cancelled", "dispatch-refused"} {
		t.Run(mode, func(t *testing.T) {
			r, owner, cell, path := skillReadFixture(t)
			writeHostSkill(t, path, "exact", "---\nname: exact\ndescription: host\n---\nbody")
			skillGrant(t, r, owner, true)
			execution := &skillReadExecution{path: path, rootID: "shared", request: skillReadRequest{Name: "exact", Length: 5}}
			if mode == "missing" {
				execution.request.Name = "missing"
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var opened *os.Root
			prepared := skillReadPrepared{
				Capability: "skills.read", Resource: "shared", Arguments: json.RawMessage(`{"name":"exact"}`),
				Acquire: func(ctx context.Context) (func(), error) {
					release, err := execution.acquire(ctx)
					opened = execution.root
					return release, err
				},
				Run: func(ctx context.Context) (any, error) {
					if mode == "cancelled" {
						cancel()
					}
					return execution.run(ctx)
				},
			}
			ledger := skillReadLedger{Store: r.store, dispatch: func(ctx context.Context, _ session.OperationID) error {
				if mode == "dispatch-refused" {
					_, err := r.store.CancelTurn(ctx, cell.TurnID)
					return err
				}
				return nil
			}}
			_, _, err := tool.NewDispatcher(ledger, r.store, prepared).Call(ctx, skillInvocation(owner, cell, mode, map[string]any{}))
			if (mode == "success") != (err == nil) || opened == nil {
				t.Fatalf("dispatch=%v acquired=%v", err, opened != nil)
			}
			if _, err := opened.Stat("."); err == nil || execution.root != nil {
				t.Fatal("dispatcher left acquired descriptor open", err)
			}
		})
	}
	execution := &skillReadExecution{path: filepath.Join(t.TempDir(), "private-unavailable")}
	if _, err := execution.acquire(t.Context()); err == nil || strings.Contains(err.Error(), execution.path) {
		t.Fatalf("unavailable root error leaked host path: %v", err)
	}
}
