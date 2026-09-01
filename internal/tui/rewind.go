package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/llm"
)

// Rewind: double-esc while idle opens a picker over the conversation's
// authored user messages. Browsing live-scrolls the transcript (opencode's
// dialog-timeline onMove). enter rewinds the conversation to just before the
// selected message — Agent.Messages and the DB are truncated, the transcript
// is rebuilt, and the message text lands back in the input for editing
// (opencode's undo: "the input restore is what makes it feel good"). The
// clipped tail is kept in memory as a redo stack: reopening the picker while
// rewound lists the clipped messages dimmed below the live ones, and enter on
// one moves forward again. Submitting anything new discards the future.
// Fork from any entry with f.

// rewindEntry is one row of the rewind picker. cut is the conversation index
// the entry points at: for a live message it is its index in agent.Messages,
// for a clipped "future" message it is its original conversation index
// (base + position in the redo stack, where base = len(agent.Messages)).
// enter rewinds to just before cut; f forks the history through cut.
type rewindEntry struct {
	cut    int
	text   string     // single-line preview
	when   *time.Time // when the message was submitted (nil = unknown)
	future bool       // clipped by the active rewind; selecting moves forward
}

type rewindState struct {
	entries []rewindEntry // chronological: oldest first, latest LAST
	sel     int           // direct index into entries; starts at the latest
	savedVP int           // viewport offset on open, restored on cancel
}

// rewindBase is where the conversation was cut. future is kept ordered by
// original conversation index (oldest first), so the boundary is simply
// len(agent.Messages); future[i] holds original index base+i.

// Cuts never split a tool_call from its results: entries sit at user
// messages and an assistant message's tool calls/results always follow it,
// so moving the cut to "before the user message" is boundary-safe.
type escArmMsg struct{} // disarms the idle double-esc window

func (m *model) rewindEntries() []rewindEntry {
	var out []rewindEntry
	for i, msg := range m.agent.Messages {
		if msg.Role == "user" && msg.Authored {
			out = append(out, rewindEntry{cut: i, text: oneLine(msg.TextContent()), when: msg.SentAt})
		}
	}
	for i, msg := range m.future {
		if msg.Role == "user" && msg.Authored {
			out = append(out, rewindEntry{
				cut: len(m.agent.Messages) + i, text: oneLine(msg.TextContent()), when: msg.SentAt, future: true,
			})
		}
	}
	return out
}

func oneLine(s string) string { return truncLine(strings.Join(strings.Fields(s), " "), 100) }

// firstLine is the collapsed tool row's text: the first non-empty line,
// whitespace-collapsed.
func firstLine(s string) string {
	for l := range strings.Lines(s) {
		if l = strings.TrimSpace(l); l != "" {
			return truncLine(strings.Join(strings.Fields(l), " "), 120)
		}
	}
	return "(no output)"
}

// lastLines is the running tool row's live tail: the last n non-empty lines,
// each truncated for width sanity, joined with newlines.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	var kept []string
	for i := len(lines) - 1; i >= 0 && len(kept) < n; i-- {
		if l := strings.TrimRight(lines[i], "\r \t"); l != "" {
			kept = append([]string{truncLine(l, 200)}, kept...)
		}
	}
	return strings.Join(kept, "\n  ")
}

// toolVerb is the present-participle a running row leads with ("Reading
// file…"-style); the tool name verbatim when there's no nicer verb.
func toolVerb(name string) string {
	switch name {
	case "read":
		return "Reading"
	case "write":
		return "Writing"
	case "edit":
		return "Editing"
	case "bash":
		return "Running"
	case "subagent", "task": // "task" was the tool's pre-rename name (old sessions)
		return "Delegating"
	case "remember", "forget":
		return "Remembering"
	case "todowrite":
		return "Planning"
	default:
		return name
	}
}

