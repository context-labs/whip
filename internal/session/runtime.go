package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	contentstore "github.com/context-labs/whip/internal/content"
)

const (
	InlineValueLimit = 8 << 10
	MaxContentRead   = contentstore.MaxReadSize
)

var ErrContentAccess = errors.New("content reference is not authorized")

type CommandScope string

const (
	CommandScopeDaemon CommandScope = "daemon"
	CommandScopeRoot   CommandScope = "root"
)

type ContentGrantScope string

const (
	ContentGrantRoot    ContentGrantScope = "root"
	ContentGrantAgent   ContentGrantScope = "agent"
	ContentGrantSubtree ContentGrantScope = "subtree"
)

type RuntimePayload struct {
	Data      []byte
	MediaType string
	Source    string
}

type RuntimeValue struct {
	Inline      []byte
	ReferenceID string
	Digest      string
	Size        int64
	MediaType   string
	Source      string
}

type ContentMetadata struct {
	ReferenceID string
	Digest      string
	Size        int64
	MediaType   string
	Source      string
}

type ContentGrant struct {
	RootID  string
	AgentID string
	Scope   ContentGrantScope
}

type RuntimeAgent struct {
	ID       string
	RootID   string
	ParentID string
	Status   string
}

type RuntimeCommand struct {
	ClientID      string
	ID            string
	Scope         CommandScope
	RootID        string
	RequestDigest string
	Status        string
	Payload       RuntimePayload
}

type RuntimeInbox struct {
	RootID  string
	AgentID string
	Seq     int64
	Kind    string
	Status  string
	Payload RuntimePayload
}

type RuntimeState struct {
	RootID        string
	AgentID       string
	Key           string
	Version       int64
	AuthorAgentID string
	Payload       RuntimePayload
}

type RuntimeEvent struct {
	RootID  string
	Seq     int64
	Kind    string
	Payload RuntimePayload
}

type RuntimeUsage struct {
	ID              string
	RootID          string
	AgentID         string
	CommandClientID string
	CommandID       string
	InputTokens     int64
	CachedTokens    int64
	OutputTokens    int64
	CostMicros      int64
}

type RuntimeTransition struct {
	Agent   *RuntimeAgent
	Command *RuntimeCommand
	Inbox   *RuntimeInbox
	State   *RuntimeState
	Event   *RuntimeEvent
	Usage   *RuntimeUsage
}

type RuntimeResult struct {
	Command RuntimeValue
	Inbox   RuntimeValue
	State   RuntimeValue
	Event   RuntimeValue
}

type preparedRuntimeValue struct {
	RuntimeValue
	grant ContentGrant
}

func (s *Store) CommitRuntime(ctx context.Context, transition RuntimeTransition) (RuntimeResult, error) {
	return s.commitRuntime(ctx, transition, nil)
}

