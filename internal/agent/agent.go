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
	// EphemeralSystem is included in every provider request for this turn but
	// is never appended to the durable/displayed transcript.
	EphemeralSystem string
	// Prefix messages are appended to the durable transcript immediately
	// before this turn's input message (a mailbox digest riding along with a
	// user submit, for example). They are ordinary unauthored user messages.
	Prefix      []llm.Message
	OnStart     func()                        // the turn owns the loop boundary
	OnText      func(delta string)            // assistant text as it streams
	OnThink     func(delta string)            // reasoning/thinking tokens as they stream
	OnToolStart func(id, name, args string)   // a tool call is about to run
	OnToolEnd   func(id, name, result string) // a tool call finished
	// OnToolCall fires as a tool call streams in (id/name/args snapshots; args
	// may be partial mid-stream), so the UI can show a pending row before
	// execution starts. Distinct from OnToolStart, which fires at run time.
	OnToolCall func(id, name, args string)
	// OnToolOutput streams partial output for a running tool call (bash and
	// rlm_exec — throttled snapshots, ~100ms apart). Fires from tool worker
	// goroutines.
	OnToolOutput func(id, outputSoFar string)
	// OnBoundary fires at every loop boundary (after a round's tool results,
	// before the next model call). Messages it returns are appended in order
	// as user messages; the daemon uses it to inject steer-class work pulled
	// from durable state. Returning messages keeps the turn going even when
	// the model produced no tool calls.
	OnBoundary func() []llm.Message
	OnCompact  func(took, kept int) // context was auto-compacted (messages removed/kept)
	// OnCompacted fires when a compaction ran: record the summary+cutoff as
	// an event (the raw log survives) and show info — which model wrote the
	// summary and its spend — in the transcript.
	OnCompacted func(summary string, cutoff int, info CompactInfo)
	// OnCompactStart fires the moment a compaction begins folding history —
	// the summary call can take seconds, so the UI shows "compacting…" while
	// it runs. took is the pre-compaction message count, estTokens the size
	// estimate that triggered it.
	OnCompactStart func(took, estTokens int)
	// OnCompaction includes the pre-compaction history so durable stores can
	// preserve the raw tail behind the derived summary.
	OnCompaction func(summary string, cutoff int, before []llm.Message)
	OnUsage      func(u llm.Usage)       // a request reported its token usage
	OnRetry      func(ev llm.RetryEvent) // a transient request failure is being retried
	// OnDecay fires when the per-turn decay pass rewrote n history messages
	// (superseded reads / aged tool outputs). The caller must re-persist the
	// affected prefix — the store's Save(from=1) INSERT OR REPLACEs it.
	OnDecay func(n int)
}

// ModelCallBudget reserves descendant model spend before a provider request
// and reconciles the provider-reported usage afterward.
type ModelCallBudget interface {
	ReserveModelCall(context.Context, int64) (func(llm.Usage) error, error)
}

