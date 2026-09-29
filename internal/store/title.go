package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

const titleClient = session.AutomaticTitleClientID

func readTitleDecision(ctx context.Context, q querier, tree session.TreeID) (value session.AutomaticTitleDecision, err error) {
	var raw string
	err = q.QueryRowContext(ctx, "SELECT snapshot FROM automatic_title_decisions WHERE tree_id=?", tree).Scan(&raw)
	if err != nil {
		return value, found(err)
	}
	err = json.Unmarshal([]byte(raw), &value)
	return
}

func (s *Store) AutomaticTitleDecision(ctx context.Context, tree session.TreeID) (session.AutomaticTitleDecision, error) {
	return readTitleDecision(ctx, s.db, tree)
}

// initializeTitle runs within the transaction owning initialization. Existing
// decisions are final, including disabled, short, manual, and ineligible ones.
// Empty authored text alone leaves the root eligible for its first later text.
func initializeTitle(ctx context.Context, tx *sql.Tx, owner session.Session, input *session.Input, reason string) error {
	if owner.ParentID != nil {
		return nil
	}
	if _, err := readTitleDecision(ctx, tx, owner.TreeID); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	tree, err := readTree(ctx, tx, owner.TreeID)
	if err != nil {
		return err
	}
	value := session.AutomaticTitleDecision{
		TreeID: owner.TreeID, SessionID: owner.ID, ConfigRevision: owner.ConfigRevision,
		ExpectedRevision: tree.Revision, Enabled: owner.Config.AutomaticTitle,
		Model: owner.Config.Model.Clone(), Reason: reason, CreatedAt: timestamp(now()),
	}
	if owner.Config.Compaction.Model != nil {
		value.Model = owner.Config.Compaction.Model.Clone()
	}
	if input != nil {
		value.InputID = &input.ID
	}
	if tree.Metadata.Title != nil {
		value.Reason = "manual"
	}
	if value.Reason == "authored" {
		value.Source = session.TitleSource(input.Parts)
		if value.Source == "" {
			return nil
		}
		if tree.Revision == math.MaxInt64 {
			return ErrConflict
		}
		tree.Metadata.Title = new(session.TitleFallback(value.Source))
		raw, err := encode(tree.Metadata)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE session_trees SET metadata=?,revision=revision+1 WHERE id=?", raw, tree.ID); err != nil {
			return err
		}
		if err := bumpTreeCatalog(ctx, tx); err != nil {
			return err
		}
		value.ExpectedRevision++
		value.Reason = "eligible"
		if !value.Enabled {
			value.Reason = "disabled"
		} else if utf8.RuneCountInString(value.Source) < 20 {
			value.Reason = "short"
		}
	}
	raw, err := encode(value)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO automatic_title_decisions(tree_id,session_id,config_revision,expected_revision,eligible,snapshot,created_at) VALUES(?,?,?,?,?,?,?)`,
		value.TreeID, value.SessionID, value.ConfigRevision, value.ExpectedRevision, value.Reason == "eligible", raw, value.CreatedAt.UnixMicro())
	return err
}

// admitTitle creates one ordinary, deterministic input at a free session boundary.
// The caller considers all previously admitted human work first. This never runs
// in human admission, so queue capacity one cannot reject the user's prompt.
func admitTitle(ctx context.Context, tx *sql.Tx, owner session.Session) (*session.Input, error) {
	decision, err := readTitleDecision(ctx, tx, owner.TreeID)
	if errors.Is(err, ErrNotFound) || err == nil && (decision.Reason != "eligible" || decision.SessionID != owner.ID) {
		return nil, nil //nolint:nilnil // No independent naming intent is ordinary no-work.
	}
	if err != nil {
		return nil, err
	}
	identity := session.AutomaticTitleIdentity(owner.TreeID)
	if _, err := readReceipt(ctx, tx, identity); err == nil {
		return nil, nil //nolint:nilnil // Claimed, cancelled, interrupted, or completed naming never re-arms.
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	tree, err := readTree(ctx, tx, owner.TreeID)
	if err != nil {
		return nil, err
	}
	if tree.Revision != decision.ExpectedRevision {
		return nil, nil //nolint:nilnil // Manual metadata superseded the unclaimed intent.
	}
	digest, err := requestDigest(session.AutomaticTitlePurpose, decision)
	if err != nil {
		return nil, err
	}
	admitted, err := admitInput(ctx, tx, identity, digest, Submission{SessionID: owner.ID, Source: session.AgentInput, Kind: session.AutomaticTitleInputKind, Parts: []session.Part{}})
	return admitted.Input, err
}

func readTitleInput(ctx context.Context, q querier, turn session.TurnID) (session.AutomaticTitleDecision, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT d.snapshot FROM automatic_title_decisions d
 JOIN receipts r ON r.client_id=? AND r.request_id=d.tree_id
 JOIN inputs i ON i.id=r.input_id AND i.session_id=d.session_id
 WHERE i.turn_id=? AND i.kind='automatic_title' AND d.eligible=1`, titleClient, turn).Scan(&raw)
	if err != nil {
		return session.AutomaticTitleDecision{}, found(err)
	}
	var value session.AutomaticTitleDecision
	err = json.Unmarshal([]byte(raw), &value)
	return value, err
}

