package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	sessionstore "github.com/context-labs/whip/internal/session"
)

// questionWaiter is one open user.ask: the question.pending payload (which
// carries the labels an answer must come from, and is replayed in snapshots to
// clients that connect mid-question) and the channel the host call blocks on
// until a client answers or dismisses it.
type questionWaiter struct {
	event     sessionstore.LifecycleEvent
	done      chan struct{}
	answer    []string
	dismissed bool
}

// questionRegistry holds the root's open questions in memory. A question does
// not outlive the turn that asked it, so nothing here is persisted; the
// question.* actor events are the durable record.
type questionRegistry struct {
	mu      sync.Mutex
	pending map[string]*questionWaiter
}

// AskUser publishes question.pending and blocks until question.answer resolves
// it or ctx ends. On ctx cancellation the question closes (question.closed) and
// ctx.Err() is returned; time spent here is host time, not cell compute.
//
// ponytail: the blocked host call keeps the cell's kernel pool slot (and
// kernel.mu) for as long as the human takes, unlike agents.wait's
// maxAgentWaitMS cap; with MaxWorkers slots a root waiting minutes on a
// question starves children's cells. Add a cap (dismiss on timeout) if pools
// stay small and that shows up.
func (s *Session) AskUser(ctx context.Context, agentID, question string, options []sessionstore.QuestionOption, multiple bool) ([]string, bool, error) {
	id := "question-" + randomRuntimeSuffix()
	event := sessionstore.LifecycleEvent{AgentID: agentID, QuestionID: id, Question: question, Options: options, Multiple: multiple}
	waiter := &questionWaiter{event: event, done: make(chan struct{})}
	registry := &s.questions
	registry.mu.Lock()
	for _, open := range registry.pending {
		if open.event.AgentID == agentID {
			registry.mu.Unlock()
			return nil, false, errors.New("a question is already open; wait for its answer before asking again")
		}
	}
	if registry.pending == nil {
		registry.pending = make(map[string]*questionWaiter)
	}
	registry.pending[id] = waiter
	registry.mu.Unlock()
	if err := s.emitQuestionEvent(ctx, "question.pending", event); err != nil {
		registry.mu.Lock()
		delete(registry.pending, id)
		registry.mu.Unlock()
		return nil, false, err
	}
	select {
	case <-waiter.done:
		return waiter.answer, waiter.dismissed, nil
	case <-ctx.Done():
		registry.mu.Lock()
		_, open := registry.pending[id]
		delete(registry.pending, id)
		registry.mu.Unlock()
		if open {
			// The turn is gone, so the dialog is moot; tell clients off the
			// cancelled ctx. Best effort: a stopping daemon may refuse it.
			_ = s.emitQuestionEvent(context.WithoutCancel(ctx), "question.closed", sessionstore.LifecycleEvent{
				AgentID: agentID, QuestionID: id, Error: ctx.Err().Error(),
			})
		}
		return nil, false, ctx.Err()
	}
}

// answerQuestion is the question.answer client op: it validates the answer
// against the open question, wakes the blocked host call, and records
// question.answered.
func (s *Session) answerQuestion(ctx context.Context, id string, answer []string, dismissed bool) (string, error) {
	registry := &s.questions
	registry.mu.Lock()
	waiter := registry.pending[id]
	if waiter == nil {
		registry.mu.Unlock()
		return "", fmt.Errorf("question %q is not open", id)
	}
	if dismissed {
		answer = nil
	} else if err := validateQuestionAnswer(waiter.event, answer); err != nil {
		registry.mu.Unlock()
		return "", err
	}
	delete(registry.pending, id)
	waiter.answer, waiter.dismissed = answer, dismissed
	close(waiter.done)
	registry.mu.Unlock()
	if err := s.emitQuestionEvent(ctx, "question.answered", sessionstore.LifecycleEvent{
		AgentID: waiter.event.AgentID, QuestionID: id, Answer: answer, Dismissed: dismissed,
	}); err != nil {
		return "", err
	}
	if dismissed {
		return "dismissed", nil
	}
	return "answered", nil
}

func validateQuestionAnswer(question sessionstore.LifecycleEvent, answer []string) error {
	if len(answer) == 0 {
		return errors.New("an answer must pick at least one option or dismiss the question")
	}
	if !question.Multiple && len(answer) != 1 {
		return errors.New("this question takes exactly one answer")
	}
	for index, label := range answer {
		offered := slices.ContainsFunc(question.Options, func(option sessionstore.QuestionOption) bool { return option.Label == label })
		if !offered || slices.Contains(answer[:index], label) {
			return fmt.Errorf("%q is not one of the options", label)
		}
	}
	return nil
}

// open lists the pending question.pending payloads for Session.Snapshot. Only
// the root asks, one question at a time, so order is moot.
func (r *questionRegistry) open() []sessionstore.LifecycleEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	questions := make([]sessionstore.LifecycleEvent, 0, len(r.pending))
	for _, waiter := range r.pending {
		questions = append(questions, waiter.event)
	}
	return questions
}

func (s *Session) emitQuestionEvent(ctx context.Context, kind string, event sessionstore.LifecycleEvent) error {
	event.RootID = s.meta.ID
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = s.store.AppendRootEvent(ctx, s.meta.ID, kind, sessionstore.RuntimePayload{Data: payload, MediaType: "application/json", Source: "actor event"})
	return err
}

const (
	maxQuestionBytes    = 4 << 10
	maxOptionLabelBytes = 256
)

// user is the Starlark user module: user.ask(question, options, multiple).
func (host *recursiveHost) user(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	node := host.session
	if operation != "ask" {
		return nil, fmt.Errorf("unknown user operation %q", operation)
	}
	if node.parentID != "" {
		return nil, errors.New("only the root agent can ask the user; send your parent a message instead")
	}
	question, _ := stringArgument(arguments, "question")
	question = strings.TrimSpace(question)
	if question == "" || len(question) > maxQuestionBytes {
		return nil, fmt.Errorf("question must be non-empty text of at most %d bytes", maxQuestionBytes)
	}
	options, err := questionOptions(arguments["options"])
	if err != nil {
		return nil, err
	}
	multiple, _ := arguments["multiple"].(bool)
	answer, dismissed, err := node.root.AskUser(ctx, node.id, question, options, multiple)
	if err != nil {
		return nil, err
	}
	if answer == nil {
		answer = []string{}
	}
	return map[string]any{"answer": answer, "dismissed": dismissed}, nil
}

func questionOptions(value any) ([]sessionstore.QuestionOption, error) {
	items, ok := value.([]any)
	if !ok || len(items) < 2 || len(items) > 6 {
		return nil, errors.New("options must be a list of 2 to 6 {label, description} entries")
	}
	options := make([]sessionstore.QuestionOption, 0, len(items))
	for _, item := range items {
		fields, ok := item.(map[string]any)
		label, _ := fields["label"].(string)
		label = strings.TrimSpace(label)
		description, _ := fields["description"].(string)
		if !ok || label == "" || len(label) > maxOptionLabelBytes || len(description) > maxQuestionBytes {
			return nil, fmt.Errorf("each option needs a non-empty label of at most %d bytes and an optional description", maxOptionLabelBytes)
		}
		if slices.ContainsFunc(options, func(option sessionstore.QuestionOption) bool { return option.Label == label }) {
			return nil, fmt.Errorf("option labels must be unique; %q repeats", label)
		}
		options = append(options, sessionstore.QuestionOption{Label: label, Description: description})
	}
	return options, nil
}
