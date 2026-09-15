package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	sessionstore "github.com/context-labs/whip/internal/session"
)

// Span writes are observational: they never fail the work they describe. A
// failed write is logged and the turn goes on. Ends use a detached context so
// a cancelled turn still closes its spans.
const spanWriteTimeout = 3 * time.Second

func (s *Session) recordSpanStart(record sessionstore.SpanRecord) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.supervisor.ctx), spanWriteTimeout)
	defer cancel()
	if err := s.store.RecordSpanStart(ctx, record); err != nil {
		config.LogEvent("trace", fmt.Sprintf("span start %s (%s %s): %v", record.ID, record.Kind, record.Name, err))
	}
}

func (s *Session) recordSpanEnd(record sessionstore.SpanRecord) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.supervisor.ctx), spanWriteTimeout)
	defer cancel()
	if err := s.store.RecordSpanEnd(ctx, record); err != nil {
		config.LogEvent("trace", fmt.Sprintf("span end %s (%s %s): %v", record.ID, record.Kind, record.Name, err))
	}
}

// turnSpan resolves the running turn of one agent in this root's runtime.
func (s *Session) turnSpan(agentID string) (turnID string, turn sessionstore.SpanLink, ok bool) {
	runtime, has := s.runtime.(interface {
		TurnSpan(string) (string, sessionstore.SpanLink, bool)
	})
	if !has {
		return "", sessionstore.SpanLink{}, false
	}
	return runtime.TurnSpan(agentID)
}

// TurnSpan reports the trace identity of the agent's running turn, or false
// when the agent is idle or unknown.
func (runtime *RecursiveRuntime) TurnSpan(agentID string) (string, sessionstore.SpanLink, bool) {
	runtime.mu.RLock()
	node := runtime.agents[agentID]
	runtime.mu.RUnlock()
	if node == nil {
		return "", sessionstore.SpanLink{}, false
	}
	return node.traceContext()
}

// NoteModelCall remembers the attempt that most recently settled for an agent,
// so the tool calls it emitted can name their cause.
func (runtime *RecursiveRuntime) NoteModelCall(agentID, callID string) {
	runtime.mu.RLock()
	node := runtime.agents[agentID]
	runtime.mu.RUnlock()
	if node == nil {
		return
	}
	node.mu.Lock()
	node.turn.LastModelCallID = callID
	node.mu.Unlock()
}

// traceContext is the running turn's identity; false outside a durable turn,
// where there is no trace to join.
func (node *AgentSession) traceContext() (turnID string, turn sessionstore.SpanLink, ok bool) {
	node.mu.Lock()
	defer node.mu.Unlock()
	if node.turn.TurnID == "" || node.turn.SpanID == "" || node.turn.TraceID == "" {
		return "", sessionstore.SpanLink{}, false
	}
	return node.turn.TurnID, sessionstore.SpanLink{TraceID: node.turn.TraceID, SpanID: node.turn.SpanID}, true
}

func (node *AgentSession) rememberSpanStart(id string, startNS int64) {
	node.mu.Lock()
	if node.spanStarts == nil {
		node.spanStarts = make(map[string]int64)
	}
	node.spanStarts[id] = startNS
	node.mu.Unlock()
}

func (node *AgentSession) takeSpanStart(id string) int64 {
	node.mu.Lock()
	defer node.mu.Unlock()
	start := node.spanStarts[id]
	delete(node.spanStarts, id)
	return start
}

func (node *AgentSession) lastModelCallID() string {
	node.mu.Lock()
	defer node.mu.Unlock()
	return node.turn.LastModelCallID
}

// toolSpanStart opens the span of one model tool call (an rlm_exec cell, a
// bash call, a custom tool) under the running turn.
func (node *AgentSession) toolSpanStart(callID, name, args string) {
	turnID, turn, ok := node.traceContext()
	if !ok || node.root == nil {
		return
	}
	id := sessionstore.ToolSpanID(node.root.ID(), node.id, turnID, callID)
	start := time.Now().UnixNano()
	node.rememberSpanStart(id, start)
	node.root.recordSpanStart(sessionstore.SpanRecord{
		ID: id, TraceID: turn.TraceID, ParentID: turn.SpanID, RootID: node.root.ID(), AgentID: node.id, TurnID: turnID,
		Kind: sessionstore.SpanKindTool, Name: name, Status: sessionstore.SpanStatusRunning, StartNS: start,
		Attrs: sessionstore.SpanAttrs(map[string]any{
			"tool_call_id": callID, "tool_name": name, "summary": toolSummary(name, args), "input": sessionstore.SpanExcerpt(args),
			"emitting_call_id": node.lastModelCallID(), "execution_engine": node.agent.ExecutionLanguage,
		}),
	})
}

