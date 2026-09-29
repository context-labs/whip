package store

import (
	"context"
	"database/sql"

	"github.com/context-labs/whip/internal/session"
)

func goalCreationDigest(owner session.SessionID, expected *session.GoalRef, request session.GoalRequest, start bool) (string, error) {
	return requestDigest("goal_create", struct {
		Owner    session.SessionID
		Expected *session.GoalRef
		Spec     session.GoalRequest
		Start    bool
	}{owner, expected, request, start})
}

// MatchGoalCreation resolves accepted intent, including deleted-owner evidence,
// without depending on mutable host defaults or current session eligibility.
func (s *Store) MatchGoalCreation(ctx context.Context, owner session.SessionID, id session.GoalID, expected *session.GoalRef, request session.GoalRequest, start bool) (result GoalAdmission, err error) {
	for _, value := range []string{string(owner), string(id)} {
		if err := session.ValidateID(value); err != nil {
			return result, err
		}
	}
	digest, err := goalCreationDigest(owner, expected, request, start)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		var previous string
		if err := tx.QueryRowContext(ctx, "SELECT initial_digest FROM goals WHERE id=?", id).Scan(&previous); err != nil {
			return found(err)
		}
		if previous != digest {
			return ErrConflict
		}
		result, err = goalAdmission(ctx, tx, owner, id)
		return err
	})
	return
}
