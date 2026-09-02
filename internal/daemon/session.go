package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	sessionstore "github.com/context-labs/whip/internal/session"
)

var ErrStopped = errors.New("root session stopped")

// Runner is the smallest daemon seam around the existing Classic agent. Turn
// calls started exactly once after it owns its loop boundary.
type Runner interface {
	Turn(context.Context, string, bool, func(), func(string)) (string, error)
	Steer(string) bool
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

type agentRunner struct {
	agent       *agent.Agent
	mu          sync.Mutex
	turn        turnJournal
	emit        func(string, StreamEvent)
	interactive *daemonInteractiveRunner
}

// NewAgentRunner adapts the production Classic agent to a daemon root.
func NewAgentRunner(value *agent.Agent) Runner { return &agentRunner{agent: value} }

func (r *agentRunner) Turn(ctx context.Context, input string, authored bool, started func(), accepted func(string)) (string, error) {
	return r.runTurn(ctx, input, nil, authored, started, accepted)
}

func (r *agentRunner) TurnParts(ctx context.Context, input string, parts []llm.ContentPart, started func(), accepted func(string)) (string, error) {
	return r.runTurn(ctx, input, parts, true, started, accepted)
}

func (r *agentRunner) runTurn(ctx context.Context, input string, parts []llm.ContentPart, authored bool, started func(), accepted func(string)) (string, error) {
	boundary := len(r.agent.MessagesSnapshot())
	journal := turnJournal{}
	emit := r.emit
	events := agent.Events{OnStart: started, OnSteer: accepted}
	if emit != nil {
		events.OnText = func(text string) { emit("stream.text", StreamEvent{Text: text}) }
		events.OnThink = func(text string) { emit("stream.reasoning", StreamEvent{Text: text}) }
		events.OnToolCall = func(id, name, args string) { emit("stream.tool.call", StreamEvent{ID: id, Name: name, Args: args}) }
		events.OnToolStart = func(id, name, args string) { emit("stream.tool.started", StreamEvent{ID: id, Name: name, Args: args}) }
		events.OnToolOutput = func(id, text string) { emit("stream.tool.output", StreamEvent{ID: id, Text: text}) }
		events.OnToolEnd = func(id, name, result string) {
			emit("stream.tool.completed", StreamEvent{ID: id, Name: name, Result: result})
		}
		events.OnCompactStart = func(_, _ int) { emit("stream.notice", StreamEvent{Text: "compacting context…"}) }
		events.OnRetry = func(event llm.RetryEvent) {
			emit("stream.notice", StreamEvent{Text: fmt.Sprintf("request failed (%v); retrying in %s", event.Err, event.Delay)})
		}
		events.OnUsage = func(usage llm.Usage) {
			payload, _ := json.Marshal(UsageEvent{Used: usage.PromptTokens, Size: r.agent.ContextLimit})
			emit("stream.usage", StreamEvent{Result: string(payload)})
		}
	}
	events.OnCompaction = func(summary string, cutoff int, before []llm.Message) {
		journal.Messages = append(journal.Messages, before[boundary:]...)
		journal.Compactions = append(journal.Compactions, turnCompaction{
			Summary: summary, Cutoff: cutoff, RawTailStart: agent.CompactionRawTailStart(before, cutoff),
		})
		boundary = len(r.agent.MessagesSnapshot())
	}
	var output string
	var err error
	if len(parts) > 0 {
		output, err = r.agent.TurnParts(ctx, input, parts, events)
	} else if authored {
		output, err = r.agent.TurnAuthored(ctx, input, events)
	} else {
		output, err = r.agent.Turn(ctx, input, events)
	}
	history := r.agent.MessagesSnapshot()
	if boundary <= len(history) {
		journal.Messages = append(journal.Messages, history[boundary:]...)
	}
	r.mu.Lock()
	r.turn = journal
	r.mu.Unlock()
	return output, err
}

type contentRunner interface {
	TurnParts(context.Context, string, []llm.ContentPart, func(), func(string)) (string, error)
}

func (r *agentRunner) Steer(text string) bool { return r.agent.DeliverSteer(text) }
func (r *agentRunner) History() []llm.Message { return r.agent.MessagesSnapshot() }
func (r *agentRunner) turnJournal() turnJournal {
	r.mu.Lock()
	defer r.mu.Unlock()
	return turnJournal{Messages: append([]llm.Message(nil), r.turn.Messages...), Compactions: append([]turnCompaction(nil), r.turn.Compactions...)}
}

func (r *agentRunner) Close() {
	r.agent.SetSteerIngress(nil)
	r.agent.Close()
	if r.agent.Services != nil {
		r.agent.Services.Close()
	}
}

func (r *agentRunner) bind(root *Session) error {
	if r.agent.Services == nil {
		return errors.New("agent services are required")
	}
	if err := r.agent.Services.BindDispatcher(root.store, root.store.Workspaces(), root.store.Processes(), root.authority); err != nil {
		return err
	}
	r.agent.SetSessionID(root.meta.ID)
	r.emit = func(kind string, event StreamEvent) {
		root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{kind: kind, event: event}})
	}
	r.interactive = newDaemonInteractiveRunner(r.emit)
	r.agent.Services.SetInteractive(r.interactive)
	r.agent.Tasks().SetSessionID(root.meta.ID)
	tasks, err := root.store.LoadTasks(root.meta.ID)
	if err != nil {
		return err
	}
	ids := make([]string, len(tasks))
	for i, task := range tasks {
		ids[i] = task.ID
	}
	r.agent.ResumeTaskIDs(ids)
	r.agent.SetSteerIngress(func(text string) { root.enqueueWake("steer", text) })
	r.agent.SetOnTodos(func(items []agent.Todo) {
		plan := PlanEvent{Items: make([]PlanItem, 0, len(items))}
		for _, item := range items {
			plan.Items = append(plan.Items, PlanItem{Content: item.Content, Status: item.Status})
		}
		payload, _ := json.Marshal(plan)
		r.emit("stream.plan", StreamEvent{Result: string(payload)})
	})
	r.agent.SetLauncher(root.supervisor.launchWorker)
	r.agent.SetSubagentRuntime(root)
	r.agent.Waits().OnWake = func(text string) { root.enqueueWake("wait", text) }
	r.agent.Tasks().OnRecord = func(sessionID string, task *agent.BackgroundTask) {
		record := sessionstore.Task{
			ID: task.ID, Description: task.Description, Prompt: task.Prompt,
			Status: string(task.Status), Report: task.Report,
			StartedAt: task.StartedAt, EndedAt: task.EndedAt,
		}
		reply := make(chan error, 1)
		root.supervisor.post(workerEnvelope{
			kind: workerTaskRecord, task: &record,
			transcript: append([]llm.Message(nil), task.SubMessages...), model: task.SubModel, reply: reply,
		})
		select {
		case <-root.supervisor.ctx.Done():
		case err := <-reply:
			if err != nil {
				r.agent.Tasks().Cancel(task.ID)
			}
		}
	}
	return nil
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
	workerTurnStarted   = "turn.started"
	workerSteerAccepted = "steer.accepted"
	workerTaskRecord    = "task.record"
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

