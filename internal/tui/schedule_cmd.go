package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/schedule"
)

// The wakeup channel: a 5s ticker checks the session's scheduled tasks and
// submits a machine-authored turn for each one that's due. A fired task is a
// normal turn — the transcript shows it with a ⏰ marker, the agent works it
// with its full tool set, and the answer lands like any other. While busy,
// the fire waits (one catch-up, grid stays anchored). One-shots complete on
// their first fire.

type scheduleTickMsg struct{}

// scheduleTick is the always-on heartbeat: it re-arms itself, so a session
// with no tasks costs one 5s no-op tick.
func scheduleTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return scheduleTickMsg{} })
}

// fireDueSchedules submits machine-authored turns for due tasks. Called on
// each tick; a busy agent defers the fire to the next one (grid stays
// anchored, so a defer doesn't drift the schedule).
func (m *model) fireDueSchedules() tea.Cmd {
	// An open trust gate (deferred to the TUI on non-TTY stdin) holds the
	// wakeup channel too: a fire is a full agent turn, and the folder is
	// untrusted until the user answers. Unstamped, so the tasks catch up on the
	// first tick after approval — or never, since a decline quits.
	if m.store == nil || m.sessionID == "" || m.busy || m.trustPending != "" {
		return nil
	}
	now := time.Now()
	for _, sc := range m.store.Schedules(m.sessionID) {
		s, err := schedule.Parse(sc.Schedule)
		if err != nil {
			continue // a row that doesn't parse just sits; /schedule can delete it
		}
		// Never-fired tasks are due once any grid slot has passed; fired ones
		// are due at the next slot after their last fire. Truncation to the
		// second absorbs sub-second skew between the ticker and the grid.
		var due bool
		var slot time.Time
		if sc.LastFire.IsZero() {
			slot = sc.Anchor
			due = !sc.Anchor.Truncate(time.Second).After(now)
			if s.Every > 0 { // first fire of an interval task is the first slot at/after the anchor
				if n, ok := s.NextAfter(sc.Anchor, sc.Anchor.Add(-time.Nanosecond)); ok {
					slot = n
				}
			}
		} else {
			slot, due = s.NextAfter(sc.Anchor, sc.LastFire)
			due = due && !slot.Truncate(time.Second).After(now)
		}
		if !due {
			continue
		}
		_ = m.store.MarkFired(m.sessionID, sc.ID, slot) // stamp the grid slot, not now
		prompt := fmt.Sprintf("⏰ Scheduled task #%d fired (%s). Work on it now:\n\n%s", sc.ID, sc.Schedule, sc.Prompt)
		m.append(dimStyle.Render(fmt.Sprintf("⏰ scheduled task #%d fired — %s", sc.ID, sc.Prompt)))
		_, cmd := m.submitTurn(prompt, false)
		return tea.Batch(cmd, m.spin.Tick) // one fire per tick; the rest catch up
	}
	return nil
}

// /schedule — create, list, cancel. The minimal surface: no editing, no
// pause/resume — delete and re-add.
func (m *model) scheduleCommand(args []string) {
	if len(args) == 0 || args[0] == "list" {
		m.scheduleList()
		return
	}
	switch args[0] {
	case "cancel", "delete", "rm":
		if len(args) < 2 {
			m.append(errStyle.Render("/schedule cancel <n>"))
			return
		}
		n, err := strconv.Atoi(args[1])
		if err != nil || m.store == nil || m.sessionID == "" {
			m.append(errStyle.Render("/schedule cancel: entry number expected"))
			return
		}
		if err := m.store.DeleteSchedule(m.sessionID, n); err != nil {
			m.append(errStyle.Render("/schedule cancel: " + err.Error()))
			return
		}
		m.append(dimStyle.Render(fmt.Sprintf("✓ scheduled task %d cancelled", n)))
	default: // /schedule @every 10m <prompt>
		if len(args) < 3 {
			m.append(errStyle.Render("/schedule @every 10m|<@at time> <prompt> — or: list, cancel <n>"))
			return
		}
		expr := strings.Join(args[:2], " ")
		s, err := schedule.Parse(expr)
		if err != nil {
			m.append(errStyle.Render("/schedule: " + err.Error()))
			return
		}
		if m.store == nil {
			m.append(errStyle.Render("/schedule: no session store"))
			return
		}
		m.persist() // a schedule needs a session row to hang off
		if m.sessionID == "" {
			m.append(errStyle.Render("/schedule: start a session first (send a message)"))
			return
		}
		prompt := strings.Join(args[2:], " ")
		anchor := time.Now()
		if !s.At.IsZero() {
			anchor = s.At
		}
		id, err := m.store.AddSchedule(m.sessionID, s.String(), prompt, anchor)
		if err != nil {
			m.append(errStyle.Render("/schedule: " + err.Error()))
			return
		}
		m.append(dimStyle.Render(fmt.Sprintf("✓ scheduled task %d: %s — %s", id, s.String(), prompt)))
	}
}

func (m *model) scheduleList() {
	if m.store == nil || m.sessionID == "" {
		m.append(dimStyle.Render("(no session — schedules are per-session)"))
		return
	}
	tasks := m.store.Schedules(m.sessionID)
	if len(tasks) == 0 {
		m.append(dimStyle.Render("(no scheduled tasks — /schedule @every 10m <prompt>)"))
		return
	}
	var b strings.Builder
	b.WriteString(dimStyle.Render("scheduled tasks — fire as ⏰ turns · /schedule cancel <n>:"))
	for _, sc := range tasks {
		line := fmt.Sprintf("\n  %d. %s — %s", sc.ID, sc.Schedule, sc.Prompt)
		if s, err := schedule.Parse(sc.Schedule); err == nil && !s.At.IsZero() && !sc.LastFire.IsZero() {
			line += dimStyle.Render(" (fired)")
		}
		b.WriteString(line)
	}
	m.append(b.String())
}
