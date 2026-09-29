package acp

import (
	"context"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/protocol"
)

// Accounting is optional evidence after settlement. Its unavailability must
// not turn completed work into a failed prompt or trigger another execution.
func (b *Bridge) accounting(ctx context.Context, s *acpSession, admitted protocol.Admission, response *acp.PromptResponse) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	unavailable := []string{}
	if admitted.Turn != nil {
		if value, err := s.handle.TurnUsage(ctx, admitted.Turn.ID); err == nil {
			response.Meta["whip_turn_usage"] = value
			response.Usage = turnUsage(value.Usage)
		} else {
			unavailable = append(unavailable, "turn_usage")
		}
	}
	if value, err := s.handle.ContextUsage(ctx); err == nil {
		// ACP UsageUpdate.Used is defined as current context occupancy. The
		// host reports a possibly stale latest prefill, so keep its explicit
		// basis and provenance in metadata instead of inventing occupancy.
		response.Meta["whip_context_usage"] = value
	} else {
		unavailable = append(unavailable, "context_usage")
	}
	if value, err := s.handle.Usage(ctx); err == nil {
		response.Meta["whip_cumulative_usage"] = value
	} else {
		unavailable = append(unavailable, "cumulative_usage")
	}
	if len(unavailable) != 0 {
		response.Meta["whip_accounting_unavailable"] = unavailable
	}
}

func turnUsage(value protocol.Usage) *acp.Usage {
	if value.Attempts.Reserved != 0 || value.Attempts.InFlight != 0 || value.Attempts.Uncertain != 0 || value.Attempts.Settled == 0 {
		return nil
	}
	input := exactTokens(value.InputTokens, value.Attempts.Settled)
	output := exactTokens(value.OutputTokens, value.Attempts.Settled)
	if input == nil || output == nil || int64(*input) > maxACPTokens-int64(*output) {
		return nil
	}
	// Cache and reasoning counts are already subsets of input/output totals.
	return &acp.Usage{
		InputTokens: *input, OutputTokens: *output, TotalTokens: *input + *output,
		CachedReadTokens:  exactTokens(value.CachedInput, value.Attempts.Settled),
		CachedWriteTokens: exactTokens(value.CachedOutput, value.Attempts.Settled),
		ThoughtTokens:     exactTokens(value.ReasoningTokens, value.Attempts.Settled),
	}
}

// ACP numbers are consumed by JavaScript editors; keep decimal Counter evidence
// in metadata when an exact JSON number (or the local int) cannot represent it.
const maxACPTokens = min(int64(1<<53-1), int64(^uint(0)>>1))

func exactTokens(value protocol.UsageQuantity, settled protocol.Counter) *int {
	if value.Overflow || value.MissingAttempts != 0 || value.KnownAttempts != settled || value.Value < 0 || int64(value.Value) > maxACPTokens {
		return nil
	}
	return new(int(value.Value))
}
