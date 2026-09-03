package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	sessionstore "github.com/context-labs/whip/internal/session"
)

var ErrStopped = errors.New("root session stopped")

// Runner is the smallest daemon seam around one model loop. Turn
// calls started exactly once after it owns its loop boundary.
type Runner interface {
	Turn(context.Context, string, bool, func(), func(string)) (string, error)
	History() []llm.Message
	Close()
}

type Closeable interface{ Close() }

type Components struct {
	Runner        Runner
	MCP           Closeable
	Runtime       Closeable
	Bind          func(*Session) error
	GoalMaxRounds int
}

type contentRunner interface {
	TurnParts(context.Context, string, []llm.ContentPart, func(), func(string)) (string, error)
}

type Completion struct {
	Sequence int64
	Output   string
	Err      error
}

type Receipt struct {
	Sequence int64
	done     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	result   Completion
}

func newReceipt(sequence int64) *Receipt {
	return &Receipt{Sequence: sequence, done: make(chan struct{})}
}

func (r *Receipt) finish(result Completion) {
	r.once.Do(func() {
		r.mu.Lock()
		r.result = result
		r.mu.Unlock()
		close(r.done)
	})
}

func (r *Receipt) Done() <-chan struct{} { return r.done }
func (r *Receipt) Wait(ctx context.Context) (Completion, error) {
	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.result, nil
	case <-ctx.Done():
		return Completion{}, ctx.Err()
	}
}

type inboxReady struct{}

const (
	workerTurn          = "turn"
	workerScheduleTick  = "schedule.tick"
	workerControl       = "control"
	workerClientCommand = "client.command"
	workerStream        = "stream"
)

type workerCompletion struct {
	sequence     int64
	output       string
	err          error
	journal      turnJournal
	workspaceSeq int
	workspaceRef string
}

type turnCompaction struct {
	Summary      string
	Cutoff       int
	RawTailStart int
}

// turnJournal is everything one turn produced that the commit must persist:
// new transcript messages, compactions, and the durable items the model saw
// (steer rows injected at a boundary, mailbox messages shown or read).
type turnJournal struct {
	Messages          []llm.Message
	Compactions       []turnCompaction
	DeliveredInbox    []int64
	DeliveredMessages []string
	seenInbox         map[int64]bool
	seenMessages      map[string]bool
}

// rootTurn identifies the root's live turn: an inbox-triggered turn by its
// inbox sequence, a mailbox-triggered one by its turn id.
type rootTurn struct {
	seq    int64
	turnID string
}

type turnJournaler interface{ turnJournal() turnJournal }

type workerEnvelope struct {
	kind       string
	completion workerCompletion
	err        error
	control    func(context.Context) error
	reply      chan error
	at         time.Time
	client     *clientCommandCompletion
	stream     *streamEnvelope
}

type streamEnvelope struct {
	kind  string
	event StreamEvent
}

type supervisor struct {
	ctx    context.Context
	cancel context.CancelFunc
	wake   chan struct{}

	mu       sync.Mutex
	stopping bool
	events   []workerEnvelope
	workers  sync.WaitGroup
}

func newSupervisor() *supervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &supervisor{ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
}

func (s *supervisor) startActor(run func()) { go run() }

func (s *supervisor) launch(kind string, work func(context.Context) workerCompletion) error {
	if !s.launchWorker(kind, func() { s.post(workerEnvelope{kind: kind, completion: work(s.ctx)}) }) {
		return ErrStopped
	}
	return nil
}

func (s *supervisor) launchWorker(kind string, work func()) bool {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return false
	}
	s.workers.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.workers.Done()
		defer func() {
			if value := recover(); value != nil {
				s.post(workerEnvelope{kind: kind, err: panicError(kind, value)})
			}
		}()
		work()
	}()
	return true
}

func (s *supervisor) report(kind string, err error) {
	s.post(workerEnvelope{kind: kind, err: fmt.Errorf("%s: %w", kind, err)})
}

