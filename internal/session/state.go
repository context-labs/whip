package session

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxStateKeyBytes = 256

var (
	ErrStateNotFound        = errors.New("state value not found")
	ErrStateVersion         = errors.New("state version does not match")
	ErrStateAppend          = errors.New("state value cannot be appended")
	ErrSubscriptionAccess   = errors.New("subscription is not owned by caller")
	ErrSubscriptionInactive = errors.New("subscription is not active")
)

type StateValue struct {
	Key           string
	Version       int64
	AuthorAgentID string
	Payload       RuntimeValue
}

type BlackboardSubscription struct {
	ID      string
	RootID  string
	AgentID string
	Key     string
	Cursor  int64
	Status  string
}

type BlackboardWake struct {
	SubscriptionID string `json:"subscription_id"`
	Key            string `json:"key"`
	Version        int64  `json:"version"`
	AuthorAgentID  string `json:"author_agent_id"`
}

func (s *Store) GetPrivateState(ctx context.Context, rootID, callerAgentID, key string) (StateValue, error) {
	if err := validateStateKey(key); err != nil {
		return StateValue{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return StateValue{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := requireActiveAgentTx(ctx, tx, rootID, callerAgentID); err != nil {
		return StateValue{}, err
	}
	value, err := loadPrivateStateTx(ctx, tx, rootID, callerAgentID, key)
	if err != nil {
		return StateValue{}, err
	}
	return value, tx.Commit()
}

func (s *Store) ListPrivateState(ctx context.Context, rootID, callerAgentID string) ([]StateValue, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := requireActiveAgentTx(ctx, tx, rootID, callerAgentID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, stateSelect(`FROM agent_state s LEFT JOIN content_references r ON r.id=s.payload_ref
		WHERE s.root_id=? AND s.agent_id=? ORDER BY s.key`), InlineValueLimit+1, rootID, callerAgentID)
	if err != nil {
		return nil, err
	}
	values, err := scanStateRows(rows)
	if err != nil {
		return nil, err
	}
	return values, tx.Commit()
}

func (s *Store) SetPrivateState(ctx context.Context, rootID, callerAgentID, key string, payload RuntimePayload) (StateValue, error) {
	return s.mutatePrivateState(ctx, rootID, callerAgentID, key, "set", 0, payload)
}

func (s *Store) AppendPrivateState(ctx context.Context, rootID, callerAgentID, key string, payload RuntimePayload) (StateValue, error) {
	return s.mutatePrivateState(ctx, rootID, callerAgentID, key, "append", 0, payload)
}

func (s *Store) CompareAndSwapPrivateState(ctx context.Context, rootID, callerAgentID, key string, expectedVersion int64, payload RuntimePayload) (StateValue, error) {
	return s.mutatePrivateState(ctx, rootID, callerAgentID, key, "cas", expectedVersion, payload)
}

func (s *Store) mutatePrivateState(ctx context.Context, rootID, callerAgentID, key, operation string, expectedVersion int64, payload RuntimePayload) (StateValue, error) {
	if err := validateStateMutation(key, operation, expectedVersion); err != nil {
		return StateValue{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StateValue{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := requireActiveAgentTx(ctx, tx, rootID, callerAgentID); err != nil {
		return StateValue{}, err
	}
	current, err := loadPrivateStateTx(ctx, tx, rootID, callerAgentID, key)
	exists := err == nil
	if err != nil && !errors.Is(err, ErrStateNotFound) {
		return StateValue{}, err
	}
	if operation != "set" && !exists {
		return StateValue{}, ErrStateNotFound
	}
	if operation == "cas" && current.Version != expectedVersion {
		return current, fmt.Errorf("%w: expected %d, current %d", ErrStateVersion, expectedVersion, current.Version)
	}
	if exists && current.Version == math.MaxInt64 {
		return StateValue{}, errors.New("state version exhausted")
	}
	if operation == "append" {
		payload, err = s.appendStatePayload(current.Payload, payload)
		if err != nil {
			return StateValue{}, err
		}
	}
	version := int64(1)
	if exists {
		version = current.Version + 1
	}
	prepared, err := s.prepareRuntimeValue(payload, ContentGrant{RootID: rootID, AgentID: callerAgentID, Scope: ContentGrantAgent})
	if err != nil {
		return StateValue{}, err
	}
	stamp := now()
	if err := insertRuntimeValue(ctx, tx, prepared, stamp); err != nil {
		return StateValue{}, err
	}
	inline, reference := runtimeValueColumns(prepared.RuntimeValue)
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_state(root_id,agent_id,key,version,author_agent_id,payload_inline,payload_ref,updated_at)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(root_id,agent_id,key) DO UPDATE SET version=excluded.version,
		author_agent_id=excluded.author_agent_id,payload_inline=excluded.payload_inline,payload_ref=excluded.payload_ref,updated_at=excluded.updated_at`,
		rootID, callerAgentID, key, version, callerAgentID, inline, reference, stamp); err != nil {
		return StateValue{}, err
	}
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "state.private."+operation, actorEvent{
		AgentID: callerAgentID, Key: key, Version: version, ExpectedVersion: expectedVersion, Attempt: "accepted",
	}, stamp); err != nil {
		return StateValue{}, err
	}
	if err := tx.Commit(); err != nil {
		return StateValue{}, err
	}
	return StateValue{Key: key, Version: version, AuthorAgentID: callerAgentID, Payload: prepared.RuntimeValue}, nil
}

func (s *Store) GetBlackboard(ctx context.Context, rootID, callerAgentID, key string) (StateValue, error) {
	if err := validateStateKey(key); err != nil {
		return StateValue{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return StateValue{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := requireActiveAgentTx(ctx, tx, rootID, callerAgentID); err != nil {
		return StateValue{}, err
	}
	value, err := loadBlackboardTx(ctx, tx, rootID, key)
	if err != nil {
		return StateValue{}, err
	}
	return value, tx.Commit()
}

func (s *Store) SetBlackboard(ctx context.Context, rootID, callerAgentID, key string, payload RuntimePayload) (StateValue, error) {
	return s.mutateBlackboard(ctx, rootID, callerAgentID, key, "set", 0, payload)
}

func (s *Store) AppendBlackboard(ctx context.Context, rootID, callerAgentID, key string, payload RuntimePayload) (StateValue, error) {
	return s.mutateBlackboard(ctx, rootID, callerAgentID, key, "append", 0, payload)
}

func (s *Store) CompareAndSwapBlackboard(ctx context.Context, rootID, callerAgentID, key string, expectedVersion int64, payload RuntimePayload) (StateValue, error) {
	return s.mutateBlackboard(ctx, rootID, callerAgentID, key, "cas", expectedVersion, payload)
}

func (s *Store) mutateBlackboard(ctx context.Context, rootID, callerAgentID, key, operation string, expectedVersion int64, payload RuntimePayload) (StateValue, error) {
	if err := validateStateMutation(key, operation, expectedVersion); err != nil {
		return StateValue{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StateValue{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := requireActiveAgentTx(ctx, tx, rootID, callerAgentID); err != nil {
		return StateValue{}, err
	}
	current, err := loadBlackboardTx(ctx, tx, rootID, key)
	exists := err == nil
	if err != nil && !errors.Is(err, ErrStateNotFound) {
		return StateValue{}, err
	}
	if operation == "cas" && (!exists || current.Version != expectedVersion) {
		actual := int64(0)
		if exists {
			actual = current.Version
		}
		stamp := now()
		if _, err := s.insertActorEventTx(ctx, tx, rootID, "blackboard.cas", actorEvent{
			AgentID: callerAgentID, Key: key, Version: actual, ExpectedVersion: expectedVersion, Attempt: "stale",
		}, stamp); err != nil {
			return StateValue{}, err
		}
		if err := tx.Commit(); err != nil {
			return StateValue{}, err
		}
		return current, fmt.Errorf("%w: expected %d, current %d", ErrStateVersion, expectedVersion, actual)
	}
	if operation == "append" && !exists {
		return StateValue{}, ErrStateNotFound
	}
	if exists && current.Version == math.MaxInt64 {
		return StateValue{}, errors.New("state version exhausted")
	}
	if operation == "append" {
		payload, err = s.appendStatePayload(current.Payload, payload)
		if err != nil {
			return StateValue{}, err
		}
	}
	version := int64(1)
	if exists {
		version = current.Version + 1
	}
	prepared, err := s.prepareRuntimeValue(payload, ContentGrant{RootID: rootID, Scope: ContentGrantRoot})
	if err != nil {
		return StateValue{}, err
	}
	stamp := now()
	if err := insertRuntimeValue(ctx, tx, prepared, stamp); err != nil {
		return StateValue{}, err
	}
	inline, reference := runtimeValueColumns(prepared.RuntimeValue)
	if _, err := tx.ExecContext(ctx, `INSERT INTO blackboard(root_id,key,version,author_agent_id,payload_inline,payload_ref,updated_at)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT(root_id,key) DO UPDATE SET version=excluded.version,
		author_agent_id=excluded.author_agent_id,payload_inline=excluded.payload_inline,payload_ref=excluded.payload_ref,updated_at=excluded.updated_at`,
		rootID, key, version, callerAgentID, inline, reference, stamp); err != nil {
		return StateValue{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO blackboard_history(root_id,key,version,author_agent_id,payload_inline,payload_ref,created_at) VALUES(?,?,?,?,?,?,?)`,
		rootID, key, version, callerAgentID, inline, reference, stamp); err != nil {
		return StateValue{}, err
	}
	eventKind := "blackboard." + operation
	if _, err := s.insertActorEventTx(ctx, tx, rootID, eventKind, actorEvent{
		AgentID: callerAgentID, Key: key, Version: version, ExpectedVersion: expectedVersion, Attempt: "accepted",
	}, stamp); err != nil {
		return StateValue{}, err
	}
	if err := s.enqueueSubscriptionWakesTx(ctx, tx, rootID, callerAgentID, key, version); err != nil {
		return StateValue{}, err
	}
	if err := tx.Commit(); err != nil {
		return StateValue{}, err
	}
	return StateValue{Key: key, Version: version, AuthorAgentID: callerAgentID, Payload: prepared.RuntimeValue}, nil
}

func (s *Store) BlackboardHistory(ctx context.Context, rootID, callerAgentID, key string) ([]StateValue, error) {
	if err := validateStateKey(key); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := requireActiveAgentTx(ctx, tx, rootID, callerAgentID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, stateSelect(`FROM blackboard_history s LEFT JOIN content_references r ON r.id=s.payload_ref
		WHERE s.root_id=? AND s.key=? ORDER BY s.version`), InlineValueLimit+1, rootID, key)
	if err != nil {
		return nil, err
	}
	values, err := scanStateRows(rows)
	if err != nil {
		return nil, err
	}
	return values, tx.Commit()
}

func (s *Store) CreateBlackboardSubscription(ctx context.Context, rootID, callerAgentID, key string) (BlackboardSubscription, error) {
	if err := validateStateKey(key); err != nil {
		return BlackboardSubscription{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BlackboardSubscription{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := requireActiveAgentTx(ctx, tx, rootID, callerAgentID); err != nil {
		return BlackboardSubscription{}, err
	}
	existing, err := loadActiveSubscriptionTx(ctx, tx, rootID, callerAgentID, key)
	if err == nil {
		return existing, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return BlackboardSubscription{}, err
	}
	var cursor int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT version FROM blackboard WHERE root_id=? AND key=?),0)`, rootID, key).Scan(&cursor); err != nil {
		return BlackboardSubscription{}, err
	}
	id, err := runtimeID()
	if err != nil {
		return BlackboardSubscription{}, err
	}
	stamp := now()
	subscription := BlackboardSubscription{ID: id, RootID: rootID, AgentID: callerAgentID, Key: key, Cursor: cursor, Status: "active"}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscriptions(id,root_id,agent_id,key,cursor,status,created_at,updated_at) VALUES(?,?,?,?,?,'active',?,?)`,
		id, rootID, callerAgentID, key, cursor, stamp, stamp); err != nil {
		return BlackboardSubscription{}, err
	}
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "subscription.created", actorEvent{
		AgentID: callerAgentID, SubscriptionID: id, Key: key, Version: cursor, Attempt: "accepted",
	}, stamp); err != nil {
		return BlackboardSubscription{}, err
	}
	if err := tx.Commit(); err != nil {
		return BlackboardSubscription{}, err
	}
	return subscription, nil
}

func (s *Store) ListBlackboardSubscriptions(ctx context.Context, rootID, callerAgentID string) ([]BlackboardSubscription, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := requireActiveAgentTx(ctx, tx, rootID, callerAgentID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,root_id,agent_id,key,cursor,status FROM subscriptions WHERE root_id=? AND agent_id=? AND status='active' ORDER BY key,id`, rootID, callerAgentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var subscriptions []BlackboardSubscription
	for rows.Next() {
		var subscription BlackboardSubscription
		if err := rows.Scan(&subscription.ID, &subscription.RootID, &subscription.AgentID, &subscription.Key, &subscription.Cursor, &subscription.Status); err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return subscriptions, tx.Commit()
}

func (s *Store) CancelBlackboardSubscription(ctx context.Context, rootID, callerAgentID, subscriptionID string) error {
	if !validSubscriptionID(subscriptionID) {
		return errors.New("invalid subscription ID")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := requireActiveAgentTx(ctx, tx, rootID, callerAgentID); err != nil {
		return err
	}
	var owner, key, status string
	if err := tx.QueryRowContext(ctx, `SELECT agent_id,key,status FROM subscriptions WHERE root_id=? AND id=?`, rootID, subscriptionID).Scan(&owner, &key, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSubscriptionAccess
		}
		return err
	}
	if owner != callerAgentID {
		return ErrSubscriptionAccess
	}
	if status != "active" {
		return ErrSubscriptionInactive
	}
	stamp := now()
	if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET status='cancelled',updated_at=? WHERE root_id=? AND id=? AND status='active'`, stamp, rootID, subscriptionID); err != nil {
		return err
	}
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "subscription.cancelled", actorEvent{
		AgentID: callerAgentID, SubscriptionID: subscriptionID, Key: key, Attempt: "accepted",
	}, stamp); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) enqueueSubscriptionWakesTx(ctx context.Context, tx *sql.Tx, rootID, authorAgentID, key string, version int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT s.id,s.agent_id FROM subscriptions s JOIN agents a ON a.root_id=s.root_id AND a.id=s.agent_id
		WHERE s.root_id=? AND s.key=? AND s.status='active' AND s.cursor<?
		AND a.status NOT IN ('failed','stopped','cancelled','interrupted','deleted','succeeded') ORDER BY s.id`, rootID, key, version)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	type target struct{ id, agentID string }
	var targets []target
	for rows.Next() {
		var target target
		if err := rows.Scan(&target.id, &target.agentID); err != nil {
			return err
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, target := range targets {
		payload, err := json.Marshal(BlackboardWake{SubscriptionID: target.id, Key: key, Version: version, AuthorAgentID: authorAgentID})
		if err != nil {
			return err
		}
		prepared, err := s.prepareRuntimeValue(RuntimePayload{Data: payload, MediaType: "application/json", Source: "blackboard subscription wake"}, ContentGrant{
			RootID: rootID, AgentID: target.agentID, Scope: ContentGrantAgent,
		})
		if err != nil {
			return err
		}
		if _, err := s.enqueueInboxTx(ctx, tx, InboxEnqueue{RootID: rootID, AgentID: target.agentID, Kind: "subscription"}, prepared,
			"subscription.wake.queued", actorEvent{SenderAgentID: authorAgentID, SubscriptionID: target.id, Key: key, Version: version}); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE subscriptions SET cursor=?,updated_at=? WHERE root_id=? AND id=? AND status='active' AND cursor<?`, version, now(), rootID, target.id, version)
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			if err != nil {
				return err
			}
			return errors.New("subscription cursor changed before wake commit")
		}
	}
	return nil
}

