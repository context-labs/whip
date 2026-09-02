// Package agent runs the LLM tool-use loop.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// Events receives streaming callbacks during a turn. All fields are optional.
type Events struct {
	OnText      func(delta string)            // assistant text as it streams
	OnThink     func(delta string)            // reasoning/thinking tokens as they stream
	OnToolStart func(id, name, args string)   // a tool call is about to run
	OnToolEnd   func(id, name, result string) // a tool call finished
	// OnToolCall fires as a tool call streams in (id/name/args snapshots; args
	// may be partial mid-stream), so the UI can show a pending row before
	// execution starts. Distinct from OnToolStart, which fires at run time.
	OnToolCall func(id, name, args string)
	// OnToolOutput streams partial output for a running tool call (bash only —
	// throttled snapshots, ~100ms apart). Fires from tool worker goroutines.
	OnToolOutput func(id, outputSoFar string)
	OnSteer      func(text string)    // a steered message was injected
	OnCompact    func(took, kept int) // context was auto-compacted (messages removed/kept)
	// OnCompacted fires when a compaction ran: record the summary+cutoff as
	// an event (the raw log survives) and show info — which model wrote the
	// summary and its spend — in the transcript.
	OnCompacted func(summary string, cutoff int, info CompactInfo)
	// OnCompactStart fires the moment a compaction begins folding history —
	// the summary call can take seconds, so the UI shows "compacting…" while
	// it runs. took is the pre-compaction message count, estTokens the size
	// estimate that triggered it.
	OnCompactStart func(took, estTokens int)
	OnUsage        func(u llm.Usage)       // a request reported its token usage
	OnRetry        func(ev llm.RetryEvent) // a transient request failure is being retried
	// OnDecay fires when the per-turn decay pass rewrote n history messages
	// (superseded reads / aged tool outputs). The caller must re-persist the
	// affected prefix — the store's Save(from=1) INSERT OR REPLACEs it.
	OnDecay func(n int)
}

// CompactInfo reports how one compaction ran: which model wrote the summary
// and what that call spent. Model is the bare model id when the compaction
// ran on the conversation's own client, or "<id> @ <host>" when a dedicated
// compaction client (a different provider route) wrote it. Usage is the
// summary call's tokens (zero when the provider didn't report any).
type CompactInfo struct {
	Model string
	Usage llm.Usage
}

// OnTodos is the agent-level hook fired by setTodos (the todowrite tool)
// whenever the plan is rewritten. Set by the ACP bridge for the duration of
// a turn; nil elsewhere. Kept off Events because todowrite is a tool call
// three layers below the turn loop — threading Events into it would leak the
// streaming abstraction into tools.
func (a *Agent) SetOnTodos(fn func(items []Todo)) {
	a.todosMu.Lock()
	a.onTodos = fn
	a.todosMu.Unlock()
}

