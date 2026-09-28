package model

import (
	"context"
	"fmt"

	"github.com/context-labs/whip/internal/inferenceauth"
	"github.com/context-labs/whip/internal/session"
)

// InferenceAuth borrows the command's private machine-key owner. Management
// authorization is deliberately absent from this inference-only boundary.
type InferenceAuth interface {
	Capture(context.Context) (inferenceauth.CapturedMachineKey, error)
	Check(context.Context, inferenceauth.CapturedMachineKey) error
}

func (p OpenAI) captureInference(ctx context.Context, route Route) (string, func(context.Context) error, error) {
	if route.Kind != "openai-chat" || route.URL != "https://api.inference.net/v1" || route.Credential != "" {
		return "", nil, fmt.Errorf("%w: managed Inference.net credentials require their exact chat gateway and no other credential", session.ErrInvalid)
	}
	if p.InferenceAuth == nil {
		return "", nil, inferenceauth.ErrKeyRequired
	}
	captured, err := p.InferenceAuth.Capture(ctx)
	if err != nil {
		return "", nil, err
	}
	// The closure owns one capture across all recorded retries. Recheck after
	// reservation and immediately before HTTP; replacement needs a new Prepare.
	return captured.Key, func(ctx context.Context) error { return p.InferenceAuth.Check(ctx, captured) }, nil
}

var _ InferenceAuth = (*inferenceauth.Manager)(nil)
