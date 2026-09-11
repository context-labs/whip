package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
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
	Runner  Runner
	MCP     Closeable
	Runtime Closeable
	Bind    func(context.Context, *Session) error
	// Definition is the root agent's effective definition. The zero value
	// selects the coding agent.
	Definition    agentdef.Definition
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
	RawCutoff    *int
}

// turnJournal is everything one turn produced that the commit must persist:
// new transcript messages, compactions, and the durable items the model saw
// (steer rows injected at a boundary, mailbox messages shown or read).
type turnJournal struct {
	TurnID            string
	BaseSeq           int
	HookNotices       []string // ephemeral lines a hook raised this turn
	Messages          []llm.Message
	Compactions       []turnCompaction
	DeliveredInbox    []int64
	DeliveredMessages []sessionstore.MailboxReceipt
	ClaimedInbox      []int64
	seenMessages      map[sessionstore.MailboxReceipt]bool
	observedMessages  map[string]int64
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
	controlCtx context.Context
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
	providers  *ProviderService
	store      *sessionstore.Store
	meta       sessionstore.Meta
	authority  capability.Authority
	definition agentdef.Definition
	executors  *executorRegistry // custom tool executors; nil when no daemon owns the root
	runner     Runner
	mcpMu      sync.RWMutex
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

	running            *rootTurn
	turnCancel         context.CancelFunc
	runConfig          *runConfiguration // last run.configure, re-applied to a replacement runtime
	clientBusy         bool
	clientIntegrations int
	clientPreparing    bool
	reloadPending      bool
	titleAttempted     bool
	autoTitle          bool
	deferredWake       time.Time

	accountingMu      sync.Mutex
	pendingAccounting map[string]llm.ModelAttemptResult

	questions questionRegistry // open user.ask prompts, keyed by question id
}

// effectiveDefinition selects the coding agent when a factory names none.
func effectiveDefinition(components Components) agentdef.Definition {
	if components.Definition.ID == "" {
		return agentdef.Coding()
	}
	return components.Definition
}

// runConfiguration is the last accepted run.configure payload. It is applied
// to the live runner and re-applied when a model change replaces the runtime,
// so per-session run behavior has one owner.
type runConfiguration struct {
	system   string
	maxTurns int
	headless bool
	cacheKey string
}