// Agent holds one conversation.
type Agent struct {
	Client    *llm.Client
	Model     string // model id sent to the API
	ModelName string // config model name (may differ from Model via id mapping)
	Provider  string // config provider name
	MaxTokens int
	Effort    string // reasoning effort: "" = parameter omitted from requests
	// Temperature/TopP are optional per-model sampling knobs for outbound
	// requests. nil omits the field, preserving provider defaults.
	Temperature *float64
	TopP        *float64
	Tools       []tools.Tool
	Messages    []llm.Message

	// ContextLimit is the model's context window in tokens, as advertised by
	// the provider's GET /models (0 when unadvertised — proactive compaction
	// is then disabled and only the reactive context-limit retry applies).
	ContextLimit int
	// CompactClient and CompactModel run the compaction summary; nil/"" uses
	// the conversation's own client and model.
	CompactClient *llm.Client
	CompactModel  string
	// CompactThreshold is the fraction of ContextLimit at which Turn compacts
	// proactively; 0 uses defaultCompactThreshold.
	CompactThreshold float64

	// TaskDefault is the default subagent route (config taskModel); the zero
	// value runs subagents on the conversation's own client and model.
	TaskDefault SubModel
	// ResolveModel resolves a per-task model override named in a task call.
	// Installed by the front-end (TUI or `whip run`) so the agent stays
	// config-free; nil rejects overrides. It runs on tool worker goroutines,
	// so implementations must not share mutable state with the UI.
	ResolveModel func(model, provider string) (SubModel, error)

	// MaxTurns caps the tool-call loop (rounds of model→tools→model) so a
	// scripted run can't run away. 0 = uncapped (the TUI default).
	MaxTurns int

	// WorktreeSubagents is the session default for running background
	// subagents in their own git worktree (isolated file edits). The subagent
	// tool's per-call `worktree` argument overrides it. Off by default.
	WorktreeSubagents bool

	mu        sync.Mutex
	pending   []pendingSteer // steered user messages awaiting injection
	compacted bool           // a compaction already happened this turn — don't retry-loop
	running   atomic.Bool    // a turn is in flight (wait delivery routes on it)
	waitReg   *waitRegistry  // lazily created by waits()

	// msgsMu guards Messages for concurrent READERS: the turn goroutine
	// mutates Messages freely, but a test/UI reader taking msgsMu sees a
	// consistent slice. Mutations hold it only for the append.
	msgsMu sync.Mutex

	files *fileLocks // per-path mutation locks for parallel tool calls
	bg    *taskRegistry

	// subagentInflight / otherInflight count in-flight tool calls by kind
	// (incremented in runTools after the mutation lock, decremented at tool
	// end). WaitingOnSubagents reads them to let the TUI steer typed input
	// into a turn that's only blocked on subagents, not queue it.
	subagentInflight atomic.Int64
	otherInflight    atomic.Int64

	// Todos is the todowrite plan, rewritten in full by the model and
	// injected per round. Like Messages, it is only mutated by the turn
	// goroutine; the TUI reads it between turns via TodosJSON.
	Todos []Todo

	// onTodos fires after each setTodos (installed per turn by the ACP
	// bridge); todosMu guards it against a raced installer.
	todosMu sync.Mutex
	onTodos func(items []Todo)

	sessionID atomic.Pointer[string] // scopes the per-session memory file + keys the prompt cache (SetSessionID)

	// toolsMu guards mcpTools: the MCP manager's OnChange can fire (server
	// settled) while a Turn is streaming, and Turn reads the tool set per
	// request.
	toolsMu  sync.Mutex
	mcpTools []tools.Tool

	// BrowserDisabled, when true, keeps browser_exec out of the tool set
	// (config browser.enabled=false) even when the manager hook exists.
	BrowserDisabled bool

	// ComputerDisabled, when true, keeps computer_exec out of the tool set
	// (config computer.enabled=false).
	ComputerDisabled bool

	// OnOrphanedSteer, when set by the TUI, receives steered messages that lost
	// the race against a turn's final loop boundary (a Steer landing after the
	// last drainPending but before the turn returned). The TUI submits each as
	// a machine turn so a mid-turn message is never silently dropped. Same
	// shape as the wait tool's OnWake; the two unify when both branches land.
	OnOrphanedSteer func(text string)

	usageMu sync.Mutex
	usage   llm.Usage // session totals across every API call (PromptTokens = input)
}

// TurnRunning reports whether a turn is currently in flight. The wait
// registry routes delivery on it: busy → Steer (drained at the next loop
// boundary), idle → the OnWake hook (a parked steer would never be seen).
func (a *Agent) TurnRunning() bool { return a.running.Load() }

// Steer queues a user message for injection at the next loop boundary of the
// running turn — after the in-flight response and its tool calls complete,
// never mid-generation. When NO turn is running (the caller raced a teardown:
// it saw WaitingOnSubagents true, then the turn ended before this Steer
// landed), there is no boundary left to drain the queue — so the steer goes
// straight to OnOrphanedSteer instead of parking forever. One guard here
// covers every Steer caller (TUI keys, wait-tool delivery, subagent fan-in).
func (a *Agent) Steer(text string) {
	if !a.running.Load() && a.OnOrphanedSteer != nil {
		a.OnOrphanedSteer(text)
		return
	}
	a.mu.Lock()
	a.pending = append(a.pending, pendingSteer{text: text})
	a.mu.Unlock()
}

// pendingSteer is a queued steered message, optionally carrying images
// (browser_exec screenshots attach to the conversation this way).
type pendingSteer struct {
	text  string
	parts []llm.ContentPart
}

// SteerImages is Steer with image parts — the model receives text and
// images together as a multimodal user message at the loop boundary.
func (a *Agent) SteerImages(text string, parts []llm.ContentPart) {
	a.mu.Lock()
	a.pending = append(a.pending, pendingSteer{text: text, parts: parts})
	a.mu.Unlock()
}

// AppendUser adds a non-authored user message to the conversation outside a
// turn — the `!` shell escape shares its output with the model this way. It
// must only be called while no turn is running (the TUI routes mid-turn
// output through Steer instead); the mutex exists so a raced caller trips
// -race on the same word rather than silently tearing the slice.
func (a *Agent) AppendUser(content string) {
	a.mu.Lock()
	a.msgsMu.Lock()
	a.Messages = append(a.Messages, llm.Message{Role: "user", Content: content})
	a.msgsMu.Unlock()
	a.mu.Unlock()
}

func (a *Agent) drainPending() []pendingSteer {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.pending
	a.pending = nil
	return p
}

