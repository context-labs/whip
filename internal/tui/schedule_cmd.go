package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/schedule"
)

// fireDueSchedules submits machine-authored turns for due tasks. Called on
// each tick; a busy agent defers the fire to the next one (grid stays
// anchored, so a defer doesn't drift the schedule).
func (m *model) fireDueSchedules() tea.Cmd {
	if m.store == nil || m.sessionID == "" || m.busy {
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