// ModelRoute is a resolved model override for a recursively spawned agent.
type ModelRoute struct {
	Client       *llm.Client
	ModelName    string
	Provider     string
	Model        string
	ContextLimit int
	MaxTokens    int
	Effort       string
	Vision       bool
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

// Agent holds one conversation.
type Agent struct {
	Client    *llm.Client
	Model     string // model id sent to the API
	ModelName string // config model name (may differ from Model via id mapping)
	Provider  string // config provider name
	MaxTokens int
	Effort    string // reasoning effort: "" = parameter omitted from requests
	Vision    bool   // model accepts image content parts
	// Temperature/TopP are optional per-model sampling knobs for outbound
	// requests. nil omits the field, preserving provider defaults.
	Temperature *float64
	TopP        *float64
	Tools       []tools.Tool
	Messages    []llm.Message
	// TransformInput may replace oversized root input with durable handle
	// metadata before it enters model context.
	TransformInput func(context.Context, string) (string, error)

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

	// ResolveModel resolves an agents.spawn model override. Descendants inherit
	// this immutable resolver from their parent.
	ResolveModel func(model, provider string) (ModelRoute, error)

	// MaxTurns caps the tool-call loop (rounds of model→tools→model) so a
	// scripted run can't run away. 0 = uncapped (the TUI default).
	MaxTurns int
	// WorkingDir scopes relative tool paths inside the session workspace.
	WorkingDir string

	mu          sync.Mutex
	pending     []pendingSteer // steered user messages awaiting injection
	launcher    func(string, func()) bool
	modelBudget ModelCallBudget
	compacted   bool        // a compaction already happened this turn — don't retry-loop
	running     atomic.Bool // a turn is in flight

	// msgsMu guards Messages for concurrent READERS: the turn goroutine
	// mutates Messages freely, but a test/UI reader taking msgsMu sees a
	// consistent slice. Mutations hold it only for the append.
	msgsMu sync.Mutex

	// toolsMu guards the runtime-owned tool surface and its client identity.
	toolsMu      sync.Mutex
	toolClientID string
	Services     *tools.Services

	usageMu sync.Mutex
	usage   llm.Usage // this agent's own API calls (PromptTokens = input), incl. its compaction summaries
	// lastPrompt is the provider-reported prompt tokens of this agent's most
	// recent conversation request — the real context size the next request
	// starts from. Drives the compaction trigger (the chars/4 estimate
	// undercounts images ~7×; real usage is the provider's bill). Set by
	// notePrompt from the turn loop only: AddUsage also receives foreground
	// subagent and summary-call usage, whose prompt sizes say nothing about
	// this conversation. Reset to 0 by compact (estimate fallback until the
	// next real request lands).
	lastPrompt int
}

// TurnRunning reports whether a turn is currently in flight.
func (a *Agent) TurnRunning() bool { return a.running.Load() }

// SetSessionID keys the provider prompt cache before the session runs.
func (a *Agent) SetSessionID(id string) {
	if a.Client != nil {
		a.Client.CacheKey = id
	}
}

// SetLauncher lets a daemon supervisor own agent-created goroutines.
func (a *Agent) SetLauncher(launcher func(string, func()) bool) {
	a.mu.Lock()
	a.launcher = launcher
	a.mu.Unlock()
}

func (a *Agent) SetModelCallBudget(budget ModelCallBudget) {
	a.mu.Lock()
	a.modelBudget = budget
	a.mu.Unlock()
}

func (a *Agent) modelCallBudget() ModelCallBudget {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.modelBudget
}

func (a *Agent) launch(kind string, work func()) bool {
	a.mu.Lock()
	launcher := a.launcher
	a.mu.Unlock()
	if launcher != nil {
		return launcher(kind, work)
	}
	go work()
	return true
}

// pendingSteer is a queued in-memory message carrying images (browser and
// computer screenshots attach to the conversation this way). Text steers are
// durable and arrive through Events.OnBoundary instead.
type pendingSteer struct {
	text  string
	parts []llm.ContentPart
}

// SteerImages queues a multimodal user message for the next loop boundary of
// the running turn. It is transient: a screenshot belongs to the turn that
// took it and is dropped if that turn ends first.
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

// notePrompt records the prompt size of one of this agent's own conversation
// requests (see lastPrompt). Zero (provider reported no usage) is ignored so
// the previous real value keeps driving the trigger.
func (a *Agent) notePrompt(u llm.Usage) {
	if u.PromptTokens <= 0 {
		return
	}
	a.usageMu.Lock()
	a.lastPrompt = u.PromptTokens
	a.usageMu.Unlock()
}

// AddUsage folds one of this agent's own requests into its totals and, on a
// subagent, forwards it to the parent's sub-usage ledger.
func (a *Agent) AddUsage(u llm.Usage) {
	a.usageMu.Lock()
	addUsage(&a.usage, u)
	a.usageMu.Unlock()
}

// addUsage accumulates u into dst, including the cached-token detail.
func addUsage(dst *llm.Usage, u llm.Usage) {
	dst.PromptTokens += u.PromptTokens
	dst.CompletionTokens += u.CompletionTokens
	if u.PromptTokensDetails != nil {
		if dst.PromptTokensDetails == nil {
			dst.PromptTokensDetails = &struct {
				CachedTokens int `json:"cached_tokens"`
			}{}
		}
		dst.PromptTokensDetails.CachedTokens += u.PromptTokensDetails.CachedTokens
	}
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
// cached-input tokens across every streamed call plus compaction and
// stateless model calls made by this session.
func (a *Agent) Usage() llm.Usage {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	return copyUsage(a.usage)
}

func copyUsage(u llm.Usage) llm.Usage {
	if u.PromptTokensDetails != nil {
		d := *u.PromptTokensDetails
		u.PromptTokensDetails = &d
	}
	return u
}

// NewRuntime constructs the provider loop without choosing a model-facing
// tool surface. Runtime owners install the exact tools the session may see.
func NewRuntime(client *llm.Client, model string, maxTokens int, systemPrompt string, services *tools.Services) *Agent {
	if services == nil {
		services = tools.NewServices()
	}
	return &Agent{
		Client:    client,
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  []llm.Message{{Role: "system", Content: systemPrompt}},
		Services:  services,
	}
}

// MessagesSnapshot returns a copy of the conversation safe to read while a
// turn runs on another goroutine. Direct field access (a.Messages) is only
// safe for the goroutine driving the turn.
func (a *Agent) MessagesSnapshot() []llm.Message {
	a.msgsMu.Lock()
	defer a.msgsMu.Unlock()
	return append([]llm.Message(nil), a.Messages...)
}

// ReplaceHistory swaps the non-system conversation while the daemon root is
// idle. The primary system prompt remains owned by the configured runner.
func (a *Agent) ReplaceHistory(history []llm.Message) {
	a.msgsMu.Lock()
	defer a.msgsMu.Unlock()
	var system []llm.Message
	if len(a.Messages) > 0 && a.Messages[0].Role == "system" {
		system = append(system, a.Messages[0])
	}
	a.Messages = append(system, history...)
}

// CompactNow runs one explicit compaction outside a model turn. The daemon
// serializes it with the root and records the returned durable compaction.
func (a *Agent) CompactNow(ctx context.Context) (string, int, CompactInfo, error) {
	return a.compact(ctx)
}

// SetSystemPrompt replaces the first system message without racing readers.
func (a *Agent) SetSystemPrompt(prompt string) {
	a.msgsMu.Lock()
	defer a.msgsMu.Unlock()
	if len(a.Messages) > 0 && a.Messages[0].Role == "system" {
		a.Messages[0].Content = prompt
	}
}

// SetExclusiveTool switches an agent to a single model-facing capability.
func (a *Agent) SetExclusiveTool(tool tools.Tool, clientID string) {
	a.toolsMu.Lock()
	a.Tools = []tools.Tool{tool}
	a.toolClientID = clientID
	a.toolsMu.Unlock()
}

// suggest lists candidate names from the runtime-owned tool surface.
func (a *Agent) suggest(name string) []string {
	all := a.AllTools()
	names := make([]string, len(all))
	for i, t := range all {
		names[i] = t.Def.Function.Name
	}
	return tools.SuggestTool(name, names)
}

// AllTools returns a snapshot of the runtime-owned tool surface.
func (a *Agent) AllTools() []tools.Tool {
	a.toolsMu.Lock()
	defer a.toolsMu.Unlock()
	return append([]tools.Tool(nil), a.Tools...)
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
	var err error
	a.toolsMu.Lock()
	clientID := a.toolClientID
	a.toolsMu.Unlock()
	if clientID == "" {
		clientID = "agent"
	}
	ctx, err = tools.WithTurnIdentity(ctx, clientID)
	if err != nil {
		return "", err
	}
	if a.TransformInput != nil {
		input, err = a.TransformInput(ctx, input)
		if err != nil {
			return "", err
		}
	}
	// Decay old tool output before the new user message lands: the pass only
	// prunes history outside the hot window, and running it pre-append keeps
	// the new message (and this turn's tool results) inside the window where
	// they stay byte-stable for the prefix cache.
	if n := a.decay(); n > 0 && ev.OnDecay != nil {
		ev.OnDecay(n)
	}
	a.mu.Lock()
	a.running.Store(true)
	a.mu.Unlock()
	defer a.finishTurn()
	if ev.OnStart != nil {
		ev.OnStart()
	}
	msg := llm.Message{Role: "user", Content: input, Parts: parts, Authored: authored}
	if authored {
		now := time.Now()
		msg.SentAt = &now
	}
	a.msgsMu.Lock()
	a.Messages = append(a.Messages, ev.Prefix...)
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
		if ev.EphemeralSystem != "" {
			msgs = append(append([]llm.Message(nil), msgs...), llm.Message{Role: "system", Content: ev.EphemeralSystem})
		}
		// Surface transient-request retries through the event hook so the UI
		// shows "retrying" instead of looking hung. Set/restored per call: the
		// client may outlive this turn's Events.
		toolDefs := tools.Defs(a.AllTools())
		request := llm.Request{
			Model:           a.Model,
			Messages:        msgs,
			Tools:           toolDefs,
			ReasoningEffort: a.Effort,
			Temperature:     a.Temperature,
			TopP:            a.TopP,
			MaxTokens:       a.MaxTokens,
		}
		settleBudget, err := a.reserveModelCall(ctx, request)
		if err != nil {
			return "", err
		}
		a.Client.OnRetry = ev.OnRetry
		msg, usage, err := a.Client.Stream(ctx, request, ev.OnText, ev.OnThink, ev.OnToolCall)
		a.Client.OnRetry = nil
		if settleBudget != nil {
			if budgetErr := settleBudget(usage); err == nil && budgetErr != nil {
				err = budgetErr
			}
		}
		a.AddUsage(usage)
		a.notePrompt(usage)
		if ev.OnUsage != nil {
			ev.OnUsage(usage)
		}
		if err != nil {
			if !a.compacted && llm.IsContextLimit(err) && ctx.Err() == nil {
				a.compacted = true
				before := append([]llm.Message(nil), a.Messages...)
				took := len(before)
				if ev.OnCompactStart != nil {
					ev.OnCompactStart(took, EstimateTokens(before))
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
				if ev.OnCompaction != nil {
					ev.OnCompaction(sum, cutoff, before)
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
			results := a.runTools(ctx, msg.ToolCalls, rounds, ev)
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
		// Loop boundary: transient image steers first, then whatever durable
		// steer-class work the owner pulls through OnBoundary.
		injected := make([]llm.Message, 0, len(a.pending))
		for _, s := range a.drainPending() {
			injected = append(injected, llm.Message{Role: "user", Content: s.text, Parts: s.parts})
		}
		if ev.OnBoundary != nil {
			injected = append(injected, ev.OnBoundary()...)
		}
		if len(injected) > 0 {
			a.msgsMu.Lock()
			a.Messages = append(a.Messages, injected...)
			a.msgsMu.Unlock()
		}
		if len(msg.ToolCalls) == 0 && len(injected) == 0 {
			// Final round: the response that just landed may have pushed the real
			// context over the threshold without a later maybeCompact running, so
			// fold now rather than paying for an over-threshold prefix next request.
			// Skipped when this turn already compacted: the history is small and a
			// second summary would re-fold a fresh fold.
			if !a.compacted {
				if cerr := a.maybeCompact(ctx, ev); cerr != nil {
					return "", cerr
				}
			}
			a.running.Store(false)
			a.compacted = false // reset for the next Turn
			return msg.Content, nil
		}
	}
}

func (a *Agent) reserveModelCall(ctx context.Context, request llm.Request) (func(llm.Usage) error, error) {
	budget := a.modelCallBudget()
	if budget == nil {
		return nil, nil //nolint:nilnil // nil settlement means no model-call budget is configured
	}
	definitionBytes, _ := json.Marshal(request.Tools)
	estimate := int64(EstimateTokens(request.Messages) + max(request.MaxTokens, 1) + (len(definitionBytes)+3)/4)
	return budget.ReserveModelCall(ctx, estimate)
}

func (a *Agent) finishTurn() {
	a.running.Store(false)
	// Steers that landed after the last loop boundary must not leak into the
	// next turn: inbox-offered ones are re-queued durably by the daemon's
	// completeTurn, and screenshot steers belong to the turn that just ended.
	a.drainPending()
}

// runTools executes a batch of tool calls concurrently, returning one result
// per call in the original order (the API matches tool results to call IDs, so
// order must be preserved even though execution is parallel). This is the
// channel-native version of pi's executeToolCallsParallel + withFileMutationQueue:
//
//   - Each call runs in its own goroutine; a buffered results channel collects
//     (index, output) pairs, and a final pass lays them back out in order.
//   - Tool services serialize workspace mutations through the capability
//     dispatcher, so parallel calls here share the same authority as RLM calls.
//   - OnToolStart/OnToolEnd fire per call so the UI shows each tool as it
//     begins and lands, not in a burst at the end.
func (a *Agent) runTools(ctx context.Context, calls []llm.ToolCall, round int, ev Events) []string {
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
		if !a.launch("tool "+tc.Function.Name, func() {
			defer wg.Done()
			name, args := tc.Function.Name, tc.Function.Arguments

			if ev.OnToolStart != nil {
				ev.OnToolStart(tc.ID, name, args)
			}
			start := time.Now()
			callCtx := tools.WithServices(ctx, a.Services)
			callCtx = tools.WithOperationIdentity(callCtx, fmt.Sprintf("%d:%s", round, tc.ID))
			callCtx = tools.WithToolCallID(callCtx, tc.ID)
			callCtx = tools.WithWorkingDirectory(callCtx, a.WorkingDir)
			if ev.OnToolOutput != nil && (name == "bash" || name == "rlm_exec") {
				callCtx = tools.WithOnUpdate(callCtx, func(soFar string) {
					ev.OnToolOutput(tc.ID, soFar)
				})
			}
			out := tools.ExecuteWithSuggester(callCtx, a.AllTools(), name, json.RawMessage(args), a.suggest)
			ms := time.Since(start).Milliseconds()
			if ev.OnToolEnd != nil {
				ev.OnToolEnd(tc.ID, name, out)
			}
			outCh <- outcome{i, out, ms, toolExitCode(out)}
		}) {
			wg.Done()
		}
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

// Compaction keeps a token-budgeted TAIL of whole user turns verbatim, not a
// fixed message count: six messages can be six 50KB tool dumps or six one-line
// acks, and only the token budget treats them alike. The budget scales with
// the model's usable window (min/max clamped) — a 1M-window model keeps more
// recent context than a 128k one. Keeping whole turns means any in-flight
// task the model is working on keeps its tool results in view, and we never
// leave an orphaned tool_call whose result the summary dropped.
const (
	compactTailMinTokens = 2_000  // budget floor: even a tiny window keeps the last exchange
	compactTailMaxTokens = 15_000 // budget ceiling: recent turns beyond this re-derive via tools
	compactTailFraction  = 0.25   // of the usable window
)

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

// maybeCompact folds old turns into a summary once the context size crosses
// the threshold fraction of ContextLimit. The primary measure is the
// provider-reported prompt size of the last request (real billing truth);
// the chars/4 estimate is the fallback for providers that return no usage.
// It no-ops when the provider didn't advertise a limit (ContextLimit == 0) —
// the reactive context-limit retry in Turn still covers that case.
func (a *Agent) maybeCompact(ctx context.Context, ev Events) error {
	if a.ContextLimit == 0 {
		return nil
	}
	limit := int(a.threshold() * float64(a.ContextLimit))
	a.usageMu.Lock()
	reported := a.lastPrompt
	a.usageMu.Unlock()
	if reported > 0 {
		// The last request's prompt is what the next one starts from. Compacts
		// the moment the real bill crosses the user's compactPct even when the
		// chars/4 estimate (which undercounts images) says we're below it.
		if reported < limit {
			return nil
		}
	} else if EstimateTokens(a.Messages) < limit {
		return nil
	}
	before := append([]llm.Message(nil), a.Messages...)
	took := len(before)
	if ev.OnCompactStart != nil {
		ev.OnCompactStart(took, EstimateTokens(before))
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
	if ev.OnCompaction != nil {
		ev.OnCompaction(sum, cutoff, before)
	}
	// Mark that this turn compacted so the final-round check does not fold a
	// fresh fold again; the reactive error path sets a.compacted itself.
	a.compacted = true
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
		total += 4 + (len(m.TextContent())+3)/4
		for _, p := range m.Parts {
			total += llm.PartTokens(p) // pixel-true for images (was: flat 1200)
		}
		for _, tc := range m.ToolCalls {
			total += 8 + (len(tc.Function.Name)+len(tc.Function.Arguments)+3)/4
		}
	}
	return total
}

// compactTailBudget is the token budget for the kept tail of a compaction:
// a quarter of the usable window, clamped to [compactTailMinTokens,
// compactTailMaxTokens]. usable = ContextLimit minus what the threshold
// reserves (the headroom we compact INTO); when ContextLimit is unknown the
// ceiling is the budget.
func (a *Agent) compactTailBudget() int {
	budget := compactTailMaxTokens
	if a.ContextLimit > 0 {
		usable := float64(a.ContextLimit) * (1 - a.threshold())
		budget = int(usable * compactTailFraction)
	}
	return max(min(budget, compactTailMaxTokens), compactTailMinTokens)
}

// compactTailStart picks the tail boundary: the index where the kept tail
// begins. It walks user turns newest→oldest, accumulating each turn's tokens
// (the user message plus its assistant replies and tool results) until adding
// one more turn would exceed the budget. Whole turns only — a turn boundary
// is the only place a summary can start without orphaning a tool_call. The
// newest turn is always kept even when it alone exceeds the budget; the caller
// clamps so the first user message is never folded.
func compactTailStart(msgs []llm.Message, budget int) int {
	acc := 0
	start := len(msgs)
	// Walk back turn by turn. A turn starts at each authored (or first) user
	// message and runs to just before the next one.
	for i := len(msgs) - 1; i >= 1; i-- {
		acc += EstimateTokens(msgs[i : i+1])
		if msgs[i].Role == "user" {
			if acc > budget && start < len(msgs) {
				break // adding this turn would bust the budget; tail stays as-is
			}
			start = i
		}
	}
	return start
}

// compact replaces old turns with an LLM-generated summary, keeping the
// system prompt and a token-budgeted tail of recent whole turns so recent
// tool results and any in-flight assistant action stay intact. It runs a
// single non-streaming completion — on CompactClient/CompactModel when set,
// else on the conversation's own client and model — and stores the summary as
// a system-role message (it must carry no tool_call IDs that the kept tail
// would orphan).
//
// It returns the summary text, the cutoff (the index in the pre-compaction
// Messages the summary replaces, i.e. where the kept tail began), and a
// CompactInfo (which model wrote the summary and its spend) so callers can
// surface the compaction in the transcript. The caller records the summary
// and cutoff as a compaction event so the raw log survives on disk.
func (a *Agent) compact(ctx context.Context) (summary string, cutoff int, info CompactInfo, err error) {
	if len(a.Messages) <= 3 { // system + ≥1 user + tail: nothing to fold
		return "", 0, CompactInfo{}, errors.New("not enough history to compact")
	}
	const sysIdx = 0
	sysPrompt := a.Messages[sysIdx]
	budget := a.compactTailBudget()
	tailStart := compactTailStart(a.Messages, budget)
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
	// Incremental compaction: when a previous summary message exists it
	// carries the folded state forward, so the new fold merges into it
	// instead of re-deriving everything from truncated transcripts. Anything
	// the merge drops is lost — the prompt says so explicitly.
	prior := ""
	if len(history) > 0 && history[0].Role == "system" &&
		strings.HasPrefix(history[0].Content, summaryPrefix) {
		prior = strings.TrimPrefix(history[0].Content, summaryPrefix)
		history = history[1:] // don't re-transcript the summary itself
	}
	summaryPrompt := buildSummaryPrompt(history, prior)
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
	request := llm.Request{
		Model:     mdl,
		MaxTokens: 4096, // room for a real state digest; 1024 clipped multi-hour sessions
		Messages: []llm.Message{
			sysPrompt,
			{Role: "user", Content: summaryPrompt},
		},
	}
	settleBudget, err := a.reserveModelCall(ctx, request)
	if err != nil {
		return "", 0, CompactInfo{}, err
	}
	sum, usage, cerr := cli.Complete(ctx, request)
	if settleBudget != nil {
		if budgetErr := settleBudget(usage); cerr == nil && budgetErr != nil {
			cerr = budgetErr
		}
	}
	a.AddUsage(usage) // the summary call is session spend too
	if cerr != nil {
		return "", 0, CompactInfo{}, fmt.Errorf("compaction summary failed: %w", cerr)
	}
	summary = strings.TrimSpace(sum)
	kept := append([]llm.Message(nil), tail...)
	a.msgsMu.Lock()
	a.Messages = append(append([]llm.Message{}, sysPrompt,
		llm.Message{Role: "system", Content: summaryPrefix + summary},
	), kept...)
	a.msgsMu.Unlock()
	// The pre-fold prompt size is stale now; fall back to the estimate until
	// the next request reports the real post-fold size, else the next round
	// would re-fold the fresh fold.
	a.usageMu.Lock()
	a.lastPrompt = 0
	a.usageMu.Unlock()
	return summary, tailStart, CompactInfo{Model: label, Usage: usage}, nil
}

// CompactionRawTailStart returns the pre-compaction index where the prior
// event's raw tail begins. Generated summaries before it have no raw row.
func CompactionRawTailStart(before []llm.Message, cutoff int) int {
	start := 2 // primary system prompt + current derived summary
	for i := 1; i < cutoff && i < len(before); i++ {
		if before[i].Role == "system" && strings.HasPrefix(before[i].Content, summaryPrefix) {
			start = i + 1
		}
	}
	return start
}

// summaryPrefix marks the folded-summary system message so a later compaction
// recognizes and merges it (incremental fold) instead of re-summarizing it.
// summaryPrefix marks the folded-summary system message so a later compaction
// recognizes and merges it (incremental fold) instead of re-summarizing it.
const summaryPrefix = "Summary of the conversation so far:\n\n"

// buildSummaryPrompt renders the unsummarized turns as a transcript the model
// folds into a concise digest. Tool results are truncated so a giant file
// read doesn't push the summary request over the window we just overflowed.
// When prior is non-empty this is an incremental fold: the model merges the
// new transcript into the running summary rather than starting over — each
// fold then stays small and nothing the summary already captured is
// re-derived from (lossy) truncated tool output.
func buildSummaryPrompt(msgs []llm.Message, prior string) string {
	var b strings.Builder
	if prior != "" {
		b.WriteString("Here is the running summary of the earlier conversation:\n\n<summary>\n")
		b.WriteString(prior)
		b.WriteString("\n</summary>\n\n")
		b.WriteString("Below are the new turns since that summary was written. Merge them into the summary: ")
		b.WriteString("keep everything still relevant (decisions, files touched, state), drop what the new turns ")
		b.WriteString("obsolete, and add the new work. Anything you do not carry into the new summary is lost. ")
	} else {
		b.WriteString("Summarize the following conversation between the user and the assistant. ")
	}
	b.WriteString("Capture the user's intent, decisions made, work completed, files touched, ")
	b.WriteString("and any open task the assistant is mid-way through. ")
	b.WriteString("Use these sections: Objective / Key decisions / Completed / Active (with the exact next step) / Blocked / Relevant files. ")
	b.WriteString("Be concise; use bullet points for code/files. Do not include verbatim tool output. ")
	b.WriteString("End with a single line: \"Open task: <what the assistant was doing last, or none>\".\n\n")
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
	before := append([]llm.Message(nil), a.Messages...)
	if ev.OnCompactStart != nil {
		ev.OnCompactStart(len(before), EstimateTokens(before))
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
	if ev.OnCompaction != nil {
		ev.OnCompaction(sum, cutoff, before)
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
	a.notePrompt(usage)
	if ev.OnUsage != nil {
		ev.OnUsage(usage)
	}
	if err != nil {
		return "", err
	}
	a.compacted = false
	return msg.Content, nil
}