// AddUsage folds one request's usage into the session totals.
func (a *Agent) AddUsage(u llm.Usage) {
	a.usageMu.Lock()
	a.usage.PromptTokens += u.PromptTokens
	a.usage.CompletionTokens += u.CompletionTokens
	if u.PromptTokensDetails != nil {
		if a.usage.PromptTokensDetails == nil {
			a.usage.PromptTokensDetails = &struct {
				CachedTokens int `json:"cached_tokens"`
			}{}
		}
		a.usage.PromptTokensDetails.CachedTokens += u.PromptTokensDetails.CachedTokens
	}
	a.usageMu.Unlock()
}

// SetUsage seeds the session totals with stored values — a resumed session
// keeps counting from where it was saved, not from zero.
func (a *Agent) SetUsage(u llm.Usage) {
	a.usageMu.Lock()
	a.usage = u
	a.usageMu.Unlock()
}

// ResetUsage zeroes the session totals — /clear starts the spend counter
// over along with the conversation.
func (a *Agent) ResetUsage() {
	a.usageMu.Lock()
	a.usage = llm.Usage{}
	a.usageMu.Unlock()
}

// Usage returns the session's cumulative token usage: input, output, and
// cached-input tokens across every streamed call (plus compaction and
// subagent calls on this agent).
func (a *Agent) Usage() llm.Usage {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	u := a.usage
	if a.usage.PromptTokensDetails != nil {
		d := *a.usage.PromptTokensDetails
		u.PromptTokensDetails = &d
	}
	return u
}

func New(client *llm.Client, model string, maxTokens int, systemPrompt string) *Agent {
	a := &Agent{
		Client:    client,
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  []llm.Message{{Role: "system", Content: systemPrompt}},
	}
	a.Tools = tools.All()
	if !a.BrowserDisabled {
		a.Tools = append(a.Tools, tools.BrowserExec())
	}
	if !a.ComputerDisabled {
		a.Tools = append(a.Tools, tools.ComputerExec())
	}
	a.Tools = append(a.Tools, taskTool(a), taskSteerTool(a))
	a.Tools = append(a.Tools, todoTool(a))
	a.Tools = append(a.Tools, waitTool(a))
	a.Tools = append(a.Tools, memoryTools(a)...)
	a.files = newFileLocks()
	a.bg = newTaskRegistry()
	return a
}

// MessagesSnapshot returns a copy of the conversation safe to read while a
// turn runs on another goroutine. Direct field access (a.Messages) is only
// safe for the goroutine driving the turn.
func (a *Agent) MessagesSnapshot() []llm.Message {
	a.msgsMu.Lock()
	defer a.msgsMu.Unlock()
	return append([]llm.Message(nil), a.Messages...)
}

// SetMCPTools swaps in the current MCP tool set (called by the MCP manager's
// OnChange whenever a server settles). MCP tools live separately from
// a.Tools so a settle mid-turn never mutates the slice a Turn is reading.
// The package-global tools.Suggester is process-wide, shared across agents
// (model switches swap the agent). It must (a) be installed/written under a
// lock so two SetMCPTools calls racing don't tear it, and (b) resolve through
// the LATEST agent, not capture the first — a stale pointer would suggest
// from a replaced agent's tool list.
var (
	suggesterMu      sync.Mutex
	suggesterCurrent atomic.Pointer[Agent]
)

// A Suggester is installed on first use so a stale/typo'd mcp__ call gets a
// "did you mean?" nudge instead of a dead end.
func (a *Agent) SetMCPTools(ts []tools.Tool) {
	a.toolsMu.Lock()
	a.mcpTools = ts
	a.toolsMu.Unlock()
	suggesterMu.Lock()
	suggesterCurrent.Store(a)
	tools.Suggester = func(name string) []string {
		if cur := suggesterCurrent.Load(); cur != nil {
			return cur.suggest(name)
		}
		return nil
	}
	suggesterMu.Unlock()
}

// suggest lists candidate names for tools.Suggester: built-ins + live MCP
// tools, filtered by the mcp package's edit-distance logic.
func (a *Agent) suggest(name string) []string {
	a.toolsMu.Lock()
	all := append(append([]tools.Tool(nil), a.Tools...), a.mcpTools...)
	a.toolsMu.Unlock()
	names := make([]string, len(all))
	for i, t := range all {
		names[i] = t.Def.Function.Name
	}
	return tools.SuggestTool(name, names)
}

// AllTools returns built-ins + the current MCP set.
func (a *Agent) AllTools() []tools.Tool {
	a.toolsMu.Lock()
	defer a.toolsMu.Unlock()
	return append(append([]tools.Tool(nil), a.Tools...), a.mcpTools...)
}

