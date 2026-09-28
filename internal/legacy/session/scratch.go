package session

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// SaveAgentScratch stores one node's Starlark scratch snapshot: the structured data
// and helper definitions plus the manifest describing what it holds.
// It is runtime-owned state, bounded by the kernel's caps rather than
// budgets, and survives worker eviction and daemon restarts.
func (s *Store) SaveAgentScratch(ctx context.Context, rootID, agentID, snapshot string, manifest []byte) error {
	if rootID == "" || agentID == "" {
		return ErrAgentAccess
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_scratch(root_id,agent_id,snapshot,manifest,bytes,updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(root_id,agent_id) DO UPDATE SET snapshot=excluded.snapshot,manifest=excluded.manifest,bytes=excluded.bytes,updated_at=excluded.updated_at`,
		rootID, agentID, snapshot, string(manifest), len(snapshot), now())
	return err
}

// LoadAgentScratch returns the stored snapshot, or empty values when none.
func (s *Store) LoadAgentScratch(ctx context.Context, rootID, agentID string) (snapshot string, manifest []byte, err error) {
	if rootID == "" || agentID == "" {
		return "", nil, ErrAgentAccess
	}
	var encoded string
	err = s.db.QueryRowContext(ctx, `SELECT snapshot,manifest FROM agent_scratch WHERE root_id=? AND agent_id=?`, rootID, agentID).Scan(&snapshot, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(snapshot) == "" {
		return "", nil, errors.New("empty stored scratch snapshot")
	}
	return snapshot, []byte(encoded), nil
}

// ScratchSkip names a global a restore could not revive, with the reason.
type ScratchSkip struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// RecordScratchRestore appends a scratch.restored actor event so a worker
// restart and what it revived are auditable without trusting the model's
// account of its ephemeral notice.
func (s *Store) RecordScratchRestore(ctx context.Context, rootID, agentID string, restored []string, notRestored []ScratchSkip) (int64, error) {
	if rootID == "" || agentID == "" {
		return 0, ErrAgentAccess
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	seq, err := s.insertActorEventTx(ctx, tx, rootID, "scratch.restored", actorEvent{
		AgentID: agentID, Status: "restored", Restored: restored, NotRestored: notRestored,
	}, now())
	if err != nil {
		return 0, err
	}
	return seq, tx.Commit()
}
