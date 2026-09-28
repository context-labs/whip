package runner

import (
	"context"
	"errors"
	"math"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

// Observations belong to this turn and this prepared route. A helper's usage
// and the admission reservation bound never measure ordinary context occupancy.
type contextPressure struct {
	inputTokens *int64
	estimate    int64
	route       contextRoute
	folds       int
	// Once a real fold leaves a high floor, suppress proactive helpers for
	// this turn even if policy changes. Hard and reactive limits still apply.
	stalled bool
}

type contextRoute struct {
	model   session.ModelSelection
	route   string
	adapter string
	window  int64
}

func (p *contextPressure) sync(prepared model.Prepared, folds int) {
	route := contextRoute{model: prepared.Snapshot.Model, route: prepared.Snapshot.Route, adapter: prepared.Snapshot.Adapter}
	if prepared.ContextWindowTokens != nil {
		route.window = *prepared.ContextWindowTokens
	}
	if route != p.route || folds != p.folds {
		p.inputTokens = nil
	}
	p.route, p.folds = route, folds
}

func (p *contextPressure) observe(input *int64, estimate int64) {
	if input != nil {
		p.inputTokens, p.estimate = new(*input), estimate
	}
}

func (p *contextPressure) occupancy(estimate int64) int64 {
	if p.inputTokens == nil {
		return estimate
	}
	growth := max(0, estimate-p.estimate)
	return *p.inputTokens + min(growth, math.MaxInt64-*p.inputTokens)
}

func pressureThreshold(window int64, percent int) int64 {
	if percent == 0 {
		percent = 50
	}
	// Round upward without multiplying the full context window by the percent.
	return window/100*int64(percent) + (window%100*int64(percent)+99)/100
}

// Preparing is local policy resolution. If a fold changes the request, prepare
// its replacement once; only the returned frozen request may be dispatched.
func (r *Runner) prepareOrdinary(ctx context.Context, turn session.Turn, configuration session.Configuration, request *model.Request, size, folds *int, correction []session.Part, pressure *contextPressure) (model.Prepared, error) {
	prepared, err := r.provider.Prepare(ctx, *request)
	if err != nil {
		return model.Prepared{}, err
	}
	if err := validatePrepared(prepared); err != nil {
		return model.Prepared{}, err
	}
	changed, err := r.proactiveContext(ctx, turn, configuration, request, size, folds, correction, pressure, prepared)
	if err != nil {
		return model.Prepared{}, err
	}
	if changed {
		prepared, err = r.provider.Prepare(ctx, *request)
		if err != nil {
			return model.Prepared{}, err
		}
		pressure.sync(prepared, *folds)
	}
	return prepared, nil
}

func (r *Runner) proactiveContext(ctx context.Context, turn session.Turn, configuration session.Configuration, request *model.Request, size, folds *int, correction []session.Part, pressure *contextPressure, prepared model.Prepared) (bool, error) {
	pressure.sync(prepared, *folds)
	window := pressure.route.window
	if r.compactions == nil || window <= 0 || pressure.stalled || *folds >= maxCompactionFolds {
		return false, nil
	}
	threshold := pressureThreshold(window, configuration.Compaction.ThresholdPercent)
	if pressure.occupancy(model.EstimateInputTokens(*request)) < threshold {
		return false, nil
	}
	before := *folds
	nextSize, err := r.replanContext(ctx, turn, configuration, request, folds, correction)
	if errors.Is(err, errNoCompaction) && *folds == before {
		// Do not stall: a later complete exchange may make a useful fold possible.
		return false, nil
	}
	if err != nil {
		return false, err
	}
	*size = nextSize
	pressure.sync(prepared, *folds)
	// A high estimated floor after a real fold must not cause one helper per
	// model round. Hard limits and a confirmed provider rejection still apply.
	pressure.stalled = model.EstimateInputTokens(*request) >= threshold
	return true, nil
}
