package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/context-labs/whip/internal/session"
)

func traceText(span *session.TraceSpan, key, value string) {
	span.Attributes = append(span.Attributes, session.TraceAttribute{Key: key, Text: new(value)})
}

func traceCount(span *session.TraceSpan, key string, value *int64) {
	if value != nil {
		span.Attributes = append(span.Attributes, session.TraceAttribute{Key: key, Count: value})
	}
}

func traceFlag(span *session.TraceSpan, key string, value bool) {
	span.Attributes = append(span.Attributes, session.TraceAttribute{Key: key, Flag: new(value)})
}

func traceOptional(span *session.TraceSpan, key string, value *string) {
	if value != nil {
		traceText(span, key, *value)
	}
}

func (p *traceProjector) source(ctx context.Context, row session.TraceRow) (session.TraceSpan, bool, error) {
	span := session.TraceSpan{Attributes: []session.TraceAttribute{}}
	cause, err := p.cause(ctx, row.TurnID, 0)
	if err != nil || !cause.present {
		return span, false, err
	}
	span.TraceID = cause.traceID
	span.ParentSpanID = new(session.TraceSpanID("turn", string(row.TurnID)))
	var start int64
	var end *int64
	switch row.SourceKind {
	case "turn":
		var inputID, failure *string
		var inputKind string
		var config, history int64
		err = p.q.QueryRowContext(ctx, `SELECT t.state,t.started_at,t.finished_at,substr(t.failure,1,512),i.id,COALESCE(i.kind,'prompt'),t.config_revision,t.history_revision FROM turns t LEFT JOIN inputs i ON i.turn_id=t.id WHERE t.id=?`, row.SourceID).Scan(&span.State, &start, &end, &failure, &inputID, &inputKind, &config, &history)
		span.Kind = "agent"
		span.Name = "turn." + inputKind
		span.ParentSpanID = cause.parent
		traceOptional(&span, "whip.input.id", inputID)
		traceOptional(&span, "error.preview", failure)
		traceCount(&span, "whip.config.revision", &config)
		traceCount(&span, "whip.history.revision", &history)
	case "attempt":
		err = p.attempt(ctx, row, &span, &start, &end)
	case "cell":
		var callID, messageID string
		var resultID *string
		err = p.q.QueryRowContext(ctx, `SELECT state,created_at,finished_at,call_id,call_message_id,result_message_id FROM cells WHERE id=?`, row.SourceID).Scan(&span.State, &start, &end, &callID, &messageID, &resultID)
		span.Kind = "tool"
		span.Name = "execute"
		traceText(&span, "whip.call.id", callID)
		traceText(&span, "whip.call.message_id", messageID)
		traceOptional(&span, "whip.result.message_id", resultID)
	case "operation":
		var cellID *string
		var arguments string
		var result *string
		var argsTruncated, resultTruncated bool
		var dispatched *int64
		err = p.q.QueryRowContext(ctx, `SELECT state,created_at,finished_at,capability,cell_id,substr(arguments,1,512),length(arguments)>512,substr(result,1,512),COALESCE(length(result)>512,0),dispatched_at FROM operations WHERE id=?`, row.SourceID).Scan(&span.State, &start, &end, &span.Name, &cellID, &arguments, &argsTruncated, &result, &resultTruncated, &dispatched)
		span.Kind = "host"
		if cellID != nil {
			span.ParentSpanID = new(session.TraceSpanID("cell", *cellID))
		}
		traceText(&span, "whip.arguments.preview", arguments)
		traceFlag(&span, "whip.arguments.truncated", argsTruncated)
		traceOptional(&span, "whip.result.preview", result)
		traceFlag(&span, "whip.result.truncated", resultTruncated)
		traceCount(&span, "whip.dispatched_at_us", dispatched)
	case "permission":
		err = p.q.QueryRowContext(ctx, `SELECT state,created_at,resolved_at FROM permissions WHERE operation_id=?`, row.SourceID).Scan(&span.State, &start, &end)
		span.Kind = "wait"
		span.Name = "permission"
		span.ParentSpanID = new(session.TraceSpanID("operation", row.SourceID))
	case "question":
		var deadline int64
		err = p.q.QueryRowContext(ctx, `SELECT COALESCE(q.close_reason,CASE WHEN o.state='succeeded' THEN 'answered' WHEN o.finished_at IS NOT NULL THEN o.state ELSE 'pending' END),q.created_at,o.finished_at,q.deadline FROM questions q JOIN operations o ON o.id=q.operation_id WHERE q.operation_id=?`, row.SourceID).Scan(&span.State, &start, &end, &deadline)
		span.Kind = "wait"
		span.Name = "question"
		span.ParentSpanID = new(session.TraceSpanID("operation", row.SourceID))
		traceCount(&span, "whip.deadline_us", &deadline)
	default:
		return span, false, fmt.Errorf("%w: unknown trace source", session.ErrInvalid)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return span, false, nil
	}
	if err != nil {
		return span, false, err
	}
	if start < 0 || start > math.MaxInt64/1000 || end != nil && (*end < 0 || *end > math.MaxInt64/1000) {
		return span, false, fmt.Errorf("%w: trace timestamp overflow", session.ErrInvalid)
	}
	span.StartNS = start * 1000
	if end != nil {
		span.EndNS = new(*end * 1000)
	}
	traceText(&span, "whip.time.precision", "microsecond")
	return span, true, nil
}