func newSession(store *sessionstore.Store, meta sessionstore.Meta, authority capability.Authority, components Components, factories ...Factory) *Session {
	goalMax := components.GoalMaxRounds
	if goalMax <= 0 {
		goalMax = config.DefaultGoalMaxRounds
	}
	root := &Session{
		store: store, meta: meta, authority: authority, definition: effectiveDefinition(components), runner: components.Runner, mcp: components.MCP, runtime: components.Runtime,
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
func (s *Session) AdmitCommand(ctx context.Context, admission sessionstore.CommandAdmission) (sessionstore.CommandAdmissionResult, *Receipt, error) {
	return s.admitCommand(ctx, admission, true)
}

// AcceptCommand admits durable inbox work without retaining a process-local waiter.
func (s *Session) AcceptCommand(ctx context.Context, admission sessionstore.CommandAdmission) (sessionstore.CommandAdmissionResult, error) {
	result, _, err := s.admitCommand(ctx, admission, false)
	return result, err
}

func (s *Session) admitCommand(ctx context.Context, admission sessionstore.CommandAdmission, wait bool) (sessionstore.CommandAdmissionResult, *Receipt, error) {
	admission.Scope = sessionstore.CommandScopeRoot
	admission.RootID = s.meta.ID
	admission.AgentID = s.authority.AgentID
	admission.Payload.Data = slices.Clone(admission.Payload.Data)
	type admittedCommand struct {
		result  sessionstore.CommandAdmissionResult
		receipt *Receipt
	}
	admitted, err := routeControlValue(s, ctx, func(actorCtx context.Context) (admittedCommand, error) {
		result, err := s.store.AdmitCommand(actorCtx, admission)
		if err != nil {
			return admittedCommand{}, err
		}
		if result.New {
			s.notify()
		}
		if !wait {
			return admittedCommand{result: result}, nil
		}
		receipt := newReceipt(result.Command.IngressSeq)
		switch result.Command.Status {
		case "queued", "running", "waiting":
			s.register(receipt)
		case "succeeded", "failed", "cancelled", "interrupted":
			output, resolveErr := s.store.ResolveRuntimeValue(actorCtx, s.meta.ID, result.Command.Outcome)
			if resolveErr != nil {
				receipt.finish(Completion{Sequence: result.Command.IngressSeq, Err: resolveErr})
			} else if result.Command.Status == "succeeded" {
				text, _ := decodeCommandPresentation(result.Command.Operation, output, result.Command.Status)
				receipt.finish(Completion{Sequence: result.Command.IngressSeq, Output: text})
			} else {
				_, message := decodeCommandPresentation(result.Command.Operation, output, result.Command.Status)
				if message == "" {
					message = "command is " + result.Command.Status
				}
				receipt.finish(Completion{Sequence: result.Command.IngressSeq, Err: errors.New(message)})
			}
		default:
			receipt.finish(Completion{Sequence: result.Command.IngressSeq, Err: fmt.Errorf("command is %s", result.Command.Status)})
		}
		return admittedCommand{result: result, receipt: receipt}, nil
	})
	if err != nil {
		return sessionstore.CommandAdmissionResult{}, nil, err
	}
	return admitted.result, admitted.receipt, nil
}

func (s *Session) Snapshot(ctx context.Context) (sessionstore.RootSnapshot, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.RootSnapshot, error) {
		s.questions.mu.Lock()
		defer s.questions.mu.Unlock()
		snapshot, err := s.store.SnapshotRoot(actorCtx, s.meta.ID)
		if err == nil {
			snapshot.Questions = s.questions.openLocked() // in memory, not in the store: a mid-question client has no question.pending to replay
		}
		return snapshot, err
	})
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
// After admission, cancellation or shutdown means the result is unavailable;
// it does not establish that the operation had no effect. Result-bearing
// callers must use routeControlValue instead of sharing variables with the actor.
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
	s.supervisor.post(workerEnvelope{kind: workerControl, control: control, controlCtx: ctx, reply: reply})
	s.admitMu.RUnlock()
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		// Prefer a completed result when cancellation and the reply coincide.
		select {
		case err := <-reply:
			return err
		default:
			return ctx.Err()
		}
	case <-s.supervisor.ctx.Done():
		select {
		case err := <-reply:
			return err
		default:
			return ErrStopped
		}
	}
}

// routeControlValue transfers ownership of the result through a buffered
// channel, so an actor may safely finish after its caller stops waiting.
func routeControlValue[T any](s *Session, ctx context.Context, control func(context.Context) (T, error)) (T, error) {
	result := make(chan T, 1)
	err := s.routeControl(ctx, func(actorCtx context.Context) error {
		value, err := control(actorCtx)
		result <- value
		return err
	})
	select {
	case value := <-result:
		return value, err
	default:
		var zero T
		return zero, err
	}
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
	// Settle replies before closing components or waiting for workers: either
	// may be waiting on a worker that routed a control through this actor.
	cleanupErr := s.flushPendingEvents()
	cleanupErr = errors.Join(cleanupErr, safeClose("runner", s.runner.Close))
	if manager := s.swapMCP(nil); manager != nil {
		cleanupErr = errors.Join(cleanupErr, safeClose("mcp", manager.Close))
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
	// Retained provider results get one bounded DB-only settlement retry before
	// terminal recovery substitutes estimates for unresolved attempts.
	cleanupErr = errors.Join(cleanupErr, s.flushPendingAccounting())
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
			if err := s.processWorkerBatch(s.supervisor.take()); err != nil {
				return err
			}
			if err := s.dispatch(); err != nil {
				return err
			}
		}
	}
}

