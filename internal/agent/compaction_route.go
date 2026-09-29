package agent

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/context-labs/whip/internal/llm"
)

// compactionSummary keeps routing request-local: title calls and descendants
// can read the configured route while a compaction is in flight.
func (a *Agent) compactionSummary(ctx context.Context, messages []llm.Message, onFallback func(string)) (string, CompactInfo, error) {
	client, model, contextLimit, maxTokens := a.Client, a.Model, a.ContextLimit, a.MaxTokens
	accounting := a.CallAccounting("compaction")
	dedicated := a.CompactClient != nil && a.CompactModel != ""
	if dedicated {
		client, model = a.CompactClient, a.CompactModel
		contextLimit, maxTokens = a.CompactContextLimit, a.CompactMaxTokens
		accounting = a.CompactAccounting("compaction")
	}
	info := CompactInfo{Fallback: a.CompactFallback}
	if err := ctx.Err(); err != nil {
		return "", info, err
	}
	canFallback := dedicated && a.Client != nil && (model != a.Model || client.BaseURL != a.Client.BaseURL)
	fallback := func(reason string) {
		info.Fallback = reason
		client, model, contextLimit, maxTokens = a.Client, a.Model, a.ContextLimit, a.MaxTokens
		accounting = a.CallAccounting("compaction")
		dedicated = false
		if onFallback != nil {
			onFallback(reason)
		}
	}
	inputTokens := llm.EstimateTokens(messages)
	outputLimit := func() int {
		if limit := client.NaturalOutputLimit(model); limit > 0 {
			return limit
		}
		limit := 4096 // room for a real state digest
		if maxTokens > 0 {
			limit = min(limit, maxTokens)
		}
		return limit
	}
	if dedicated && contextLimit > 0 && inputTokens > contextLimit-outputLimit() {
		if !canFallback {
			return "", info, errors.New("custom summarizer cannot fit this summary request")
		}
		fallback("Custom summarizer cannot fit this summary request; using this conversation’s model.")
	} else if !dedicated && info.Fallback != "" && onFallback != nil {
		onFallback(info.Fallback)
	}
	request := llm.Request{Messages: messages}
	hadAttempt := false
	for {
		request.Model, request.Accounting, request.MaxTokens = model, accounting, outputLimit()
		if client.NaturalOutputLimit(model) == 0 && contextLimit > inputTokens {
			request.MaxTokens = min(request.MaxTokens, contextLimit-inputTokens)
		}
		info.Model, info.Provider = model, accounting.Provider
		if dedicated {
			if u, err := url.Parse(client.BaseURL); err == nil && u.Host != "" {
				info.Model += " @ " + u.Host
			}
		}
		// Native retries remain bounded by the client. Every attempt in the
		// custom group must be a definite rejection, not an unknown transport
		// completion or a discarded partial response, before changing routes.
		copyClient := *client
		safe := true
		copyClient.OnRetry = func(event llm.RetryEvent) {
			safe = safe && !event.Regenerating && compactionRejected(event.Err)
			if client.OnRetry != nil {
				client.OnRetry(event)
			}
		}
		summary, usage, err := copyClient.Complete(ctx, request)
		a.AddUsage(usage)
		// A rejected custom attempt can still bill input. Keep that usage,
		// including charges, without losing details of the successful summary.
		previous := info.Usage
		info.Usage = copyUsage(usage)
		if hadAttempt {
			addUsage(&info.Usage, previous)
			info.Usage.Reported = usage.HasUsage() || previous.HasUsage()
			info.Usage.Cost = nil // unknown unless both routes reported charges
			if previous.Cost != nil && usage.Cost != nil {
				cost := *previous.Cost + *usage.Cost
				if !math.IsInf(cost, 0) && !math.IsNaN(cost) {
					info.Usage.Cost = &cost
				}
			}
		}
		hadAttempt = true
		if !dedicated || !safe || ctx.Err() != nil || summary != "" || usage.CompletionTokens != 0 ||
			!compactionRejected(err) || !canFallback {
			return summary, info, err
		}
		// At most one conversation-route group. Admission and settlement still
		// use the same budget, with the conversation's own price snapshot.
		fallback("Custom summarizer rejected the request; using this conversation’s model.")
	}
}

func compactionRejected(err error) bool {
	// A direct HTTP rejection excludes accounting/validation wrappers even
	// when they contain an HTTPError. Timeouts can hide completed work.
	rejection, ok := err.(*llm.HTTPError) //nolint:errorlint // direct cast intentionally excludes wrapped errors
	if !ok || rejection.ResponseStarted {
		return false
	}
	fields := strings.Fields(rejection.Status)
	if len(fields) == 0 {
		return false
	}
	status, _ := strconv.Atoi(fields[0])
	return status >= 400 && status < 500 && status != http.StatusRequestTimeout
}
