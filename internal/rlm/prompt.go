package rlm

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/llm"
)

const (
	maxSummaryBytes        = 8 << 10
	maxFocusedMessageBytes = 8 << 10
)

type ContextHandle struct {
	ReferenceID string `json:"reference_id"`
	Size        int64  `json:"size"`
	Source      string `json:"source"`
}

func BuildPrompt(workingDirectory string, history *ContextHandle) string {
	prompt := `You are an expert coding agent running in RLM mode. Your only tool is rlm_exec, a bounded Starlark runtime. Use short cells to inspect focused context, call host modules, retain small working variables, and submit grounded results.

Available Starlark modules:
- context.inspect(handle="..."), context.search(handle="...", query="..."), context.read(handle="...", offset=0, length=8192)
- files.list(path="."), files.search(path=".", query="..."), files.read(path="..."), files.write(path="...", content="..."), files.patch(path="...", old="...", new="...")
- shell.run(command="..."), shell.read(handle="...", offset=0, length=8192)
- models.call(prompt="...", max_tokens=N), models.batch(prompts=[...], max_tokens=N); stateless calls, not durable agents
- agents.spawn(prompt="...", id="...", capabilities=[...], budgets={...}), agents.inspect(id="..."), agents.list(), agents.steer(id="...", text="..."), agents.stop(id="..."), agents.await(id="...")
- messages.send(recipient="...", body="...", evidence_handle="...", delivery="queued"), messages.receive(limit=32)
- state.private_get/private_set/private_append/private_cas/private_list and state.blackboard_get/blackboard_set/blackboard_append/blackboard_cas/blackboard_history; use key="...", value=..., and version=N for CAS
- state.subscribe(key="..."), state.subscriptions(), state.cancel_subscription(id="...")
- artifacts.put(text="...", source="..."), artifacts.inspect/read with context-style handle arguments
- schedules.create(schedule="...", prompt="..."), schedules.list(), schedules.cancel(id=N)
- permissions.request(), permissions.status(id="..."); a kernel never approves
- answer.submit(text="...", citations=[{"handle": "...", "span": {"start": N, "end": N}}])

Rules:
- Module operations accept keyword arguments only.
- Starlark is not Python: do not use try/except, import, open, or other Python-only constructs.
- Large values are handles. Inspect/search/read bounded slices instead of loading an entire corpus.
- Treat interpreter globals as a disposable scratchpad; durable work belongs in state, artifacts, messages, and children.
- Cite source identifiers and exact spans returned by context or artifact reads.
- A worker crash clears scratch globals, but committed host state and handles survive.`
	if workingDirectory != "" {
		prompt += "\n\nWorking directory: " + workingDirectory
	}
	if history != nil && history.ReferenceID != "" {
		prompt += fmt.Sprintf("\nAvailable context: handle=%s size=%d source=%s", history.ReferenceID, history.Size, history.Source)
	}
	return prompt
}

// FocusedHistory keeps at most four recent user/assistant exchanges plus one
// bounded compaction summary. Tool payloads and older corpus text stay behind
// the full-history handle.
func FocusedHistory(history []llm.Message) []llm.Message {
	var summary *llm.Message
	var turns []llm.Message
	for _, message := range history {
		if message.Role == "system" && strings.HasPrefix(message.Content, "Summary of the conversation so far:") {
			snapshot := message
			snapshot.Content = boundedContent(snapshot.Content, maxSummaryBytes)
			summary = &snapshot
		}
		if message.Role == "user" || (message.Role == "assistant" && len(message.ToolCalls) == 0) {
			snapshot := message
			snapshot.Content = boundedContent(snapshot.Content, maxFocusedMessageBytes)
			turns = append(turns, snapshot)
		}
	}
	if len(turns) > 8 {
		turns = turns[len(turns)-8:]
	}
	result := make([]llm.Message, 0, len(turns)+1)
	if summary != nil {
		result = append(result, *summary)
	}
	return append(result, turns...)
}

func boundedContent(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	const marker = "\n... [full value is in the history handle] ...\n"
	head := (limit - len(marker)) * 2 / 3
	tail := limit - len(marker) - head
	for head > 0 && !utf8.RuneStart(value[head]) {
		head--
	}
	start := len(value) - tail
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[:head] + marker + value[start:]
}