// Turn sends user input and loops until the model stops calling tools.
// It returns the final assistant text. When the estimated conversation size
// crosses CompactThreshold (default 50%) of the provider-advertised context
// limit, Turn compacts
// proactively before the next request; if the provider still rejects the
// request because the conversation exceeded its context window, Turn
// auto-compacts (summarizing old turns) and retries once before surfacing
// the error to the caller.
func (a *Agent) Turn(ctx context.Context, input string, ev Events) (string, error) {
	return a.turn(ctx, input, nil, false, ev)
}

// TurnAuthored is Turn for a message the human actually typed and submitted
// (vs. a steered background-task result or goal-continuation whip injects).
// The message is marked Authored so input-history recall cycles only real
// submissions.
func (a *Agent) TurnAuthored(ctx context.Context, input string, ev Events) (string, error) {
	return a.turn(ctx, input, nil, true, ev)
}

// TurnParts is TurnAuthored with full control over content parts — the ACP
// bridge builds mixed text/image submissions from client content blocks this
// way. With nil parts it behaves exactly like TurnAuthored.
func (a *Agent) TurnParts(ctx context.Context, input string, parts []llm.ContentPart, ev Events) (string, error) {
	return a.turn(ctx, input, parts, true, ev)
}

// TurnWithImages is TurnAuthored for a submission that attaches images. Each
// part is a vision ContentPart (see llm.ImagePart); the model receives the
// text and the images together as a multimodal content array.
func (a *Agent) TurnWithImages(ctx context.Context, input string, parts []llm.ContentPart, ev Events) (string, error) {
	return a.turn(ctx, input, parts, true, ev)
}

func (a *Agent) turn(ctx context.Context, input string, parts []llm.ContentPart, authored bool, ev Events) (string, error) {
	// Decay old tool output before the new user message lands: the pass only
	// prunes history outside the hot window, and running it pre-append keeps
	// the new message (and this turn's tool results) inside the window where
	// they stay byte-stable for the prefix cache.
	if n := a.decay(); n > 0 && ev.OnDecay != nil {
		ev.OnDecay(n)
	}
	a.running.Store(true)
	defer func() {
		a.running.Store(false)
		a.drainOrphanedSteers() // catch steers that lost the race to teardown
	}()
	msg := llm.Message{Role: "user", Content: input, Parts: parts, Authored: authored}
	if authored {
		now := time.Now()
		msg.SentAt = &now
	}
	a.msgsMu.Lock()
	a.Messages = append(a.Messages, msg)
	a.msgsMu.Unlock()
	rounds := 0
	for {
		if a.MaxTurns > 0 && rounds >= a.MaxTurns {
			// Cap reached: instead of failing, make one final no-tools call so
			// the model produces an answer from what it already gathered.
			return a.finalAnswer(ctx, ev)
		}
		rounds++
		if err := a.maybeCompact(ctx, ev); err != nil {
			return "", err
		}
		msgs := a.Messages
		if block := a.todoBlock(); block != "" {
			// Open plan items ride along as an ephemeral system message each
			// round: a.Messages stays clean, and the plan survives long tool
			// loops and compaction because it is re-derived, not stored.
			msgs = append(append([]llm.Message(nil), a.Messages...),
				llm.Message{Role: "system", Content: block})
		}
		if nudge := a.contextPressureNudge(); nudge != "" {
			// Same ephemeral-append pattern as todoBlock: re-derived each round,
			// never persisted, so it appears exactly while pressure is high.
			msgs = append(append([]llm.Message(nil), msgs...),
				llm.Message{Role: "system", Content: nudge})
		}
		// Surface transient-request retries through the event hook so the UI
		// shows "retrying" instead of looking hung. Set/restored per call: the
		// client may outlive this turn's Events.
		a.Client.OnRetry = ev.OnRetry
		msg, usage, err := a.Client.Stream(ctx, llm.Request{
			Model:           a.Model,
			Messages:        msgs,
			Tools:           tools.Defs(a.AllTools()),
			ReasoningEffort: a.Effort,
			Temperature:     a.Temperature,
			TopP:            a.TopP,
		}, ev.OnText, ev.OnThink, ev.OnToolCall)
		a.Client.OnRetry = nil
		a.AddUsage(usage)
		if ev.OnUsage != nil {
			ev.OnUsage(usage)
		}
		if err != nil {
			if !a.compacted && llm.IsContextLimit(err) && ctx.Err() == nil {
				a.compacted = true
				took := len(a.Messages)
				if ev.OnCompactStart != nil {
					ev.OnCompactStart(took, EstimateTokens(a.Messages))
				}
				sum, cutoff, info, cerr := a.compact(ctx)
				if cerr != nil {
					// restore the guard on hard errors so a manual /compact
					// can still attempt a compaction for the next turn
					a.compacted = false
					return "", cerr
				}
				if ev.OnCompact != nil {
					ev.OnCompact(took-len(a.Messages), len(a.Messages))
				}
				if ev.OnCompacted != nil {
					ev.OnCompacted(sum, cutoff, info)
				}
				continue // retry the (now-smaller) request
			}
			return "", err
		}
		msg.Usage = &usage
		msg.Model = a.Model + " @ " + a.Provider
		a.msgsMu.Lock()
		a.Messages = append(a.Messages, msg)
		a.msgsMu.Unlock()
		if len(msg.ToolCalls) > 0 {
			results := a.runTools(ctx, msg.ToolCalls, ev)
			a.msgsMu.Lock()
			for i, tc := range msg.ToolCalls {
				a.Messages = append(a.Messages, llm.Message{
					Role:       "tool",
					Content:    results[i],
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
				})
			}
			a.msgsMu.Unlock()
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
		}
		steered := a.drainPending()
		if len(steered) > 0 {
			a.msgsMu.Lock()
		}
		for _, s := range steered {
			if ev.OnSteer != nil {
				ev.OnSteer(s.text)
			}
			a.Messages = append(a.Messages, llm.Message{Role: "user", Content: s.text, Parts: s.parts})
		}
		if len(steered) > 0 {
			a.msgsMu.Unlock()
		}
		if len(msg.ToolCalls) == 0 && len(steered) == 0 {
			a.compacted = false // reset for the next Turn
			return msg.Content, nil
		}
	}
}

