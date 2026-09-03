package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrCommandConflict = errors.New("command ID was reused with a different request")

// CommandAdmission is the durable protocol identity and work item admitted by
// either the daemon-control actor or one root actor.
type CommandAdmission struct {
	ClientID      string
	CommandID     string
	Scope         CommandScope
	RootID        string
	AgentID       string
	Kind          string
	RequestDigest string
	Payload       RuntimePayload
}

type CommandRecord struct {
	ClientID      string
	CommandID     string
	Scope         CommandScope
	RootID        string
	RequestDigest string
	Status        string
	IngressSeq    int64
	Outcome       RuntimeValue
}

type CommandAdmissionResult struct {
	Command  CommandRecord
	EventSeq int64
	New      bool
}

// AdmitCommand compares command identity and request digest and, for a new
// root command, inserts the command and its actor inbox item in one commit.
func (s *Store) AdmitCommand(ctx context.Context, admission CommandAdmission) (CommandAdmissionResult, error) {
	if err := validateCommandAdmission(admission); err != nil {
		return CommandAdmissionResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CommandAdmissionResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if existing, found, err := loadCommandTx(ctx, tx, admission.ClientID, admission.CommandID); err != nil {
		return CommandAdmissionResult{}, err
	} else if found {
		if existing.Scope != admission.Scope || existing.RootID != admission.RootID || existing.RequestDigest != admission.RequestDigest {
			return CommandAdmissionResult{}, ErrCommandConflict
		}
		return CommandAdmissionResult{Command: existing}, nil
	}
	commandValue, err := s.prepareRuntimeValue(admission.Payload, ContentGrant{RootID: admission.RootID, Scope: ContentGrantRoot})
	if err != nil {
		return CommandAdmissionResult{}, err
	}
	var inboxValue preparedRuntimeValue
	if admission.Scope == CommandScopeRoot {
		inboxValue, err = s.prepareRuntimeValue(admission.Payload, ContentGrant{
			RootID: admission.RootID, AgentID: admission.AgentID, Scope: ContentGrantAgent,
		})
		if err != nil {
			return CommandAdmissionResult{}, err
		}
	}

	stamp := now()
	if err := insertRuntimeValue(ctx, tx, commandValue, stamp); err != nil {
		return CommandAdmissionResult{}, err
	}
	result := CommandAdmissionResult{New: true}
	if admission.Scope == CommandScopeRoot {
		sequence, err := s.enqueueInboxTx(ctx, tx, InboxEnqueue{
			RootID: admission.RootID, AgentID: admission.AgentID, Kind: admission.Kind,
			CommandClientID: admission.ClientID, CommandID: admission.CommandID,
			Payload: admission.Payload,
		}, inboxValue, "command.queued", actorEvent{})
		if err != nil {
			return CommandAdmissionResult{}, err
		}
		result.Command.IngressSeq = sequence.InboxSeq
		result.EventSeq = sequence.EventSeq
	} else if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ingress_seq),0)+1 FROM commands WHERE scope='daemon'`).Scan(&result.Command.IngressSeq); err != nil {
		return CommandAdmissionResult{}, err
	}
	inline, reference := runtimeValueColumns(commandValue.RuntimeValue)
	if _, err := tx.ExecContext(ctx, `INSERT INTO commands(client_id,command_id,scope,root_id,request_digest,status,payload_inline,payload_ref,ingress_seq,created_at,updated_at)
		VALUES(?,?,?,?,?,'queued',?,?,?,?,?)`, admission.ClientID, admission.CommandID, admission.Scope, nullableString(admission.RootID),
		admission.RequestDigest, inline, reference, result.Command.IngressSeq, stamp, stamp); err != nil {
		return CommandAdmissionResult{}, err
	}
	result.Command.ClientID = admission.ClientID
	result.Command.CommandID = admission.CommandID
	result.Command.Scope = admission.Scope
	result.Command.RootID = admission.RootID
	result.Command.RequestDigest = admission.RequestDigest
	result.Command.Status = "queued"
	if err := tx.Commit(); err != nil {
		return CommandAdmissionResult{}, err
	}
	return result, nil
}

// AdmitControlCommand durably admits a root-scoped command that executes on
// the actor itself instead of becoming model input. Matching retries observe
// the existing command; only a newly inserted row may execute its action.
func (s *Store) AdmitControlCommand(ctx context.Context, admission CommandAdmission) (CommandAdmissionResult, error) {
	if err := validateCommandAdmission(admission); err != nil {
		return CommandAdmissionResult{}, err
	}
	if admission.Scope != CommandScopeRoot {
		return CommandAdmissionResult{}, errors.New("control command must be root scoped")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CommandAdmissionResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if existing, found, err := loadCommandTx(ctx, tx, admission.ClientID, admission.CommandID); err != nil {
		return CommandAdmissionResult{}, err
	} else if found {
		if existing.Scope != admission.Scope || existing.RootID != admission.RootID || existing.RequestDigest != admission.RequestDigest {
			return CommandAdmissionResult{}, ErrCommandConflict
		}
		return CommandAdmissionResult{Command: existing}, nil
	}
	commandValue, err := s.prepareRuntimeValue(admission.Payload, ContentGrant{RootID: admission.RootID, Scope: ContentGrantRoot})
	if err != nil {
		return CommandAdmissionResult{}, err
	}
	stamp := now()
	if err := insertRuntimeValue(ctx, tx, commandValue, stamp); err != nil {
		return CommandAdmissionResult{}, err
	}
	eventSeq, err := s.insertActorEventTx(ctx, tx, admission.RootID, "command.control.queued", actorEvent{
		AgentID: admission.AgentID, Status: "queued", CommandClientID: admission.ClientID, CommandID: admission.CommandID,
	}, stamp)
	if err != nil {
		return CommandAdmissionResult{}, err
	}
	// Positive root ingress values are reserved for model-input commands and
	// equal their inbox sequence. Actor-local controls use a separate negative
	// sequence so they cannot collide with that durable correlation.
	var ingressSeq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(ingress_seq),0)-1 FROM commands WHERE root_id=? AND ingress_seq<0`, admission.RootID).Scan(&ingressSeq); err != nil {
		return CommandAdmissionResult{}, err
	}
	inline, reference := runtimeValueColumns(commandValue.RuntimeValue)
	if _, err := tx.ExecContext(ctx, `INSERT INTO commands(client_id,command_id,scope,root_id,request_digest,status,payload_inline,payload_ref,ingress_seq,created_at,updated_at)
		VALUES(?,?,?,?,?,'queued',?,?,?,?,?)`, admission.ClientID, admission.CommandID, admission.Scope, admission.RootID,
		admission.RequestDigest, inline, reference, ingressSeq, stamp, stamp); err != nil {
		return CommandAdmissionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CommandAdmissionResult{}, err
	}
	return CommandAdmissionResult{Command: CommandRecord{
		ClientID: admission.ClientID, CommandID: admission.CommandID, Scope: admission.Scope, RootID: admission.RootID,
		RequestDigest: admission.RequestDigest, Status: "queued", IngressSeq: ingressSeq,
	}, EventSeq: eventSeq, New: true}, nil
}

