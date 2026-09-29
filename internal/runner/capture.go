package runner

import (
	"context"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func (r *Runner) prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	var evidence *session.ModelContextEvidence
	if request.Purpose == "" || request.Purpose == "turn" || request.Purpose == "final" {
		if source, ok := r.transcript.(interface {
			ModelContextScope(context.Context, session.TurnID) (session.ModelContextScope, error)
		}); ok {
			scope, err := source.ModelContextScope(ctx, request.TurnID)
			if err != nil {
				return model.Prepared{}, err
			}
			if scope.SessionID != request.SessionID {
				return model.Prepared{}, session.ErrInvalid
			}
			evidence = &session.ModelContextEvidence{ModelContextScope: scope, EstimatedTokens: model.EstimateInputTokens(request)}
		}
	}
	capture := model.Inspect(request)
	prepared, err := r.provider.Prepare(ctx, request)
	if err == nil {
		capture.RequestDigest = prepared.Snapshot.RequestDigest
		prepared.Capture = &capture
		if evidence != nil {
			if prepared.ContextWindowTokens != nil {
				prepared.ContextWindowTokens = new(*prepared.ContextWindowTokens)
				evidence.ContextWindowTokens = new(*prepared.ContextWindowTokens)
			}
			prepared.Snapshot.Context = evidence
		}
	}
	return prepared, err
}