// processWorkerBatch owns every reply in a taken batch until it is settled,
// including the current event if its handler returns early or panics.
func (s *Session) processWorkerBatch(events []workerEnvelope) (err error) {
	next := 0
	defer func() {
		if value := recover(); value != nil {
			err = panicError("actor event", value)
		}
		if err != nil {
			for i := next; i < len(events); i++ {
				replyErr := ErrStopped
				if i == next {
					replyErr = err
				}
				s.replyWorkerEvent(&events[i], CommandResult{}, replyErr)
			}
		}
	}()
	for ; next < len(events); next++ {
		if err := s.handleWorkerEvent(&events[next]); err != nil {
			return err
		}
	}
	return nil
}

// replyWorkerEvent is the only sender for actor-owned replies. Clearing the
// channel transfers ownership and lets batch cleanup safely revisit an event.
func (s *Session) replyWorkerEvent(event *workerEnvelope, result CommandResult, err error) {
	if err != nil && event.client != nil {
		event.client.replacement.close()
	}
	if event.reply != nil {
		reply := event.reply
		event.reply = nil
		reply <- err
	}
	if event.client != nil && event.client.reply != nil {
		reply := event.client.reply
		event.client.reply = nil
		reply <- clientCommandReply{result: result, err: err}
	}
}

func (s *Session) handleWorkerEvent(event *workerEnvelope) error {
	if event.err != nil {
		return event.err
	}
	switch event.kind {
	case workerControl:
		ctx := event.controlCtx
		if ctx == nil {
			ctx = s.supervisor.ctx
		}
		if err := ctx.Err(); err != nil {
			s.replyWorkerEvent(event, CommandResult{}, err)
			return nil
		}
		if s.supervisor.ctx.Err() != nil {
			s.replyWorkerEvent(event, CommandResult{}, ErrStopped)
			return nil
		}
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(s.supervisor.ctx, cancel)
		defer func() { stop(); cancel() }()
		err := errors.New("actor control is missing")
		if event.control != nil {
			err = event.control(ctx)
		}
		s.replyWorkerEvent(event, CommandResult{}, err)
	case workerClientCommand:
		result, err := s.completeClientCommand(event.client)
		s.replyWorkerEvent(event, result, err)
		return err
	case workerStream:
		return s.recordStreamEvent(event.stream)
	case workerScheduleTick:
		return s.fireDueSchedules(event.at)
	case workerTurn:
		return s.completeTurn(event.completion)
	}
	return nil
}

