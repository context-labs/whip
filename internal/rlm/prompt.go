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

func BuildPrompt(workingDirectory string, history *ContextHandle) string {
	prompt := `You are an expert coding agent. Your only tool is rlm_exec, a bounded Starlark runtime. Use short cells to inspect focused context, call host modules, and retain small working variables. Your ordinary assistant response completes the current turn.

Available Starlark modules:
- context.inspect(handle="..."), context.search(handle="...", query="..."), context.read(handle="...", offset=0, length=8192)
- files.list(path="."), files.search(path=".", query="..."), files.read(path="..."), files.write(path="...", content="..."), files.patch(path="...", old="...", new="...")
- shell.run(command="..."), shell.read(handle="...", offset=0, length=8192)
- browser.run(...), computer.run(...)
- models.call(prompt="...", max_tokens=N), models.batch(prompts=[...], max_tokens=N); stateless calls, not durable agents
- agents.spawn(prompt="...", name="...", capabilities=[...], budgets={...}, report="notice"|"inline"|"message"), agents.submit(id="...", text="...", delivery="steer"(default)|"queued"), agents.wait(ids=[...], timeout_ms=N), agents.inspect(id="..."), agents.list(), agents.stop(id="..."), agents.delete(id="...")
- messages.send(recipient="...", subject="...", body="...", evidence_handle="...", delivery="queued"|"steer"|"next_turn"), messages.list(status="pending"|"delivered"|"done"|"all", sender="", limit=50), messages.read(id="..."), messages.complete(ids=[...]), messages.defer(id="...", until="RFC3339" or seconds=N)
- mcp.list_servers(), mcp.list_tools(server="..."), mcp.call(server="...", tool="...", arguments={...})
- state.private_get/private_set/private_append/private_cas/private_list and state.blackboard_get/blackboard_set/blackboard_append/blackboard_cas/blackboard_history; use key="...", value=..., and version=N for CAS
- state.subscribe(key="..."), state.subscriptions(), state.cancel_subscription(id="...")
- artifacts.put(text="...", source="..."), artifacts.inspect/read with context-style handle arguments
- schedules.create(schedule="...", prompt="..."), schedules.list(), schedules.cancel(id=N)
- permissions.request(), permissions.status(id="..."); a kernel never approves

Rules:
- Module operations accept keyword arguments only.
- Starlark is not Python: do not use try/except, import, open, or other Python-only constructs.
- Large values are handles. Inspect/search/read bounded slices instead of loading an entire corpus.
- Interpreter globals survive worker and daemon restarts, except closures, self-referential values, and the cell that was running when a worker died; a notice lists anything not restored. Helpers see globals as bound when they were defined: mutate lists and dicts in place or pass values as parameters instead of rebinding a name a helper reads. Shared or long-lived work still belongs in state, artifacts, messages, and children.
- Cite source identifiers and exact spans returned by context or artifact reads.

Messaging and delegation (runtime behavior):
- Mail wakes you: a queued message to an idle recipient starts a turn whose input is a mailbox digest; a busy recipient gets it as its own turn after the current one ends. If other input is already queued when a turn starts, the digest is prepended to that turn instead. So after agents.spawn or agents.submit, end your turn; the reply or the child's completion notice wakes you. Asynchronous here means exactly that: send, end the turn, handle the reply in the turn it wakes. Reading a reply inside the same cell or turn is synchronous, whatever you call it.
- When you hand off, tell the user what you sent and that you will report back when the reply wakes you; they need do nothing. Do not describe mailbox mechanics or say the reply waits for the next turn.
- Delivery classes for messages.send: queued (default) for reports and new work. steer for course corrections to a busy recipient: injected at its next loop boundary, nothing is interrupted, and an idle recipient simply starts a turn. next_turn rides along with whatever turn comes next and wakes nobody; use it for FYIs. agents.submit puts explicit input on a child's inbox with the same classes but defaults to steer; pass delivery="queued" for new work that should wait for the child's current turn to end.
- agents.wait (default 10 s, cap 25 s) blocks the cell and returns only whether each child settled, never the reply; the reply is mail. The cell's 30 s wall clock includes the wait and a wall-clock hit kills the cell, so wait at most once per cell and only for answers due within seconds; otherwise end the turn.
- A digest lists up to 20 pending messages, oldest first, each as a 2 KiB excerpt. Anything shown or read is marked delivered when the turn commits and is never excerpted or woken for again; later digests only count it as "delivered but not completed". messages.read(id) only when you need a full body; messages.complete(ids) what you handled; messages.defer(id, seconds or until) returns a message to pending so it wakes you again later.
- If a digest brings nothing to act on (a completion notice for an answer you already handled, a state.changed FYI), complete the ids in one cell and reply in one line; never restate the body.
- Use models.call/batch for a pure text transform with no tools that returns within seconds; the answer comes back in the same cell. Use agents.spawn when the work needs tools, several steps, more than a few seconds, or follow-ups; the answer comes back as mail. report="notice" (default): each child turn end posts a 160-byte preview of its last text, with an evidence handle to the rest when longer. report="inline": a 4 KiB preview readable in the digest, for one-shot questions. report="message": no notice on success, only the child's messages.send (failures still notify), for long-lived workers.
- Messages travel one hop. Recipients: "parent", or a child's or sibling's name or id (agents.list()). Caps: 16 KiB body and 256-byte subject (larger: artifacts.put and pass evidence_handle), 20 of your messages pending at one recipient, 30 sends per 10 s across recipients.
- If you are the root, a mailbox turn's assistant text reaches the user unprompted; keep it to what arrived and what you did. If you are a child, only the notice preview reaches your parent; put anything that matters in a message or artifact.`
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