type turnJournal struct {
	Messages    []llm.Message
	Compactions []turnCompaction
}

type turnJournaler interface{ turnJournal() turnJournal }

type workerEnvelope struct {
	kind       string
	completion workerCompletion
	err        error
	task       *sessionstore.Task
	transcript []llm.Message
	model      string
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
		if last.kind == workerStream && last.stream != nil && last.stream.kind == event.stream.kind && last.stream.event.ID == event.stream.event.ID {
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
	authority  capability.ClassicAuthority
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

	scanSequence int64
	pending      []sessionstore.InboxItem
	running      *sessionstore.InboxItem
	turnStarted  bool
	turnCancel   context.CancelFunc
	clientBusy   bool
	offered      []sessionstore.InboxItem
	accepted     []sessionstore.InboxItem
	children     map[string]*liveSubagent

	pricingMu sync.RWMutex
	pricing   modelPricing
}

func newSession(store *sessionstore.Store, meta sessionstore.Meta, authority capability.ClassicAuthority, components Components, factories ...Factory) *Session {
	goalMax := components.GoalMaxRounds
	if goalMax <= 0 {
		goalMax = config.DefaultGoalMaxRounds
	}
	root := &Session{
		store: store, meta: meta, authority: authority, runner: components.Runner, mcp: components.MCP, runtime: components.Runtime,
		supervisor: newSupervisor(), mailbox: make(chan inboxReady, 1), done: make(chan struct{}),
		receipts: make(map[int64][]*Receipt), goalMax: goalMax, children: make(map[string]*liveSubagent),
	}
	if len(factories) > 0 {
		root.factory = factories[0]
	}
	return root
}

func (s *Session) ID() string               { return s.meta.ID }
func (s *Session) AgentID() string          { return s.authority.AgentID }
func (s *Session) Mode() sessionstore.Mode  { return s.meta.Mode }
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
	return s.enqueue(ctx, "submit", text, true)
}

