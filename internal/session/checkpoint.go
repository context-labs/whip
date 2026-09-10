package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	MaxCheckpointBytes       = 40 << 20
	MaxRootCheckpointBytes   = 256 << 20
	MaxDaemonCheckpointBytes = 1 << 30
)

// checkpointIdentity validates the passive storage envelope without interpreting
// engine state. The kernel additionally checks exact build and ABI compatibility.
type checkpointIdentity struct {
	FormatVersion int    `json:"format_version"`
	RootID        string `json:"root_id"`
	AgentID       string `json:"agent_id"`
	Engine        string `json:"engine"`
	Bytes         int    `json:"bytes"`
	SHA256        string `json:"sha256"`
}

func validateCheckpoint(rootID, agentID string, envelope, image []byte) (checkpointIdentity, error) {
	var identity checkpointIdentity
	if len(envelope) > 1<<20 || json.Unmarshal(envelope, &identity) != nil {
		return identity, errors.New("invalid checkpoint envelope")
	}
	if rootID == "" || agentID == "" || identity.RootID != rootID || identity.AgentID != agentID {
		return identity, ErrAgentAccess
	}
	if identity.FormatVersion != 1 || (identity.Engine != "starlark" && identity.Engine != "quickjs") {
		return identity, errors.New("unsupported checkpoint format or engine")
	}
	if len(image) == 0 || len(image) > MaxCheckpointBytes || identity.Bytes != len(image) {
		return identity, errors.New("invalid checkpoint size")
	}
	sum := sha256.Sum256(image)
	if identity.SHA256 != hex.EncodeToString(sum[:]) {
		return identity, errors.New("checkpoint integrity mismatch")
	}
	return identity, nil
}

// SaveAgentCheckpoint atomically replaces one settled image. Failed publication
// leaves the last committed image available; checkpoints never enter content APIs.
func (s *Store) SaveAgentCheckpoint(ctx context.Context, rootID, agentID string, envelope, image []byte) error {
	identity, err := validateCheckpoint(rootID, agentID, envelope, image)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var engine string
	if err := tx.QueryRowContext(ctx, `SELECT s.execution_engine FROM sessions s JOIN agents a ON a.root_id=s.id WHERE s.id=? AND a.id=? AND a.status<>'deleted'`, rootID, agentID).Scan(&engine); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAgentAccess
		}
		return err
	}
	if identity.Engine != engine {
		return fmt.Errorf("checkpoint engine %s does not match session %s", identity.Engine, engine)
	}
	var rootBytes, daemonBytes int64
	if err := tx.QueryRowContext(ctx, `SELECT
 COALESCE(SUM(CASE WHEN root_id=? THEN bytes+length(envelope) ELSE 0 END),0),COALESCE(SUM(bytes+length(envelope)),0)
 FROM agent_checkpoints WHERE NOT (root_id=? AND agent_id=?)`, rootID, rootID, agentID).Scan(&rootBytes, &daemonBytes); err != nil {
		return err
	}
	// Preserved legacy Starlark originals consume the same storage budget.
	var legacyRoot, legacyDaemon int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN root_id=? THEN bytes+length(manifest) ELSE 0 END),0),COALESCE(SUM(bytes+length(manifest)),0) FROM agent_scratch`, rootID).Scan(&legacyRoot, &legacyDaemon); err != nil {
		return err
	}
	rootBytes += legacyRoot
	daemonBytes += legacyDaemon
	if rootBytes+int64(len(image)+len(envelope)) > MaxRootCheckpointBytes || daemonBytes+int64(len(image)+len(envelope)) > MaxDaemonCheckpointBytes {
		return errors.New("checkpoint storage quota exceeded")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_checkpoints(root_id,agent_id,envelope,image,bytes,updated_at) VALUES(?,?,?,?,?,?)
 ON CONFLICT(root_id,agent_id) DO UPDATE SET envelope=excluded.envelope,image=excluded.image,bytes=excluded.bytes,updated_at=excluded.updated_at`, rootID, agentID, envelope, image, len(image), now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) LoadAgentCheckpoint(ctx context.Context, rootID, agentID string) (envelope, image []byte, err error) {
	if rootID == "" || agentID == "" {
		return nil, nil, ErrAgentAccess
	}
	var engine string
	err = s.db.QueryRowContext(ctx, `SELECT c.envelope,substr(c.image,1,?),s.execution_engine
 FROM agent_checkpoints c JOIN sessions s ON s.id=c.root_id WHERE c.root_id=? AND c.agent_id=?`, MaxCheckpointBytes+1, rootID, agentID).Scan(&envelope, &image, &engine)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	identity, err := validateCheckpoint(rootID, agentID, envelope, image)
	if err != nil {
		return nil, nil, err
	}
	if identity.Engine != engine {
		return nil, nil, errors.New("checkpoint engine does not match session")
	}
	return envelope, image, nil
}