// drainOrphanedSteers re-drains any steered messages that lost the race
// against a turn's final loop boundary: a Steer landing after the last
// drainPending but before running flips false would otherwise sit in pending
// forever (a user's mid-turn message while waiting on subagents, the wait
// registry's busy delivery). The deferred teardown hands each survivor to
// OnOrphanedSteer, which the TUI installs to submit it as a machine turn.
func (a *Agent) drainOrphanedSteers() {
	if a.OnOrphanedSteer == nil {
		return
	}
	for _, s := range a.drainPending() {
		a.OnOrphanedSteer(s.text)
	}
}

// trackTool adjusts the in-flight counts by tool kind.
func (a *Agent) trackTool(name string, delta int64) {
	if name == "subagent" {
		a.subagentInflight.Add(delta)
	} else {
		a.otherInflight.Add(delta)
	}
}

// WaitingOnSubagents reports whether a turn is running and its only in-flight
// work is subagent calls — the model is blocked waiting on them, so a user
// message can be steered in as a mid-turn correction instead of queued behind
// the whole turn (it isn't an interruption if the agent is just waiting).
// Empty in-flight means mid-generation, which keeps the queue behavior.
func (a *Agent) WaitingOnSubagents() bool {
	return a.TurnRunning() && a.subagentInflight.Load() > 0 && a.otherInflight.Load() == 0
}

// runTools executes a batch of tool calls concurrently, returning one result
// per call in the original order (the API matches tool results to call IDs, so
// order must be preserved even though execution is parallel). This is the
// channel-native version of pi's executeToolCallsParallel + withFileMutationQueue:
//
//   - Each call runs in its own goroutine; a buffered results channel collects
//     (index, output) pairs, and a final pass lays them back out in order.
//   - Mutations to the same file serialize through a per-path channel
//     semaphore (fileLocks), so two edits to foo.go can't interleave; edits to
//     different files run truly in parallel.
//   - bash takes a global lock: its side effects aren't attributable to a path.
//   - OnToolStart/OnToolEnd fire per call so the UI shows each tool as it
//     begins and lands, not in a burst at the end.
func (a *Agent) runTools(ctx context.Context, calls []llm.ToolCall, ev Events) []string {
	results := make([]string, len(calls))
	type outcome struct {
		i    int
		out  string
		ms   int64 // wall-clock run time, stored on the ToolCall for /tools perf
		code int   // exit/status: 0 ok, 1 error (best-effort from the output)
	}
	outCh := make(chan outcome, len(calls)) // buffered: never blocks the workers

	var wg sync.WaitGroup
	for i, tc := range calls {
		wg.Add(1)
		go func(i int, tc llm.ToolCall) {
			defer wg.Done()
			name, args := tc.Function.Name, tc.Function.Arguments

			// Serialize against other mutations before starting. Acquiring here
			// (before OnToolStart) keeps "running" rows honest: a tool only
			// shows as running once it actually holds its lock.
			var release func()
			if path, ok := toolMutationPath(name, args); ok {
				release = a.files.acquirePath(path)
			} else if name == "bash" {
				release = a.files.acquireGlobal()
			}
			if release != nil {
				defer release()
			}

			if ev.OnToolStart != nil {
				ev.OnToolStart(tc.ID, name, args)
			}
			a.trackTool(name, 1)
			defer a.trackTool(name, -1)
			start := time.Now()
			callCtx := ctx
			if ev.OnToolOutput != nil && name == "bash" {
				callCtx = tools.WithOnUpdate(ctx, func(soFar string) {
					ev.OnToolOutput(tc.ID, soFar)
				})
			}
			out := tools.Execute(callCtx, a.AllTools(), name, json.RawMessage(args))
			ms := time.Since(start).Milliseconds()
			if ev.OnToolEnd != nil {
				ev.OnToolEnd(tc.ID, name, out)
			}
			outCh <- outcome{i, out, ms, toolExitCode(out)}
		}(i, tc)
	}

	// Close the channel when all workers finish so the range loop terminates.
	go func() {
		wg.Wait()
		close(outCh)
	}()
	for oc := range outCh {
		results[oc.i] = oc.out
		calls[oc.i].DurationMs = oc.ms
		calls[oc.i].ExitCode = oc.code
	}
	return results
}