func (s *Session) Steer(ctx context.Context, text string) (*Receipt, error) {
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
		case "succeeded":
			output, resolveErr := s.store.ResolveRuntimeValue(actorCtx, s.meta.ID, result.Command.Outcome)
			receipt.finish(Completion{Sequence: result.Command.IngressSeq, Output: string(output), Err: resolveErr})
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
	for _, child := range s.children {
		if child.running {
			return true
		}
	}
	return false
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
	s.supervisor.wait()
	cleanupErr = errors.Join(cleanupErr, s.flushPendingEvents())
	if failed {
		_, err := s.store.FailClassicRoot(context.Background(), s.meta.ID, actorErr.Error())
		cleanupErr = errors.Join(cleanupErr, err)
	} else if s.isTerminal() {
		_, err := s.store.StopClassicRoot(context.Background(), s.meta.ID, ErrStopped.Error())
		cleanupErr = errors.Join(cleanupErr, err)
	} else {
		_, err := s.store.InterruptClassicRoot(context.Background(), s.meta.ID, ErrStopped.Error())
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if failed {
		s.finish(errors.Join(actorErr, cleanupErr))
	} else {
		s.finish(errors.Join(ErrStopped, cleanupErr))
	}
}

func (s *Session) actor() error {
	if err := s.loadInbox(); err != nil {
		return err
	}
	if err := s.dispatch(); err != nil {
		return err
	}
	for {
		select {
		case <-s.supervisor.ctx.Done():
			return s.supervisor.ctx.Err()
		case <-s.mailbox:
			if err := s.loadInbox(); err != nil {
				return err
			}
			if err := s.dispatch(); err != nil {
				return err
			}
		case <-s.supervisor.wake:
			events := s.supervisor.take()
			var taskErr error
			for _, event := range events {
				if event.kind != workerTaskRecord {
					continue
				}
				err := s.recordTask(event)
				event.reply <- err
				taskErr = errors.Join(taskErr, err)
			}
			if taskErr != nil {
				for _, pending := range events {
					if pending.kind == workerControl {
						pending.reply <- ErrStopped
					}
				}
				return taskErr
			}
			for i, event := range events {
				if event.kind == workerTaskRecord {
					continue
				}
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
				case workerTurnStarted:
					s.turnStarted = true
				case workerSteerAccepted:
					if len(s.offered) == 0 {
						return errors.New("runner acknowledged an unoffered steer")
					}
					s.accepted = append(s.accepted, s.offered[0])
					s.offered = s.offered[1:]
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
			if err := s.loadInbox(); err != nil {
				return err
			}
			if err := s.dispatch(); err != nil {
				return err
			}
		}
	}
}

func (s *Session) recordTask(event workerEnvelope) error {
	if event.task == nil {
		return errors.New("task record is missing")
	}
	agentID := s.taskAgentID(event.task.ID)
	err := s.store.RecordClassicTaskTranscript(s.supervisor.ctx, s.meta.ID, agentID, *event.task, event.transcript, event.model, "")
	if errors.Is(err, context.Canceled) {
		err = s.store.RecordClassicTaskTranscript(context.Background(), s.meta.ID, agentID, *event.task, event.transcript, event.model, "")
	}
	return err
}

func (s *Session) taskAgentID(taskID string) string {
	if child := s.children[taskID]; child != nil {
		return child.agentID
	}
	return s.authority.AgentID
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
		if event.kind != workerTaskRecord || event.task == nil {
			continue
		}
		err := s.store.RecordClassicTaskTranscript(context.Background(), s.meta.ID, s.taskAgentID(event.task.ID), *event.task, event.transcript, event.model, "")
		flushErr = errors.Join(flushErr, err)
		select {
		case event.reply <- err:
		default:
		}
	}
	return flushErr
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

func (s *Session) loadInbox() error {
	s.admitMu.Lock()
	defer s.admitMu.Unlock()
	limit := sessionstore.MaxInboxBatch - len(s.pending)
	if limit <= 0 {
		return nil
	}
	items, err := s.store.LoadQueuedInbox(s.supervisor.ctx, s.meta.ID, s.authority.AgentID, s.scanSequence, limit)
	if err != nil || len(items) == 0 {
		return err
	}
	s.pending = append(s.pending, items...)
	s.scanSequence = items[len(items)-1].Seq
	if len(items) == limit {
		s.notify()
	}
	return nil
}

func (s *Session) dispatch() error {
	if s.clientBusy {
		return nil
	}
	for len(s.pending) > 0 {
		item := s.pending[0]
		if s.running != nil {
			if !s.turnStarted || !isImmediateWake(item.Kind) {
				return nil
			}
			text, err := s.inboxText(item)
			if err != nil {
				return err
			}
			if !s.runner.Steer(text) {
				return nil
			}
			s.offered = append(s.offered, item)
			s.pending = s.pending[1:]
			continue
		}
		text, parts, err := s.inboxInput(item)
		if err != nil {
			return err
		}
		s.pending = s.pending[1:]
		s.running = &item
		s.turnStarted = false
		authored := item.Kind == "submit" || item.Kind == "submit.parts"
		historyLength := len(s.runner.History())
		if err := s.store.StartClassicTurn(s.supervisor.ctx, s.meta.ID, s.authority.AgentID, item.Seq); err != nil {
			return err
		}
		turnCtx, turnCancel := context.WithCancel(s.supervisor.ctx)
		s.turnCancel = turnCancel
		if err := s.supervisor.launch(workerTurn, func(context.Context) workerCompletion {
			workspaceRef := ""
			workspace, snapshotsWorkspace := s.runner.(workspaceSnapshotRunner)
			if snapshotsWorkspace {
				workspaceRef = workspace.CaptureWorkspace(turnCtx)
			}
			var once sync.Once
			started := func() { once.Do(func() { s.supervisor.post(workerEnvelope{kind: workerTurnStarted}) }) }
			accepted := func(string) { s.supervisor.post(workerEnvelope{kind: workerSteerAccepted}) }
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
				sequence: item.Seq, output: output, err: err, journal: journal,
				workspaceSeq: historyLength, workspaceRef: workspaceRef,
			}
		}); err != nil {
			return err
		}
		return nil
	}
	return nil
}

func isImmediateWake(kind string) bool {
	return kind == "steer" || kind == "wait" || kind == "task" || kind == "goal"
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
	if item.Kind != "submit.parts" {
		return string(data), nil, nil
	}
	var payload SubmitPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", nil, errors.New("invalid content-parts submission")
	}
	return payload.Text, payload.Parts, nil
}