func loadActiveSubscriptionTx(ctx context.Context, tx *sql.Tx, rootID, agentID, key string) (BlackboardSubscription, error) {
	var subscription BlackboardSubscription
	err := tx.QueryRowContext(ctx, `SELECT id,root_id,agent_id,key,cursor,status FROM subscriptions WHERE root_id=? AND agent_id=? AND key=? AND status='active' ORDER BY id LIMIT 1`,
		rootID, agentID, key).Scan(&subscription.ID, &subscription.RootID, &subscription.AgentID, &subscription.Key, &subscription.Cursor, &subscription.Status)
	return subscription, err
}

func loadPrivateStateTx(ctx context.Context, tx *sql.Tx, rootID, agentID, key string) (StateValue, error) {
	value, err := scanState(tx.QueryRowContext(ctx, stateSelect(`FROM agent_state s LEFT JOIN content_references r ON r.id=s.payload_ref
		WHERE s.root_id=? AND s.agent_id=? AND s.key=?`), InlineValueLimit+1, rootID, agentID, key))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrStateNotFound
	}
	return value, err
}

func loadBlackboardTx(ctx context.Context, tx *sql.Tx, rootID, key string) (StateValue, error) {
	value, err := scanState(tx.QueryRowContext(ctx, stateSelect(`FROM blackboard s LEFT JOIN content_references r ON r.id=s.payload_ref
		WHERE s.root_id=? AND s.key=?`), InlineValueLimit+1, rootID, key))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrStateNotFound
	}
	return value, err
}

