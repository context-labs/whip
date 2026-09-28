// Package rpc validates wire requests and maps them onto runtime operations.
// It owns connections, never accepted execution.
package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/account"
	"github.com/context-labs/whip/internal/inferenceaccount"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

var (
	ErrIdentity = errors.New("runtime identity mismatch")
	ErrMethod   = errors.New("unknown method")
)

func decode[P any](raw json.RawMessage, fn func(P) (any, error)) (any, error) {
	var params P
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("%w: %w", session.ErrInvalid, err)
	}
	return fn(params)
}

func Dispatch(ctx context.Context, r *runtime.Runtime, host HostServices, method string, raw json.RawMessage) (any, error) {
	found := false
	for _, op := range protocol.Operations() {
		if op.Name == method {
			found = true
			if err := protocol.Validate(op.Params.Name(), raw); err != nil {
				return nil, fmt.Errorf("%w: %w", session.ErrInvalid, err)
			}
			break
		}
	}
	if !found {
		return nil, ErrMethod
	}
	switch method {
	case "accounts.openai.begin", "accounts.openai.get", "accounts.openai.list", "accounts.openai.cancel", "accounts.openai.status", "accounts.openai.setup", "accounts.openai.logout":
		return dispatchAccount(ctx, host.OpenAI, method, raw)
	case "accounts.inference.begin", "accounts.inference.get", "accounts.inference.list", "accounts.inference.cancel", "accounts.inference.team", "accounts.inference.project", "accounts.inference.create_project", "accounts.inference.retry", "accounts.inference.rotate", "accounts.inference.status", "accounts.inference.setup", "accounts.inference.logout", "accounts.inference.cleanup", "accounts.inference.retry_cleanup":
		return dispatchInferenceAccount(ctx, host, method, raw)
	case "sessions.compact", "context.head", "context.compaction", "context.compactions", "context.select", "context.snapshot", "context.list", "context.read", "context.search":
		return dispatchContext(ctx, r, method, raw)
	case "turns.output":
		return decode(raw, func(p protocol.TurnParams) (any, error) {
			value, err := r.TurnOutput(ctx, session.TurnID(p.TurnID))
			return protocol.TurnOutputFromDomain(value), err
		})
	case "skills.list":
		return decode(raw, func(p protocol.ListSkillsParams) (any, error) {
			values, next, err := r.Skills(ctx, session.SessionID(p.SessionID), p.Prefix, p.After, p.Limit)
			result := protocol.ListSkillsResult{Items: []protocol.SkillMetadata{}, NextAfter: next}
			for _, value := range values {
				result.Items = append(result.Items, protocol.SkillMetadata{Name: value.Name, Description: value.Description, Disabled: value.Disabled, Source: protocol.InstructionSourceFromDomain(value.Source)})
			}
			return result, err
		})
	case "turns.instructions":
		return decode(raw, func(p protocol.TurnParams) (any, error) {
			value, err := r.InstructionManifest(ctx, session.TurnID(p.TurnID))
			return protocol.InstructionManifestFromDomain(value), err
		})
	case "completions.list", "completions.read":
		return dispatchCompletion(ctx, r, method, raw)
	case "state.subscribe", "state.subscriptions", "state.unsubscribe":
		return dispatchStateSubscription(ctx, r, method, raw)
	case "state.get", "state.write", "state.append", "state.read", "state.list", "state.history":
		return dispatchState(ctx, r, method, raw)
	case "mail.send", "mail.list", "mail.read":
		return dispatchMail(ctx, r, method, raw)
	case "goals.create", "goals.current", "goals.get", "goals.resume", "goals.cancel", "goals.formulate", "goals.formulation":
		return dispatchGoal(ctx, r, method, raw)
	case "schedules.create", "schedules.get", "schedules.list", "schedules.cancel":
		return dispatchSchedule(ctx, r, method, raw)
	case "resources.list", "resources.set":
		return dispatchResource(ctx, r, method, raw)
	case "budgets.list", "budgets.set":
		return dispatchBudget(ctx, r, method, raw)
	case "sessions.observe":
		return dispatchObservation(ctx, r, raw)
	case "grants.create", "grants.list", "grants.revoke", "operations.get", "turns.operations", "permissions.list", "permissions.resolve", "cells.get", "turns.cells":
		return dispatchOperation(ctx, r, method, raw)
	case "content.put":
		return decode(raw, func(p protocol.PutContentParams) (any, error) {
			if len(p.DataBase64) > base64.StdEncoding.EncodedLen(session.MaxContentBytes) {
				return nil, fmt.Errorf("%w: content exceeds 4 MiB", session.ErrInvalid)
			}
			data, err := base64.StdEncoding.Strict().DecodeString(p.DataBase64)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid content encoding", session.ErrInvalid)
			}
			value, err := r.PutContent(ctx, session.SessionID(p.SessionID), string(p.ReferenceID), p.MediaType, data)
			return protocol.ContentReferenceFromDomain(value), err
		})
	case "content.read":
		return decode(raw, func(p protocol.ReadContentParams) (any, error) {
			value, data, err := r.ReadContent(ctx, session.SessionID(p.SessionID), string(p.ReferenceID), session.MaxContentBytes)
			return protocol.ReadContentResult{Reference: protocol.ContentReferenceFromDomain(value), DataBase64: base64.StdEncoding.EncodeToString(data)}, err
		})
	case "initialize":
		return decode(raw, func(p protocol.InitializeParams) (any, error) {
			if p.ExpectedRuntimeID != nil && string(*p.ExpectedRuntimeID) != string(r.Identity()) {
				return nil, ErrIdentity
			}
			refs, err := r.Builtins()
			if err != nil {
				return nil, err
			}
			result := protocol.InitializeResult{Major: protocol.Major, Minor: protocol.Minor, RuntimeID: protocol.ID(r.Identity()), Builtins: []protocol.DefinitionRef{}}
			for _, ref := range refs {
				result.Builtins = append(result.Builtins, protocol.DefinitionRef{ID: protocol.ID(ref.ID), Revision: ref.Revision})
			}
			return result, nil
		})
	case "trees.create":
		return decode(raw, func(p protocol.CreateTreeParams) (any, error) {
			patch, err := p.Overrides.Domain()
			if err != nil {
				return nil, err
			}
			tree, root, err := r.CreateTree(ctx, store.CreateTree{Metadata: session.TreeMetadata(p.Metadata), Engine: session.Engine(p.Engine), Resources: protocol.ResourceLimitsDomain(p.Resources), Definition: definitionRef(p.Definition), Overrides: patch, WorkingDirectory: p.WorkingDirectory})
			if err != nil {
				return nil, err
			}
			wire, err := protocol.SessionFromDomain(root)
			return protocol.CreateTreeResult{Tree: protocol.TreeFromDomain(tree), Root: wire}, err
		})
	case "trees.get":
		return decode(raw, func(p protocol.TreeParams) (any, error) {
			value, err := r.Tree(ctx, session.TreeID(p.TreeID))
			return protocol.TreeFromDomain(value), err
		})
	case "trees.update":
		return decode(raw, func(p protocol.UpdateTreeParams) (any, error) {
			value, err := r.UpdateTree(ctx, session.TreeID(p.TreeID), session.Revision(p.ExpectedRevision), session.TreeMetadata(p.Metadata))
			return protocol.TreeFromDomain(value), err
		})
	case "sessions.get":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			value, err := r.Session(ctx, session.SessionID(p.SessionID))
			if err != nil {
				return nil, err
			}
			return protocol.SessionFromDomain(value)
		})
	case "sessions.spawn":
		return decode(raw, func(p protocol.SpawnSessionParams) (any, error) {
			patch, err := p.Overrides.Domain()
			if err != nil {
				return nil, err
			}
			request := store.ChildRequest{ParentID: session.SessionID(p.ParentID), Overrides: patch, Resources: protocol.ResourceLimitsDomain(p.Resources)}
			for _, limit := range p.Budgets {
				request.Budgets = append(request.Budgets, limit.Domain())
			}
			for _, part := range p.Parts {
				request.Parts = append(request.Parts, part.Domain())
			}
			if p.GrantIDs != nil {
				request.GrantIDs = make([]session.GrantID, len(p.GrantIDs))
				for i, id := range p.GrantIDs {
					request.GrantIDs[i] = session.GrantID(id)
				}
			}
			if p.Definition != nil {
				ref := definitionRef(*p.Definition)
				request.Definition = &ref
			}
			if p.WorkingDirectory != nil {
				request.WorkingDirectory = *p.WorkingDirectory
			}
			value, err := r.SpawnChild(ctx, identity(p.Identity), request)
			if err != nil {
				return nil, err
			}
			result := protocol.SpawnSessionResult{Admission: admission(value.Admission)}
			if value.Session != nil {
				wire, err := protocol.SessionFromDomain(*value.Session)
				if err != nil {
					return nil, err
				}
				result.Session = &wire
			}
			return result, nil
		})
	case "sessions.list":
		return decode(raw, func(p protocol.ListSessionsParams) (any, error) {
			var after session.SessionID
			if p.After != nil {
				after = session.SessionID(*p.After)
			}
			values, err := r.Sessions(ctx, session.TreeID(p.TreeID), after, p.Limit)
			if err != nil {
				return nil, err
			}
			result := protocol.ListSessionsResult{Items: []protocol.Session{}}
			for _, value := range values {
				wire, err := protocol.SessionFromDomain(value)
				if err != nil {
					return nil, err
				}
				result.Items = append(result.Items, wire)
			}
			return result, nil
		})
	case "sessions.configure":
		return decode(raw, func(p protocol.UpdateConfigurationParams) (any, error) {
			patch, err := p.Patch.Domain()
			if err != nil {
				return nil, err
			}
			value, err := r.UpdateConfiguration(ctx, session.SessionID(p.SessionID), session.Revision(p.ExpectedRevision), patch)
			if err != nil {
				return nil, err
			}
			return protocol.SessionFromDomain(value)
		})
	case "sessions.submit":
		return decode(raw, func(p protocol.SubmitParams) (any, error) {
			parts := make([]session.Part, len(p.Parts))
			for i, part := range p.Parts {
				parts[i] = part.Domain()
			}
			value, err := r.Admit(ctx, identity(p.Identity), store.Submission{SessionID: session.SessionID(p.SessionID), Source: session.InputSource(p.Source), Parts: parts})
			return admission(value), err
		})
	case "sessions.history":
		return decode(raw, func(p protocol.HistoryParams) (any, error) {
			snapshot, values, err := r.HistoryPage(ctx, session.SessionID(p.SessionID), int64(p.After), p.Limit, expectedHistoryRevision(p.ExpectedRevision))
			if err != nil {
				return nil, err
			}
			result := protocol.HistoryResult{Snapshot: protocol.HistorySnapshotFromDomain(snapshot), Items: []protocol.Message{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.MessageFromDomain(value))
			}
			return result, nil
		})
	case "sessions.rewind":
		return decode(raw, func(p protocol.RewindParams) (any, error) {
			value, err := r.Rewind(ctx, session.RewindRequest{
				ID: session.HistoryEditID(p.EditID), SessionID: session.SessionID(p.SessionID),
				ExpectedRevision: session.Revision(p.ExpectedRevision), ObservedThrough: int64(p.ObservedThrough),
				KeepThrough: int64(p.KeepThrough),
			})
			return protocol.HistoryEditFromDomain(value), err
		})
	case "sessions.fork":
		return decode(raw, func(p protocol.ForkParams) (any, error) {
			value, err := r.Fork(ctx, session.ForkRequest{
				ID: session.ForkID(p.ForkID), SessionID: session.SessionID(p.SessionID),
				ExpectedHistoryRevision: session.Revision(p.ExpectedHistoryRevision), ExpectedConfigRevision: session.Revision(p.ExpectedConfigRevision),
				ObservedThrough: int64(p.ObservedThrough), KeepThrough: int64(p.KeepThrough), Title: p.Title,
			})
			if err != nil {
				return nil, err
			}
			return protocol.ForkResultFromDomain(value)
		})
	case "sessions.lifecycle":
		return decode(raw, func(p protocol.LifecycleParams) (any, error) {
			value, err := r.SetLifecycle(ctx, session.SessionID(p.SessionID), session.Lifecycle(p.Lifecycle))
			if err != nil {
				return nil, err
			}
			return protocol.SessionFromDomain(value)
		})
	case "sessions.delete":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			err := r.DeleteSubtree(ctx, session.SessionID(p.SessionID))
			return protocol.DeleteResult{Deleted: err == nil}, err
		})
	case "turns.get":
		return decode(raw, func(p protocol.TurnParams) (any, error) {
			value, err := r.Turn(ctx, session.TurnID(p.TurnID))
			return protocol.TurnFromDomain(value), err
		})
	case "turns.attempts":
		return decode(raw, func(p protocol.ModelAttemptsParams) (any, error) {
			var after session.ModelAttemptID
			if p.After != nil {
				after = session.ModelAttemptID(*p.After)
			}
			values, err := r.ModelAttempts(ctx, session.TurnID(p.TurnID), after, p.Limit)
			if err != nil {
				return nil, err
			}
			result := protocol.ModelAttemptsResult{Items: []protocol.ModelAttempt{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.ModelAttemptFromDomain(value))
			}
			return result, nil
		})
	case "turns.cancel":
		return decode(raw, func(p protocol.TurnParams) (any, error) {
			value, err := r.CancelTurn(ctx, session.TurnID(p.TurnID))
			return protocol.TurnFromDomain(value), err
		})
	case "inputs.cancel":
		return decode(raw, func(p protocol.InputParams) (any, error) {
			value, err := r.CancelInput(ctx, session.InputID(p.InputID))
			return protocol.InputFromDomain(value), err
		})
	case "receipts.get":
		return decode(raw, func(p protocol.RequestIdentity) (any, error) {
			value, err := r.Admission(ctx, identity(p))
			return admission(value), err
		})
	case "definitions.register":
		return decode(raw, func(p protocol.DefinitionDocument) (any, error) {
			document, err := p.Domain()
			if err != nil {
				return nil, err
			}
			value, err := r.RegisterDefinition(ctx, document)
			if err != nil {
				return nil, err
			}
			return protocol.DefinitionFromDomain(value)
		})
	case "definitions.get":
		return decode(raw, func(p protocol.DefinitionRef) (any, error) {
			value, err := r.Definition(ctx, definitionRef(p))
			if err != nil {
				return nil, err
			}
			return protocol.DefinitionFromDomain(value)
		})
	default:
		return nil, ErrMethod
	}
}