func (s *supervisor) post(event workerEnvelope) {
	s.mu.Lock()
	if event.kind == workerStream && event.stream != nil && len(s.events) > 0 {
		last := &s.events[len(s.events)-1]
		if last.kind == workerStream && last.stream != nil && last.stream.kind == event.stream.kind &&
			last.stream.event.AgentID == event.stream.event.AgentID && last.stream.event.ID == event.stream.event.ID {
			switch event.stream.kind {
			case "stream.text", "stream.reasoning", "stream.terminal.output":
				if len(last.stream.event.Text)+len(event.stream.event.Text) <= 32<<10 {
					last.stream.event.Text += event.stream.event.Text
					s.mu.Unlock()
					return
				}
			case "stream.tool.call", "stream.tool.output":
				last.stream.event = event.stream.event
				s.mu.Unlock()
				return
			}
		}
	}
	s.events = append(s.events, event)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *supervisor) take() []workerEnvelope {
	s.mu.Lock()
	events := append([]workerEnvelope(nil), s.events...)
	s.events = nil
	s.mu.Unlock()
	return events
}

func (s *supervisor) stop() {
	s.mu.Lock()
	if !s.stopping {
		s.stopping = true
		s.cancel()
	}
	s.mu.Unlock()
}

func (s *supervisor) wait() {
	s.workers.Wait()
}

type Session struct {
	store      *sessionstore.Store
	meta       sessionstore.Meta
	authority  capability.Authority
	runner     Runner
	mcp        Closeable
	runtime    Closeable
	factory    Factory
	supervisor *supervisor
	mailbox    chan inboxReady
	done       chan struct{}

	admitMu  sync.RWMutex
	stopping bool
	terminal bool

	waitMu     sync.Mutex
	receipts   map[int64][]*Receipt
	err        error
	goalRounds int
	goalMax    int

	running        *rootTurn
	turnCancel     context.CancelFunc
	clientBusy     bool
	reloadPending  bool
	titleAttempted bool
	autoTitle      bool
	deferredWake   time.Time

	pricingMu sync.RWMutex
	pricing   modelPricing
}

func newSession(store *sessionstore.Store, meta sessionstore.Meta, authority capability.Authority, components Components, factories ...Factory) *Session {
	goalMax := components.GoalMaxRounds
	if goalMax <= 0 {
		goalMax = config.DefaultGoalMaxRounds
	}
	root := &Session{
		store: store, meta: meta, authority: authority, runner: components.Runner, mcp: components.MCP, runtime: components.Runtime,
		supervisor: newSupervisor(), mailbox: make(chan inboxReady, 1), done: make(chan struct{}),
		receipts: make(map[int64][]*Receipt), goalMax: goalMax,
	}
	if len(factories) > 0 {
		root.factory = factories[0]
	}
	return root
}

func (s *Session) ID() string               { return s.meta.ID }
func (s *Session) AgentID() string          { return s.authority.AgentID }
func (s *Session) WorkingDirectory() string { return s.meta.CWD }
func (s *Session) Done() <-chan struct{}    { return s.done }

func (s *Session) Err() error {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	return s.err
}

// History reads only committed reconstruction state, never the live runner.
func (s *Session) History() (sessionstore.Meta, []llm.Message, error) {
	return s.store.Load(s.meta.ID)
}

func (s *Session) Submit(ctx context.Context, text string) (*Receipt, error) {
	if s.meta.Kind != sessionstore.SessionKindAgent {
		return nil, errors.New("tool-host sessions cannot submit model turns")
	}
	return s.enqueue(ctx, "submit", text, true)
}

func (s *Session) Steer(ctx context.Context, text string) (*Receipt, error) {
	if s.meta.Kind != sessionstore.SessionKindAgent {
		return nil, errors.New("tool-host sessions cannot steer model turns")
	}
	return s.enqueue(ctx, "steer", text, true)
}

