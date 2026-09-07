package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	sessionstore "github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func (session *AgentSession) Turn(ctx context.Context, input string, authored bool, started func(), accepted func(string)) (string, error) {
	return session.RunTurn(ctx, input, nil, authored, started, accepted, nil)
}

func (session *AgentSession) TurnParts(ctx context.Context, input string, parts []llm.ContentPart, started func(), accepted func(string)) (string, error) {
	return session.RunTurn(ctx, input, parts, true, started, accepted, nil)
}

// RunTurn is the only model-backed execution path for roots and descendants.
// It owns the shared delivery engine: a mailbox digest of ready mail joins
// the turn at its start, steer-class work is injected at every loop
// boundary, and everything the model saw is recorded in the turn journal so
// the caller's commit marks it delivered. Callers retain separate durable
// envelopes around its returned transcript.
func (session *AgentSession) RunTurn(ctx context.Context, input string, parts []llm.ContentPart, authored bool, started func(), _ func(string), prepare func(context.Context) (string, []llm.ContentPart, error)) (string, error) {
	session.mu.Lock()
	session.turn = turnJournal{}
	session.mu.Unlock()
	if session.runtime != nil {
		session.runtime.observeRunTurn(session)
	}
	var release func()
	var start rlm.TurnStart
	if session.kernel != nil {
		var err error
		ctx, start, release, err = session.kernel.AcquireTurn(ctx)
		if err != nil {
			return "", err
		}
		defer release()
	}
	if prepare != nil {
		var err error
		input, parts, err = prepare(ctx)
		if err != nil {
			return "", err
		}
	}
	if err := session.refreshPrompt(ctx); err != nil {
		return "", fmt.Errorf("%w: %w", sessionstore.ErrInvalidInput, err)
	}
	if authored {
		var err error
		input, parts, err = session.prepareAuthoredInput(ctx, input, parts)
		if err != nil {
			return "", fmt.Errorf("%w: %w", sessionstore.ErrInvalidInput, err)
		}
	}
	var turnID string
	var baseSeq int
	if session.root != nil {
		var err error
		turnID, err = session.root.store.RunningTurnID(ctx, session.root.ID(), session.id)
		if err != nil {
			return "", err
		}
		bounds, err := session.root.store.TranscriptBounds(ctx, session.root.ID(), session.id)
		if err != nil {
			return "", err
		}
		baseSeq = bounds.LastSeq
	}
	session.mu.Lock()
	session.turn.TurnID, session.turn.BaseSeq = turnID, baseSeq
	session.mu.Unlock()
	events := agent.Events{OnStart: started}
	events.OnMessage = session.recordTranscriptMessage
	events.OnToolsComplete = session.recordToolMetadata
	digest, receipts, err := session.mailboxDigest(ctx)
	if err != nil {
		return "", err
	}
	if digest != "" {
		for _, receipt := range receipts {
			session.recordDelivered(receipt)
		}
		if strings.TrimSpace(input) == "" && len(parts) == 0 {
			// A mailbox-triggered turn: the digest is the whole input.
			input, authored = digest, false
		} else {
			events.Prefix = []llm.Message{{Role: "user", Content: digest}}
		}
	}
	if strings.TrimSpace(input) == "" && len(parts) == 0 {
		return "", fmt.Errorf("%w: turn has no input", sessionstore.ErrInvalidInput)
	}
	events.OnBoundary = func() ([]llm.Message, error) { return session.pullSteers(ctx, turnID) }
	if notice := scratchNotice(start); notice != "" {
		events.EphemeralSystem = notice
	}
	if emit := session.emit; emit != nil {
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
			emit("stream.usage", StreamEvent{Usage: &UsageEvent{Used: usage.PromptTokens, Size: session.agent.ContextLimit, Usage: usage}})
		}
	}
	events.OnCompaction = func(summary string, cutoff int, before []llm.Message) {
		rawCutoff := agent.RawCompactionCutoff(before, cutoff)
		session.mu.Lock()
		session.turn.Compactions = append(session.turn.Compactions, turnCompaction{
			Summary: summary, Cutoff: cutoff, RawCutoff: &rawCutoff,
		})
		session.mu.Unlock()
	}
	var output string
	if len(parts) > 0 {
		output, err = session.agent.TurnParts(ctx, input, parts, events)
	} else if authored {
		output, err = session.agent.TurnAuthored(ctx, input, events)
	} else {
		output, err = session.agent.Turn(ctx, input, events)
	}
	return output, err
}

