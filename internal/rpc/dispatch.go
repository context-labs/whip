// Package rpc validates wire requests and maps them onto runtime operations.
// It owns connections, never accepted execution.
package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

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

func Dispatch(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
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
			tree, root, err := r.CreateTree(ctx, store.CreateTree{Metadata: session.TreeMetadata(p.Metadata), Engine: session.Engine(p.Engine), Policy: session.TreePolicy(p.Policy), Definition: definitionRef(p.Definition), Overrides: patch, WorkingDirectory: p.WorkingDirectory})
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
			request := store.SpawnSession{ParentID: session.SessionID(p.ParentID), Overrides: patch}
			if p.Definition != nil {
				ref := definitionRef(*p.Definition)
				request.Definition = &ref
			}
			if p.WorkingDirectory != nil {
				request.WorkingDirectory = *p.WorkingDirectory
			}
			value, err := r.SpawnSession(ctx, request)
			if err != nil {
				return nil, err
			}
			return protocol.SessionFromDomain(value)
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
			values, err := r.History(ctx, session.SessionID(p.SessionID), int64(p.After), p.Limit)
			if err != nil {
				return nil, err
			}
			result := protocol.HistoryResult{Items: []protocol.Message{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.MessageFromDomain(value))
			}
			return result, nil
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