// batchSuffix returns " 2/3"-style text when id is one of several same-name
// tool calls in the current batch (the model emitted N parallel subagent calls
// in one message); "" for a singleton. Self is 1-indexed among same-name rows
// in id order — the order the model listed them. Counts both queued and
// running rows since a batch transitions through toolCallMsg then toolStartMsg.
func (m *model) batchSuffix(name, self string) string {
	var ids []string
	for _, b := range m.blocks {
		if (b.kind == blockToolQueued || b.kind == blockToolRun) && b.toolID != "" && b.toolName == name {
			ids = append(ids, b.toolID)
		}
	}
	if !slices.Contains(ids, self) {
		ids = append(ids, self) // this row isn't on screen yet
	}
	if len(ids) < 2 {
		return ""
	}
	slices.Sort(ids)
	return " " + strconv.Itoa(slices.Index(ids, self)+1) + "/" + strconv.Itoa(len(ids))
}

// scrollToMsg live-scrolls the viewport so the block rendering
// agent.Messages[msgIdx] is near the top.
func (m *model) scrollToMsg(msgIdx int) {
	if msgIdx < 0 || msgIdx >= len(m.msgBlock) {
		return
	}
	bi := m.msgBlock[msgIdx]
	if bi < 0 || bi >= len(m.blocks) {
		return
	}
	m.follow = false
	m.vp.SetYOffset(max(m.blocks[bi].y0-1, 0))
}

func (m *model) openRewind() {
	entries := m.rewindEntries()
	if len(entries) == 0 {
		m.append(dimStyle.Render("(nothing to rewind to yet)"))
		return
	}
	m.rew = &rewindState{entries: entries, sel: len(entries) - 1, savedVP: m.vp.YOffset}
	m.scrollToMsg(entries[len(entries)-1].cut) // selection starts on the latest
}

func (m *model) rewindKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	r := m.rew
	sel := func() rewindEntry { return r.entries[r.sel] }
	switch msg.Type {
	case tea.KeyEsc:
		m.vp.SetYOffset(r.savedVP) // put the scroll back where the user had it
		m.rew = nil
	case tea.KeyUp: // up the list = toward the oldest (top)
		r.sel = max(r.sel-1, 0)
		m.scrollToMsg(sel().cut)
	case tea.KeyDown: // down the list = toward the latest (bottom)
		r.sel = min(r.sel+1, len(r.entries)-1)
		m.scrollToMsg(sel().cut)
	case tea.KeyEnter:
		e := sel()
		text := m.applyRewind(e.cut)
		m.rew = nil
		if !e.future {
			m.input.SetValue(text) // restore the rewound message for editing
			m.input.CursorEnd()
			m.growInput()
		}
	case tea.KeyRunes:
		if string(msg.Runes) == "f" {
			e := sel()
			m.rew = nil
			m.openForkPrompt(e.cut, true) // the copy keeps the selected message
			return m, nil
		}
	}
	return m, nil
}

// applyRewind moves the conversation boundary to cut (an index into
// agent.Messages, clamped to the system prompt). Anything beyond it becomes
// the redo stack; the DB and transcript follow. Returns the authored user
// text at the cut, if any, for restoring into the input.
func (m *model) applyRewind(cut int) string {
	cut = max(cut, 1) // keep the system prompt
	base := len(m.agent.Messages)
	restored, restoreErr := 0, error(nil)
	switch {
	case cut > base: // forward: pull clipped messages back in
		m.agent.Messages = append(m.agent.Messages, m.future[:cut-base]...)
		m.future = append([]llm.Message(nil), m.future[cut-base:]...)
	case cut < base: // back: clip the tail into the redo stack (oldest first)
		clipped := append([]llm.Message(nil), m.agent.Messages[cut:]...)
		m.future = append(clipped, m.future...)
		m.agent.Messages = m.agent.Messages[:cut]
		m.saved = min(m.saved, cut)
		if m.store != nil && m.sessionID != "" {
			if err := m.store.DeleteFrom(m.sessionID, cut); err != nil {
				m.append(errStyle.Render("session save failed: " + err.Error()))
			}
		}
		// restore the workspace to the earliest snapshot being rewound past
		// (the state before the oldest clipped turn ran). Consumed snapshots
		// are dropped from map and DB (DeleteFrom trimmed the rows above) so
		// a later rewind doesn't re-apply them.
		best, bestIdx := "", -1
		for idx, ref := range m.snapshots {
			if idx >= cut && (bestIdx == -1 || idx < bestIdx) {
				best, bestIdx = ref, idx
			}
		}
		if best != "" {
			restored, restoreErr = restoreWorkspace(best)
			for idx := range m.snapshots {
				if idx >= cut {
					delete(m.snapshots, idx)
				}
			}
		}
	}
	m.persist() // re-save any rows pulled back in; no-op otherwise
	m.rebuildTranscript()
	// the workspace note lands AFTER the rebuild — rebuildTranscript resets
	// the block list, so anything appended before it is wiped
	switch {
	case restoreErr != nil:
		m.append(errStyle.Render("workspace rewind failed: " + restoreErr.Error()))
	case restored > 0:
		m.append(dimStyle.Render(fmt.Sprintf("⟲ workspace rewound — %d file(s) restored", restored)))
	}
	text := ""
	if cut < len(m.agent.Messages)+len(m.future) {
		if msg := m.messageAt(cut); msg.Role == "user" && msg.Authored {
			text = msg.TextContent()
		}
	}
	return text
}

