package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
)

// /fork copies the conversation (whole, or up to a rewind-picker selection)
// into a NEW session with a chosen title and switches to it — "copy
// conversation with new name"; the original stays untouched and /resume-able
// (opencode's Session.fork, packages/opencode/src/session/session.ts:691).
// Mid-turn the copy lands immediately and the switch defers to turn end
// (busyFork/pendingForkID): the point of forking while the model works is
// that the copy is resume-able in another whip process right away.
// /rename retitles the current session. Both share one inline prompt: the
// input box is repurposed with a prefixed label, enter commits, esc cancels.

type namePrompt struct {
	label string // input prefix, e.g. "⑂ fork name:"
	draft string // input content stashed while the prompt owns the box
	mask  bool   // render the value as ••• (secret entry, e.g. /auth)
	onOK  func(string)
}

// openNamePrompt repurposes the input box as a one-shot text prompt. The
// in-progress draft is stashed and restored when the prompt closes.
func (m *model) openNamePrompt(label, value string, onOK func(string)) {
	m.namePrompt = &namePrompt{label: label, draft: m.input.Value(), onOK: onOK}
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.growInput()
}

// closeNamePrompt dismisses the prompt, restoring the stashed draft.
func (m *model) closeNamePrompt() {
	m.input.SetValue(m.namePrompt.draft)
	m.input.CursorEnd()
	m.namePrompt = nil
	m.growInput()
}

// maskedValue renders the input value for the prompt line: ••• when the
// prompt masks (auth keys never echo), the raw value otherwise.
func (p *namePrompt) maskedValue(v string) string {
	if !p.mask {
		return v
	}
	return strings.Repeat("•", len([]rune(v)))
}

// forkCommand implements /fork [name].
func (m *model) forkCommand(arg string) {
	if m.store == nil {
		m.append(errStyle.Render("no session store"))
		return
	}
	if arg != "" {
		m.fork(len(m.agent.Messages), arg)
		return
	}
	// bare: suggest "<title> (fork #N)" and let the user rename inline
	suggest := "session (fork #1)"
	if m.sessionID != "" {
		if meta, _, err := m.store.Load(m.sessionID); err == nil {
			if t, err := m.store.ForkTitle(meta.Title); err == nil {
				suggest = t
			}
		}
	}
	m.openForkPrompt(len(m.agent.Messages), false, suggest)
}

// openForkPrompt asks for a name, then forks at cut. picker notes when the
// prompt came from the rewind picker, for the confirmation line.
func (m *model) openForkPrompt(cut int, picker bool, suggest ...string) {
	name := ""
	if len(suggest) > 0 {
		name = suggest[0]
	}
	m.openNamePrompt("⑂ fork name:", name, func(title string) {
		m.fork(cut, title)
	})
	if picker {
		m.append(dimStyle.Render("⑂ forking from the selected message — name the copy (enter) or esc"))
	}
}

// fork copies the history through conversation index cut (inclusive) into a
// new session. Mid-turn it hands off to busyFork (the turn goroutine owns
// Agent.Messages and the session id, so only the copy happens now); idle it
// switches to the copy immediately.
func (m *model) fork(cut int, title string) {
	if m.busy {
		m.busyFork(title)
		return
	}
	title = strings.TrimSpace(title)
	if title == "" {
		m.append(errStyle.Render("fork needs a name"))
		return
	}
	if len(m.agent.Messages)+len(m.future) <= 1 {
		m.append(dimStyle.Render("(nothing to fork yet)"))
		return
	}
	// picker cuts may point into the redo stack (beyond the live messages):
	// the clipped tail up to the cut comes along. Rewind to just after the
	// entry first so persist() writes those rows before the copy.
	if len(m.future) > 0 {
		if cut+1 <= len(m.agent.Messages) {
			m.future = nil
		} else {
			m.applyRewind(cut + 1)
		}
	}
	m.persist() // every row must exist in the DB before the copy
	if m.sessionID == "" {
		return // persist failed; it already reported why
	}
	cut = min(max(cut, 0), len(m.agent.Messages)-1)
	oldID := m.sessionID
	oldTitle := oldID
	if meta, _, err := m.store.Load(oldID); err == nil && meta.Title != "" {
		oldTitle = meta.Title
	}
	newID, err := m.store.Fork(oldID, cut, title) // copies stored rows seq < cut
	if err != nil {
		m.append(errStyle.Render("fork failed: " + err.Error()))
		return
	}
	m.sessionID = newID
	m.agent.Tasks().SetSessionID(newID)
	m.agent.Messages = m.agent.Messages[:cut+1]
	m.future = nil
	m.saved = cut + 1
	m.rebuildTranscript()
	m.append(dimStyle.Render(fmt.Sprintf("⑂ forked %q → %q (%s) — the original is under /resume", oldTitle, title, newID)))
}

