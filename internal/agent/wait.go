package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// A wait is a harness-owned poller: the model names a shell command and a
// success condition, and a goroutine re-runs the command on an interval —
// zero LLM turns while waiting. When the condition resolves (met, timed out,
// or the command keeps failing), exactly one message re-enters the agent
// loop: steered at the next loop boundary if a turn is running, or handed to
// OnWake (the TUI's machine-turn hook) when idle. This is the cross-harness
// pattern from .ai-docs/plans/agent-ux-batch/wait-tool-research.md: the timer
// lives outside the model and only the terminal delta is ever delivered —
// never a sleep+check loop burning a turn per poll.

// WaitStatus is one wait's lifecycle state.
type WaitStatus string

const (
	WaitRunning WaitStatus = "running"
	WaitMet     WaitStatus = "condition met"
	WaitTimeout WaitStatus = "timed out"
	WaitFailed  WaitStatus = "command failing"
	WaitKilled  WaitStatus = "cancelled"
)

// waitTask is one registered wait. Done closes on delivery, mirroring
// BackgroundTask: any number of watchers wake on the same close. Detail is
// written before Done closes, so readers must wait on Done (or Status) first.
type waitTask struct {
	ID        string
	Command   string
	Until     string // regex source, "" = exit-0-only
	Interval  time.Duration
	Timeout   time.Duration
	Started   time.Time
	status    atomic.Value // WaitStatus; string-typed, atomic for raced readers
	Detail    string       // delivered message — read only after Done closes
	Done      chan struct{}
	pollDone  chan struct{}
	cancel    context.CancelFunc
	delivered atomic.Bool
}

// Status returns the wait's current lifecycle state.
func (w *waitTask) Status() WaitStatus {
	if v := w.status.Load(); v != nil {
		return v.(WaitStatus)
	}
	return WaitRunning
}

// setStatus sets the lifecycle state (called only before Done closes, or in
// CancelWait before close).
func (w *waitTask) setStatus(s WaitStatus) { w.status.Store(s) }

// waitRegistry owns the live waits for one agent. OnWake is installed by the
// TUI to submit a machine turn when no turn is running; nil in headless runs,
// where idle delivery falls back to queueing a steer for the next turn (an
// idle headless agent has no loop boundary coming, so the text is discarded
// there by design — `whip run` waits are best observed via the busy path).
type waitRegistry struct {
	mu     sync.Mutex
	waits  map[string]*waitTask
	OnWake func(text string)
	agent  *Agent
	ctx    context.Context
	stop   context.CancelFunc
}

var waitIDCounter atomic.Int64

func newWaitRegistry(a *Agent) *waitRegistry {
	ctx, stop := context.WithCancel(context.Background())
	return &waitRegistry{waits: map[string]*waitTask{}, agent: a, ctx: ctx, stop: stop}
}

// Waits returns the agent's wait registry, creating it lazily. The TUI
// installs OnWake on it; the wait tool registers pollers on it.
func (a *Agent) Waits() *waitRegistry { return a.waits() }

// waits lazily attaches the registry.
func (a *Agent) waits() *waitRegistry {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.waitReg == nil {
		a.waitReg = newWaitRegistry(a)
	}
	return a.waitReg
}

// WaitTaskSpec is one wait request from the model.
type WaitTaskSpec struct {
	Command  string
	Until    string        // optional regex matched against stdout
	Interval time.Duration // poll cadence; min 2s
	Timeout  time.Duration // give-up wall clock; max 1h
}

const (
	waitMinInterval = 2 * time.Second
	waitMaxTimeout  = time.Hour
	// waitMaxErrStrikes ends a wait whose command keeps erroring (hermes'
	// 3-strike lesson): a broken command must not poll for the full timeout.
	waitMaxErrStrikes = 3
)