func (session *AgentSession) recordTranscriptMessage(message llm.Message) int {
	if message.Role == "" || message.Role == "system" {
		return 0
	}
	// Tool execution later updates duration fields on the live tool calls.
	// The raw journal owns a separate slice, so reads cannot race those writes.
	message.ToolCalls = slices.Clone(message.ToolCalls)
	message.Parts = slices.Clone(message.Parts)
	session.mu.Lock()
	defer session.mu.Unlock()
	message.RawSequence = session.turn.BaseSeq + len(session.turn.Messages) + 1
	session.turn.Messages = append(session.turn.Messages, message)
	return message.RawSequence
}

func journalCompactions(journal turnJournal) []sessionstore.RootCompaction {
	result := make([]sessionstore.RootCompaction, len(journal.Compactions))
	for i, value := range journal.Compactions {
		result[i] = sessionstore.RootCompaction{Summary: value.Summary, Cutoff: value.Cutoff, RawTailStart: value.RawTailStart, RawCutoff: value.RawCutoff}
	}
	return result
}

func (session *AgentSession) recordToolMetadata(calls []llm.ToolCall) {
	session.mu.Lock()
	defer session.mu.Unlock()
	// No later journal message is appended until this batch has settled.
	if last := len(session.turn.Messages) - 1; last >= 0 {
		session.turn.Messages[last].ToolCalls = slices.Clone(calls)
	}
}

// scratchNotice tells the model what a replaced worker revived. It is
// ephemeral: it rides with this turn's requests and never enters history.
func scratchNotice(start rlm.TurnStart) string {
	const tail = " Durable state, messages, artifacts, transcripts, and workspace files remain available."
	if start.Restore == nil {
		if !start.Restarted {
			return ""
		}
		return "Runtime notice: this session's Starlark worker was replaced and no scratch snapshot existed, so its globals were cleared." + tail
	}
	var b strings.Builder
	b.WriteString("Runtime notice: your Starlark worker restarted.")
	if names := start.Restore.Restored; len(names) > 0 {
		fmt.Fprintf(&b, " Scratch restored: %s (%d).", boundedNames(names, 30), len(names))
	} else {
		b.WriteString(" No scratch globals were restored.")
	}
	if len(start.Restore.Failed) > 0 {
		parts := make([]string, 0, len(start.Restore.Failed))
		for _, item := range start.Restore.Failed {
			parts = append(parts, item.Name+" ("+item.Reason+")")
		}
		fmt.Fprintf(&b, " Not restored: %s.", boundedNames(parts, 30))
	}
	b.WriteString(tail)
	return b.String()
}

func boundedNames(names []string, limit int) string {
	if len(names) <= limit {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(names[:limit], ", "), len(names)-limit)
}

// mailboxDigest renders the ready mail for this node at turn start. It
// returns the text and the exact message revisions it showed.
func (session *AgentSession) mailboxDigest(ctx context.Context) (string, []sessionstore.MailboxReceipt, error) {
	if session.root == nil || session.id == "" {
		return "", nil, nil
	}
	digest, err := session.root.ReadMailboxDigest(ctx, session.id)
	if err != nil {
		return "", nil, err
	}
	text, receipts := renderMailboxDigest(digest)
	return text, receipts, nil
}