func stateSelect(from string) string {
	return `SELECT s.key,s.version,s.author_agent_id,substr(s.payload_inline,1,?),COALESCE(s.payload_ref,''),
		COALESCE(r.digest,''),COALESCE(r.size,0),COALESCE(r.media_type,''),COALESCE(r.source,'') ` + from
}

type stateScanner interface{ Scan(...any) error }

func scanState(scanner stateScanner) (StateValue, error) {
	var value StateValue
	if err := scanner.Scan(&value.Key, &value.Version, &value.AuthorAgentID, &value.Payload.Inline, &value.Payload.ReferenceID,
		&value.Payload.Digest, &value.Payload.Size, &value.Payload.MediaType, &value.Payload.Source); err != nil {
		return StateValue{}, err
	}
	if value.Version < 1 {
		return StateValue{}, errors.New("state value has an invalid version")
	}
	if value.Payload.ReferenceID == "" {
		if len(value.Payload.Inline) > InlineValueLimit {
			return StateValue{}, errors.New("state value has an oversized inline payload")
		}
		value.Payload.Size = int64(len(value.Payload.Inline))
	} else {
		value.Payload.Inline = nil
		if value.Payload.Digest == "" || value.Payload.Size < 0 {
			return StateValue{}, errors.New("state value references missing content")
		}
	}
	return value, nil
}