// toolExitCode infers an exit status from a tool's output. Tools signal errors
// by prefixing their output; 0 means success, 1 means the tool reported a
// failure. Best-effort: the exact status lives in the tool, not the output.
func toolExitCode(out string) int {
	if strings.HasPrefix(out, "error") || strings.HasPrefix(out, "Error") {
		return 1
	}
	return 0
}

// compactKeepBack counts assistant turns (and any tool results they pulled in)
// preserved verbatim at the tail of the history. Keeping recent context means
// any in-flight task the model is working on keeps its tool results in view,
// and we never leave an orphaned tool_call whose result the summary dropped.
const compactKeepBack = 6

// defaultCompactThreshold is the fraction of the provider-advertised context
// window at which Turn compacts proactively when CompactThreshold is unset.
// 50% keeps compaction deterministic instead of letting the context bloat.
const defaultCompactThreshold = 0.5

// threshold is the proactive-compaction fraction of ContextLimit.
func (a *Agent) threshold() float64 {
	if a.CompactThreshold > 0 {
		return a.CompactThreshold
	}
	return defaultCompactThreshold
}

// nudgeThreshold is the fraction of ContextLimit at which Turn starts
// reminding the model to delegate bulk work to subagents instead of pulling
// it into the main context. It sits below the compaction threshold so the
// nudge lands while delegation can still prevent the fold, not after it.
// 35% is early enough to matter, late enough not to nag on short turns.
const nudgeThreshold = 0.35

// contextPressureNudge returns the ephemeral "delegate before you bloat"
// reminder, or "" when context pressure is low (or the provider advertised
// no limit). Turn appends it as a trailing system message each round it
// applies — never persisted, so it can't fossilize in the transcript after
// compaction shrinks things back down. The message is deliberately not a
// compaction warning: the model doesn't control when compaction fires, only
// how much it hoards — so it pushes the behavior the model *does* control.
func (a *Agent) contextPressureNudge() string {
	if a.ContextLimit == 0 {
		return ""
	}
	pct := 100 * EstimateTokens(a.Messages) / a.ContextLimit
	if pct < int(nudgeThreshold*100) {
		return ""
	}
	return fmt.Sprintf("Context usage is at ~%d%% of the window and old turns will be folded into a summary at %.0f%%. Before doing more bulk reading or exploration inline, delegate it to a subagent — its context absorbs the file contents and search results, and only the distilled report lands here. Finish the current step, then hand the next big investigation off (parallel subagent calls in one message for independent questions; background:true for work you don't need to block on).", pct, a.threshold()*100)
}

// maybeCompact folds old turns into a summary once the estimated token count
// crosses the threshold fraction of ContextLimit. It no-ops when the provider
// didn't advertise a limit (ContextLimit == 0) — the reactive context-limit
// retry in Turn still covers that case.
func (a *Agent) maybeCompact(ctx context.Context, ev Events) error {
	if a.ContextLimit == 0 || EstimateTokens(a.Messages) < int(a.threshold()*float64(a.ContextLimit)) {
		return nil
	}
	took := len(a.Messages)
	if ev.OnCompactStart != nil {
		ev.OnCompactStart(took, EstimateTokens(a.Messages))
	}
	sum, cutoff, info, err := a.compact(ctx)
	if err != nil {
		if err.Error() == "not enough history to compact" {
			return nil // too little history to fold; rely on the reactive retry
		}
		return err
	}
	if ev.OnCompact != nil {
		ev.OnCompact(took-len(a.Messages), len(a.Messages))
	}
	if ev.OnCompacted != nil {
		ev.OnCompacted(sum, cutoff, info)
	}
	return nil
}