// AdmitCommand binds one stable protocol command to the root actor's durable
// inbox. Matching retries attach to the existing sequence or terminal result.
func (s *Session) AdmitCommand(ctx context.Context, admission sessionstore.CommandAdmission) (result sessionstore.CommandAdmissionResult, receipt *Receipt, err error) {
	admission.Scope = sessionstore.CommandScopeRoot
	admission.RootID = s.meta.ID
	admission.AgentID = s.authority.AgentID
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		var admitErr error
		result, admitErr = s.store.AdmitCommand(actorCtx, admission)
		if admitErr != nil {
			return admitErr
		}
		receipt = newReceipt(result.Command.IngressSeq)
		switch result.Command.Status {
		case "queued", "running", "waiting":
			s.register(receipt)
		case "succeeded", "failed", "cancelled", "interrupted":
			output, resolveErr := s.store.ResolveRuntimeValue(actorCtx, s.meta.ID, result.Command.Outcome)
			if resolveErr != nil {
				receipt.finish(Completion{Sequence: result.Command.IngressSeq, Err: resolveErr})
			} else if result.Command.Status == "succeeded" {
				receipt.finish(Completion{Sequence: result.Command.IngressSeq, Output: string(output)})
			} else {
				message := string(output)
				if message == "" {
					message = "command is " + result.Command.Status
				}
				receipt.finish(Completion{Sequence: result.Command.IngressSeq, Err: errors.New(message)})
			}
		default:
			receipt.finish(Completion{Sequence: result.Command.IngressSeq, Err: fmt.Errorf("command is %s", result.Command.Status)})
		}
		return nil
	})
	if err != nil {
		return sessionstore.CommandAdmissionResult{}, nil, err
	}
	if result.New {
		s.notify()
	}
	return result, receipt, nil
}

func (s *Session) Snapshot(ctx context.Context) (snapshot sessionstore.RootSnapshot, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		var snapshotErr error
		snapshot, snapshotErr = s.store.SnapshotRoot(actorCtx, s.meta.ID)
		return snapshotErr
	})
	return snapshot, err
}

func (s *Session) hasRunningAgent() bool {
	if s.running != nil {
		return true
	}
	runtime, ok := s.runtime.(interface{ HasRunningAgents() bool })
	return ok && runtime.HasRunningAgents()
}

func (s *Session) enqueueWake(kind, text string) {
	if _, err := s.enqueue(context.Background(), kind, text, false); err != nil && !errors.Is(err, ErrStopped) {
		s.supervisor.report(kind+" wake", err)
	}
}

func (s *Session) enqueue(ctx context.Context, kind, text string, receipt bool) (*Receipt, error) {
	s.admitMu.RLock()
	if s.stopping {
		s.admitMu.RUnlock()
		return nil, ErrStopped
	}
	sequence, err := s.store.EnqueueInbox(ctx, sessionstore.InboxEnqueue{
		RootID: s.meta.ID, AgentID: s.authority.AgentID, Kind: kind,
		Payload: sessionstore.RuntimePayload{Data: []byte(text), MediaType: "text/plain", Source: kind},
	})
	if err != nil {
		s.admitMu.RUnlock()
		return nil, err
	}
	var result *Receipt
	if receipt {
		result = newReceipt(sequence.InboxSeq)
		s.register(result)
	}
	s.admitMu.RUnlock()
	s.notify()
	if !receipt {
		return nil, nil //nolint:nilnil // internal wake messages intentionally have no receipt
	}
	return result, nil
}

func (s *Session) notify() {
	select {
	case s.mailbox <- inboxReady{}:
	default:
	}
}

func (s *Session) register(receipt *Receipt) {
	s.waitMu.Lock()
	s.receipts[receipt.Sequence] = append(s.receipts[receipt.Sequence], receipt)
	s.waitMu.Unlock()
}

func (s *Session) settle(sequence int64, result Completion) {
	s.waitMu.Lock()
	receipts := s.receipts[sequence]
	delete(s.receipts, sequence)
	s.waitMu.Unlock()
	for _, receipt := range receipts {
		receipt.finish(result)
	}
}

// routeControl serializes a reply-bearing operation through the root actor.
func (s *Session) routeControl(ctx context.Context, control func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.admitMu.RLock()
	if s.stopping {
		s.admitMu.RUnlock()
		return ErrStopped
	}
	reply := make(chan error, 1)
	s.supervisor.post(workerEnvelope{kind: workerControl, control: control, reply: reply})
	s.admitMu.RUnlock()
	return <-reply
}