// pullSteers is the shared loop-boundary hook. It injects queued human steer
// rows and ready steer-class mail as user messages and records them in the
// turn journal so the commit consumes exactly what the model saw.
func (session *AgentSession) pullSteers(ctx context.Context, turnID string) ([]llm.Message, error) {
	if session.root == nil || session.id == "" || turnID == "" {
		return nil, nil
	}
	digest, err := session.root.ReadMailboxDigest(ctx, session.id)
	if err != nil {
		return nil, err
	}
	items, err := session.root.ClaimSteers(ctx, session.id, turnID)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	for _, item := range items {
		session.turn.ClaimedInbox = append(session.turn.ClaimedInbox, item.Seq)
	}
	session.mu.Unlock()
	var out []llm.Message
	var delivered []int64
	for _, item := range items {
		text, parts, err := session.root.decodeInboxInput(ctx, item)
		if err == nil {
			err = session.validateImageInput(parts)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !errors.Is(err, sessionstore.ErrInvalidInput) {
				return nil, err
			}
			if err := session.root.RejectTurnInput(ctx, session.id, turnID, item.Seq, err); err != nil {
				return nil, err
			}
			if session.emit != nil {
				session.emit("stream.notice", StreamEvent{Text: "Rejected input: " + err.Error()})
			}
			continue
		}
		delivered = append(delivered, item.Seq)
		out = append(out, llm.Message{Role: "user", Content: text, Parts: parts, Authored: true})
	}
	session.mu.Lock()
	session.turn.DeliveredInbox = append(session.turn.DeliveredInbox, delivered...)
	session.mu.Unlock()
	for _, message := range digest.Pending {
		if message.Delivery == sessionstore.MessageDeliverySteer && session.recordDelivered(sessionstore.MailboxReceipt{ID: message.ID, Revision: message.Revision}) {
			out = append(out, llm.Message{Role: "user", Content: renderMailboxLine(message, digest)})
		}
	}
	return out, nil
}

// Observations authorize explicit mailbox controls; only delivery receipts
// participate in automatic turn acknowledgement.
func (session *AgentSession) recordObserved(receipt sessionstore.MailboxReceipt) {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.observeMessageLocked(receipt)
}

func (session *AgentSession) observeMessageLocked(receipt sessionstore.MailboxReceipt) {
	if session.turn.observedMessages == nil {
		session.turn.observedMessages = map[string]int64{}
	}
	session.turn.observedMessages[receipt.ID] = receipt.Revision
}

func (session *AgentSession) messageReceipts(ids []string) []sessionstore.MailboxReceipt {
	session.mu.Lock()
	defer session.mu.Unlock()
	receipts := make([]sessionstore.MailboxReceipt, len(ids))
	for i, id := range ids {
		receipts[i] = sessionstore.MailboxReceipt{ID: id, Revision: session.turn.observedMessages[id]}
	}
	return receipts
}

func (session *AgentSession) recordDelivered(receipt sessionstore.MailboxReceipt) bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.observeMessageLocked(receipt)
	if session.turn.seenMessages == nil {
		session.turn.seenMessages = map[sessionstore.MailboxReceipt]bool{}
	}
	if session.turn.seenMessages[receipt] {
		return false
	}
	session.turn.seenMessages[receipt] = true
	session.turn.DeliveredMessages = append(session.turn.DeliveredMessages, receipt)
	return true
}