// toolSpanEnd closes a tool call's span. Tools report failure by prefixing
// their output, the same heuristic the agent loop uses for exit codes.
func (node *AgentSession) toolSpanEnd(callID, name, result string) {
	turnID, turn, ok := node.traceContext()
	if !ok || node.root == nil {
		return
	}
	id := sessionstore.ToolSpanID(node.root.ID(), node.id, turnID, callID)
	status := sessionstore.SpanStatusOK
	attrs := map[string]any{"output": sessionstore.SpanExcerpt(result)}
	if strings.HasPrefix(result, "error") || strings.HasPrefix(result, "Error") {
		status = sessionstore.SpanStatusError
		attrs["error"] = sessionstore.SpanExcerpt(result)
	}
	node.root.recordSpanEnd(sessionstore.SpanRecord{
		ID: id, TraceID: turn.TraceID, ParentID: turn.SpanID, RootID: node.root.ID(), AgentID: node.id, TurnID: turnID,
		Kind: sessionstore.SpanKindTool, Name: name, Status: status, StartNS: node.takeSpanStart(id), EndNS: time.Now().UnixNano(),
		Attrs: sessionstore.SpanAttrs(attrs),
	})
}

// toolSummary is the one-line label of a tool call: the first line of the
// code a cell runs or of the command bash runs.
func toolSummary(name, args string) string {
	var fields map[string]any
	if json.Unmarshal([]byte(args), &fields) != nil {
		return ""
	}
	for _, key := range []string{"code", "command", "cmd", "query", "path", "prompt"} {
		if value, ok := fields[key].(string); ok && strings.TrimSpace(value) != "" {
			line := strings.TrimSpace(value)
			if cut := strings.IndexByte(line, '\n'); cut >= 0 {
				line = line[:cut]
			}
			return sessionstore.SpanExcerpt(line)
		}
	}
	_ = name
	return ""
}

func (node *AgentSession) hostSpanID(turnID string, call rlm.HostCall) string {
	return sessionstore.HostSpanID(node.root.ID(), node.id, turnID, call.CallID, call.InvocationID)
}

// hostSpanStart opens a host call's span under its cell.
func (node *AgentSession) hostSpanStart(call rlm.HostCall) {
	turnID, turn, ok := node.traceContext()
	if !ok || node.root == nil {
		return
	}
	id := node.hostSpanID(turnID, call)
	start := time.Now().UnixNano()
	node.rememberSpanStart(id, start)
	node.root.recordSpanStart(sessionstore.SpanRecord{
		ID: id, TraceID: turn.TraceID, ParentID: sessionstore.ToolSpanID(node.root.ID(), node.id, turnID, call.CallID),
		RootID: node.root.ID(), AgentID: node.id, TurnID: turnID,
		Kind: sessionstore.SpanKindHost, Name: call.Module + "." + call.Operation, Status: sessionstore.SpanStatusRunning, StartNS: start,
		Attrs: sessionstore.SpanAttrs(map[string]any{
			"tool_call_id": call.CallID, "invocation_id": call.InvocationID, "summary": sessionstore.SpanExcerpt(call.Summary),
		}),
	})
}

// hostSpanEnd closes a host call's span. The kernel measured the duration
// around the call itself, so the end is start plus duration rather than now.
func (node *AgentSession) hostSpanEnd(call rlm.HostCall) {
	turnID, turn, ok := node.traceContext()
	if !ok || node.root == nil {
		return
	}
	id := node.hostSpanID(turnID, call)
	start := node.takeSpanStart(id)
	end := time.Now().UnixNano()
	if start > 0 && call.Duration > 0 {
		end = start + call.Duration.Nanoseconds()
	}
	status := sessionstore.SpanStatusOK
	switch call.Status {
	case "failed":
		status = sessionstore.SpanStatusError
	case "cancelled":
		status = sessionstore.SpanStatusCancelled
	}
	node.root.recordSpanEnd(sessionstore.SpanRecord{
		ID: id, TraceID: turn.TraceID, ParentID: sessionstore.ToolSpanID(node.root.ID(), node.id, turnID, call.CallID),
		RootID: node.root.ID(), AgentID: node.id, TurnID: turnID,
		Kind: sessionstore.SpanKindHost, Name: call.Module + "." + call.Operation, Status: status, StartNS: start, EndNS: end,
		Attrs: sessionstore.SpanAttrs(map[string]any{
			"operation_id": call.OperationID, "error": sessionstore.SpanExcerpt(call.Err), "duration_ms": call.Duration.Milliseconds(),
		}),
	})
}

// hostSpanLink names the host call a context is running under, as the cause
// of work it queues for another agent. Empty when the caller is not inside a
// host call of a durable turn.
func (node *AgentSession) hostSpanLink(ctx context.Context) sessionstore.SpanLink {
	call, ok := rlm.HostCallFromContext(ctx)
	if !ok || node.root == nil {
		return sessionstore.SpanLink{}
	}
	turnID, turn, ok := node.traceContext()
	if !ok {
		return sessionstore.SpanLink{}
	}
	return sessionstore.SpanLink{TraceID: turn.TraceID, SpanID: node.hostSpanID(turnID, call)}
}