func definitionRef(p protocol.DefinitionRef) session.DefinitionRef {
	return session.DefinitionRef{ID: string(p.ID), Revision: p.Revision}
}

func identity(p protocol.RequestIdentity) session.RequestIdentity {
	return session.RequestIdentity{ClientID: string(p.ClientID), RequestID: string(p.RequestID)}
}

func admission(value store.Admission) protocol.Admission {
	result := protocol.Admission{Receipt: protocol.ReceiptFromDomain(value.Receipt)}
	if value.Input != nil {
		wire := protocol.InputFromDomain(*value.Input)
		result.Input = &wire
	}
	if value.Turn != nil {
		wire := protocol.TurnFromDomain(*value.Turn)
		result.Turn = &wire
	}
	return result
}

func wireError(err error) *protocol.RPCError {
	kinds := []struct {
		err  error
		code int
		kind string
	}{
		{session.ErrInvalid, -32602, "INVALID"},
		{account.ErrInvalid, -32602, "INVALID"},
		{account.ErrNotFound, -32004, "NOT_FOUND"},
		{account.ErrLimit, -32011, "LIMIT"},
		{account.ErrClosed, -32013, "CLOSED"},
		{account.ErrCredentials, -32020, "ACCOUNT_CREDENTIALS"},
		{account.ErrSetupRequired, -32021, "ACCOUNT_SETUP"},
		{account.ErrConfiguration, -32022, "ACCOUNT_CONFIGURATION"},
		{account.ErrLogout, -32023, "ACCOUNT_LOGOUT"},
		{inferenceaccount.ErrInvalid, -32602, "INVALID"},
		{inferenceaccount.ErrNotFound, -32004, "NOT_FOUND"},
		{inferenceaccount.ErrBusy, -32010, "BUSY"},
		{inferenceaccount.ErrLimit, -32011, "LIMIT"},
		{inferenceaccount.ErrClosed, -32013, "CLOSED"},
		{inferenceaccount.ErrCredentials, -32020, "ACCOUNT_CREDENTIALS"},
		{inferenceaccount.ErrSetup, -32021, "ACCOUNT_SETUP"},
		{inferenceaccount.ErrManagement, -32024, "ACCOUNT_MANAGEMENT"},
		{store.ErrNotFound, -32004, "NOT_FOUND"},
		{store.ErrConflict, -32009, "CONFLICT"},
		{store.ErrBusy, -32010, "BUSY"},
		{store.ErrLimit, -32011, "LIMIT"},
		{store.ErrStopped, -32012, "STOPPED"},
		{runtime.ErrClosed, -32013, "CLOSED"},
		{ErrIdentity, -32014, "IDENTITY"},
		{ErrMethod, -32601, "METHOD"},
	}
	for _, kind := range kinds {
		if errors.Is(err, kind.err) {
			return &protocol.RPCError{Code: kind.code, Kind: kind.kind, Message: kind.err.Error()}
		}
	}
	return &protocol.RPCError{Code: -32603, Kind: "INTERNAL", Message: "runtime operation failed"}
}
