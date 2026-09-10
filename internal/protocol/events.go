package protocol

import (
	"reflect"

	"github.com/context-labs/whip/internal/session"
)

// EventPayloads defines payload objects inside ordered root event envelopes.
// Transport notifications are separately described by Events.
func EventPayloads() map[string]reflect.Type {
	result := map[string]reflect.Type{}
	for _, kind := range []string{
		"stream.text", "stream.reasoning", "stream.tool.call", "stream.tool.started", "stream.tool.output", "stream.tool.completed",
		"stream.notice", "stream.usage", "stream.accounting", "stream.cell.host.started", "stream.cell.host", "stream.tool.progress", "stream.terminal.started", "stream.terminal.output",
		"stream.terminal.awaiting", "stream.terminal.completed",
	} {
		result[kind] = reflect.TypeFor[StreamEvent]()
	}
	for _, kind := range []string{
		"session.cwd.updated", "session.effort.updated", "session.title.updated", "session.model.updated",
		"session.permission_mode.updated", "session.archived.updated",
	} {
		result[kind] = reflect.TypeFor[SessionUpdateEvent]()
	}
	for _, kind := range []string{
		"inbox.queued", "inbox.consumed", "inbox.failed", "schedule.fired", "command.queued", "command.control.queued",
		"permission.pending", "permission.auto_approved", "capability.delegated", "capability.revoked",
		"budget.capped", "budget.active_child.reserved", "agent.admitted", "agent.prompt.queued",
		"question.pending", "question.answered", "question.closed", "session.reload.failed",
	} {
		result[kind] = reflect.TypeFor[session.LifecycleEvent]()
	}
	for _, kind := range []string{
		"turn.started", "turn.succeeded", "turn.failed", "turn.cancelled", "turn.interrupted",
		"agent.turn.started", "agent.turn.succeeded", "agent.turn.failed", "agent.turn.cancelled", "agent.turn.interrupted",
		"command.running", "command.waiting", "command.succeeded", "command.failed", "command.cancelled", "command.interrupted",
		"root.failed", "root.interrupted", "root.stopped", "agent.subtree.stopped", "agent.subtree.deleted",
		"goal.continued", "state.private.set", "state.private.append", "state.private.cas",
		"blackboard.set", "blackboard.append", "blackboard.cas", "subscription.created", "subscription.cancelled",
		"model.call.started", "model.call.settled", "model.call.corrected", "model.call.interrupted", "message.updated", "message.queued", "message.done", "message.deferred", "message.delivered", "scratch.restored",
	} {
		result[kind] = reflect.TypeFor[session.LifecycleEvent]()
	}
	return result
}

// ContentEventPayload replaces a large event payload while preserving its kind.
type ContentEventPayload struct {
	// Stream identity remains inline so large cumulative updates stay attached to
	// their agent, turn and call. Older events may contain only the reference.
	StreamEvent
	Content   ContentHandle `json:"content"`
	Truncated bool          `json:"truncated"`
}