// messageAt reads conversation index i across the live/redo boundary.
func (m *model) messageAt(i int) llm.Message {
	if i < len(m.agent.Messages) {
		return m.agent.Messages[i]
	}
	return m.future[i-len(m.agent.Messages)]
}

// rebuildTranscript resets the block list from agent.Messages (rewind moves
// the boundary, so blocks beyond the cut must go).
func (m *model) rebuildTranscript() {
	m.blocks = nil
	m.msgBlock = nil
	m.seedTranscript(m.agent.Messages[1:], 1) // skip the system prompt
}

// rewindView renders the picker strip above the input: oldest at the top,
// latest at the bottom, so ↑ moves toward older and ↓ toward newer. Each entry
// takes two rows — the preview line, then a dimmed timestamp beneath it.
func (m *model) rewindView() string {
	r := m.rew
	const maxRows = 8 // entry rows; each renders as 2 lines
	// window over entries; sel starts at the latest (bottom) so anchor there
	start := max(0, min(r.sel-maxRows/2, len(r.entries)-maxRows))
	end := min(start+maxRows, len(r.entries))
	var b strings.Builder
	b.WriteString(dimStyle.Render("⏪ rewind — enter: rewind here · f: fork from here · esc: cancel"))
	for row := start; row < end; row++ {
		e := r.entries[row]
		b.WriteString("\n")
		if row == r.sel {
			b.WriteString(youStyle.Render(glyphUser + e.text))
		} else if e.future {
			b.WriteString(dimStyle.Render("  " + e.text + " (rewound)"))
		} else {
			b.WriteString("  " + e.text)
		}
		b.WriteString("\n    " + m.rewindTurnMeta(row))
	}
	fmt.Fprintf(&b, "\n%s", dimStyle.Render(fmt.Sprintf("  (%d/%d) ↑ older · ↓ newer", r.sel+1, len(r.entries))))
	return b.String()
}

// turnUsage sums the usage of the turn that starts at conversation index cut
// (the authored user message): every assistant message up to the next user
// message. A tool-looping turn is several API calls, so the sum is the turn's
// total burn. last is the final assistant message's usage: its prompt size is
// the chat's size when the turn ended, which is how the user watches context
// grow turn over turn. ok is false when the turn recorded no usage at all.
func (m *model) turnUsage(cut int) (sum, last llm.Usage, ok bool) {
	for i := cut + 1; i < len(m.agent.Messages)+len(m.future); i++ {
		msg := m.messageAt(i)
		if msg.Role == "user" {
			break // the turn ends where the next submission begins
		}
		if msg.Role == "assistant" && msg.Usage != nil {
			ok = true
			last = *msg.Usage
			sum.PromptTokens += msg.Usage.PromptTokens
			sum.CompletionTokens += msg.Usage.CompletionTokens
			if c := msg.Usage.Cached(); c > 0 {
				if sum.PromptTokensDetails == nil {
					sum.PromptTokensDetails = &struct {
						CachedTokens int `json:"cached_tokens"`
					}{}
				}
				sum.PromptTokensDetails.CachedTokens += c
			}
		}
	}
	return sum, last, ok
}