// EstimateTokens approximates the token count of a conversation. No real
// tokenizer is wired in, so this uses the common ~4 chars/token heuristic for
// message content and tool-call arguments, plus a small per-message overhead
// for roles and tool-call framing. It intentionally overestimates slightly:
// false positives just compact a little early, false negatives cost a
// rejected request.
func EstimateTokens(msgs []llm.Message) int {
	total := 0
	for _, m := range msgs {
		total += 4 + (len(m.TextContent())+3)/4 + 1200*len(m.Parts) // ~tokens for an image
		for _, tc := range m.ToolCalls {
			total += 8 + (len(tc.Function.Name)+len(tc.Function.Arguments)+3)/4
		}
	}
	return total
}

// compact replaces old turns with an LLM-generated summary, keeping the
// system prompt and the last compactKeepBack (ish) messages so recent tool
// results and any in-flight assistant action stay intact. It runs a single
// non-streaming completion — on CompactClient/CompactModel when set, else
// on the conversation's own client and model — and stores the summary as a
// system-role message (it must carry no tool_call IDs that the kept tail
// would orphan).
//
// It returns the summary text, the cutoff (the index in the pre-compaction
// Messages the summary replaces, i.e. where the kept tail began), and a
// CompactInfo (which model wrote the summary and its spend) so callers can
// surface the compaction in the transcript. The caller records the summary
// and cutoff as a compaction event so the raw log survives on disk.
func (a *Agent) compact(ctx context.Context) (summary string, cutoff int, info CompactInfo, err error) {
	if len(a.Messages) <= compactKeepBack+2 { // system + ≥1 user + tail: nothing to fold
		return "", 0, CompactInfo{}, errors.New("not enough history to compact")
	}
	const sysIdx = 0
	sysPrompt := a.Messages[sysIdx]
	tailStart := len(a.Messages) - compactKeepBack
	if tailStart <= sysIdx+1 {
		tailStart = sysIdx + 2 // never drop the first user message entirely
	}
	tail := a.Messages[tailStart:]
	// orphan safety: a kept tail that begins with role "tool" references a
	// tool_call the summary would erase. Walk backwards to the owning
	// assistant message so both stay or both go.
	for len(tail) > 4 && tail[0].Role == "tool" {
		tail = a.Messages[tailStart-1:]
		tailStart--
	}
	history := a.Messages[sysIdx+1 : tailStart]
	summaryPrompt := buildSummaryPrompt(history)
	cli, mdl := a.CompactClient, a.CompactModel
	dedicated := cli != nil
	if cli == nil {
		cli = a.Client
	}
	if mdl == "" {
		mdl = a.Model
	}
	label := mdl
	if dedicated {
		// a dedicated compaction route: name the host so the transcript can
		// tell a cheap summarizer apart from the conversation's own model
		if u, perr := url.Parse(cli.BaseURL); perr == nil && u.Host != "" {
			label = mdl + " @ " + u.Host
		}
	}
	sum, usage, cerr := cli.Complete(ctx, llm.Request{
		Model:     mdl,
		MaxTokens: 1024,
		Messages: []llm.Message{
			sysPrompt,
			{Role: "user", Content: summaryPrompt},
		},
	})
	a.AddUsage(usage) // the summary call is session spend too
	if cerr != nil {
		return "", 0, CompactInfo{}, fmt.Errorf("compaction summary failed: %w", cerr)
	}
	summary = strings.TrimSpace(sum)
	kept := append([]llm.Message(nil), tail...)
	a.msgsMu.Lock()
	a.Messages = append(append([]llm.Message{}, sysPrompt,
		llm.Message{Role: "system", Content: "Summary of the conversation so far:\n\n" + summary},
	), kept...)
	a.msgsMu.Unlock()
	return summary, tailStart, CompactInfo{Model: label, Usage: usage}, nil
}

// buildSummaryPrompt renders the unsummarized turns as a transcript the model
// folds into a concise digest. Tool results are truncated so a giant file
// read doesn't push the summary request over the window we just overflowed.
func buildSummaryPrompt(msgs []llm.Message) string {
	var b strings.Builder
	b.WriteString("Summarize the following conversation between the user and the assistant. ")
	b.WriteString("Capture the user's intent, decisions made, work completed, files touched, ")
	b.WriteString("and any open task the assistant is mid-way through. ")
	b.WriteString("Be concise (a few short paragraphs at most); use bullet points for code/files. ")
	b.WriteString("Do not include verbatim tool output. End with a single line: ")
	b.WriteString("\"Open task: <what the assistant was doing last, or none>\".\n\n")
	b.WriteString("---\n\n")
	writeTranscript(&b, msgs)
	b.WriteString("\n---\n\nWrite the summary now.")
	return b.String()
}