// busyFork copies the stored conversation into a new session NOW (the reason
// /fork runs mid-turn: the copy is immediately resumable in another whip
// process — whip --resume <id>) and marks it for the switch, which turnDoneMsg
// performs once the turn goroutine stops owning Agent.Messages, the session
// id, and the workspace snapshots. It touches none of those.
func (m *model) busyFork(title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		m.append(errStyle.Render("fork needs a name"))
		return
	}
	if m.sessionID == "" { // nothing persisted yet — nothing to copy
		m.append(dimStyle.Render("(nothing to fork yet — the first turn hasn't been saved; /fork again after this turn)"))
		return
	}
	if m.pendingForkID != "" {
		m.append(dimStyle.Render("(already forked — switching to the copy when this turn ends)"))
		return
	}
	oldTitle := m.sessionID
	if meta, _, err := m.store.Load(m.sessionID); err == nil && meta.Title != "" {
		oldTitle = meta.Title
	}
	// Full copy: the stored rows run through the last completed exchange
	// (mid-turn appends live only in the turn goroutine's Messages until the
	// turn persists), so no cut applies.
	newID, err := m.store.Fork(m.sessionID, 1<<30, title)
	if err != nil {
		m.append(errStyle.Render("fork failed: " + err.Error()))
		return
	}
	m.pendingForkID = newID
	m.append(dimStyle.Render(fmt.Sprintf("⑂ forked %q → %q (%s) — open it in another session now: whip --resume %s · whip switches to the copy when this turn ends", oldTitle, title, newID, newID)))
}

// switchToForked moves the live conversation onto the fork created mid-turn.
// The turn has ended (final persist and snapshot bookkeeping are done), so
// rebuilding the agent from the copy is safe; the original keeps the
// finished turn and survives untouched under /resume. Modeled on resume(),
// minus cross-session restores that don't cross a fork (task dock, todos,
// snapshots stay; the fork's workspace IS the current one).
func (m *model) switchToForked(id string) bool {
	if reason := m.replacementBlocked(); reason != "" {
		m.append(errStyle.Render("cannot switch to fork while " + reason))
		return false
	}
	meta, msgs, err := m.store.Load(id)
	if err != nil {
		m.append(errStyle.Render("fork switch failed: " + err.Error()))
		return false
	}
	m.flushThink()
	m.flushCurrent()
	m.queue, m.queueSel = nil, -1 // queued lines name the old conversation
	m.future = nil                // the copy's branch point is its tail; no redo across
	oldID := m.sessionID
	// Prefer the session's model/provider; fall back to the current agent.
	previousAgent := m.agent
	var nextAgent *agent.Agent
	var nextModel, nextProvider string
	if ag, mn, pn, err := buildAgent(m.cfg, meta.Model, meta.Provider, m.sysPrompt, m.agent.Services); err == nil {
		nextAgent, nextModel, nextProvider = ag, mn, pn
	} else {
		nextAgent = agent.NewWithServices(previousAgent.Client, previousAgent.Model, previousAgent.MaxTokens, m.sysPrompt, previousAgent.Services)
		nextAgent.ModelName, nextAgent.Provider = m.modelName, m.provName
		nextAgent.ContextLimit = m.contextLimitFor(m.provName, nextAgent.Model)
		nextModel, nextProvider = m.modelName, m.provName
	}
	nextAgent.WorkingDir = meta.CWD
	nextAgent.Services.SetProcessMarkers(meta.ID, nextAgent.Model)
	if err := m.bindSessionAuthority(nextAgent, meta.ID); err != nil {
		previousAgent.Services.SetProcessMarkers(oldID, previousAgent.Model)
		m.append(errStyle.Render("fork switch failed: " + err.Error()))
		return false
	}
	previousAgent.Close()
	m.agent, m.modelName, m.provName = nextAgent, nextModel, nextProvider
	m.sessionID = meta.ID
	m.bindToolServices(m.agent)
	m.applyCompactModel()
	m.applyTaskModel()
	m.agent.CompactThreshold = compactThresholdFor(m.cfg)
	m.wireTasks() // also publishes the new session id for task records
	// The fork's effort column carried over from the source; "" means the row
	// pre-dates per-session effort — keep the agent's current default then.
	if meta.Effort != "" && slices.Contains(m.effortsFor(), meta.Effort) {
		m.agent.Effort = meta.Effort
	}
	if meta.UsageIn > 0 || meta.UsageOut > 0 {
		u := llm.Usage{PromptTokens: meta.UsageIn, CompletionTokens: meta.UsageOut}
		if meta.UsageCached > 0 {
			u.PromptTokensDetails = &struct {
				CachedTokens int `json:"cached_tokens"`
			}{CachedTokens: meta.UsageCached}
		}
		m.agent.SetUsage(u)
	}
	m.agent.Messages = append(m.agent.Messages, msgs...)
	m.saved = len(m.agent.Messages)
	m.goal = meta.Goal
	m.goalRounds = 0
	m.titled = true // the fork was named at creation; no auto-title attempt
	m.rebuildTranscript()
	m.append(dimStyle.Render(fmt.Sprintf("⑂ switched to the fork %q (%s) — the original %s kept the finished turn and is under /resume", meta.Title, meta.ID, oldID)))
	return true
}

// renameCommand implements /rename [title].
func (m *model) renameCommand(arg string) {
	if m.store == nil {
		m.append(errStyle.Render("no session store"))
		return
	}
	if arg != "" {
		m.rename(arg)
		return
	}
	cur := ""
	if m.sessionID != "" {
		if meta, _, err := m.store.Load(m.sessionID); err == nil {
			cur = meta.Title
		}
	}
	m.openNamePrompt("✎ session name:", cur, m.rename)
}

func (m *model) rename(title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		m.append(errStyle.Render("rename needs a title"))
		return
	}
	m.persist() // a session row must exist before it can be titled
	if m.sessionID == "" {
		return
	}
	if err := m.store.SetTitle(m.sessionID, title); err != nil {
		m.append(errStyle.Render("rename failed: " + err.Error()))
		return
	}
	m.sessTitle = title
	m.append(dimStyle.Render("✎ session renamed: " + title))
}