func scanStateRows(rows *sql.Rows) ([]StateValue, error) {
	defer func() { _ = rows.Close() }()
	var values []StateValue
	for rows.Next() {
		value, err := scanState(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func requireActiveAgentTx(ctx context.Context, tx *sql.Tx, rootID, agentID string) (RuntimeAgent, error) {
	if rootID == "" || agentID == "" {
		return RuntimeAgent{}, ErrAgentAccess
	}
	agent, err := loadAgentTx(ctx, tx, rootID, agentID)
	if err != nil {
		return RuntimeAgent{}, err
	}
	if isTerminalAgentStatus(agent.Status) {
		return RuntimeAgent{}, ErrAgentTerminal
	}
	return agent, nil
}

func validateStateMutation(key, operation string, expectedVersion int64) error {
	if err := validateStateKey(key); err != nil {
		return err
	}
	if operation != "set" && operation != "append" && operation != "cas" {
		return errors.New("invalid state mutation")
	}
	if operation == "cas" && expectedVersion < 1 {
		return errors.New("state CAS requires a positive expected version")
	}
	return nil
}

func validateStateKey(key string) error {
	if key == "" || len(key) > maxStateKeyBytes || !utf8.ValidString(key) || strings.TrimSpace(key) != key || strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return errors.New("invalid state key")
	}
	return nil
}

func validSubscriptionID(id string) bool {
	if len(id) != 32 || strings.ToLower(id) != id {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (s *Store) appendStatePayload(current RuntimeValue, suffix RuntimePayload) (RuntimePayload, error) {
	data := current.Inline
	if current.ReferenceID != "" {
		var err error
		data, err = s.readContentBody(current.Digest, current.Size)
		if err != nil {
			return RuntimePayload{}, err
		}
	}
	currentType := baseMediaType(current.MediaType)
	requestedType := baseMediaType(suffix.MediaType)
	if currentType == "" {
		if _, ok := decodeJSONArray(data); ok {
			currentType = "application/json"
		} else if utf8.Valid(data) {
			currentType = "text/plain"
		} else {
			return RuntimePayload{}, fmt.Errorf("%w: stored value has no supported encoding", ErrStateAppend)
		}
	}
	if requestedType != "" && requestedType != currentType {
		return RuntimePayload{}, fmt.Errorf("%w: media type %q does not match %q", ErrStateAppend, requestedType, currentType)
	}
	mediaType := current.MediaType
	if mediaType == "" {
		mediaType = suffix.MediaType
	}
	if mediaType == "" {
		mediaType = currentType
	}
	source := suffix.Source
	if source == "" {
		source = current.Source
	}
	switch currentType {
	case "text/plain":
		if !utf8.Valid(data) || !utf8.Valid(suffix.Data) {
			return RuntimePayload{}, fmt.Errorf("%w: text append requires UTF-8", ErrStateAppend)
		}
		result := make([]byte, 0, len(data)+len(suffix.Data))
		result = append(result, data...)
		result = append(result, suffix.Data...)
		return RuntimePayload{Data: result, MediaType: mediaType, Source: source}, nil
	case "application/json":
		items, ok := decodeJSONArray(data)
		if !ok || !json.Valid(suffix.Data) {
			return RuntimePayload{}, fmt.Errorf("%w: JSON append requires an array and one valid JSON value", ErrStateAppend)
		}
		items = append(items, append(json.RawMessage(nil), suffix.Data...))
		result, err := json.Marshal(items)
		if err != nil {
			return RuntimePayload{}, err
		}
		return RuntimePayload{Data: result, MediaType: mediaType, Source: source}, nil
	default:
		return RuntimePayload{}, fmt.Errorf("%w: unsupported media type %q", ErrStateAppend, currentType)
	}
}

func baseMediaType(value string) string {
	if index := strings.IndexByte(value, ';'); index >= 0 {
		value = value[:index]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func decodeJSONArray(data []byte) ([]json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, false
	}
	var items []json.RawMessage
	return items, json.Unmarshal(trimmed, &items) == nil
}
