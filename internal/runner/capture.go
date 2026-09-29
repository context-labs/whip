package runner

import (
	"context"

	"github.com/context-labs/whip/internal/model"
)

func (r *Runner) prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	capture := model.Inspect(request)
	prepared, err := r.provider.Prepare(ctx, request)
	if err == nil {
		capture.RequestDigest = prepared.Snapshot.RequestDigest
		prepared.Capture = &capture
	}
	return prepared, err
}