func (s *Store) LoadCommand(ctx context.Context, clientID, commandID string) (CommandRecord, error) {
	record, found, err := loadCommand(ctx, s.db, clientID, commandID)
	if err != nil {
		return CommandRecord{}, err
	}
	if !found {
		return CommandRecord{}, sql.ErrNoRows
	}
	return record, nil
}

// CreateSessionForCommand commits the daemon command's only durable side
// effect and terminal outcome together, closing the crash window between the
// two records.
func (s *Store) CreateSessionForCommand(ctx context.Context, clientID, commandID string, kind SessionKind, cwd, model, provider string) (CommandRecord, error) {
	if err := validateSessionIdentity(kind, cwd, model, provider); err != nil {
		return CommandRecord{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CommandRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	record, found, err := loadCommandTx(ctx, tx, clientID, commandID)
	if err != nil {
		return CommandRecord{}, err
	}
	if !found || record.Scope != CommandScopeDaemon {
		return CommandRecord{}, errors.New("daemon session command was not admitted")
	}
	if record.Status != "queued" {
		return record, nil
	}
	rootID, err := runtimeID()
	if err != nil {
		return CommandRecord{}, err
	}
	stamp := now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(id,kind,created_at,updated_at,cwd,model,provider) VALUES(?,?,?,?,?,?,?)`,
		rootID, kind, stamp, stamp, cwd, model, provider); err != nil {
		return CommandRecord{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE commands SET status='succeeded',outcome_inline=?,updated_at=?
		WHERE client_id=? AND command_id=? AND status='queued'`, []byte(rootID), stamp, clientID, commandID)
	if err != nil {
		return CommandRecord{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return CommandRecord{}, err
		}
		return CommandRecord{}, errors.New("daemon session command changed during execution")
	}
	if err := tx.Commit(); err != nil {
		return CommandRecord{}, err
	}
	record.Status = "succeeded"
	record.Outcome = RuntimeValue{Inline: []byte(rootID)}
	return record, nil
}

type commandQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadCommand(ctx context.Context, q commandQueryer, clientID, commandID string) (CommandRecord, bool, error) {
	var record CommandRecord
	var rootID sql.NullString
	var outcomeReference string
	err := q.QueryRowContext(ctx, `SELECT c.client_id,c.command_id,c.scope,c.root_id,c.request_digest,c.status,c.ingress_seq,
		substr(c.outcome_inline,1,?),COALESCE(c.outcome_ref,''),COALESCE(r.digest,''),COALESCE(r.size,0),COALESCE(r.media_type,''),COALESCE(r.source,'')
		FROM commands c LEFT JOIN content_references r ON r.id=c.outcome_ref WHERE c.client_id=? AND c.command_id=?`,
		InlineValueLimit+1, clientID, commandID).Scan(&record.ClientID, &record.CommandID, &record.Scope, &rootID,
		&record.RequestDigest, &record.Status, &record.IngressSeq, &record.Outcome.Inline, &outcomeReference,
		&record.Outcome.Digest, &record.Outcome.Size, &record.Outcome.MediaType, &record.Outcome.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return CommandRecord{}, false, nil
	}
	if err != nil {
		return CommandRecord{}, false, err
	}
	record.RootID = rootID.String
	if outcomeReference != "" {
		record.Outcome.ReferenceID = outcomeReference
		record.Outcome.Inline = nil
	} else if len(record.Outcome.Inline) > InlineValueLimit {
		return CommandRecord{}, false, errors.New("command has an oversized inline outcome")
	}
	return record, true, nil
}

func loadCommandTx(ctx context.Context, tx *sql.Tx, clientID, commandID string) (CommandRecord, bool, error) {
	return loadCommand(ctx, tx, clientID, commandID)
}

func validateCommandAdmission(admission CommandAdmission) error {
	if admission.ClientID == "" || admission.CommandID == "" || admission.RequestDigest == "" {
		return errors.New("command admission requires client, command, and request digest")
	}
	switch admission.Scope {
	case CommandScopeRoot:
		if admission.RootID == "" || admission.AgentID == "" || admission.Kind == "" {
			return errors.New("root command admission requires root, agent, and kind")
		}
	case CommandScopeDaemon:
		if admission.RootID != "" || admission.AgentID != "" {
			return errors.New("daemon command admission cannot name a root or agent")
		}
		if len(admission.Payload.Data) > InlineValueLimit {
			return errors.New("daemon-scoped large content has no root grant")
		}
	default:
		return fmt.Errorf("invalid command scope %q", admission.Scope)
	}
	return nil
}
