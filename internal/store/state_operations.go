package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

// ApplyStateOperation commits authorization, metadata and the operation result
// together. Immutable bodies are published before this transaction, and never
// copied into the operation ledger. Reads retain the version actually observed.
func (s *Store) ApplyStateOperation(ctx context.Context, id session.OperationID) (result json.RawMessage, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		operation, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		switch operation.Capability {
		case "state.get", "state.read", "state.write", "state.append", "state.list", "state.history", "state.subscribe", "state.subscriptions", "state.unsubscribe":
		default:
			return ErrConflict
		}
		owner, err := readSession(ctx, tx, operation.SessionID)
		if err != nil {
			return err
		}
		if operation.Resource != string(owner.TreeID) {
			return ErrConflict
		}
		if operation.State == session.OperationSucceeded && operation.Result != nil {
			result = operation.Result.Value
			return nil
		}
		dispatch, err := dispatchOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if !dispatch {
			return ErrConflict
		}
		value, err := applyStateOperation(ctx, tx, operation)
		if err != nil {
			return err
		}
		result, err = json.Marshal(value)
		if err != nil {
			return err
		}
		operation.State = session.OperationDispatched
		_, err = settleOperation(ctx, tx, operation, session.OperationResult{State: session.OperationSucceeded, Value: result})
		return err
	})
	return
}

func applyStateOperation(ctx context.Context, tx *sql.Tx, operation session.Operation) (any, error) {
	switch operation.Capability {
	case "state.subscribe":
		var request session.StateSubscribe
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, err
		}
		if err := request.Validate(); err != nil {
			return nil, err
		}
		id := fmt.Sprintf("subscription_%x", sha256.Sum256([]byte(operation.ID)))
		return subscribeState(ctx, tx, operation.SessionID, id, request)
	case "state.subscriptions":
		var request session.StateSubscriptionList
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, err
		}
		values, err := listStateSubscriptions(ctx, tx, operation.SessionID, request.After, request.Limit)
		return session.StateSubscriptionItems{Items: values}, err
	case "state.unsubscribe":
		var request session.StateSubscriptionID
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, err
		}
		return unsubscribeState(ctx, tx, operation.SessionID, request.ID)
	case "state.write", "state.append":
		var request session.StateWrite
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, err
		}
		request.ID = fmt.Sprintf("state_%x", sha256.Sum256([]byte(operation.ID)))
		request.SessionID = operation.SessionID
		if err := request.Validate(); err != nil {
			return nil, err
		}
		return writeState(ctx, tx, request)
	case "state.get":
		var request session.StateKey
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, err
		}
		if err := session.ValidateStateKey(request.Key); err != nil {
			return nil, err
		}
		tree, owner, err := stateOwner(ctx, tx, operation.SessionID, request.Scope)
		if err != nil {
			return nil, err
		}
		return readState(ctx, tx, tree, owner, request.Key)
	case "state.read":
		var request session.StateRead
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, err
		}
		if request.Offset < 0 || request.Length < 1 || request.Length > session.MaxStateReadBytes {
			return nil, session.ErrInvalid
		}
		value, err := stateValue(ctx, tx, operation.SessionID, request.ID)
		if err != nil {
			return nil, err
		}
		if request.Offset > value.Size {
			return nil, session.ErrInvalid
		}
		return value, nil
	case "state.list":
		var request session.StateList
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, err
		}
		values, err := listState(ctx, tx, operation.SessionID, request.Scope, "", request.After, 0, request.Limit)
		return session.StateItems{Items: values}, err
	case "state.history":
		var request session.StateHistory
		if err := json.Unmarshal(operation.Arguments, &request); err != nil {
			return nil, err
		}
		if err := session.ValidateStateKey(request.Key); err != nil {
			return nil, err
		}
		if request.After < 0 {
			return nil, session.ErrInvalid
		}
		values, err := listState(ctx, tx, operation.SessionID, request.Scope, request.Key, "", request.After, request.Limit)
		return session.StateItems{Items: values}, err
	default:
		return nil, ErrConflict
	}
}