func (p *traceProjector) attempt(ctx context.Context, row session.TraceRow, span *session.TraceSpan, start *int64, end **int64) error {
	var purpose, provider, model, digest, costSource, logical string
	var operation, output, failure *string
	var cost, input, outputTokens, reasoning, cachedInput, cachedOutput, elapsed, batch, dispatched *int64
	var number int64
	err := p.q.QueryRowContext(ctx, `SELECT state,created_at,finished_at,json_extract(request,'$.purpose'),json_extract(request,'$.model.provider'),json_extract(request,'$.model.name'),json_extract(request,'$.request_digest'),operation_id,message_id,substr(json_extract(result,'$.failure'),1,512),cost_nano_usd,cost_source,json_extract(result,'$.usage.input'),json_extract(result,'$.usage.output'),json_extract(result,'$.usage.reasoning'),json_extract(result,'$.usage.cached_input'),json_extract(result,'$.usage.cached_output'),json_extract(result,'$.elapsed_millis'),batch_index,number,logical_id,dispatched_at FROM model_attempts WHERE id=?`, row.SourceID).Scan(&span.State, start, end, &purpose, &provider, &model, &digest, &operation, &output, &failure, &cost, &costSource, &input, &outputTokens, &reasoning, &cachedInput, &cachedOutput, &elapsed, &batch, &number, &logical, &dispatched)
	if err != nil {
		return err
	}
	span.Kind = "llm"
	span.Name = purpose + " " + model
	if operation != nil {
		span.ParentSpanID = new(session.TraceSpanID("operation", *operation))
	}
	traceText(span, "gen_ai.operation.name", purpose)
	traceText(span, "gen_ai.provider.name", provider)
	traceText(span, "gen_ai.request.model", model)
	traceText(span, "whip.request.digest", digest)
	traceText(span, "whip.logical_id", logical)
	traceText(span, "whip.cost.source", costSource)
	traceFlag(span, "whip.input.body_available", false)
	traceOptional(span, "whip.output.message_id", output)
	traceOptional(span, "error.preview", failure)
	traceCount(span, "gen_ai.usage.input_tokens", input)
	traceCount(span, "gen_ai.usage.output_tokens", outputTokens)
	traceCount(span, "gen_ai.usage.reasoning_tokens", reasoning)
	traceCount(span, "gen_ai.usage.cache_read.input_tokens", cachedInput)
	traceCount(span, "whip.usage.cached_output_tokens", cachedOutput)
	traceCount(span, "whip.cost.nano_usd", cost)
	traceCount(span, "whip.elapsed_ms", elapsed)
	traceCount(span, "whip.batch.index", batch)
	traceCount(span, "whip.attempt.number", &number)
	traceCount(span, "whip.dispatched_at_us", dispatched)
	return nil
}
