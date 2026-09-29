package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/context-labs/whip/internal/session"
)

// matchAdmission observes only existing durable evidence. It never admits input,
// starts work, checks mutable session policy or resolves current host defaults.
func (s *Store) matchAdmission(ctx context.Context, identity session.RequestIdentity, digest string) (result Admission, err error) {
	for _, id := range []string{identity.ClientID, identity.RequestID} {
		if err := session.ValidateID(id); err != nil {
			return result, err
		}
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		receipt, err := readReceipt(ctx, tx, identity)
		if errors.Is(err, ErrNotFound) {
			if _, reserved := readReceipt(ctx, tx, childTransferIdentity(identity)); reserved == nil {
				return ErrConflict
			} else if !errors.Is(reserved, ErrNotFound) {
				return reserved
			}
		}
		if err != nil {
			return err
		}
		if receipt.Digest != digest {
			return ErrConflict
		}
		result, err = readAdmission(ctx, tx, identity)
		return err
	})
	return
}

func (s *Store) MatchSubmission(ctx context.Context, identity session.RequestIdentity, request Submission) (Admission, error) {
	request, err := normalizeSubmission(request)
	if err != nil {
		return Admission{}, err
	}
	digest, err := requestDigest("submit", request)
	if err != nil {
		return Admission{}, err
	}
	return s.matchAdmission(ctx, identity, digest)
}

func (s *Store) MatchChild(ctx context.Context, identity session.RequestIdentity, request ChildRequest) (Admission, error) {
	if len(request.BrowserAttachments) != 0 {
		result, err := s.ChildTransferResult(ctx, identity, request)
		return result.Admission, err
	}
	digest, err := requestDigest("spawn_child", request)
	if err != nil {
		return Admission{}, err
	}
	return s.matchAdmission(ctx, identity, digest)
}

func goalFormulationDigest(owner session.SessionID, request session.GoalFormulationRequest) (string, error) {
	return requestDigest("goal_formulation", struct {
		Owner   session.SessionID
		Request session.GoalFormulationRequest
	}{owner, request})
}

func (s *Store) MatchGoalFormulation(ctx context.Context, identity session.RequestIdentity, owner session.SessionID, request session.GoalFormulationRequest) (Admission, error) {
	digest, err := goalFormulationDigest(owner, request)
	if err != nil {
		return Admission{}, err
	}
	return s.matchAdmission(ctx, identity, digest)
}

func goalResumeDigest(owner session.SessionID, ref session.GoalRef) (string, error) {
	return requestDigest("goal_resume", struct {
		Owner session.SessionID
		Goal  session.GoalRef
	}{owner, ref})
}

func (s *Store) MatchGoalResume(ctx context.Context, identity session.RequestIdentity, owner session.SessionID, ref session.GoalRef) (Admission, error) {
	digest, err := goalResumeDigest(owner, ref)
	if err != nil {
		return Admission{}, err
	}
	return s.matchAdmission(ctx, identity, digest)
}

func hostOperationSubmission(owner session.SessionID, operation session.HostOperation) (Submission, string, error) {
	operation, err := operation.Normalize()
	if err != nil {
		return Submission{}, "", err
	}
	request := Submission{SessionID: owner, Source: session.UserInput, Kind: session.HostOperationInputKind, Parts: []session.Part{}, HostOperation: &operation}
	digest, err := requestDigest("host_operation", request)
	return request, digest, err
}

func (s *Store) MatchHostOperation(ctx context.Context, identity session.RequestIdentity, owner session.SessionID, operation session.HostOperation) (Admission, error) {
	_, digest, err := hostOperationSubmission(owner, operation)
	if err != nil {
		return Admission{}, err
	}
	return s.matchAdmission(ctx, identity, digest)
}