// writeTranscript renders messages as a role-tagged transcript for a
// meta-prompt (compaction summary, goal formulation). Tool results are
// truncated so a giant file read doesn't blow up the request.
func writeTranscript(b *strings.Builder, msgs []llm.Message) {
	for _, m := range msgs {
		switch m.Role {
		case "user":
			fmt.Fprintf(b, "user: %s\n", truncateField(m.TextContent(), 2000))
		case "assistant":
			if c := strings.TrimSpace(m.TextContent()); c != "" {
				fmt.Fprintf(b, "assistant: %s\n", truncateField(c, 2000))
			}
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(b, "assistant called %s(%s)\n", tc.Function.Name, truncateField(tc.Function.Arguments, 500))
			}
		case "tool":
			fmt.Fprintf(b, "tool result: %s\n", truncateField(m.Content, 500))
		}
	}
}

// GoalFromContextDefaultWindow is how many tail messages /goal-from-context
// distills when the user doesn't pass a count.
const GoalFromContextDefaultWindow = 8

// GoalFromContextMessages returns the last n conversation messages (the
// window /goal-from-context distills), skipping the system prompt. n <= 0
// means GoalFromContextDefaultWindow. Fewer than two messages in the window
// means there isn't enough context to formulate a goal.
func GoalFromContextMessages(msgs []llm.Message, n int) ([]llm.Message, error) {
	if n <= 0 {
		n = GoalFromContextDefaultWindow
	}
	if len(msgs) == 0 {
		return nil, errors.New("not enough context to formulate a goal — chat a bit first")
	}
	conv := msgs[1:]
	if len(conv) < 2 {
		return nil, errors.New("not enough context to formulate a goal — chat a bit first")
	}
	if n > len(conv) {
		n = len(conv)
	}
	return conv[len(conv)-n:], nil
}

// BuildGoalFromContextPrompt asks the model to distill the given tail
// messages into a concrete, verifiable goal statement suitable for /goal.
// The reply must be the bare goal text — the TUI sets it verbatim.
func BuildGoalFromContextPrompt(tail []llm.Message) string {
	var b strings.Builder
	b.WriteString("Distill the end of this conversation into a detailed goal the assistant should keep working on until it is verifiably done.\n\n")
	b.WriteString("Reply with ONLY the goal: a first line stating the concrete outcome, then a short bullet list of the specific, checkable completion criteria ")
	b.WriteString("(files to change, commands that must pass, behavior to confirm). Include the key constraints, decisions, and identifiers (file paths, function names, ")
	b.WriteString("error messages) from the conversation so the goal stands alone. No preamble, no quotes, no explanation.\n\n---\n\n")
	writeTranscript(&b, tail)
	b.WriteString("\n---\n\nWrite the goal now.")
	return b.String()
}

func truncateField(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

// ManualCompact lets the TUI's /compact command compact on demand. It calls
// OnCompact and reports whether compaction ran (false when there's too
// little history). It is safe to call while a turn is not in flight.
func (a *Agent) ManualCompact(ctx context.Context, ev Events) error {
	if ev.OnCompactStart != nil {
		ev.OnCompactStart(len(a.Messages), EstimateTokens(a.Messages))
	}
	sum, cutoff, info, err := a.compact(ctx)
	if err != nil {
		return err
	}
	if ev.OnCompact != nil {
		ev.OnCompact(0, len(a.Messages))
	}
	if ev.OnCompacted != nil {
		ev.OnCompacted(sum, cutoff, info)
	}
	return nil
}

// finalAnswer makes one last completion with tools disabled, so a run that hit
// the tool-turn cap still returns the model's best answer instead of an error.
// A system nudge tells the model to stop calling tools and answer now.
func (a *Agent) finalAnswer(ctx context.Context, ev Events) (string, error) {
	msgs := append(append([]llm.Message(nil), a.Messages...),
		llm.Message{Role: "system", Content: "You have reached the tool-call limit. Do NOT request any more tools. Give your final answer now using only what you have already gathered."})
	a.Client.OnRetry = ev.OnRetry
	msg, usage, err := a.Client.Stream(ctx, llm.Request{
		Model:           a.Model,
		Messages:        msgs,
		Tools:           nil, // no tools — force a text answer
		ReasoningEffort: a.Effort,
		Temperature:     a.Temperature,
		TopP:            a.TopP,
	}, ev.OnText, ev.OnThink, ev.OnToolCall)
	a.Client.OnRetry = nil
	a.AddUsage(usage)
	if ev.OnUsage != nil {
		ev.OnUsage(usage)
	}
	if err != nil {
		return "", err
	}
	a.compacted = false
	return msg.Content, nil
}