func (s *Store) commitRuntime(ctx context.Context, transition RuntimeTransition, beforeCommit func() error) (RuntimeResult, error) {
	if transition.Command != nil {
		c := transition.Command
		if c.Scope != CommandScopeRoot && c.Scope != CommandScopeDaemon || c.Scope == CommandScopeRoot && c.RootID == "" || c.Scope == CommandScopeDaemon && c.RootID != "" {
			return RuntimeResult{}, fmt.Errorf("command scope %q does not match root %q", c.Scope, c.RootID)
		}
	}
	var transitionRoot string
	addRoot := func(kind, rootID string) error {
		if rootID == "" {
			return fmt.Errorf("%s requires a root", kind)
		}
		if transitionRoot == "" {
			transitionRoot = rootID
			return nil
		}
		if transitionRoot != rootID {
			return fmt.Errorf("%s root %q does not match transition root %q", kind, rootID, transitionRoot)
		}
		return nil
	}
	type rooted struct{ kind, id string }
	var roots []rooted
	if v := transition.Agent; v != nil {
		roots = append(roots, rooted{"agent", v.RootID})
	}
	if v := transition.Command; v != nil && v.Scope == CommandScopeRoot {
		roots = append(roots, rooted{"command", v.RootID})
	}
	if v := transition.Inbox; v != nil {
		roots = append(roots, rooted{"inbox item", v.RootID})
	}
	if v := transition.State; v != nil {
		roots = append(roots, rooted{"state update", v.RootID})
	}
	if v := transition.Event; v != nil {
		roots = append(roots, rooted{"event", v.RootID})
	}
	if v := transition.Usage; v != nil {
		roots = append(roots, rooted{"usage charge", v.RootID})
	}
	for _, item := range roots {
		if err := addRoot(item.kind, item.id); err != nil {
			return RuntimeResult{}, err
		}
	}
	if transition.Command != nil && transition.Command.Scope == CommandScopeDaemon && transitionRoot != "" {
		return RuntimeResult{}, fmt.Errorf("daemon command cannot share a transition with root %q", transitionRoot)
	}
	var prepared struct {
		command, inbox, state, event preparedRuntimeValue
	}
	var err error
	if transition.Command != nil {
		grant := ContentGrant{RootID: transition.Command.RootID, Scope: ContentGrantRoot}
		if transition.Command.Scope == CommandScopeDaemon && len(transition.Command.Payload.Data) > InlineValueLimit {
			return RuntimeResult{}, fmt.Errorf("daemon-scoped large content has no root grant")
		}
		prepared.command, err = s.prepareRuntimeValue(transition.Command.Payload, grant)
	}
	if err == nil && transition.Inbox != nil {
		prepared.inbox, err = s.prepareRuntimeValue(transition.Inbox.Payload, ContentGrant{RootID: transition.Inbox.RootID, AgentID: transition.Inbox.AgentID, Scope: ContentGrantAgent})
	}
	if err == nil && transition.State != nil {
		prepared.state, err = s.prepareRuntimeValue(transition.State.Payload, ContentGrant{RootID: transition.State.RootID, AgentID: transition.State.AgentID, Scope: ContentGrantAgent})
	}
	if err == nil && transition.Event != nil {
		prepared.event, err = s.prepareRuntimeValue(transition.Event.Payload, ContentGrant{RootID: transition.Event.RootID, Scope: ContentGrantRoot})
	}
	if err != nil {
		return RuntimeResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeResult{}, err
	}
	defer tx.Rollback()
	stamp := now()
	if transition.Agent != nil {
		a := transition.Agent
		if _, err := tx.ExecContext(ctx, `INSERT INTO agents(id,root_id,parent_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
			a.ID, a.RootID, nullableString(a.ParentID), a.Status, stamp, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	for _, value := range []preparedRuntimeValue{prepared.command, prepared.inbox, prepared.state, prepared.event} {
		if err := insertRuntimeValue(ctx, tx, value, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	if transition.Command != nil {
		c := transition.Command
		rootID := nullableString(c.RootID)
		inline, reference := runtimeValueColumns(prepared.command.RuntimeValue)
		if _, err := tx.ExecContext(ctx, `INSERT INTO commands(client_id,command_id,scope,root_id,request_digest,status,payload_inline,payload_ref,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			c.ClientID, c.ID, c.Scope, rootID, c.RequestDigest, c.Status, inline, reference, stamp, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	if transition.Inbox != nil {
		v := transition.Inbox
		inline, reference := runtimeValueColumns(prepared.inbox.RuntimeValue)
		if _, err := tx.ExecContext(ctx, `INSERT INTO inbox(root_id,agent_id,seq,kind,status,payload_inline,payload_ref,created_at) VALUES(?,?,?,?,?,?,?,?)`,
			v.RootID, v.AgentID, v.Seq, v.Kind, v.Status, inline, reference, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	if transition.State != nil {
		v := transition.State
		inline, reference := runtimeValueColumns(prepared.state.RuntimeValue)
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_state(root_id,agent_id,key,version,author_agent_id,payload_inline,payload_ref,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
			v.RootID, v.AgentID, v.Key, v.Version, v.AuthorAgentID, inline, reference, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	if transition.Event != nil {
		v := transition.Event
		inline, reference := runtimeValueColumns(prepared.event.RuntimeValue)
		if _, err := tx.ExecContext(ctx, `INSERT INTO events(root_id,seq,kind,payload_inline,payload_ref,created_at) VALUES(?,?,?,?,?,?)`,
			v.RootID, v.Seq, v.Kind, inline, reference, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	if transition.Usage != nil {
		v := transition.Usage
		if v.AgentID != "" {
			var agents int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE root_id=? AND id=?`, v.RootID, v.AgentID).Scan(&agents); err != nil {
				return RuntimeResult{}, err
			}
			if agents != 1 {
				return RuntimeResult{}, fmt.Errorf("usage agent is not in root")
			}
		}
		if (v.CommandClientID == "") != (v.CommandID == "") {
			return RuntimeResult{}, fmt.Errorf("usage command identity is incomplete")
		}
		if v.CommandID != "" {
			var commands int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM commands WHERE root_id=? AND client_id=? AND command_id=?`, v.RootID, v.CommandClientID, v.CommandID).Scan(&commands); err != nil {
				return RuntimeResult{}, err
			}
			if commands != 1 {
				return RuntimeResult{}, fmt.Errorf("usage command is not in root")
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_charges(id,root_id,agent_id,command_client_id,command_id,input_tokens,cached_tokens,output_tokens,cost_micros,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			v.ID, v.RootID, v.AgentID, v.CommandClientID, v.CommandID, v.InputTokens, v.CachedTokens, v.OutputTokens, v.CostMicros, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	if beforeCommit != nil {
		if err := beforeCommit(); err != nil {
			return RuntimeResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return RuntimeResult{}, err
	}
	return RuntimeResult{
		Command: prepared.command.RuntimeValue,
		Inbox:   prepared.inbox.RuntimeValue,
		State:   prepared.state.RuntimeValue,
		Event:   prepared.event.RuntimeValue,
	}, nil
}

func (s *Store) FinishCommand(ctx context.Context, clientID, commandID, status string, outcome RuntimePayload) (RuntimeValue, error) {
	if status != "succeeded" && status != "failed" && status != "cancelled" && status != "interrupted" {
		return RuntimeValue{}, fmt.Errorf("command outcome status %q is not terminal", status)
	}
	var scope CommandScope
	var rootID sql.NullString
	var currentStatus string
	if err := s.db.QueryRowContext(ctx, `SELECT scope,root_id,status FROM commands WHERE client_id=? AND command_id=?`, clientID, commandID).Scan(&scope, &rootID, &currentStatus); err != nil {
		return RuntimeValue{}, err
	}
	if currentStatus != "queued" && currentStatus != "running" && currentStatus != "waiting" {
		return RuntimeValue{}, fmt.Errorf("command is already terminal with status %q", currentStatus)
	}
	if scope == CommandScopeDaemon && len(outcome.Data) > InlineValueLimit {
		return RuntimeValue{}, fmt.Errorf("daemon-scoped large content has no root grant")
	}
	prepared, err := s.prepareRuntimeValue(outcome, ContentGrant{RootID: rootID.String, Scope: ContentGrantRoot})
	if err != nil {
		return RuntimeValue{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeValue{}, err
	}
	defer tx.Rollback()
	stamp := now()
	if err := insertRuntimeValue(ctx, tx, prepared, stamp); err != nil {
		return RuntimeValue{}, err
	}
	inline, reference := runtimeValueColumns(prepared.RuntimeValue)
	result, err := tx.ExecContext(ctx, `UPDATE commands SET status=?,outcome_inline=?,outcome_ref=?,updated_at=? WHERE client_id=? AND command_id=? AND status IN ('queued','running','waiting')`,
		status, inline, reference, stamp, clientID, commandID)
	if err != nil {
		return RuntimeValue{}, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return RuntimeValue{}, err
		}
		return RuntimeValue{}, fmt.Errorf("command became terminal before outcome commit")
	}
	if err := tx.Commit(); err != nil {
		return RuntimeValue{}, err
	}
	return prepared.RuntimeValue, nil
}

func (s *Store) StoreContent(ctx context.Context, grant ContentGrant, payload RuntimePayload) (RuntimeValue, error) {
	if err := s.validateContentGrant(ctx, grant); err != nil {
		return RuntimeValue{}, err
	}
	prepared, err := s.prepareContentReference(payload, grant)
	if err != nil {
		return RuntimeValue{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeValue{}, err
	}
	defer tx.Rollback()
	if err := insertRuntimeValue(ctx, tx, prepared, now()); err != nil {
		return RuntimeValue{}, err
	}
	if err := tx.Commit(); err != nil {
		return RuntimeValue{}, err
	}
	return prepared.RuntimeValue, nil
}

func (s *Store) ReadContent(ctx context.Context, referenceID, rootID, agentID string, offset int64, length int) ([]byte, ContentMetadata, error) {
	var meta ContentMetadata
	meta.ReferenceID = referenceID
	if err := s.db.QueryRowContext(ctx, `SELECT r.digest,r.size,r.media_type,r.source FROM content_references r WHERE r.id=?`, referenceID).
		Scan(&meta.Digest, &meta.Size, &meta.MediaType, &meta.Source); err != nil {
		return nil, ContentMetadata{}, ErrContentAccess
	}
	rows, err := s.db.QueryContext(ctx, `SELECT scope,agent_id FROM content_grants WHERE reference_id=? AND root_id=? AND revoked_at=''`, referenceID, rootID)
	if err != nil {
		return nil, ContentMetadata{}, err
	}
	type grant struct {
		scope   ContentGrantScope
		agentID string
	}
	var grants []grant
	for rows.Next() {
		var g grant
		if err := rows.Scan(&g.scope, &g.agentID); err != nil {
			rows.Close()
			return nil, ContentMetadata{}, err
		}
		grants = append(grants, g)
	}
	if err := rows.Close(); err != nil {
		return nil, ContentMetadata{}, err
	}
	authorized := false
	for _, grant := range grants {
		switch grant.scope {
		case ContentGrantRoot:
			authorized = agentID == "" || s.agentInRoot(ctx, rootID, agentID)
		case ContentGrantAgent:
			authorized = agentID != "" && agentID == grant.agentID && s.agentInRoot(ctx, rootID, agentID)
		case ContentGrantSubtree:
			authorized = agentID != "" && s.agentInSubtree(ctx, rootID, grant.agentID, agentID)
		}
		if authorized {
			break
		}
	}
	if !authorized {
		return nil, ContentMetadata{}, ErrContentAccess
	}
	data, err := s.content.Read(meta.Digest, offset, length)
	return data, meta, err
}

func (s *Store) RevokeContentGrant(ctx context.Context, referenceID, rootID, agentID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE content_grants SET revoked_at=? WHERE reference_id=? AND root_id=? AND agent_id=? AND revoked_at=''`,
		now(), referenceID, rootID, agentID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrContentAccess
	}
	return nil
}

type ContentOrphan struct {
	Digest      string
	Size        int64
	FirstSeenAt string
	LastSeenAt  string
}

func (s *Store) OrphanContent(ctx context.Context) ([]ContentOrphan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT digest,size,first_seen_at,last_seen_at FROM content_orphans ORDER BY digest`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContentOrphan
	for rows.Next() {
		var orphan ContentOrphan
		if err := rows.Scan(&orphan.Digest, &orphan.Size, &orphan.FirstSeenAt, &orphan.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, orphan)
	}
	return out, rows.Err()
}

func (s *Store) prepareRuntimeValue(payload RuntimePayload, grant ContentGrant) (preparedRuntimeValue, error) {
	value := RuntimeValue{Size: int64(len(payload.Data)), MediaType: payload.MediaType, Source: payload.Source}
	if len(payload.Data) <= InlineValueLimit {
		value.Inline = append([]byte(nil), payload.Data...)
		return preparedRuntimeValue{RuntimeValue: value}, nil
	}
	return s.prepareContentReference(payload, grant)
}

func (s *Store) prepareContentReference(payload RuntimePayload, grant ContentGrant) (preparedRuntimeValue, error) {
	if grant.RootID == "" {
		return preparedRuntimeValue{}, fmt.Errorf("large content requires a root grant")
	}
	if grant.Scope != ContentGrantRoot && grant.Scope != ContentGrantAgent && grant.Scope != ContentGrantSubtree {
		return preparedRuntimeValue{}, fmt.Errorf("invalid content grant scope %q", grant.Scope)
	}
	if grant.Scope != ContentGrantRoot && grant.AgentID == "" {
		return preparedRuntimeValue{}, fmt.Errorf("%s content grant requires an agent", grant.Scope)
	}
	body, err := s.content.Put(payload.Data)
	if err != nil {
		return preparedRuntimeValue{}, err
	}
	referenceID, err := runtimeID()
	if err != nil {
		return preparedRuntimeValue{}, err
	}
	value := RuntimeValue{
		ReferenceID: referenceID,
		Digest:      body.Digest,
		Size:        int64(len(payload.Data)),
		MediaType:   payload.MediaType,
		Source:      payload.Source,
	}
	return preparedRuntimeValue{RuntimeValue: value, grant: grant}, nil
}

func insertRuntimeValue(ctx context.Context, tx *sql.Tx, value preparedRuntimeValue, stamp string) error {
	if value.ReferenceID == "" {
		return nil
	}
	if value.grant.Scope == ContentGrantRoot {
		if value.grant.AgentID != "" {
			return fmt.Errorf("root content grant cannot name an agent")
		}
	} else {
		var agents int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE root_id=? AND id=?`, value.grant.RootID, value.grant.AgentID).Scan(&agents); err != nil {
			return err
		}
		if agents != 1 {
			return fmt.Errorf("content grant agent is not in root")
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO content_objects(digest,size,created_at) VALUES(?,?,?)`, value.Digest, value.Size, stamp); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO content_references(id,digest,size,media_type,source,created_at) VALUES(?,?,?,?,?,?)`,
		value.ReferenceID, value.Digest, value.Size, value.MediaType, value.Source, stamp); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO content_grants(reference_id,root_id,agent_id,scope,created_at) VALUES(?,?,?,?,?)`,
		value.ReferenceID, value.grant.RootID, value.grant.AgentID, value.grant.Scope, stamp)
	return err
}

func runtimeValueColumns(value RuntimeValue) (any, any) {
	if value.ReferenceID != "" {
		return nil, value.ReferenceID
	}
	return value.Inline, nil
}

func (s *Store) validateContentGrant(ctx context.Context, grant ContentGrant) error {
	if grant.RootID == "" {
		return fmt.Errorf("content grant requires a root")
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=?`, grant.RootID).Scan(&exists); err != nil {
		return err
	}
	if exists != 1 {
		return fmt.Errorf("unknown content root %q", grant.RootID)
	}
	if grant.Scope == ContentGrantRoot && grant.AgentID == "" {
		return nil
	}
	if grant.Scope != ContentGrantAgent && grant.Scope != ContentGrantSubtree {
		return fmt.Errorf("invalid content grant")
	}
	if !s.agentInRoot(ctx, grant.RootID, grant.AgentID) {
		return fmt.Errorf("content grant agent is not in root")
	}
	return nil
}

func (s *Store) agentInRoot(ctx context.Context, rootID, agentID string) bool {
	var n int
	return s.db.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE root_id=? AND id=?`, rootID, agentID).Scan(&n) == nil && n == 1
}

func (s *Store) agentInSubtree(ctx context.Context, rootID, ancestorID, agentID string) bool {
	var n int
	err := s.db.QueryRowContext(ctx, `WITH RECURSIVE descendants(id) AS (
		SELECT id FROM agents WHERE root_id=? AND id=?
		UNION
		SELECT a.id FROM agents a JOIN descendants d ON a.parent_id=d.id WHERE a.root_id=?
	) SELECT count(*) FROM descendants WHERE id=?`, rootID, ancestorID, rootID, agentID).Scan(&n)
	return err == nil && n == 1
}

func runtimeID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Recover applies daemon-startup recovery after exclusive ownership is held.
// Opening the store alone must not interrupt work owned by a running daemon.
func (s *Store) Recover(ctx context.Context) error {
	if err := recoverRuntime(ctx, s); err != nil {
		return err
	}
	return recordOrphanContent(ctx, s)
}

func recoverRuntime(ctx context.Context, s *Store) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now()
	for _, table := range []string{"commands", "turns", "child_executions", "operations", "leases"} {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET status='interrupted',updated_at=? WHERE status IN ('queued','running','waiting')`, stamp); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func recordOrphanContent(ctx context.Context, s *Store) error {
	referenced := map[string]struct{}{}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT digest FROM content_references`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var digest string
		if err := rows.Scan(&digest); err != nil {
			rows.Close()
			return err
		}
		referenced[digest] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	orphans, err := s.content.Orphans(referenced)
	if err != nil {
		return err
	}
	if len(orphans) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now()
	for _, orphan := range orphans {
		if _, err := tx.ExecContext(ctx, `INSERT INTO content_orphans(digest,size,first_seen_at,last_seen_at) VALUES(?,?,?,?)
			ON CONFLICT(digest) DO UPDATE SET size=excluded.size,last_seen_at=excluded.last_seen_at`,
			orphan.Digest, orphan.Size, stamp, stamp); err != nil {
			return err
		}
	}
	return tx.Commit()
}