// rewindTurnMeta renders an entry's second line: the timestamp, then the
// turn's token flow and cost, then the context's size at turn end —
// "when · turn 1.3M in (1.1M cached) / 4.7k out · $x · context 67.8k".
// The turn figure sums every round (a tool-looping turn is several API
// calls); the context figure is the final round's prompt, i.e. how big the
// conversation had grown, with the growth since the previous entry in green
// ("context 75.6k (+7.5k)"). Cost is summed per message at that message's own
// model rates; hidden when nothing contributing is priced. row is the entry's
// index in the picker (growth diffs against the row above).
func (m *model) rewindTurnMeta(row int) string {
	e := m.rew.entries[row]
	meta := dimStyle.Render(rewindWhen(e.when))
	sum, last, ok := m.turnUsage(e.cut)
	if !ok {
		return meta
	}
	meta += dimStyle.Render(" · turn " + fmtTurn(sum))
	if cost, ok := m.turnCost(e.cut); ok {
		meta += dimStyle.Render(" · " + fmtCost(cost))
	}
	ctxSeg := " · context " + fmtTok(last.PromptTokens)
	if prev, ok := m.prevContextTokens(row); ok {
		if delta := last.PromptTokens - prev; delta > 0 {
			ctxSeg += growStyle.Render(fmt.Sprintf(" (+%s)", fmtTok(delta)))
		}
	}
	return meta + dimStyle.Render(ctxSeg)
}

// prevContextTokens finds the context size recorded by the picker row above
// this one (its last round's prompt), skipping rows whose turn recorded no
// usage. ok is false for the first usable row — there is nothing to diff.
func (m *model) prevContextTokens(row int) (int, bool) {
	for i := row - 1; i >= 0; i-- {
		if _, last, ok := m.turnUsage(m.rew.entries[i].cut); ok {
			return last.PromptTokens, true
		}
	}
	return 0, false
}

// fmtTurn renders a turn's token flow in words — "1.3M in (1.1M cached) /
// 4.7k out". The status line's bare in(cached)/out shape is fine where space
// is tight and the format is always visible; on a picker row the words earn
// their width.
func fmtTurn(u llm.Usage) string {
	in := fmtTok(u.PromptTokens) + " in"
	if c := u.Cached(); c > 0 {
		in += fmt.Sprintf(" (%s cached)", fmtTok(c))
	}
	return fmt.Sprintf("%s / %s out", in, fmtTok(u.CompletionTokens))
}

// turnCost prices the turn starting at cut by summing each assistant
// message's usage at its own recorded model's advertised rates.
func (m *model) turnCost(cut int) (float64, bool) {
	total := 0.0
	for i := cut + 1; i < len(m.agent.Messages)+len(m.future); i++ {
		msg := m.messageAt(i)
		if msg.Role == "user" {
			break
		}
		if msg.Role != "assistant" || msg.Usage == nil {
			continue
		}
		// msg.Model is "id @ provider"; pre-field messages (and providers
		// without a pricing catalog) contribute nothing.
		modelID, prov, _ := strings.Cut(msg.Model, " @ ")
		cat, ok := m.catalogs[prov]
		if !ok {
			continue
		}
		in, out, cacheRead, ok := cat.Pricing(modelID)
		if !ok {
			continue
		}
		total += llm.SessionCost(*msg.Usage, in, out, cacheRead)
	}
	return total, total > 0
}

// rewindWhen renders an entry's submission time for the picker. Pre-SentAt
// sessions have no per-message timestamp; show an em dash rather than a wrong
// or blank line.
func rewindWhen(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04") + " · " + ago(*t)
}

// discardFuture drops the redo stack: any new activity while rewound makes
// the clipped tail unreachable (branch-point semantics).
func (m *model) discardFuture() { m.future = nil }
