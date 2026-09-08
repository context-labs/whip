package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/context-labs/whip/internal/protocol"
	sessionstore "github.com/context-labs/whip/internal/session"
)

// questionWaiter is one open user.ask: the question.pending payload (which
// carries the labels an answer must come from, and is replayed in snapshots to
// clients that connect mid-question) and the channel the host call blocks on
// until a client answers or dismisses it.
type questionWaiter struct {
	event   sessionstore.LifecycleEvent
	done    chan struct{}
	results []sessionstore.QuestionResult
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
func (s *Session) AskUser(ctx context.Context, agentID string, questions []sessionstore.QuestionSet) ([]sessionstore.QuestionResult, error) {
	id := "question-" + randomRuntimeSuffix()
	event := sessionstore.LifecycleEvent{AgentID: agentID, QuestionID: id, Questions: questions}
	if len(questions) > 0 {
		event.Question, event.Options, event.Multiple = questions[0].Question, questions[0].Options, questions[0].Multiple
	}
	waiter := &questionWaiter{event: event, done: make(chan struct{})}
	registry := &s.questions
	registry.mu.Lock()
	for _, open := range registry.pending {
		if open.event.AgentID == agentID {
			registry.mu.Unlock()
			return nil, errors.New("a question is already open; wait for its answer before asking again")
		}
	}
	if registry.pending == nil {
		registry.pending = make(map[string]*questionWaiter)
	}
	registry.pending[id] = waiter
	if err := s.emitQuestionEvent(ctx, "question.pending", event); err != nil {
		delete(registry.pending, id)
		registry.mu.Unlock()
		return nil, err
	}
	registry.mu.Unlock()
	select {
	case <-waiter.done:
		return waiter.results, nil
	case <-ctx.Done():
		registry.mu.Lock()
		_, open := registry.pending[id]
		delete(registry.pending, id)
		if open {
			// The turn is gone, so the dialog is moot; tell clients off the
			// cancelled ctx. Best effort: a stopping daemon may refuse it.
			_ = s.emitQuestionEvent(context.WithoutCancel(ctx), "question.closed", sessionstore.LifecycleEvent{
				AgentID: agentID, QuestionID: id, Error: ctx.Err().Error(),
			})
		}
		registry.mu.Unlock()
		return nil, ctx.Err()
	}
}

// answerQuestion is the question.answer client op: it validates the answers
// against the open question, wakes the blocked host call, and records
// question.answered. A batch answer carries one entry per asked question;
// the legacy single answer/dismissed pair fills one entry.
func (s *Session) answerQuestion(ctx context.Context, id string, params protocol.QuestionAnswerParams) (string, error) {
	registry := &s.questions
	registry.mu.Lock()
	waiter := registry.pending[id]
	if waiter == nil {
		registry.mu.Unlock()
		return "", rpcFailure(-32009, fmt.Sprintf("question %q is not open", id))
	}
	results := make([]sessionstore.QuestionResult, len(waiter.event.Questions))
	if params.Answers != nil {
		if len(params.Answers) != len(waiter.event.Questions) {
			registry.mu.Unlock()
			return "", fmt.Errorf("answers must have one entry per question (%d)", len(waiter.event.Questions))
		}
		for i, entry := range params.Answers {
			results[i] = sessionstore.QuestionResult{Answer: entry.Answer, Dismissed: entry.Dismissed}
		}
	} else {
		results[0] = sessionstore.QuestionResult{Answer: params.Answer, Dismissed: params.Dismissed}
	}
	for i := range results {
		if results[i].Dismissed {
			results[i].Answer = nil
			continue
		}
		if err := validateQuestionAnswer(waiter.event.Questions[i], results[i].Answer); err != nil {
			registry.mu.Unlock()
			return "", err
		}
	}
	answered := sessionstore.LifecycleEvent{
		AgentID: waiter.event.AgentID, QuestionID: id, Answers: results,
	}
	answered.Question, answered.Options, answered.Multiple = waiter.event.Question, waiter.event.Options, waiter.event.Multiple
	answered.Answer, answered.Dismissed = results[0].Answer, results[0].Dismissed
	if err := s.emitQuestionEvent(ctx, "question.answered", answered); err != nil {
		registry.mu.Unlock()
		return "", err
	}
	delete(registry.pending, id)
	waiter.results = results
	close(waiter.done)
	registry.mu.Unlock()
	for _, result := range results {
		if !result.Dismissed {
			return "answered", nil
		}
	}
	return "dismissed", nil
}

// validateQuestionAnswer checks one question's answer. Free text is always
// allowed alongside option labels; option labels must be offered and
// unrepeated, and a single-answer question takes exactly one entry.
func validateQuestionAnswer(question sessionstore.QuestionSet, answer []string) error {
	if len(answer) == 0 {
		return errors.New("an answer must pick at least one option, write a response, or skip the question")
	}
	if !question.Multiple && len(answer) != 1 {
		return errors.New("this question takes exactly one answer")
	}
	for index, label := range answer {
		if strings.TrimSpace(label) == "" || len(label) > maxQuestionBytes {
			return errors.New("answer entries must be non-empty text")
		}
		offered := slices.ContainsFunc(question.Options, func(option sessionstore.QuestionOption) bool { return option.Label == label })
		if offered && slices.Contains(answer[:index], label) {
			return fmt.Errorf("%q is picked twice", label)
		}
	}
	return nil
}

// open lists the pending question.pending payloads for Session.Snapshot. Only
// the root asks, one question at a time, so order is moot.
func (r *questionRegistry) open() []sessionstore.LifecycleEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.openLocked()
}