func (s *Session) completeTurn(completion workerCompletion) error {
	if s.running == nil || s.running.Seq != completion.sequence {
		return fmt.Errorf("turn completion sequence %d has no matching inbox item", completion.sequence)
	}
	current := *s.running
	acknowledged := make([]int64, len(s.accepted))
	for i, item := range s.accepted {
		acknowledged[i] = item.Seq
	}
	errorText := ""
	if completion.err != nil {
		errorText = completion.err.Error()
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
	compactions := make([]sessionstore.ClassicCompaction, len(completion.journal.Compactions))
	for i, compaction := range completion.journal.Compactions {
		compactions[i] = sessionstore.ClassicCompaction{
			Summary: compaction.Summary, Cutoff: compaction.Cutoff, RawTailStart: compaction.RawTailStart,
		}
	}
	if err := s.store.CommitClassicTurn(s.supervisor.ctx, sessionstore.ClassicTurnCommit{
		RootID: s.meta.ID, AgentID: s.authority.AgentID, InboxSeq: current.Seq,
		AcknowledgedInbox: acknowledged, Messages: completion.journal.Messages, Compactions: compactions,
		WorkspaceSeq: completion.workspaceSeq, WorkspaceRef: completion.workspaceRef,
		ClearGoal: clearGoal, GoalContinuation: goalContinuation,
		Model: s.meta.Model, Provider: s.meta.Provider, Error: errorText,
		Outcome: sessionstore.RuntimePayload{Data: []byte(completion.output), MediaType: "text/plain", Source: "command outcome"},
	}); err != nil {
		return err
	}
	if clearGoal {
		s.meta.Goal = ""
		s.goalRounds = 0
	} else if goalContinuation != "" {
		s.goalRounds++
	}
	for _, item := range s.accepted {
		s.settle(item.Seq, Completion{Sequence: item.Seq})
	}
	if len(s.offered) > 0 {
		s.pending = append(append([]sessionstore.InboxItem(nil), s.offered...), s.pending...)
	}
	s.running = nil
	s.turnStarted = false
	if s.turnCancel != nil {
		s.turnCancel()
		s.turnCancel = nil
	}
	s.offered = nil
	s.accepted = nil
	s.settle(current.Seq, Completion{Sequence: current.Seq, Output: completion.output, Err: completion.err})
	return nil
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