func (s *Session) flushPendingEvents() error {
	events := s.supervisor.take()
	var flushErr error
	// Answer the whole batch before best-effort stream persistence, which may
	// itself fail. No late stream event can strand an admitted control.
	for i := range events {
		flushErr = errors.Join(flushErr, events[i].err)
		s.replyWorkerEvent(&events[i], CommandResult{}, ErrStopped)
	}
	for _, event := range events {
		if event.kind == workerStream {
			flushErr = errors.Join(flushErr, s.recordStreamEvent(event.stream))
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
	if len(payload) > sessionstore.InlineValueLimit {
		value, err := s.store.StoreContent(s.supervisor.ctx, sessionstore.ContentGrant{
			RootID: s.meta.ID, Scope: sessionstore.ContentGrantRoot,
		}, sessionstore.RuntimePayload{Data: payload, MediaType: "application/json", Source: stream.kind})
		if err != nil {
			return err
		}
		event := stream.event
		// Keep complete small fields; partial JSON arguments/results would invent
		// parse failures. Full bodies stay explicitly readable through the handle.
		if len(event.Text) > 1024 {
			event.Text = ""
		}
		if len(event.Args) > 1024 {
			event.Args = ""
		}
		if len(event.Result) > 1024 {
			if stream.kind == "stream.cell.host" && event.HostStatus == "" {
				event.HostStatus = "failed"
			}
			event.Result = ""
		}
		event.Accounting, event.Usage = nil, nil
		payload, err = json.Marshal(protocol.ContentEventPayload{StreamEvent: event,
			Content: protocol.ContentHandle{ReferenceID: value.ReferenceID, Digest: value.Digest,
				Size: value.Size, MediaType: value.MediaType, Source: value.Source}, Truncated: true})
		if err != nil {
			return err
		}
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
	var input *sessionstore.InboxItem
	authored := false
	if len(items) > 0 {
		item := items[0]
		input = &item
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
		if blocked, err := s.store.RootMailboxNeedsInput(ctx, s.meta.ID, s.authority.AgentID); err != nil || blocked {
			return err
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
		var text string
		var parts []llm.ContentPart
		if input != nil {
			var err error
			text, parts, err = s.decodeInboxInput(turnCtx, *input)
			if err != nil {
				return workerCompletion{sequence: current.seq, err: err}
			}
		}
		content, _ := s.runner.(contentRunner)
		if len(parts) > 0 && content == nil {
			return workerCompletion{sequence: current.seq, err: fmt.Errorf("%w: session runner does not support content parts", sessionstore.ErrInvalidInput)}
		}
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
			output, err = content.TurnParts(turnCtx, text, parts, started, accepted)
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
	return s.decodeInboxInput(s.supervisor.ctx, item)
}

func (s *Session) decodeInboxInput(ctx context.Context, item sessionstore.InboxItem) (string, []llm.ContentPart, error) {
	data, err := s.store.ResolveInboxPayload(ctx, item)
	if err != nil {
		return "", nil, err
	}
	if item.Kind != "submit.parts" && item.Kind != "steer.parts" {
		return string(data), nil, nil
	}
	var payload SubmitPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", nil, fmt.Errorf("%w: invalid content-parts submission", sessionstore.ErrInvalidInput)
	}
	return s.resolveAttachments(ctx, item.AgentID, payload)
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
	if completion.err == nil && s.meta.Goal != "" && s.definition.Surface.GoalLoop {
		if agent.GoalMet(completion.output) {
			clearGoal = true
		} else if s.goalRounds < s.goalMax {
			goalContinuation = agent.GoalContinuePrompt(s.meta.Goal)
		}
	}
	outcome := completion.output
	if completion.err != nil {
		outcome = completion.err.Error()
	}
	if err := s.store.CommitRootTurn(s.supervisor.ctx, sessionstore.RootTurnCommit{
		RootID: s.meta.ID, AgentID: s.authority.AgentID, InboxSeq: current.seq, TurnID: current.turnID,
		AcknowledgedInbox: acknowledged, DeliveredMessages: completion.journal.DeliveredMessages,
		Messages: completion.journal.Messages, Compactions: journalCompactions(completion.journal),
		WorkspaceSeq: completion.workspaceSeq, WorkspaceRef: completion.workspaceRef,
		ClearGoal: clearGoal, GoalContinuation: goalContinuation,
		Model: s.meta.Model, Provider: s.meta.Provider, Status: status, Error: errorText,
		Outcome: sessionstore.RuntimePayload{Data: encodeCommandOutcome("submit", outcome, completion.err), MediaType: "application/json", Source: "command outcome"},
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
		s.settle(seq, Completion{Sequence: seq, Err: completion.err})
	}
	for _, seq := range completion.journal.ClaimedInbox {
		if !slices.Contains(acknowledged, seq) {
			s.settle(seq, Completion{Sequence: seq, Err: errors.New("input interrupted before delivery")})
		}
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
	s.startPendingReload()
	return nil
}

func (s *Session) applyPendingReloadAfterAgent() {
	_ = s.routeControl(context.Background(), func(ctx context.Context) error {
		s.startPendingReload()
		return nil
	})
}

func (s *Session) maybeGenerateTitle() {
	if !s.autoTitle || s.titleAttempted || s.meta.Kind != sessionstore.SessionKindAgent || !s.definition.Surface.AutoTitle {
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
	placeholder := sessionstore.ProvisionalTitle(history)
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
