package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

// TraceBodies returns canonical JSON only for the exact indexed source at a
// fixed revision. It never rebuilds a historical model request or follows content
// references. Each body is at most 1 MiB; a larger body rejects the whole export.
func (s *Store) TraceBodies(ctx context.Context, row session.TraceRow, revision int64) (input, output *string, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var head int64
	var exact bool
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0),EXISTS(SELECT 1 FROM trace_index WHERE root_id=? AND kind=? AND source_id=? AND sequence=? AND session_id=? AND turn_id=?) FROM trace_index WHERE root_id=?`, row.RootID, row.SourceKind, row.SourceID, row.Sequence, row.SessionID, row.TurnID, row.RootID).Scan(&head, &exact)
	if err != nil {
		return nil, nil, err
	}
	if !exact || head != revision {
		return nil, nil, ErrConflict
	}
	var inputSize, outputSize int64
	switch row.SourceKind {
	case "operation":
		err = tx.QueryRowContext(ctx, `SELECT substr(CAST(arguments AS BLOB),1,1048576),substr(CAST(result AS BLOB),1,1048576),length(CAST(arguments AS BLOB)),COALESCE(length(CAST(result AS BLOB)),0) FROM operations WHERE id=?`, row.SourceID).Scan(&input, &output, &inputSize, &outputSize)
	case "attempt":
		err = tx.QueryRowContext(ctx, `SELECT substr(CAST(COALESCE(m.parts,CASE WHEN c.id IS NOT NULL THEN json_array(json_object('type','text','text',c.text)) END) AS BLOB),1,1048576),COALESCE(length(CAST(COALESCE(m.parts,CASE WHEN c.id IS NOT NULL THEN json_array(json_object('type','text','text',c.text)) END) AS BLOB)),0) FROM model_attempts a LEFT JOIN messages m ON m.id=a.message_id AND m.turn_id=a.turn_id LEFT JOIN compactions c ON c.attempt_id=a.id AND c.turn_id=a.turn_id WHERE a.id=?`, row.SourceID).Scan(&output, &outputSize)
	case "cell":
		err = tx.QueryRowContext(ctx, `SELECT substr(CAST(json_extract(part.value,'$.call') AS BLOB),1,1048576),length(CAST(json_extract(part.value,'$.call') AS BLOB)),substr(CAST(result.parts AS BLOB),1,1048576),COALESCE(length(CAST(result.parts AS BLOB)),0)
 FROM cells c JOIN messages call ON call.id=c.call_message_id JOIN json_each(call.parts) part
 LEFT JOIN messages result ON result.id=c.result_message_id
 WHERE c.id=? AND json_extract(part.value,'$.type')='tool_call' AND json_extract(part.value,'$.call.id')=c.call_id`, row.SourceID).Scan(&input, &inputSize, &output, &outputSize)
	}
	if err != nil {
		return nil, nil, found(err)
	}
	if inputSize > 1<<20 || outputSize > 1<<20 {
		return nil, nil, fmt.Errorf("%w: canonical trace body exceeds 1 MiB", ErrLimit)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return input, output, nil
}