func (s *Session) Stop() {
	s.requestStop(true)
	<-s.done
}

func (s *Session) requestStop(terminal bool) {
	s.admitMu.Lock()
	s.terminal = s.terminal || terminal
	if !s.stopping {
		s.stopping = true
		s.supervisor.cancel()
	}
	s.admitMu.Unlock()
}

func (s *Session) isTerminal() bool {
	s.admitMu.RLock()
	defer s.admitMu.RUnlock()
	return s.terminal
}

func (s *Session) run() {
	var actorErr error
	failed := false
	func() {
		defer func() {
			if value := recover(); value != nil {
				failed = true
				actorErr = panicError("actor", value)
			}
		}()
		if err := s.actor(); err != nil && s.supervisor.ctx.Err() == nil {
			failed = true
			actorErr = err
		}
	}()

	s.admitMu.Lock()
	s.stopping = true
	s.admitMu.Unlock()
	s.supervisor.stop()
	cleanupErr := safeClose("runner", s.runner.Close)
	if s.mcp != nil {
		cleanupErr = errors.Join(cleanupErr, safeClose("mcp", s.mcp.Close))
	}
	if s.runtime != nil {
		cleanupErr = errors.Join(cleanupErr, safeClose("runtime", s.runtime.Close))
	}
	cleanupErr = errors.Join(cleanupErr, s.store.Processes().StopRoot(s.meta.ID))
	// Settle control calls that were admitted before stopping. Some of those
	// callers are supervised workers, so waiting for workers first would leave
	// each side waiting on the other.
	cleanupErr = errors.Join(cleanupErr, s.flushPendingEvents())
	cleanupErr = errors.Join(cleanupErr, s.drainWorkers())
	if failed {
		_, err := s.store.FailRoot(context.Background(), s.meta.ID, actorErr.Error())
		cleanupErr = errors.Join(cleanupErr, err)
	} else if s.isTerminal() {
		_, err := s.store.StopRoot(context.Background(), s.meta.ID, ErrStopped.Error())
		cleanupErr = errors.Join(cleanupErr, err)
	} else {
		_, err := s.store.InterruptRoot(context.Background(), s.meta.ID, ErrStopped.Error())
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if failed {
		s.finish(errors.Join(actorErr, cleanupErr))
	} else {
		s.finish(errors.Join(ErrStopped, cleanupErr))
	}
}

func (s *Session) actor() error {
	if err := s.dispatch(); err != nil {
		return err
	}
	for {
		select {
		case <-s.supervisor.ctx.Done():
			return s.supervisor.ctx.Err()
		case <-s.mailbox:
			if err := s.dispatch(); err != nil {
				return err
			}
		case <-s.supervisor.wake:
			events := s.supervisor.take()
			for i, event := range events {
				if event.err != nil {
					for _, pending := range events[i+1:] {
						if pending.kind == workerControl {
							pending.reply <- ErrStopped
						}
					}
					return event.err
				}
				switch event.kind {
				case workerControl:
					err := errors.New("actor control is missing")
					if event.control != nil {
						err = event.control(s.supervisor.ctx)
					}
					event.reply <- err
				case workerClientCommand:
					if err := s.completeClientCommand(event.client); err != nil {
						return err
					}
				case workerStream:
					if err := s.recordStreamEvent(event.stream); err != nil {
						return err
					}
				case workerScheduleTick:
					if err := s.fireDueSchedules(event.at); err != nil {
						return err
					}
				case workerTurn:
					if err := s.completeTurn(event.completion); err != nil {
						return err
					}
				}
			}
			if err := s.dispatch(); err != nil {
				return err
			}
		}
	}
}

func (s *Session) flushPendingEvents() error {
	var flushErr error
	for _, event := range s.supervisor.take() {
		flushErr = errors.Join(flushErr, event.err)
		if event.kind == workerControl {
			event.reply <- ErrStopped
			continue
		}
		if event.kind == workerClientCommand && event.client != nil {
			event.client.reply <- clientCommandReply{err: ErrStopped}
			continue
		}
		if event.kind == workerStream {
			flushErr = errors.Join(flushErr, s.recordStreamEvent(event.stream))
			continue
		}
	}
	return flushErr
}

func (s *Session) drainWorkers() error {
	done := make(chan struct{})
	go func() {
		s.supervisor.wait()
		close(done)
	}()
	var drainErr error
	for {
		select {
		case <-done:
			return errors.Join(drainErr, s.flushPendingEvents())
		case <-s.supervisor.wake:
			drainErr = errors.Join(drainErr, s.flushPendingEvents())
		}
	}
}

func (s *Session) recordStreamEvent(stream *streamEnvelope) error {
	if stream == nil || stream.kind == "" {
		return errors.New("worker stream event is incomplete")
	}
	payload, err := json.Marshal(stream.event)
	if err != nil {
		return err
	}
	_, err = s.store.AppendRootEvent(s.supervisor.ctx, s.meta.ID, stream.kind, sessionstore.RuntimePayload{
		Data: payload, MediaType: "application/json", Source: stream.kind,
	})
	return err
}

// dispatch starts the root's next turn when it is idle: the oldest queued
// inbox row first (one at a time), otherwise a mailbox-triggered turn when
// ready mail exists. Steer-class work arriving during a turn is injected by
// the shared boundary hook in AgentSession.RunTurn, not here.
func (s *Session) dispatch() error {
	if s.clientBusy || s.running != nil {
		return nil
	}
	ctx := s.supervisor.ctx
	// The write lock excludes enqueue's read-locked publication window, so a
	// row is never claimed before its receipt is registered.
	s.admitMu.Lock()
	items, err := s.store.LoadQueuedInbox(ctx, s.meta.ID, s.authority.AgentID, 0, 1)
	s.admitMu.Unlock()
	if err != nil {
		return err
	}
	var current rootTurn
	var text string
	var parts []llm.ContentPart
	authored := false
	if len(items) > 0 {
		item := items[0]
		text, parts, err = s.inboxInput(item)
		if err != nil {
			return err
		}
		authored = item.Kind == "submit" || item.Kind == "submit.parts" || item.Kind == "steer" || item.Kind == "steer.parts"
		if err := s.store.StartRootTurn(ctx, s.meta.ID, s.authority.AgentID, item.Seq); err != nil {
			return err
		}
		current.seq = item.Seq
	} else {
		work, err := s.store.AgentWorkStatus(ctx, s.meta.ID, s.authority.AgentID, time.Now())
		if err != nil {
			return err
		}
		if !work.HasReadyMail {
			s.scheduleDeferredWake(work.NextDeferredAt)
			return nil
		}
		turnID, err := s.store.StartRootMailboxTurn(ctx, s.meta.ID, s.authority.AgentID)
		if err != nil {
			return err
		}
		current.turnID = turnID
	}
	s.running = &current
	historyLength := len(s.runner.History())
	turnCtx, turnCancel := context.WithCancel(ctx)
	s.turnCancel = turnCancel
	return s.supervisor.launch(workerTurn, func(context.Context) workerCompletion {
		workspaceRef := ""
		workspace, snapshotsWorkspace := s.runner.(workspaceSnapshotRunner)
		if snapshotsWorkspace {
			workspaceRef = workspace.CaptureWorkspace(turnCtx)
		}
		started := func() {}
		accepted := func(string) {}
		var output string
		var err error
		if len(parts) > 0 {
			if runner, ok := s.runner.(contentRunner); ok {
				output, err = runner.TurnParts(turnCtx, text, parts, started, accepted)
			} else {
				err = errors.New("session runner does not support content parts")
			}
		} else {
			output, err = s.runner.Turn(turnCtx, text, authored, started, accepted)
		}
		journal := turnJournal{}
		if source, ok := s.runner.(turnJournaler); ok {
			journal = source.turnJournal()
		} else if history := s.runner.History(); historyLength <= len(history) {
			journal.Messages = append(journal.Messages, history[historyLength:]...)
		}
		if workspaceRef != "" && workspace.WorkspaceClean(turnCtx) {
			workspace.DropWorkspaceSnapshot(turnCtx, workspaceRef)
			workspaceRef = ""
		}
		return workerCompletion{
			sequence: current.seq, output: output, err: err, journal: journal,
			workspaceSeq: historyLength, workspaceRef: workspaceRef,
		}
	})
}

// scheduleDeferredWake arms one in-memory wake for the earliest deferred
// message so the actor re-derives readiness when it matures.
func (s *Session) scheduleDeferredWake(at time.Time) {
	if at.IsZero() {
		return
	}
	if !s.deferredWake.IsZero() && s.deferredWake.After(time.Now()) && !at.Before(s.deferredWake) {
		return
	}
	s.deferredWake = at
	time.AfterFunc(max(time.Until(at), 0)+time.Second, s.notify)
}

func (s *Session) inboxText(item sessionstore.InboxItem) (string, error) {
	text, _, err := s.inboxInput(item)
	return text, err
}

func (s *Session) inboxInput(item sessionstore.InboxItem) (string, []llm.ContentPart, error) {
	var data []byte
	if item.Payload.ReferenceID == "" {
		data = item.Payload.Inline
	} else {
		var err error
		data, _, err = s.store.ReadContent(s.supervisor.ctx, item.Payload.ReferenceID, s.meta.ID, s.authority.AgentID, 0, sessionstore.MaxContentRead)
		if err != nil {
			return "", nil, err
		}
	}
	if item.Kind != "submit.parts" && item.Kind != "steer.parts" {
		return string(data), nil, nil
	}
	var payload SubmitPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", nil, errors.New("invalid content-parts submission")
	}
	return payload.Text, payload.Parts, nil
}

