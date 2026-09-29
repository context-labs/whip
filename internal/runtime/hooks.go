package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/executor"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

// BeforeTool uses only the active cell's captured configuration. A rewrite
// returns to ordinary preparation and permission admission; it supplies no grant.
func (r *Runtime) BeforeTool(ctx context.Context, current session.Session, call tool.Invocation) (tool.Invocation, error) {
	if len(current.Config.Hooks) == 0 {
		return call, nil
	}
	turn, err := r.invocationTurn(ctx, current.ID, call)
	if err != nil {
		return call, err
	}
	name := call.Module + "." + call.Name
	if hook, ok := current.Config.Hooks["before_tool"]; ok && (hook.Operations == nil || slices.Contains(hook.Operations, name)) {
		arguments := call.Arguments
		if arguments == nil {
			arguments = map[string]any{}
		}
		raw, err := json.Marshal(arguments)
		if err != nil {
			return call, err
		}
		request := executor.Request{SessionID: current.ID, TurnID: turn, CellID: call.CellID, HostOperation: call.DirectTurnID != "", Operation: name, Arguments: raw}
		result, err := r.askHook(ctx, current, "before_tool", hook, request)
		if err != nil {
			return call, err
		}
		if result.Arguments != nil {
			rewritten, err := hookArguments(result.Arguments)
			if err != nil {
				return call, err
			}
			call.Arguments = rewritten
			r.hookDecision(current.ID, turn, HookDecision{InvocationID: result.InvocationID, Hook: "before_tool", Operation: name, Decision: "rewrite", Reason: result.Reason})
			r.hookNotice(current.ID, turn, "Hook before_tool rewrote "+name+" arguments to "+boundedHookText(string(result.Arguments), 512))
		}
	}
	if call.Module != "agents" || call.Name != "spawn" {
		return call, nil
	}
	hook, ok := current.Config.Hooks["before_spawn"]
	if !ok {
		return call, nil
	}
	request, err := parseSpawn(current.ID, call.Arguments)
	if err != nil {
		return call, err
	}
	resolved, err := r.store.PreviewChild(ctx, call.CellID, request)
	if err != nil {
		return call, err
	}
	preview, err := json.Marshal(struct {
		Request  map[string]any     `json:"request"`
		Resolved store.ChildPreview `json:"resolved"`
	}{call.Arguments, resolved})
	if err != nil {
		return call, err
	}
	result, err := r.askHook(ctx, current, "before_spawn", hook, executor.Request{SessionID: current.ID, TurnID: turn, CellID: call.CellID, Operation: "agents.spawn", Spawn: preview})
	if err != nil {
		return call, err
	}
	if result.Spawn != nil {
		rewritten, err := hookArguments(result.Spawn)
		if err != nil {
			return call, err
		}
		request, err = parseSpawn(current.ID, rewritten)
		if err != nil {
			return call, fmt.Errorf("hook before_spawn rewrite rejected: %w", err)
		}
		if _, err := r.store.PreviewChild(ctx, call.CellID, request); err != nil {
			return call, fmt.Errorf("hook before_spawn rewrite rejected: %w", err)
		}
		call.Arguments = rewritten
		r.hookDecision(current.ID, turn, HookDecision{InvocationID: result.InvocationID, Hook: "before_spawn", Operation: "agents.spawn", Decision: "rewrite", Reason: result.Reason})
		r.hookNotice(current.ID, turn, "Hook before_spawn rewrote the child request to "+boundedHookText(string(result.Spawn), 512))
	}
	return call, nil
}

func parseSpawn(parent session.SessionID, arguments map[string]any) (store.ChildRequest, error) {
	var args struct {
		Prompt           string                  `json:"prompt"`
		Definition       *session.DefinitionRef  `json:"definition,omitempty"`
		Overrides        session.ConfigPatch     `json:"overrides"`
		WorkingDirectory string                  `json:"working_directory,omitempty"`
		GrantIDs         []session.GrantID       `json:"grant_ids"`
		Budgets          []session.BudgetLimit   `json:"budgets,omitempty"`
		Resources        []session.ResourceLimit `json:"resources,omitempty"`
	}
	if err := decodeArguments(arguments, &args); err != nil {
		return store.ChildRequest{}, err
	}
	if err := session.ValidateText(args.Prompt, session.MaxDocumentBytes/2); err != nil {
		return store.ChildRequest{}, err
	}
	return store.ChildRequest{ParentID: parent, Definition: args.Definition, Overrides: args.Overrides, WorkingDirectory: args.WorkingDirectory, Parts: []session.Part{{Type: "text", Text: args.Prompt}}, GrantIDs: args.GrantIDs, Budgets: args.Budgets, Resources: args.Resources}, nil
}