// StartWait registers a wait and spawns its poller goroutine. Returns the
// wait id (wait-<slug>-<n>) or an error for an unusable spec.
func (a *Agent) StartWait(spec WaitTaskSpec) (*waitTask, error) {
	if spec.Command == "" {
		return nil, errors.New("command is required")
	}
	var untilRe *regexp.Regexp
	if spec.Until != "" {
		re, err := regexp.Compile(spec.Until)
		if err != nil {
			return nil, fmt.Errorf("until regex: %w", err)
		}
		untilRe = re
	}
	if spec.Interval < waitMinInterval {
		spec.Interval = waitMinInterval
	}
	if spec.Timeout <= 0 {
		spec.Timeout = 10 * time.Minute
	}
	if spec.Timeout > waitMaxTimeout {
		spec.Timeout = waitMaxTimeout
	}
	r := a.waits()
	r.mu.Lock()
	id := taskSlug(spec.Command, waitIDCounter.Add(1))
	id = "wait-" + id
	ctx, cancel := context.WithCancel(r.ctx)
	w := &waitTask{
		ID: id, Command: spec.Command, Until: spec.Until,
		Interval: spec.Interval, Timeout: spec.Timeout,
		Started: time.Now(), Done: make(chan struct{}), pollDone: make(chan struct{}),
		cancel: cancel,
	}
	r.waits[id] = w
	r.mu.Unlock()

	go r.poll(ctx, w, untilRe)
	return w, nil
}

// poll is the wait's poller goroutine: runs the command each interval until
// the condition resolves, the timeout fires, or the registry stops. Owner of
// the goroutine; exits on every path.
func (r *waitRegistry) poll(ctx context.Context, w *waitTask, until *regexp.Regexp) {
	defer close(w.pollDone)
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()
	deadline := time.NewTimer(w.Timeout)
	defer deadline.Stop()
	strikes := 0
	check := func() (done bool) {
		// Per-run budget is decoupled from the poll cadence: a slow command
		// (gh pr checks, a cold curl) must not eat a strike just because the
		// interval is short. Floor 30s so short-interval waits still let real
		// commands finish; capped at 60s so a hung command can't stall the
		// ticker for the whole session timeout.
		timeout := min(max(w.Interval, 30*time.Second), 60*time.Second)
		res, runErr := r.agent.Services.RunBash(tools.WithWorkingDirectory(ctx, r.agent.WorkingDir), w.Command, timeout)
		if ctx.Err() != nil {
			return true // cancelled / registry stopped — deliver nothing
		}
		if runErr != nil {
			res.Exit = runErr.Error()
		}
		if res.TimedOut || res.Exit != "" {
			strikes++
			if strikes >= waitMaxErrStrikes {
				r.deliver(w, WaitFailed, fmt.Sprintf("[wait %s] gave up: command failed %d consecutive times (last: %s)\n\nLast output:\n%s",
					w.ID, strikes, res.Exit, tailLines(res.Output, 20)))
				return true
			}
			return false
		}
		strikes = 0
		if until == nil || until.MatchString(res.Output) {
			r.deliver(w, WaitMet, fmt.Sprintf("[wait %s done] condition met (ran every %s):\n$ %s\n\n%s",
				w.ID, w.Interval, w.Command, tailLines(res.Output, 40)))
			return true
		}
		return false
	}
	// First check immediately — a condition already true resolves in one run.
	if check() {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			r.deliver(w, WaitTimeout, fmt.Sprintf("[wait %s timeout] %s elapsed without the condition being met:\n$ %s",
				w.ID, w.Timeout, w.Command))
			return
		case <-ticker.C:
			if check() {
				return
			}
		}
	}
}

// deliver settles the wait exactly once (the atomic CAS is the once-guard —
// timeout and a concurrent strike can race) and routes the terminal message:
// steered at the next loop boundary while a turn runs, OnWake when idle.
func (r *waitRegistry) deliver(w *waitTask, status WaitStatus, msg string) {
	if !w.delivered.CompareAndSwap(false, true) {
		return
	}
	w.setStatus(status)
	w.Detail = msg
	// Route BEFORE closing Done: a waiter woken by the close must already
	// find the terminal message delivered (steered or woken) — the old order
	// (close → steer) let a woken test/caller drain pending before the steer
	// landed. Same ordering rule as the task registry's settle.
	if r.agent.TurnRunning() {
		r.agent.Steer(msg)
	} else if r.OnWake != nil {
		r.OnWake(msg)
	}
	// Headless idle (no turn, no hook): the message is dropped by design —
	// an idle headless agent has no loop boundary coming (see waitRegistry doc).
	close(w.Done)
	w.cancel() // stop the ticker select
	// ponytail: no listing surface exists yet (no /waits command), so a
	// settled wait serves no one — drop it to keep the map bounded. If a
	// listing lands later, keep settled rows and bound by count instead.
	r.mu.Lock()
	delete(r.waits, w.ID)
	r.mu.Unlock()
}

