package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

// handleDefinitions serves the agent definition registry: register an authored
// document, read one definition, or list every id a session may select.
func (s *Server) handleDefinitions(connection *serverConn, request rpcMessage) (any, *RPCError, bool) {
	switch request.Method {
	case "definitions.register":
		var params struct {
			Definition json.RawMessage `json:"definition"`
		}
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		result, err := registerDefinition(connection.ctx, s.daemon.store, params.Definition, connection.client.ClientID)
		if err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		return result, nil, true
	case "definitions.get":
		var params protocol.DefinitionParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		record, err := describeDefinition(connection.ctx, s.daemon.store, params)
		if err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		return record, nil, true
	case "definitions.list":
		list, err := listDefinitions(connection.ctx, s.daemon.store)
		if err != nil {
			return nil, rpcFailure(-32603, err.Error()), true
		}
		return list, nil, true
	}
	return nil, nil, false
}

// registerDefinition validates an authored document, canonicalizes it, and
// stores it under its content revision. Re-registering an unchanged document
// reports the existing revision.
func registerDefinition(ctx context.Context, store *session.Store, document json.RawMessage, clientID string) (protocol.DefinitionRegisterResult, error) {
	definition, err := agentdef.Decode(document)
	if err != nil {
		return protocol.DefinitionRegisterResult{}, err
	}
	if err := definition.ValidateRegistration(); err != nil {
		return protocol.DefinitionRegisterResult{}, err
	}
	canonical, err := agentdef.Encode(definition)
	if err != nil {
		return protocol.DefinitionRegisterResult{}, err
	}
	revision, err := agentdef.Revision(definition)
	if err != nil {
		return protocol.DefinitionRegisterResult{}, err
	}
	created, err := store.RegisterDefinition(ctx, definition.ID, revision, canonical, clientID)
	if err != nil {
		return protocol.DefinitionRegisterResult{}, err
	}
	return protocol.DefinitionRegisterResult{ID: definition.ID, Revision: revision, Created: created}, nil
}

func describeDefinition(ctx context.Context, store *session.Store, params protocol.DefinitionParams) (protocol.DefinitionRecord, error) {
	if params.ID == "" {
		return protocol.DefinitionRecord{}, errors.New("definition id is required")
	}
	if definition, ok := agentdef.Lookup(params.ID); ok {
		if params.Revision != "" {
			return protocol.DefinitionRecord{}, errors.New("built-in definitions have no revisions")
		}
		return protocol.DefinitionRecord{Definition: definition.Normalize(), BuiltIn: true}, nil
	}
	var record session.DefinitionRecord
	var err error
	if params.Revision == "" {
		record, err = store.LatestDefinition(ctx, params.ID)
	} else {
		record, err = store.LoadDefinition(ctx, params.ID, params.Revision)
	}
	if err != nil {
		return protocol.DefinitionRecord{}, err
	}
	definition, err := agentdef.Decode(record.Body)
	if err != nil {
		return protocol.DefinitionRecord{}, err
	}
	return protocol.DefinitionRecord{
		Definition: definition, Revision: record.Revision, RegisteredBy: record.RegisteredBy,
		CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func listDefinitions(ctx context.Context, store *session.Store) (protocol.DefinitionList, error) {
	list := protocol.DefinitionList{Items: make([]protocol.DefinitionSummary, 0, len(agentdef.IDs()))}
	for _, id := range agentdef.IDs() {
		list.Items = append(list.Items, protocol.DefinitionSummary{ID: id, BuiltIn: true})
	}
	records, err := store.ListDefinitions(ctx)
	if err != nil {
		return protocol.DefinitionList{}, err
	}
	for _, record := range records {
		list.Items = append(list.Items, protocol.DefinitionSummary{
			ID: record.ID, Revision: record.Revision, RegisteredBy: record.RegisteredBy,
			CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return list, nil
}