func (s *Session) completeTurn(completion workerCompletion) error {
	if s.running == nil || s.running.seq != completion.sequence {
		return fmt.Errorf("turn completion sequence %d has no matching turn", completion.sequence)
	}
	current := *s.running
	acknowledged := append([]int64(nil), completion.journal.DeliveredInbox...)
	errorText := ""
	status := "succeeded"
	if completion.err != nil {
		errorText = completion.err.Error()
		status = "failed"
		if errors.Is(completion.err, context.Canceled) {
			status = "cancelled"
		}
	}
	clearGoal := false
	goalContinuation := ""
	if completion.err == nil && s.meta.Goal != "" {
		if agent.GoalMet(completion.output) {
			clearGoal = true
		} else if s.goalRounds < s.goalMax {
			goalContinuation = agent.GoalContinuePrompt(s.meta.Goal)
		}
	}
	compactions := make([]sessionstore.RootCompaction, len(completion.journal.Compactions))
	for i, compaction := range completion.journal.Compactions {
		compactions[i] = sessionstore.RootCompaction{
			Summary: compaction.Summary, Cutoff: compaction.Cutoff, RawTailStart: compaction.RawTailStart,
		}
	}
	outcome := completion.output
	if completion.err != nil {
		outcome = completion.err.Error()
	}
	if err := s.store.CommitRootTurn(s.supervisor.ctx, sessionstore.RootTurnCommit{
		RootID: s.meta.ID, AgentID: s.authority.AgentID, InboxSeq: current.seq, TurnID: current.turnID,
		AcknowledgedInbox: acknowledged, DeliveredMessages: completion.journal.DeliveredMessages,
		Messages: completion.journal.Messages, Compactions: compactions,
		WorkspaceSeq: completion.workspaceSeq, WorkspaceRef: completion.workspaceRef,
		ClearGoal: clearGoal, GoalContinuation: goalContinuation,
		Model: s.meta.Model, Provider: s.meta.Provider, Status: status, Error: errorText,
		Outcome: sessionstore.RuntimePayload{Data: []byte(outcome), MediaType: "text/plain", Source: "command outcome"},
	}); err != nil {
		return err
	}
	if clearGoal {
		s.meta.Goal = ""
		s.goalRounds = 0
	} else if goalContinuation != "" {
		s.goalRounds++
	}
	// Steers injected at a boundary were consumed by the commit; their
	// receipts settle without an output of their own.
	for _, seq := range acknowledged {
		s.settle(seq, Completion{Sequence: seq})
	}
	s.running = nil
	if s.turnCancel != nil {
		s.turnCancel()
		s.turnCancel = nil
	}
	if current.seq > 0 {
		s.settle(current.seq, Completion{Sequence: current.seq, Output: completion.output, Err: completion.err})
	}
	if completion.err == nil {
		s.maybeGenerateTitle()
	}
	if s.reloadPending && !s.hasRunningAgent() {
		s.reloadPending = false
		if _, err := s.replaceModel(s.supervisor.ctx, s.meta.Model, s.meta.Provider, true); err != nil {
			payload, _ := json.Marshal(sessionstore.LifecycleEvent{Error: err.Error()})
			_, _ = s.store.AppendRootEvent(s.supervisor.ctx, s.meta.ID, "session.reload.failed", sessionstore.RuntimePayload{
				Data: payload, MediaType: "application/json", Source: "session reload failure",
			})
		}
	}
	return nil
}

