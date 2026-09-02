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
- context: inspect, search, read
- files: list, search, read, write, patch
- shell: run, read
- models: call, batch (stateless model calls, not durable agents)
- agents: spawn, inspect, list, steer, stop, await (durable children)
- messages: send, receive
- state: private and blackboard get/set/append/CAS/list/subscriptions
- artifacts: put, inspect, read
- schedules: create, list, cancel
- permissions: request, status (never approves)
- answer: submit with source handles and spans

Rules:
- Module operations accept keyword arguments only.
- Large values are handles. Inspect/search/read bounded slices instead of loading an entire corpus.
- Treat interpreter globals as a disposable scratchpad; durable work belongs in state, artifacts, messages, and children.
- Cite source identifiers and exact spans returned by context or artifact reads.
- A worker crash clears scratch globals, but committed host state and handles survive.`
	if workingDirectory != "" {
		prompt += "\n\nWorking directory: " + workingDirectory
	}
	if history != nil && history.ReferenceID != "" {
		prompt += fmt.Sprintf("\nFull prior history: handle=%s size=%d source=%s", history.ReferenceID, history.Size, history.Source)
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