// modelCallSpanStart opens the span of one provider attempt under the agent's
// running turn. startedAt is when the attempt asked for admission.
func (s *Session) modelCallSpanStart(agentID, callID string, attempt llm.ModelAttempt, startedAt time.Time) {
	turnID, turn, ok := s.turnSpan(agentID)
	if !ok {
		return
	}
	name := attempt.Model
	if attempt.Provider != "" {
		name = attempt.Provider + "/" + attempt.Model
	}
	if attempt.Purpose == "compact" {
		name = "compaction"
	}
	s.recordSpanStart(sessionstore.SpanRecord{
		ID: sessionstore.ModelCallSpanID(s.meta.ID, callID), TraceID: turn.TraceID, ParentID: turn.SpanID,
		RootID: s.meta.ID, AgentID: agentID, TurnID: turnID,
		Kind: sessionstore.SpanKindLLM, Name: name, Status: sessionstore.SpanStatusRunning, StartNS: startedAt.UnixNano(),
		Attrs: sessionstore.SpanAttrs(map[string]any{
			"model": attempt.Model, "provider": attempt.Provider, "purpose": attempt.Purpose,
			"model_call_id": callID, "logical_id": attempt.LogicalID, "attempt": attempt.Number, "input_tokens_estimate": attempt.InputTokens,
		}),
	})
}

// modelCallSpanEnd closes an attempt's span with what it settled to.
func (s *Session) modelCallSpanEnd(agentID, callID string, attempt llm.ModelAttempt, settled sessionstore.ModelCallSettlement, result llm.ModelAttemptResult, settleErr error) {
	turnID, turn, ok := s.turnSpan(agentID)
	if !ok {
		return
	}
	status := sessionstore.SpanStatusOK
	attrs := map[string]any{
		"prompt_tokens": settled.PromptTokens, "completion_tokens": settled.CompletionTokens, "cached_tokens": settled.CachedTokens, "reasoning_tokens": settled.ReasoningTokens,
		"cost_micros": settled.CostMicros, "cost_input_micros": settled.CostInputMicros, "cost_cache_read_micros": settled.CostCacheReadMicros, "cost_output_micros": settled.CostOutputMicros,
		"cost_source": settled.CostSource, "usage_source": settled.UsageSource, "elapsed_ms": settled.ElapsedMillis,
	}
	switch {
	case settleErr != nil:
		status, attrs["error"] = sessionstore.SpanStatusError, sessionstore.SpanExcerpt(settleErr.Error())
	case !result.Dispatched:
		status, attrs["error"] = sessionstore.SpanStatusCancelled, "attempt was not dispatched"
	case result.Failed:
		status = sessionstore.SpanStatusError
		if attrs["elapsed_ms"] == int64(0) {
			attrs["elapsed_ms"] = result.Elapsed.Milliseconds()
		}
	}
	if settled.Status == "interrupted" {
		status = sessionstore.SpanStatusInterrupted
	}
	name := attempt.Model
	if attempt.Provider != "" {
		name = attempt.Provider + "/" + attempt.Model
	}
	s.recordSpanEnd(sessionstore.SpanRecord{
		ID: sessionstore.ModelCallSpanID(s.meta.ID, callID), TraceID: turn.TraceID, ParentID: turn.SpanID,
		RootID: s.meta.ID, AgentID: agentID, TurnID: turnID,
		Kind: sessionstore.SpanKindLLM, Name: name, Status: status, EndNS: time.Now().UnixNano(), Attrs: sessionstore.SpanAttrs(attrs),
	})
	if runtime, has := s.runtime.(interface{ NoteModelCall(string, string) }); has {
		runtime.NoteModelCall(agentID, callID)
	}
}

// questionSpanStart and questionSpanEnd bracket a user.ask prompt as a wait
// span under the asking agent's turn.
func (s *Session) questionSpanStart(agentID, questionID, question string) {
	turnID, turn, ok := s.turnSpan(agentID)
	if !ok {
		return
	}
	s.recordSpanStart(sessionstore.SpanRecord{
		ID: sessionstore.WaitSpanID(s.meta.ID, questionID), TraceID: turn.TraceID, ParentID: turn.SpanID,
		RootID: s.meta.ID, AgentID: agentID, TurnID: turnID,
		Kind: sessionstore.SpanKindWait, Name: "question", Status: sessionstore.SpanStatusRunning, StartNS: time.Now().UnixNano(),
		Attrs: sessionstore.SpanAttrs(map[string]any{"question_id": questionID, "input": sessionstore.SpanExcerpt(question)}),
	})
}

func (s *Session) questionSpanEnd(agentID, questionID, outcome string, closed bool) {
	turnID, turn, ok := s.turnSpan(agentID)
	if !ok {
		return
	}
	status := sessionstore.SpanStatusOK
	if closed {
		status = sessionstore.SpanStatusCancelled
	}
	s.recordSpanEnd(sessionstore.SpanRecord{
		ID: sessionstore.WaitSpanID(s.meta.ID, questionID), TraceID: turn.TraceID, ParentID: turn.SpanID,
		RootID: s.meta.ID, AgentID: agentID, TurnID: turnID,
		Kind: sessionstore.SpanKindWait, Name: "question", Status: status, EndNS: time.Now().UnixNano(),
		Attrs: sessionstore.SpanAttrs(map[string]any{"output": sessionstore.SpanExcerpt(outcome)}),
	})
}