func (s *Session) applyPendingReloadAfterAgent() {
	_ = s.routeControl(context.Background(), func(ctx context.Context) error {
		if !s.reloadPending || s.hasRunningAgent() {
			return nil
		}
		s.reloadPending = false
		if _, err := s.replaceModel(ctx, s.meta.Model, s.meta.Provider, true); err != nil {
			payload, _ := json.Marshal(sessionstore.LifecycleEvent{Error: err.Error()})
			_, _ = s.store.AppendRootEvent(ctx, s.meta.ID, "session.reload.failed", sessionstore.RuntimePayload{
				Data: payload, MediaType: "application/json", Source: "session reload failure",
			})
		}
		return nil
	})
}

func (s *Session) maybeGenerateTitle() {
	if !s.autoTitle || s.titleAttempted || s.meta.Kind != sessionstore.SessionKindAgent {
		return
	}
	runner, ok := s.runner.(interface {
		GenerateTitle(context.Context) (string, llm.Usage, error)
	})
	if !ok {
		return
	}
	meta, history, err := s.store.Load(s.meta.ID)
	if err != nil {
		return
	}
	placeholder := ""
	for _, message := range history {
		if message.Role == "user" && message.Authored {
			placeholder = strings.Join(strings.Fields(message.TextContent()), " ")
			if runes := []rune(placeholder); len(runes) > 64 {
				placeholder = string(runes[:64])
			}
			break
		}
	}
	if placeholder == "" || meta.Title != placeholder {
		s.titleAttempted = true
		return
	}
	s.titleAttempted = true
	s.supervisor.launchWorker("automatic session title", func() {
		ctx, cancel := context.WithTimeout(s.supervisor.ctx, 20*time.Second)
		defer cancel()
		title, _, titleErr := runner.GenerateTitle(ctx)
		if titleErr == nil {
			if changed, _ := s.store.SetTitleIf(s.meta.ID, placeholder, title); changed {
				s.emitSessionUpdate(ctx, "session.title.updated", SessionUpdateEvent{Title: title})
			}
		}
	})
}

func (s *Session) finish(err error) {
	s.admitMu.Lock()
	s.stopping = true
	s.admitMu.Unlock()
	s.waitMu.Lock()
	s.err = err
	var receipts []*Receipt
	for _, waiting := range s.receipts {
		receipts = append(receipts, waiting...)
	}
	s.receipts = make(map[int64][]*Receipt)
	s.waitMu.Unlock()
	for _, receipt := range receipts {
		receipt.finish(Completion{Sequence: receipt.Sequence, Err: err})
	}
	close(s.done)
}

func panicError(kind string, value any) error {
	return fmt.Errorf("%s panic: %v\n%s", kind, value, debug.Stack())
}

func safeClose(kind string, closeFn func()) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = panicError(kind+" close", value)
		}
	}()
	closeFn()
	return nil
}