func (s *Store) AutomaticTitleInput(ctx context.Context, turn session.TurnID) (session.AutomaticTitleDecision, error) {
	return readTitleInput(ctx, s.db, turn)
}

func validateTitleAttempt(ctx context.Context, tx *sql.Tx, turn session.Turn, spec session.ModelAttemptSpec) error {
	decision, err := readTitleInput(ctx, tx, turn.ID)
	if err != nil {
		return err
	}
	if spec.Number != 1 || !spec.Request.Model.Equal(decision.Model) || spec.Request.TimeoutMillis > 20000 || turn.ConfigRevision != decision.ConfigRevision {
		return fmt.Errorf("%w: automatic title requires its captured model and a single bounded attempt", session.ErrInvalid)
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM model_attempts WHERE turn_id=?)", turn.ID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrConflict
	}
	return nil
}

func readTitleResult(ctx context.Context, q querier, tree session.TreeID, attempt session.ModelAttemptID) (value session.AutomaticTitleResult, err error) {
	var created int64
	value.TreeID, value.AttemptID = tree, attempt
	err = q.QueryRowContext(ctx, "SELECT text,applied,created_at FROM automatic_title_results WHERE tree_id=? AND attempt_id=?", tree, attempt).Scan(&value.Text, &value.Applied, &created)
	if err != nil {
		return value, found(err)
	}
	value.CreatedAt = timestamp(created)
	return
}

func (s *Store) AutomaticTitleResult(ctx context.Context, tree session.TreeID, attempt session.ModelAttemptID) (session.AutomaticTitleResult, error) {
	return readTitleResult(ctx, s.db, tree, attempt)
}

// SettleAutomaticTitle atomically commits billing, immutable bounded candidate
// evidence, and the whole-tree metadata CAS. Exact retries never reapply a title.
func (s *Store) SettleAutomaticTitle(ctx context.Context, id session.ModelAttemptID, outcome session.ModelAttemptResult, draft *session.AutomaticTitleDraft) (result session.AutomaticTitleSettlement, err error) {
	if err := outcome.Validate(); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		attempt, err := readAttempt(ctx, tx, id)
		if err != nil {
			return err
		}
		if attempt.Request.Purpose != session.AutomaticTitlePurpose {
			return fmt.Errorf("%w: attempt is not automatic naming", session.ErrInvalid)
		}
		decision, err := readTitleInput(ctx, tx, attempt.TurnID)
		if err != nil {
			return err
		}
		result.Attempt, err = settleAttempt(ctx, tx, attempt, outcome, nil)
		if err != nil {
			return err
		}
		if attempt.FinishedAt != nil {
			candidate, err := readTitleResult(ctx, tx, decision.TreeID, id)
			if errors.Is(err, ErrNotFound) {
				if draft != nil {
					result.Rejection = new("attempt already settled without a title candidate")
				}
				return nil
			}
			if err != nil {
				return err
			}
			if draft == nil || candidate.Text != draft.Text {
				return ErrConflict
			}
			result.Candidate = &candidate
			return nil
		}
		if draft == nil {
			return nil
		}
		if outcome.State != session.AttemptSucceeded || session.ValidateAutomaticTitle(draft.Text) != nil {
			result.Rejection = new("automatic title candidate requires a successful bounded single-line result")
			return nil //nolint:nilerr // Invalid candidate text is billed evidence, not a transaction failure.
		}
		tree, err := readTree(ctx, tx, decision.TreeID)
		if err != nil {
			return err
		}
		turn, err := readTurn(ctx, tx, attempt.TurnID)
		if err != nil {
			return err
		}
		applied := tree.Revision == decision.ExpectedRevision && tree.Revision < math.MaxInt64 && turn.State == session.Running
		if applied {
			if err := bumpTreeCatalog(ctx, tx); errors.Is(err, ErrConflict) {
				// Exhausted metadata counters cannot prevent settlement of incurred billing.
				applied = false
			} else if err != nil {
				return err
			}
		}
		if applied {
			tree.Metadata.Title = &draft.Text
			raw, err := encode(tree.Metadata)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE session_trees SET metadata=?,revision=revision+1 WHERE id=? AND revision=?", raw, tree.ID, decision.ExpectedRevision); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO automatic_title_results(attempt_id,tree_id,text,applied,created_at) VALUES(?,?,?,?,?)", id, decision.TreeID, draft.Text, applied, now()); err != nil {
			return err
		}
		candidate, err := readTitleResult(ctx, tx, decision.TreeID, id)
		result.Candidate = &candidate
		return err
	})
	return
}
