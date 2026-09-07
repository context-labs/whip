package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

// Scheduled tasks persist in the session store and survive a reload — the
// durability half of the wakeup channel.
func TestSchedulePersistence(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	st, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	m := compactCmdModel()
	m.store = st

	// /schedule needs a session row; persist() creates one when a turn exists
	m.agent.Messages = append(m.agent.Messages, llm.Message{Role: "user", Content: "q", Authored: true})
	m.persist()
	if m.sessionID == "" {
		t.Fatal("persist should create the session")
	}

	m.scheduleCommand([]string{"@every", "10m", "check the deploy status"})
	m.scheduleCommand([]string{"@at", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "one-shot reminder"})

	tasks := st.Schedules(m.sessionID)
	if len(tasks) != 2 {
		t.Fatalf("two tasks stored, got %d", len(tasks))
	}
	if tasks[0].Schedule != "@every 10m0s" || tasks[0].Prompt != "check the deploy status" {
		t.Fatalf("task 1: %+v", tasks[0])
	}
	if !strings.HasPrefix(tasks[1].Schedule, "@at ") {
		t.Fatalf("task 2: %+v", tasks[1])
	}

	// cancel removes it
	m.scheduleCommand([]string{"cancel", "1"})
	if tasks := st.Schedules(m.sessionID); len(tasks) != 1 {
		t.Fatalf("after cancel: %d tasks", len(tasks))
	}
}

// An open deferred trust gate holds due schedules: a fire is a full agent
// turn in an untrusted folder. The task stays unstamped, so it fires on the
// first tick after the gate closes.
func TestScheduleHeldWhileTrustPending(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	m := compactCmdModel()
	st, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m.store = st
	m.sessionID, _ = st.Create("/tmp", m.modelName, m.provName)
	m.agent.SetSessionID(m.sessionID)

	if _, err := st.AddSchedule(m.sessionID, "@every 10m", "say hi", time.Now().Add(-2*time.Minute).Truncate(time.Second)); err != nil {
		t.Fatal(err)
	}

	m.trustPending = "/untrusted/dir"
	if cmd := m.fireDueSchedules(); cmd != nil {
		t.Fatal("an open trust gate must hold due schedules — no turn in an untrusted folder")
	}
	if got := st.Schedules(m.sessionID); !got[0].LastFire.IsZero() {
		t.Fatal("a held schedule must not be stamped fired — it catches up after approval")
	}

	m.trustPending = "" // approved: the gate is closed
	if cmd := m.fireDueSchedules(); cmd == nil {
		t.Fatal("after the gate closes the held schedule should fire")
	}
}

// A due task fires a machine-authored turn into the agent loop (the wakeup),
// and a fired one-shot never fires again.
func TestScheduleFiresWakeup(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	m := compactCmdModel() // agent client points at the canned test server
	st, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m.store = st
	m.sessionID, _ = st.Create("/tmp", m.modelName, m.provName)
	m.agent.SetSessionID(m.sessionID)

	// an already-due interval task: anchored in the past
	if _, err := st.AddSchedule(m.sessionID, "@every 10m", "say hi", time.Now().Add(-2*time.Minute).Truncate(time.Second)); err != nil {
		t.Fatal(err)
	}

	if cmd := m.fireDueSchedules(); cmd == nil {
		t.Fatal("a due task should fire")
	}
	// the turn was submitted: the agent is busy, the wakeup prompt lands in
	// the conversation (the turn runs on a goroutine — wait for it), and the
	// transcript carries the ⏰ marker
	if !m.busy {
		t.Fatal("the fired task should have started a turn")
	}
	var wakeup llm.Message
	for range 200 { // up to 2s for the turn goroutine to append
		for _, v := range slices.Backward(m.agent.MessagesSnapshot()) {
			if v.Role == "user" {
				wakeup = v
				break
			}
		}
		if wakeup.Role != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(wakeup.Content, "Scheduled task") {
		t.Fatalf("wakeup should be a machine-authored user turn, got %+v", wakeup)
	}
	var sawMarker bool
	for _, b := range m.blocks {
		if strings.Contains(ansi.Strip(b.render(m.width)), "⏰ scheduled task") {
			sawMarker = true
		}
	}
	if !sawMarker {
		t.Fatal("transcript should show the ⏰ fire marker")
	}

	// the fire was stamped on the grid; not due again immediately
	tasks := st.Schedules(m.sessionID)
	if tasks[0].LastFire.IsZero() {
		t.Fatal("fire should stamp last_fire")
	}
	m.busy = false
	if cmd := m.fireDueSchedules(); cmd != nil {
		t.Fatal("a just-fired task is not due again")
	}
}

// A fired one-shot completes: it stays listed (audit trail) but never fires
// again.
func TestScheduleOneShotCompletes(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	m := compactCmdModel()
	st, _ := session.Open(filepath.Join(t.TempDir(), "s.db"))
	defer st.Close()
	m.store = st
	m.sessionID, _ = st.Create("/tmp", m.modelName, m.provName)

	past := time.Now().Add(-time.Minute).UTC()
	if _, err := st.AddSchedule(m.sessionID, "@at "+past.Format(time.RFC3339), "remind", past); err != nil {
		t.Fatal(err)
	}
	if cmd := m.fireDueSchedules(); cmd == nil {
		t.Fatal("a past @at should fire once")
	}
	m.busy = false
	if cmd := m.fireDueSchedules(); cmd != nil {
		t.Fatal("a fired one-shot is done")
	}
	// but it stays listed as (fired)
	m.blocks = nil
	m.scheduleCommand(nil)
	var listed bool
	for _, b := range m.blocks {
		if strings.Contains(ansi.Strip(b.render(m.width)), "(fired)") {
			listed = true
		}
	}
	if !listed {
		t.Fatal("a fired one-shot should stay listed as (fired)")
	}
}

// A busy agent defers the fire — the grid slot is stamped when it lands, so
// a slow turn doesn't drift the schedule.
func TestScheduleDefersWhileBusy(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	m := compactCmdModel()
	st, _ := session.Open(filepath.Join(t.TempDir(), "s.db"))
	defer st.Close()
	m.store = st
	m.sessionID, _ = st.Create("/tmp", m.modelName, m.provName)
	st.AddSchedule(m.sessionID, "@every 10m", "ping", time.Now().Add(-2*time.Minute).Truncate(time.Second))

	m.busy = true
	if cmd := m.fireDueSchedules(); cmd != nil {
		t.Fatal("a busy agent defers the fire")
	}
	if tasks := st.Schedules(m.sessionID); !tasks[0].LastFire.IsZero() {
		t.Fatal("a deferred fire is not stamped")
	}
}