// openLocked requires mu; snapshots hold it across their SQLite read transaction.
func (r *questionRegistry) openLocked() []sessionstore.LifecycleEvent {
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

// user is the Starlark user module: user.ask(question, options, multiple) for
// one question, or user.ask(questions=[{question, options, multiple}, ...])
// for a batch.
func (host *recursiveHost) user(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	node := host.session
	if operation != "ask" {
		return nil, fmt.Errorf("unknown user operation %q", operation)
	}
	if node.parentID != "" {
		return nil, errors.New("only the root agent can ask the user; send your parent a message instead")
	}
	questions, err := questionSets(arguments)
	if err != nil {
		return nil, err
	}
	results, err := node.root.AskUser(ctx, node.id, questions)
	if err != nil {
		return nil, err
	}
	if len(questions) == 1 && arguments["questions"] == nil {
		answer := results[0].Answer
		if answer == nil {
			answer = []string{}
		}
		return map[string]any{"answer": answer, "dismissed": results[0].Dismissed}, nil
	}
	answers := make([]any, len(results))
	for i, result := range results {
		answer := result.Answer
		if answer == nil {
			answer = []string{}
		}
		answers[i] = map[string]any{"answer": answer, "dismissed": result.Dismissed}
	}
	return map[string]any{"answers": answers, "dismissed": allDismissed(results)}, nil
}

func allDismissed(results []sessionstore.QuestionResult) bool {
	for _, result := range results {
		if !result.Dismissed {
			return false
		}
	}
	return true
}

// questionSets parses the user.ask arguments into a batch of 1 to 8
// questions: either the batch form (questions=[...]) or the single form
// (question, options, multiple).
func questionSets(arguments map[string]any) ([]sessionstore.QuestionSet, error) {
	if value, ok := arguments["questions"]; ok && value != nil {
		items, ok := value.([]any)
		if !ok || len(items) < 1 || len(items) > 8 {
			return nil, errors.New("questions must be a list of 1 to 8 {question, options, multiple} entries")
		}
		questions := make([]sessionstore.QuestionSet, 0, len(items))
		for _, item := range items {
			fields, ok := item.(map[string]any)
			if !ok {
				return nil, errors.New("each question must be a {question, options, multiple} entry")
			}
			set, err := questionSet(fields)
			if err != nil {
				return nil, err
			}
			questions = append(questions, set)
		}
		return questions, nil
	}
	set, err := questionSet(arguments)
	if err != nil {
		return nil, err
	}
	return []sessionstore.QuestionSet{set}, nil
}

func questionSet(fields map[string]any) (sessionstore.QuestionSet, error) {
	question, _ := stringArgument(fields, "question")
	question = strings.TrimSpace(question)
	if question == "" || len(question) > maxQuestionBytes {
		return sessionstore.QuestionSet{}, fmt.Errorf("question must be non-empty text of at most %d bytes", maxQuestionBytes)
	}
	options, err := questionOptions(fields["options"])
	if err != nil {
		return sessionstore.QuestionSet{}, err
	}
	multiple, _ := fields["multiple"].(bool)
	return sessionstore.QuestionSet{Question: question, Options: options, Multiple: multiple}, nil
}

func questionOptions(value any) ([]sessionstore.QuestionOption, error) {
	items, ok := value.([]any)
	if !ok || len(items) < 2 || len(items) > 6 {
		return nil, errors.New("options must be a list of 2 to 6 {label, description, recommended} entries")
	}
	options := make([]sessionstore.QuestionOption, 0, len(items))
	recommended := 0
	for _, item := range items {
		fields, ok := item.(map[string]any)
		label, _ := fields["label"].(string)
		label = strings.TrimSpace(label)
		description, _ := fields["description"].(string)
		flag, _ := fields["recommended"].(bool)
		if !ok || label == "" || len(label) > maxOptionLabelBytes || len(description) > maxQuestionBytes {
			return nil, fmt.Errorf("each option needs a non-empty label of at most %d bytes and an optional description", maxOptionLabelBytes)
		}
		if slices.ContainsFunc(options, func(option sessionstore.QuestionOption) bool { return option.Label == label }) {
			return nil, fmt.Errorf("option labels must be unique; %q repeats", label)
		}
		if flag {
			recommended++
		}
		if recommended > 1 {
			return nil, errors.New("at most one option per question can be recommended")
		}
		options = append(options, sessionstore.QuestionOption{Label: label, Description: description, Recommended: flag})
	}
	return options, nil
}
