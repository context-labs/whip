package tui

import (
	"strconv"
	"strings"
)

// Compaction events are recorded in raw-log coordinates so Load never
// double-folds a summary. The agent reports its cutoff in compacted
// coordinates (indices into its current Messages); rawCutoff maps that to the
// raw row the kept tail begins at. The raw log normally omits the agent's
// system prompt, and each compacted history adds a derived summary after it.
func (m *model) rawCutoff(cutoff, rawTailStart int) int {
	if m.store == nil || m.sessionID == "" {
		return cutoff
	}
	events := m.store.Compactions(m.sessionID)
	if len(events) == 0 {
		raw := m.store.RawMessages(m.sessionID)
		if len(raw) > 0 && raw[0].Role != "system" {
			return cutoff - 1
		}
		return cutoff
	}
	if rawTailStart < 1 {
		rawTailStart = 2
	}
	return events[len(events)-1].Cutoff + cutoff - rawTailStart
}

// /compact retry — drop the latest compaction event and re-compact from the
// raw log. This is the whole point of recording compactions as events: a bad
// summary is inspectable (/compact log) and erasable without losing history.
func (m *model) compactRetry() {
	if m.store == nil || m.sessionID == "" {
		m.append(dimStyle.Render("(no session to retry a compaction in)"))
		return
	}
	events := m.store.Compactions(m.sessionID)
	if len(events) == 0 {
		m.append(dimStyle.Render("(no compaction to retry)"))
		return
	}
	last := events[len(events)-1]
	if err := m.store.DeleteCompaction(m.sessionID, last.Seq); err != nil {
		m.append(errStyle.Render("/compact retry: " + err.Error()))
		return
	}
	m.append(dimStyle.Render("⟲ compaction " + strconv.Itoa(last.Seq) + " undone — raw history restored; run /compact to re-compact"))
	// rebuild the in-memory conversation from the raw log so the next
	// compaction (or turn) starts from the unfolded history
	_, msgs, err := m.store.Load(m.sessionID)
	if err != nil {
		m.append(errStyle.Render("/compact retry: reload failed: " + err.Error()))
		return
	}
	if len(msgs) > 0 && msgs[0].Role == "system" {
		msgs = msgs[1:]
	}
	m.agent.Messages = append(m.agent.Messages[:1], msgs...)
	m.saved = 1 // re-save from scratch next persist
	m.rebuildTranscript()
}

// /compact log — the recorded compaction events (the inspection surface).
func (m *model) compactLog() {
	if m.store == nil || m.sessionID == "" {
		m.append(dimStyle.Render("(no session)"))
		return
	}
	events := m.store.Compactions(m.sessionID)
	if len(events) == 0 {
		m.append(dimStyle.Render("(no compactions recorded)"))
		return
	}
	var b strings.Builder
	b.WriteString(dimStyle.Render("compactions — raw history preserved; /compact retry undoes the latest:"))
	for _, c := range events {
		summary := strings.Join(strings.Fields(c.Summary), " ")
		if len(summary) > 80 {
			summary = summary[:80] + "…"
		}
		b.WriteString("\n  " + dimStyle.Render("#"+strconv.Itoa(c.Seq)+" folded through message "+strconv.Itoa(c.Cutoff)+": ") + summary)
	}
	m.append(b.String())
}