func renderMailboxDigest(digest sessionstore.MailboxDigest) (string, []sessionstore.MailboxReceipt) {
	if digest.PendingTotal == 0 && digest.DeliveredOpen == 0 {
		return "", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Mailbox digest: %d pending", digest.PendingTotal)
	if digest.DeliveredOpen > 0 {
		fmt.Fprintf(&b, ", %d delivered but not completed", digest.DeliveredOpen)
	}
	if !digest.NextDeferredAt.IsZero() {
		fmt.Fprintf(&b, ", next deferred message at %s", digest.NextDeferredAt.UTC().Format(time.RFC3339))
	}
	b.WriteString(". Excerpts follow; use messages.read(id=...) for a full body, messages.complete(ids=[...]) once handled, messages.defer(id=..., until=...) to revisit later.")
	ids := make([]sessionstore.MailboxReceipt, 0, len(digest.Pending))
	for _, message := range digest.Pending {
		b.WriteString("\n\n")
		b.WriteString(renderMailboxLine(message, digest))
		ids = append(ids, sessionstore.MailboxReceipt{ID: message.ID, Revision: message.Revision})
	}
	if digest.PendingTotal > len(digest.Pending) {
		fmt.Fprintf(&b, "\n\n... and %d more; use messages.list().", digest.PendingTotal-len(digest.Pending))
	}
	return b.String(), ids
}

func renderMailboxLine(message sessionstore.MailboxMessage, digest sessionstore.MailboxDigest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[message id=%s revision=%d from %s", message.ID, message.Revision, digest.Relationships[message.SenderAgentID])
	if name := digest.SenderNames[message.SenderAgentID]; name != "" {
		fmt.Fprintf(&b, " %s", name)
	}
	fmt.Fprintf(&b, " (%s) kind=%s delivery=%s", message.SenderAgentID, message.Kind, message.Delivery)
	if message.Subject != "" {
		fmt.Fprintf(&b, " subject=%s", strconv.Quote(message.Subject))
	}
	fmt.Fprintf(&b, " size=%d]", message.Body.Size)
	if message.Excerpt != "" {
		b.WriteString("\n")
		b.WriteString(message.Excerpt)
	}
	if remaining := message.Body.Size - int64(len(message.Excerpt)); remaining > 0 {
		fmt.Fprintf(&b, "\n... [%d more bytes; messages.read(id=%q)]", remaining, message.ID)
	}
	if message.EvidenceReferenceID != "" {
		fmt.Fprintf(&b, "\nevidence handle: %s", message.EvidenceReferenceID)
	}
	return b.String()
}

func (session *AgentSession) History() []llm.Message { return session.agent.MessagesSnapshot() }

func (session *AgentSession) GenerateTitle(ctx context.Context) (string, llm.Usage, error) {
	var userText, assistantText string
	for _, message := range session.agent.MessagesSnapshot() {
		if userText == "" && message.Role == "user" && message.Authored {
			userText = message.TextContent()
		} else if userText != "" && message.Role == "assistant" && strings.TrimSpace(message.TextContent()) != "" {
			assistantText = message.TextContent()
			break
		}
	}
	if userText == "" || assistantText == "" {
		return "", llm.Usage{}, errors.New("title requires a completed exchange")
	}
	client, model := session.agent.CompactClient, session.agent.CompactModel
	if client == nil || model == "" {
		client, model = session.agent.Client, session.agent.Model
	}
	output, usage, err := client.Complete(ctx, llm.Request{
		Model: model, MaxTokens: 24,
		Messages: []llm.Message{
			{Role: "system", Content: "Name this coding session. Reply with a plain 3-6 word title: no quotes and no trailing period."},
			{Role: "user", Content: "Request: " + boundedTitleText(userText, 300) + "\nResponse: " + boundedTitleText(assistantText, 200)},
		},
	})
	if err != nil {
		return "", usage, err
	}
	title := strings.Trim(strings.TrimSpace(output), "\"'.")
	if title == "" || len(title) > 80 {
		return "", usage, errors.New("model returned an invalid title")
	}
	session.agent.AddUsage(usage)
	return title, usage, nil
}

func boundedTitleText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func (session *AgentSession) ContextAudit() ContextAuditResult {
	messages := session.agent.MessagesSnapshot()
	result := ContextAuditResult{WorkingDirectory: session.agent.WorkingDir}
	focused := messages
	if len(messages) > 0 && messages[0].Role == "system" {
		result.Rows = append(result.Rows, ContextAuditRow{Label: "RLM system prompt", Bytes: len(messages[0].Content)})
		focused = messages[1:]
	}
	result.Rows = append(result.Rows, ContextAuditRow{
		Label: "focused conversation", Bytes: agent.EstimateTokens(focused) * 4,
		Note: fmt.Sprintf("%d messages", len(focused)),
	})
	tools := session.agent.AllTools()
	toolBytes := 0
	for _, tool := range tools {
		definition, _ := json.Marshal(tool.Def)
		toolBytes += len(definition)
	}
	result.Rows = append(result.Rows, ContextAuditRow{
		Label: "model-facing tool schemas", Bytes: toolBytes,
		Note: fmt.Sprintf("%d tool(s); recursive sessions normally expose only rlm_exec", len(tools)),
	})
	session.mu.Lock()
	applied := session.prompt
	session.mu.Unlock()
	if applied.AppliedAt.IsZero() {
		result.Rows = append(result.Rows, ContextAuditRow{Label: "environment sources", Note: "not applied yet; loaded at the next turn"})
	} else {
		result.WorkingDirectory = applied.WorkingDirectory
		result.Rows = append(result.Rows, ContextAuditRow{Label: "environment sources", Note: "applied " + applied.AppliedAt.Format(time.RFC3339) + "; file and cwd changes apply next turn"})
		for _, source := range applied.Sources {
			result.Rows = append(result.Rows, ContextAuditRow{Label: source.Kind, Bytes: source.Bytes, Note: source.Path + " · scope " + source.Scope})
		}
	}
	result.Rows = append(result.Rows, ContextAuditRow{Label: "retained history", Note: "context.history() retrieves this agent's raw transcript; current-turn entries are provisional"})
	if manager := session.root.mcpManager(); manager != nil {
		mcpTools := manager.Tools()
		mcpBytes := 0
		for _, tool := range mcpTools {
			definition, _ := json.Marshal(tool.Def)
			mcpBytes += len(definition)
		}
		result.Rows = append(result.Rows, ContextAuditRow{
			Label: "MCP host schemas", Bytes: mcpBytes,
			Note: fmt.Sprintf("%d tool(s) behind the Starlark mcp module", len(mcpTools)),
		})
	}
	return result
}

// complete runs a model-backed helper call under the same concrete session
// boundary as ordinary turns. It is intentionally private so no second model
// execution adapter can grow beside AgentSession.
func (session *AgentSession) complete(ctx context.Context, prompt string, maxTokens int) (string, llm.Usage, error) {
	if maxTokens <= 0 {
		maxTokens = session.agent.MaxTokens
	}
	request := llm.Request{
		Model: session.agent.Model, MaxTokens: maxTokens,
		Messages: []llm.Message{{Role: "user", Content: prompt}},
	}
	settle := func(llm.Usage) error { return nil }
	if session.root != nil && session.id != "" {
		estimate := int64(agent.EstimateTokens(request.Messages) + max(maxTokens, 1))
		var err error
		settle, err = session.root.ReserveAgentModelCall(ctx, session.id, estimate)
		if err != nil {
			return "", llm.Usage{}, err
		}
	}
	output, usage, err := session.agent.Client.Complete(ctx, request)
	if settleErr := settle(usage); err == nil {
		err = settleErr
	}
	session.agent.AddUsage(usage)
	return output, usage, err
}

func (session *AgentSession) turnJournal() turnJournal {
	session.mu.Lock()
	defer session.mu.Unlock()
	return turnJournal{
		TurnID:            session.turn.TurnID,
		BaseSeq:           session.turn.BaseSeq,
		Messages:          append([]llm.Message(nil), session.turn.Messages...),
		Compactions:       append([]turnCompaction(nil), session.turn.Compactions...),
		DeliveredInbox:    append([]int64(nil), session.turn.DeliveredInbox...),
		DeliveredMessages: append([]sessionstore.MailboxReceipt(nil), session.turn.DeliveredMessages...),
		ClaimedInbox:      append([]int64(nil), session.turn.ClaimedInbox...),
	}
}

func (session *AgentSession) Close() { session.close(true) }

func (session *AgentSession) permissionServices() *tools.Services { return session.agent.Services }

func (session *AgentSession) bind(root *Session) error {
	if session.agent.Services == nil {
		return errors.New("agent services are required")
	}
	session.agent.Services.SetMCPProvider(root.mcpProvider)
	if err := session.agent.Services.BindDispatcher(root.store, root.store.Workspaces(), root.store.Processes(), root.authority); err != nil {
		return err
	}
	session.agent.SetSessionID(root.meta.ID)
	session.bindPresentation(root)
	return nil
}

func (session *AgentSession) bindPresentation(root *Session) {
	session.emit = func(kind string, event StreamEvent) {
		event.AgentID = session.id
		root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{kind: kind, event: event}})
	}
	session.interactive = newDaemonInteractiveRunner(session.emit)
	session.agent.Services.SetInteractive(session.interactive)
	session.agent.SetLauncher(root.supervisor.launchWorker)
}
