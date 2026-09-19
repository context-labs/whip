//go:build integration && unix

package daemon

import (
	"context"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
)

// A deterministic model-loop boundary using the production steer hook and commit
// journal. Only its model output and boundary/finish gates are simulated.
func (r *sdkFixtureRunner) queueBoundaryTurn(ctx context.Context, input string, started func()) (string, error) {
	started()
	appendMessages := func(messages ...llm.Message) {
		r.mu.Lock()
		r.history = append(r.history, messages...)
		r.mu.Unlock()
	}
	emit := func(text string) {
		r.root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
			kind: "stream.text", event: StreamEvent{Text: text},
		}})
	}
	appendMessages(llm.Message{Role: "user", Content: input, Authored: true}, llm.Message{Role: "assistant", Content: "Before the queue boundary."})
	emit("Before the queue boundary.")
	if _, err := r.control.turn(ctx, "hold:queue-boundary", false); err != nil {
		return "", err
	}
	turnID, err := r.root.store.RunningTurnID(ctx, r.root.ID(), r.root.ID())
	if err != nil {
		return "", err
	}
	node := &AgentSession{root: r.root, id: r.root.ID(), agent: &agent.Agent{Vision: true}}
	messages, err := node.pullSteers(ctx, turnID)
	r.mu.Lock()
	r.boundaryJournal = node.turnJournal()
	r.mu.Unlock()
	if err != nil {
		return "", err
	}
	for _, message := range messages {
		if _, err := r.control.turn(ctx, message.Content, true); err != nil {
			return "", err
		}
	}
	appendMessages(messages...)
	appendMessages(llm.Message{Role: "assistant", Content: "After the queue boundary."})
	emit("After the queue boundary.")
	_, err = r.control.turn(ctx, "hold:queue-finish", false)
	return "After the queue boundary.", err
}