func hookArguments(raw json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil || result == nil {
		return nil, fmt.Errorf("%w: hook rewrite must be an object", session.ErrInvalid)
	}
	return result, nil
}

type hookReply struct {
	executor.Result
	InvocationID string
}

func (r *Runtime) askHook(ctx context.Context, current session.Session, name string, hook session.HookDeclaration, request executor.Request) (hookReply, error) {
	request.Timeout = time.Duration(hook.TimeoutMillis) * time.Millisecond
	policy, err := r.store.PermissionPolicy(ctx, current.ID)
	if err != nil {
		return hookReply{}, err
	}
	request.PermissionMode = string(policy.Mode)
	var result executor.Result
	invocationID := ""
	if current.Config.HooksDefinition == nil {
		err = errors.New("hook executor unavailable: declaration has no registered owner")
	} else {
		var call *executor.Call
		call, err = r.executors.Acquire(ctx, *current.Config.HooksDefinition, executor.Hook, name)
		if err == nil {
			defer call.Close()
			invocationID = call.ID()
			result, err = call.Invoke(ctx, request, nil)
		}
	}
	if err == nil && result.Failure != "" {
		err = errors.New(result.Failure)
	}
	if ctx.Err() != nil {
		return hookReply{}, ctx.Err()
	}
	if err != nil {
		decision := "deny"
		if hook.Optional {
			decision = "skipped"
		}
		r.hookDecision(current.ID, request.TurnID, HookDecision{InvocationID: invocationID, Hook: name, Operation: request.Operation, Decision: decision, Reason: err.Error()})
		if hook.Optional {
			r.hookNotice(current.ID, request.TurnID, "Hook "+name+" was skipped: "+err.Error())
			return hookReply{}, nil
		}
		return hookReply{}, fmt.Errorf("required hook %s failed: %w", name, err)
	}
	if result.Decision == "deny" && name != "turn_start" {
		reason := result.Reason
		if reason == "" {
			reason = "denied by the captured hook policy"
		}
		r.hookDecision(current.ID, request.TurnID, HookDecision{InvocationID: invocationID, Hook: name, Operation: request.Operation, Decision: "deny", Reason: reason})
		return hookReply{}, fmt.Errorf("hook %s denied %s: %s", name, request.Operation, reason)
	}
	return hookReply{Result: result, InvocationID: invocationID}, nil
}

func (r *Runtime) turnStart(ctx context.Context, current session.Session, turn session.Turn, input *session.Input) (string, error) {
	hook, ok := current.Config.Hooks["turn_start"]
	if !ok {
		return "", nil
	}
	hook.Optional = true // Turn-start context never grants or gates execution.
	var preview strings.Builder
	if input != nil {
		for _, part := range input.Parts {
			if part.Type == "text" && preview.Len() < 2048 {
				preview.WriteString(boundedHookText(part.Text, 2048-preview.Len()))
			}
		}
	}
	var result hookReply
	err := r.withReleasedWorker(ctx, turn.ID, func(ctx context.Context) error {
		var err error
		result, err = r.askHook(ctx, current, "turn_start", hook, executor.Request{SessionID: current.ID, TurnID: turn.ID, Input: preview.String()})
		return err
	})
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		r.mu.Lock()
		owner := r.active[current.ID]
		ownsWorker := owner != nil && owner.turn == turn.ID && owner.worker && !r.closed
		r.mu.Unlock()
		if !ownsWorker {
			return "", err
		}
		r.hookNotice(current.ID, turn.ID, "Hook turn_start was skipped: "+err.Error())
		r.hookDecision(current.ID, turn.ID, HookDecision{Hook: "turn_start", Decision: "skipped", Reason: err.Error()})
		return "", nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r.mu.Lock()
	owner := r.active[current.ID]
	live := owner != nil && owner.turn == turn.ID && !r.closed
	r.mu.Unlock()
	if !live {
		return "", context.Canceled
	}
	return strings.TrimSpace(result.Context), nil
}

func boundedHookText(text string, limit int) string {
	text = strings.ToValidUTF8(strings.ReplaceAll(text, "\x00", "�"), "�")
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}