// Close stops every poller goroutine. Called when the agent is being torn
// down (session switch/exit); in-flight polls see ctx cancellation and
// deliver nothing.
func (r *waitRegistry) Close() {
	if r.stop != nil {
		r.stop()
	}
	r.mu.Lock()
	waits := make([]*waitTask, 0, len(r.waits))
	for _, wait := range r.waits {
		waits = append(waits, wait)
	}
	r.mu.Unlock()
	for _, wait := range waits {
		<-wait.pollDone
	}
}

// CancelWait stops a running wait. Returns false if unknown or already
// settled. The CAS is the once-guard against a poller mid-deliver: whoever
// wins it owns the close, so Done can never close twice.
func (r *waitRegistry) CancelWait(id string) bool {
	r.mu.Lock()
	w, ok := r.waits[id]
	running := ok && w.Status() == WaitRunning
	r.mu.Unlock()
	if !running {
		return false
	}
	if !w.delivered.CompareAndSwap(false, true) {
		return false // a deliver is already settling it
	}
	w.setStatus(WaitKilled)
	close(w.Done)
	w.cancel()
	r.mu.Lock()
	delete(r.waits, id)
	r.mu.Unlock()
	return true
}

// tailLines keeps the last n lines of s (delivery messages quote output; the
// tail is where the interesting state is).
func tailLines(s string, n int) string {
	lines := []byte(s)
	count, idx := 0, len(lines)
	for idx > 0 && count < n {
		idx--
		if lines[idx] == '\n' {
			count++
		}
	}
	if idx > 0 {
		return "[…]\n" + string(lines[idx+1:])
	}
	return s
}

// waitTool is the model-facing surface: `wait` registers a poller and
// returns immediately.
func waitTool(a *Agent) tools.Tool {
	return tools.Tool{
		Def: llm.NewTool("wait",
			"Wait for an external condition without burning LLM turns: a background poller re-runs the shell command on the given interval (no model involvement while waiting) and you are notified EXACTLY ONCE when the condition is met, the timeout elapses, or the command keeps failing. Use this instead of `sleep N && check` loops (those spend a full turn per poll). Typical uses: CI finishing (`gh pr checks 55 | grep -q pass` or until the command exits 0), a deploy going live, a server coming up. The notification arrives as a message — do NOT poll for it.",
			`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to run repeatedly; success means exit 0"},"until":{"type":"string","description":"Optional regex the command's output must match (in addition to exit 0) to count as met"},"interval":{"type":"number","description":"Seconds between runs (default 10, min 2)"},"timeout":{"type":"number","description":"Seconds before giving up (default 600, max 3600)"}},"required":["command"]}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var spec struct {
				Command  string  `json:"command"`
				Until    string  `json:"until"`
				Interval float64 `json:"interval"`
				Timeout  float64 `json:"timeout"`
			}
			if err := json.Unmarshal(args, &spec); err != nil {
				return "", err
			}
			w, err := a.StartWait(WaitTaskSpec{
				Command:  spec.Command,
				Until:    spec.Until,
				Interval: time.Duration(spec.Interval * float64(time.Second)),
				Timeout:  time.Duration(spec.Timeout * float64(time.Second)),
			})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Waiting (%s): `%s` every %s, giving up after %s. You will be notified once — keep working or answer the user; do NOT sleep-poll.",
				w.ID, w.Command, w.Interval, w.Timeout), nil
		},
	}
}
