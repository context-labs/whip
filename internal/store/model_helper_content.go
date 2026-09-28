package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

// RegisterModelHelperContent follows durable publication of the actual provider
// output bytes. It registers only metadata for an already-accounted successful
// item; the caller must never repeat model execution to repair publication.
// Derived evidence consumes normal content capacity, not a user logical write.
func (s *Store) RegisterModelHelperContent(ctx context.Context, id session.ModelAttemptID, digest string, size int64) (result session.ContentReference, err error) {
	if err := session.ValidateID(string(id)); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		attempt, err := readAttempt(ctx, tx, id)
		if err != nil {
			return err
		}
		if attempt.Request.Purpose != session.ModelHelperPurpose || attempt.State != session.AttemptSucceeded || attempt.FinishedAt == nil || attempt.DispatchedAt == nil || attempt.OperationID == nil || attempt.BatchIndex == nil {
			return ErrConflict
		}
		turn, err := readTurn(ctx, tx, attempt.TurnID)
		if err != nil {
			return err
		}
		operation, err := readOperation(ctx, tx, *attempt.OperationID)
		if err != nil {
			return err
		}
		if operation.TurnID != turn.ID || operation.SessionID != turn.SessionID || operation.DispatchedAt == nil || operation.Capability != "models.call" && operation.Capability != "models.batch" {
			return ErrConflict
		}
		reference := session.ContentReference{
			ID:        fmt.Sprintf("helper_output_%x", sha256.Sum256([]byte("whip.model-helper-output.v1\x00"+string(id)))),
			SessionID: turn.SessionID, Digest: digest, Size: size, MediaType: "text/plain",
		}
		if err := reference.Validate(); err != nil {
			return err
		}
		// Exact metadata retries remain valid after operation/turn settlement.
		// The owner and operation reads above prevent resurrection after deletion.
		_, err = readContent(ctx, tx, reference.SessionID, reference.ID)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				return err
			}
			if err := validateHelperOperation(ctx, tx, turn, operation.ID, *attempt.BatchIndex, attempt.Request.MaxOutputTokens); err != nil {
				return err
			}
		}
		result, err = registerContent(ctx, tx, reference)
		return err
	})
	return
}
