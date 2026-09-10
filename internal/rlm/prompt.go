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

// Identity tells a node who it is, how to address its parent, and how its
// parent hears from it. Every node receives one; the root's says so.
type Identity struct {
	AgentID    string
	Name       string
	ParentID   string
	ParentName string
	Depth      int
	Report     string // spawn report mode: "" or "notice", "inline", "message"
}

// IdentityBlock renders the identity appended to a node's system prompt.
func IdentityBlock(identity Identity) string {
	if identity.AgentID == "" {
		return ""
	}
	if identity.ParentID == "" {
		return fmt.Sprintf("\n\nIdentity: root agent (id %s, name %q).", identity.AgentID, identity.Name)
	}
	report := "When your turn ends your parent gets an agent.completed notice with a 160-byte preview of your last text (plus an evidence handle to the rest when it is longer); answer with messages.send(recipient=\"parent\", ...) only when the task needs more than that."
	switch identity.Report {
	case "inline":
		report = "When your turn ends your parent gets a notice carrying up to 4 KiB of your last text; put your answer in that final text and do not also send it as a message."
	case "message":
		report = "Your parent gets no completion notice when you succeed; it hears from you only through messages.send(recipient=\"parent\", ...), so always send your answer."
	}
	return fmt.Sprintf("\n\nIdentity: agent %q (id %s), depth %d; parent %q (id %s). %s",
		identity.Name, identity.AgentID, identity.Depth, identity.ParentName, identity.ParentID, report)
}

// FocusedHistory keeps at most four recent user/assistant exchanges plus one
// bounded compaction summary. Tool payloads and older corpus text stay behind
// context.history() and context.search().
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
	const marker = "\n... [retrieve the original with context.history() or context.search()] ...\n"
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
