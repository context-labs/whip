package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

// PrepareCoordination resolves a guest request to immutable, owner-bound SQL
// intent. The store rechecks the persisted intent and authority at application.
func (r *Runtime) PrepareCoordination(ctx context.Context, current session.Session, call tool.Invocation) (tool.Prepared, error) {
	if call.Module == "permissions" {
		return r.preparePermissionInspection(current, call)
	}
	if call.Module == "browser" {
		return r.prepareBrowser(ctx, current, call)
	}
	if call.Module == "computer" {
		return r.prepareComputer(ctx, current, call)
	}
	if call.Module == "shell" {
		return r.prepareShell(current, call)
	}
	if call.Module == "user" {
		return r.prepareQuestion(current, call)
	}
	if call.Module == "tools" {
		return r.prepareCustomTool(ctx, current, call)
	}
	if call.Module == "mcp" {
		return r.prepareMCP(ctx, current, call)
	}
	if call.Module == "models" {
		return r.prepareModel(ctx, current, call)
	}
	if call.Module == "goals" {
		return r.prepareGoal(current, call)
	}
	if call.Module == "schedules" {
		return r.prepareSchedule(current, call)
	}
	if call.Module == "skills" {
		return r.prepareSkillRead(ctx, current, call)
	}
	if call.Module == "context" {
		return r.prepareHistory(ctx, current, call)
	}
	if call.Module == "artifacts" {
		return r.prepareArtifact(current, call)
	}
	if call.Module == "state" {
		return r.prepareState(ctx, current, call)
	}
	if call.Module == "mail" {
		return r.prepareMail(current, call)
	}
	if call.Module == "agents" {
		switch call.Name {
		case "pending_reports", "read_report":
			return r.prepareCompletionRead(current, call)
		case "submit", "inspect", "list", "stop", "delete":
			return r.prepareChildControl(current, call)
		}
	}
	if call.Module == "agents" && call.Name == "wait_after_cell" {
		var request store.ChildWait
		if err := decodeArguments(call.Arguments, &request); err != nil {
			return tool.Prepared{}, err
		}
		arguments, err := json.Marshal(request)
		if err != nil {
			return tool.Prepared{}, err
		}
		return tool.Prepared{Capability: "agents.wait_after_cell", Resource: string(current.TreeID), Arguments: arguments, Apply: func(ctx context.Context, id session.OperationID) (any, error) {
			return r.store.RegisterChildWait(ctx, id)
		}}, nil
	}
	if call.Module != "agents" || call.Name != "spawn" {
		return tool.Prepared{}, fmt.Errorf("%w: unsupported host operation %s.%s", session.ErrInvalid, call.Module, call.Name)
	}
	request, err := parseSpawn(current.ID, call.Arguments)
	if err != nil {
		return tool.Prepared{}, err
	}
	if len(request.BrowserAttachments) != 0 {
		return r.prepareBrowserTransfer(ctx, current, call, request)
	}
	arguments, err := json.Marshal(request)
	if err != nil {
		return tool.Prepared{}, err
	}
	return tool.Prepared{Capability: "agents.spawn", Resource: string(current.TreeID), Arguments: arguments, Apply: func(ctx context.Context, id session.OperationID) (any, error) {
		admitted, err := r.store.SpawnChildOperation(ctx, id)
		if err != nil {
			return nil, err
		}
		r.Wake()
		if admitted.Session == nil || admitted.Admission.Input == nil {
			return nil, errors.New("child admission was deleted")
		}
		return map[string]any{"session_id": string(admitted.Session.ID), "input_id": string(admitted.Admission.Input.ID)}, nil
	}}, nil
}

func (r *Runtime) waitAfterCell(ctx context.Context, turn session.Turn, cell session.CellID) error {
	inputs, err := r.store.CellWaitInputs(ctx, cell)
	if err != nil || len(inputs) == 0 {
		return err
	}
	complete, err := r.store.ChildInputsComplete(ctx, turn.SessionID, inputs)
	if err != nil || complete {
		return err
	}
	return r.withReleasedWorker(ctx, turn.ID, func(ctx context.Context) error {
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			complete, err := r.store.ChildInputsComplete(ctx, turn.SessionID, inputs)
			if err != nil || complete {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	})
}

func decodeArguments(arguments map[string]any, target any) error {
	raw, err := json.Marshal(arguments)
	if err != nil {
		return fmt.Errorf("%w: %w", session.ErrInvalid, err)
	}
	if len(raw) > session.MaxDocumentBytes {
		return fmt.Errorf("%w: arguments exceed limit", session.ErrInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %w", session.ErrInvalid, err)
	}
	return nil
}
