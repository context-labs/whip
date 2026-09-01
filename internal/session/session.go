// Package session persists chat histories in ~/.whip/sessions.db (SQLite).
package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/context-labs/whip/internal/llm"
)

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	cwd        TEXT NOT NULL,
	model      TEXT NOT NULL,
	provider   TEXT NOT NULL,
	title      TEXT NOT NULL DEFAULT '',
	goal       TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS messages (
	session_id TEXT NOT NULL REFERENCES sessions(id),
	seq        INTEGER NOT NULL,
	role       TEXT NOT NULL,
	content    TEXT NOT NULL, -- llm.Message JSON
	PRIMARY KEY (session_id, seq)
);
CREATE TABLE IF NOT EXISTS tasks (
	session_id  TEXT NOT NULL REFERENCES sessions(id),
	task_id     TEXT NOT NULL,
	description TEXT NOT NULL,
	prompt      TEXT NOT NULL,
	status      TEXT NOT NULL,
	report      TEXT NOT NULL DEFAULT '',
	started_at  TEXT NOT NULL,
	ended_at    TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (session_id, task_id)
);
-- Workspace snapshots: one git stash ref per turn (keyed by the conversation
-- index the turn started at), so a conversation rewind can also restore the
-- files that turn changed. Same seq semantics as messages, so DeleteFrom
-- trims both together.
CREATE TABLE IF NOT EXISTS snapshots (
	session_id TEXT NOT NULL REFERENCES sessions(id),
	seq        INTEGER NOT NULL,
	ref        TEXT NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY (session_id, seq)
);
-- Scheduled tasks: the wakeup channel's durable records. One row per task,
-- keyed by the session it fires into. The store keeps the schedule
-- expression, anchor, and last fire; the TUI's ticker evaluates due tasks.
CREATE TABLE IF NOT EXISTS schedules (
	session_id TEXT NOT NULL REFERENCES sessions(id),
	id         INTEGER NOT NULL,
	schedule   TEXT NOT NULL,      -- '@every 10m' | '@at <rfc3339>'
	prompt     TEXT NOT NULL,      -- the machine-authored turn to submit on fire
	anchor     TEXT NOT NULL,      -- grid origin (RFC3339)
	last_fire  TEXT NOT NULL DEFAULT '', -- last fire time ("" = never); one-shots complete here
	created_at TEXT NOT NULL,
	PRIMARY KEY (session_id, id)
);
-- Compaction events: append-only. Each row records a compaction as summary +
-- cutoff (the raw-log seq it folded). The messages table is never rewritten
-- by a compaction — Load derives the compacted view from the latest event,
-- so a bad compaction is inspectable and retryable.
CREATE TABLE IF NOT EXISTS compactions (
	session_id TEXT NOT NULL REFERENCES sessions(id),
	seq        INTEGER NOT NULL, -- compaction generation, 1-based
	cutoff     INTEGER NOT NULL, -- raw-log seq the summary replaces (1..cutoff-1)
	summary    TEXT NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY (session_id, seq)
);`

// extraColumns are added idempotently after the base schema: SQLite's
// ADD COLUMN errors if the column already exists, so each is guarded by an
// information check in migrate(). New per-session bookkeeping lands here, not
// in the CREATE above (which only runs on a fresh DB).
var extraColumns = []struct{ name, def string }{
	{"forked_from", "forked_from TEXT NOT NULL DEFAULT ''"},     // source session id
	{"fork_seq", "fork_seq INTEGER NOT NULL DEFAULT 0"},         // branch point in the source
	{"tags", "tags TEXT NOT NULL DEFAULT ''"},                   // comma-separated labels
	{"pinned", "pinned INTEGER NOT NULL DEFAULT 0"},             // 1 = keep / sort first
	{"effort", "effort TEXT NOT NULL DEFAULT ''"},               // reasoning effort in effect ("" = global default)
	{"usage_in", "usage_in INTEGER NOT NULL DEFAULT 0"},         // cumulative input tokens (provider-reported)
	{"usage_cached", "usage_cached INTEGER NOT NULL DEFAULT 0"}, // of usage_in, tokens served from the prompt cache
	{"usage_out", "usage_out INTEGER NOT NULL DEFAULT 0"},       // cumulative output tokens
	{"todos", "todos TEXT NOT NULL DEFAULT ''"},                 // todowrite plan JSON ([]agent.Todo)
	{"task_id", "task_id TEXT NOT NULL DEFAULT ''"},             // non-empty = this session is a subagent's transcript; the value is the task id
}

// Meta is a session's bookkeeping row.
type Meta struct {
	ID          string
	Title       string
	Model       string
	Provider    string
	CWD         string
	Goal        string
	ForkedFrom  string   // source session id when created by /fork ("" = root)
	ForkSeq     int      // conversation index the fork branched at
	Tags        []string // freeform labels, for filtering /resume
	Pinned      bool     // pinned sessions sort first and survive cleanup
	Effort      string   // reasoning effort for this session ("" = use the global default)
	UsageIn     int      // cumulative input tokens across the session's API calls
	UsageCached int      // of UsageIn, tokens served from the provider's prompt cache
	UsageOut    int      // cumulative output tokens
	UpdatedAt   time.Time
	// TaskID is non-empty when this session is a subagent's persisted
	// transcript (the value is the task id, e.g. "survey-context-3"); the
	// parent session id is ForkedFrom. Empty for ordinary sessions.
	TaskID string
}

type Store struct{ db *sql.DB }

// Open opens (creating if needed) the sessions database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	for _, pragma := range []string{
		"PRAGMA busy_timeout=5000",
		"PRAGMA journal_mode=WAL",   // faster commits, no read/write blocking
		"PRAGMA synchronous=NORMAL", // safe in WAL; skips per-commit fsync
		"PRAGMA temp_store=MEMORY",
	} {
		if _, err := db.ExecContext(context.Background(), pragma); err != nil {
			return nil, err
		}
	}
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		return nil, err
	}
	// migrate pre-goal databases; duplicate-column errors are expected
	_, _ = db.ExecContext(context.Background(), `ALTER TABLE sessions ADD COLUMN goal TEXT NOT NULL DEFAULT ''`)
	// later per-session bookkeeping (fork linkage, tags, pinned); the same
	// duplicate-column-tolerant migration as goal
	for _, c := range extraColumns {
		_, _ = db.ExecContext(context.Background(), `ALTER TABLE sessions ADD COLUMN `+c.def)
	}
	return &Store{db: db}, nil
}

// SetGoal stores the session's active goal ("" clears it).
func (s *Store) SetGoal(id, goal string) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE sessions SET goal=? WHERE id=?`, goal, id)
	return err
}

// SetTodos stores the session's todowrite plan as JSON ("" clears it). The
// plan is a whole-list snapshot: the model rewrites it in full each call, so
// this is a plain overwrite, not a merge.
func (s *Store) SetTodos(id, todosJSON string) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE sessions SET todos=? WHERE id=?`, todosJSON, id)
	return err
}

// Todos returns the session's stored todowrite plan JSON ("" when unset or
// the session is unknown). The agent package owns the schema.
func (s *Store) Todos(id string) string {
	var v string
	_ = s.db.QueryRowContext(context.Background(), `SELECT todos FROM sessions WHERE id=?`, id).Scan(&v)
	return v
}

// SetEffort stores the session's reasoning effort. "" means the row pre-dates
// per-session effort or never set one: resume falls back to the current global
// default and stamps it on the next save.
func (s *Store) SetEffort(id, effort string) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE sessions SET effort=? WHERE id=?`, effort, id)
	return err
}

// SetUsage stores the session's cumulative token totals (absolute values, not
// deltas) so a resumed session keeps its spend across restarts and
// compactions. Rows from before this column existed read as zero and get
// stamped with real totals on the next save.
func (s *Store) SetUsage(id string, in, cached, out int) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE sessions SET usage_in=?, usage_cached=?, usage_out=? WHERE id=?`, in, cached, out, id)
	return err
}

// Task is one background subagent's persisted record. It deliberately
// mirrors agent.BackgroundTask's exported fields without importing agent
// (session is a leaf; the TUI converts between them).
type Task struct {
	ID          string
	Description string
	Prompt      string
	Status      string // "running", "done", "error", "cancelled"
	Report      string
	StartedAt   time.Time
	EndedAt     time.Time
}

// SaveTask upserts a background subagent's record for a session. Called on
// start and on settle, so the final row holds the settled status/report.
func (s *Store) SaveTask(sessionID string, t Task) error {
	ended := ""
	if !t.EndedAt.IsZero() {
		ended = t.EndedAt.UTC().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(context.Background(), `INSERT OR REPLACE INTO tasks
		(session_id, task_id, description, prompt, status, report, started_at, ended_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		sessionID, t.ID, t.Description, t.Prompt, t.Status, t.Report,
		t.StartedAt.UTC().Format(time.RFC3339), ended)
	return err
}

// LoadTasks returns a session's persisted background subagents, oldest first.
func (s *Store) LoadTasks(sessionID string) ([]Task, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT task_id, description, prompt, status, report, started_at, ended_at
		FROM tasks WHERE session_id=? ORDER BY started_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		var t Task
		var started, ended string
		if err := rows.Scan(&t.ID, &t.Description, &t.Prompt, &t.Status, &t.Report, &started, &ended); err != nil {
			return nil, err
		}
		t.StartedAt, _ = time.Parse(time.RFC3339, started)
		if ended != "" {
			t.EndedAt, _ = time.Parse(time.RFC3339, ended)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// SaveSubagentTranscript persists a background subagent's conversation as its
// own attributed session row: id "task-<parentID>-<taskID>" (prefixed so it
// never prefix-collides with the parent session's id in Load), forked_from
// set to the parent session, task_id set to the task. Idempotent — re-saving
// after a follow-up turn replaces the same row and rewrites the messages from
// seq 0. Returns the session id, or "" when there's no parent to attribute to
// (a headless run with no store never calls this).
func (s *Store) SaveSubagentTranscript(parentID, taskID string, msgs []llm.Message, model, provider string) (string, error) {
	if parentID == "" || taskID == "" {
		return "", nil
	}
	id := subagentSessionID(parentID, taskID)
	if _, err := s.db.ExecContext(context.Background(), `INSERT INTO sessions
		(id, created_at, updated_at, cwd, model, provider, title, forked_from, task_id)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET updated_at=excluded.updated_at, model=excluded.model, provider=excluded.provider`,
		id, now(), now(), "", model, provider, "subagent "+taskID, parentID, taskID); err != nil {
		return "", err
	}
	if err := s.Save(id, 0, msgs, model, provider); err != nil {
		return "", err
	}
	// Re-save rewrites rows [0, len(msgs)); a follow-up turn that compacted
	// the transcript writes FEWER rows than last time, and Save's per-seq
	// INSERT OR REPLACE never touches the tail — without this sweep the stale
	// rows at seq >= len(msgs) linger and reload as phantom trailing messages.
	if _, err := s.db.ExecContext(context.Background(), `DELETE FROM messages WHERE session_id=? AND seq>=?`, id, len(msgs)); err != nil {
		return "", err
	}
	return id, nil
}

// SubagentTranscript loads a subagent's persisted conversation by parent +
// task id ("" when never persisted). Loads by exact id (loadMessages), not
// Load's prefix scan: sibling task ids can share a stem, which would make the
// prefix form ambiguous.
func (s *Store) SubagentTranscript(parentID, taskID string) ([]llm.Message, error) {
	if parentID == "" || taskID == "" {
		return nil, nil
	}
	return s.loadMessages(subagentSessionID(parentID, taskID))
}

// subagentSessionID builds the attributed session id for a subagent's
// transcript. The "task-" prefix guarantees it can't prefix-collide with the
// parent session's hex id in Load's prefix match (a plain "<parent>/…" suffix
// form would make resume(parentID) ambiguous).
func subagentSessionID(parentID, taskID string) string {
	return "task-" + parentID + "-" + taskID
}

// Create inserts a new session and returns its id.
func (s *Store) Create(cwd, model, provider string) (string, error) {
	b := make([]byte, 4)
	rand.Read(b)
	id := hex.EncodeToString(b)
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO sessions (id, created_at, updated_at, cwd, model, provider) VALUES (?,?,?,?,?,?)`,
		id, now(), now(), cwd, model, provider)
	return id, err
}

// Save persists msgs[from:] (the conversation without the system prompt) and
// refreshes the session's metadata.
func (s *Store) Save(id string, from int, msgs []llm.Message, model, provider string) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i := from; i < len(msgs); i++ {
		// Placeholder rows (zero-value messages the caller never meant to
		// write, e.g. padding before a post-compaction tail) must not
		// clobber the raw log — skip them.
		if msgs[i].Role == "" {
			continue
		}
		data, err := json.Marshal(msgs[i])
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(context.Background(), `INSERT OR REPLACE INTO messages (session_id, seq, role, content) VALUES (?,?,?,?)`,
			id, i, msgs[i].Role, string(data)); err != nil {
			return err
		}
	}
	title := ""
	for _, m := range msgs {
		if m.Role == "user" {
			title = truncate(strings.Join(strings.Fields(m.TextContent()), " "), 64)
			break
		}
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE sessions SET updated_at=?, model=?, provider=?, title=CASE WHEN title='' THEN ? ELSE title END WHERE id=?`,
		now(), model, provider, title, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Load resolves idOrPrefix to a session and returns its metadata and messages.
func (s *Store) Load(idOrPrefix string) (Meta, []llm.Message, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, title, model, provider, cwd, goal, forked_from, fork_seq, tags, pinned, effort, usage_in, usage_cached, usage_out, task_id, updated_at FROM sessions WHERE id LIKE ?||'%' LIMIT 3`, idOrPrefix)
	if err != nil {
		return Meta{}, nil, err
	}
	metas, err := scanMetas(rows)
	if err != nil {
		return Meta{}, nil, err
	}
	switch len(metas) {
	case 0:
		return Meta{}, nil, fmt.Errorf("no session matching %q", idOrPrefix)
	case 1:
	default:
		return Meta{}, nil, fmt.Errorf("session id %q is ambiguous", idOrPrefix)
	}
	meta := metas[0]
	msgs, err := s.loadMessages(meta.ID)
	if err != nil {
		return Meta{}, nil, err
	}
	return meta, msgs, nil
}

// loadMessages reads one session's full message log by EXACT id — the read
// half of Load once the meta row is known. Split out so subagent transcripts
// (whose ids are built deterministic by subagentSessionID, never typed by the
// user) load by exact match instead of Load's prefix scan: a prefix query
// over transcript ids goes "ambiguous" the moment two sibling task ids share
// a stem (task-<parent>-foo-1 vs task-<parent>-foo-12).
func (s *Store) loadMessages(id string) ([]llm.Message, error) {
	// pre-size the slice: a long session is hundreds of rows; the COUNT is
	// one index scan and avoids O(log n) reallocs while scanning
	var count int
	_ = s.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM messages WHERE session_id=?`, id).Scan(&count)

	mrows, err := s.db.QueryContext(context.Background(), `SELECT content FROM messages WHERE session_id=? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mrows.Close() }()
	msgs := make([]llm.Message, 0, count)
	for mrows.Next() {
		var data string
		if err := mrows.Scan(&data); err != nil {
			return nil, err
		}
		var m llm.Message
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return answerDanglingToolCalls(applyCompaction(s.db, id, msgs)), mrows.Err()
}

// applyCompaction derives the compacted view from the raw log: the latest
// compaction event's summary replaces raw messages [1, cutoff), keeping the
// system prompt (seq 0) and the raw tail. "Raw" matters: a stored row that is
// itself a summary (system role past index 0) is a *derived* row saved after
// a compaction — folding it again would nest summaries — so the cutoff only
// ever applies to non-system rows. No event → the log loads verbatim. This is
// what makes a compaction non-destructive: the event is metadata, the raw
// rows are the history.
func applyCompaction(db *sql.DB, sessionID string, msgs []llm.Message) []llm.Message {
	var cutoff int
	var summary string
	err := db.QueryRowContext(context.Background(), `SELECT cutoff, summary FROM compactions WHERE session_id=? ORDER BY seq DESC LIMIT 1`,
		sessionID).Scan(&cutoff, &summary)
	if err != nil || cutoff <= 1 || cutoff > len(msgs) {
		return msgs // no event, or one that post-dates the raw log
	}
	// the fold point is the first raw (non-system) row at or past cutoff
	fold := len(msgs)
	for i := cutoff; i < len(msgs); i++ {
		if msgs[i].Role != "system" {
			fold = i
			break
		}
	}
	out := make([]llm.Message, 0, len(msgs))
	out = append(out, msgs[0],
		llm.Message{Role: "system", Content: "Summary of the conversation so far:\n\n" + summary})
	// keep the last derived summary before the fold (a second compaction's
	// saved row — it summarizes history the new summary doesn't reach)
	var prior []llm.Message
	for i := 1; i < fold; i++ {
		if msgs[i].Role == "system" {
			prior = append(prior, msgs[i])
		}
	}
	if len(prior) > 0 {
		out = append(out, prior[len(prior)-1])
	}
	return append(out, msgs[fold:]...)
}

// answerDanglingToolCalls appends a synthetic error result for every
// persisted tool call that has none — a ctrl+c or crash mid-turn interrupts
// between the assistant message and its results, and the API rejects a
// resumed conversation with an unanswered tool_call. Results go right after
// the assistant message (the API wants them before the next non-tool
// message); a fully-answered history is returned unchanged.
func answerDanglingToolCalls(msgs []llm.Message) []llm.Message {
	answered := make(map[string]bool, len(msgs))
	dangling := false
	for _, m := range msgs {
		if m.Role == "tool" {
			answered[m.ToolCallID] = true
		}
	}
	for _, m := range msgs {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				dangling = dangling || !answered[tc.ID]
			}
		}
	}
	if !dangling {
		return msgs
	}
	out := make([]llm.Message, 0, len(msgs)+4)
	for _, m := range msgs {
		out = append(out, m)
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if !answered[tc.ID] {
				out = append(out, llm.Message{
					Role:       "tool",
					Content:    "Error: tool call interrupted — the session ended before a result was recorded",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
				})
			}
		}
	}
	return out
}

// Recent returns up to n sessions, newest first.
func (s *Store) Recent(n int) ([]Meta, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, title, model, provider, cwd, goal, forked_from, fork_seq, tags, pinned, effort, usage_in, usage_cached, usage_out, task_id, updated_at FROM sessions
		WHERE EXISTS (SELECT 1 FROM messages WHERE session_id = sessions.id)
		ORDER BY updated_at DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	return scanMetas(rows)
}

// RecentForCWD returns up to n sessions from cwd, newest first. Startup
// continue/browse intentionally stay within the current project; Recent is
// still available for the cross-project /resume browser and `whip sessions`.
func (s *Store) RecentForCWD(cwd string, n int) ([]Meta, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, title, model, provider, cwd, goal, forked_from, fork_seq, tags, pinned, effort, usage_in, usage_cached, usage_out, task_id, updated_at FROM sessions
		WHERE cwd=? AND EXISTS (SELECT 1 FROM messages WHERE session_id = sessions.id)
		ORDER BY updated_at DESC LIMIT ?`, cwd, n)
	if err != nil {
		return nil, err
	}
	return scanMetas(rows)
}

// UserHistory returns user-message contents across ALL sessions (every folder),
// newest first and de-duplicated, for up-arrow input recall. Order is by the
// session's last activity then the message's position within it, so the most
// recently typed input comes first. Only messages the human actually typed are
// recalled: steered background-task
// results and goal-continuation prompts are stored as role "user" too, but
// they're injected by whip, not written by the user. Those carry Authored=false
// and are skipped; only Authored=true messages come back.
func (s *Store) UserHistory(limit int) ([]string, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT m.content FROM messages m
		JOIN sessions s ON s.id = m.session_id
		WHERE m.role='user'
		ORDER BY s.updated_at DESC, m.seq DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var msg llm.Message
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			continue // skip malformed rows rather than fail the whole recall
		}
		if !msg.Authored {
			continue // injected by whip (steered task result / goal prompt), not typed
		}
		content := strings.TrimSpace(msg.TextContent())
		if content == "" || seen[content] {
			continue
		}
		seen[content] = true
		out = append(out, content)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

// LastExchange returns the text of the session's last user message and last
// assistant response, for previews.
func (s *Store) LastExchange(id string) (user, assistant string) {
	for _, q := range []struct {
		role string
		dst  *string
	}{{"user", &user}, {"assistant", &assistant}} {
		var data string
		if err := s.db.QueryRowContext(context.Background(), `SELECT content FROM messages WHERE session_id=? AND role=? ORDER BY seq DESC LIMIT 1`,
			id, q.role).Scan(&data); err == nil {
			var m llm.Message
			if json.Unmarshal([]byte(data), &m) == nil {
				*q.dst = m.TextContent()
			}
		}
	}
	return user, assistant
}

// ClearMessages deletes the stored message rows for a session (the session
// row is kept). Used after compaction rewrites history: the compacted
// messages are smaller and re-seqenced from 0, so the old rows must go first.
func (s *Store) ClearMessages(id string) error {
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM messages WHERE session_id=?`, id)
	return err
}

// DeleteFrom drops every stored message with seq >= from, plus the workspace
// snapshots for those turns (their refs stop being restorable once the
// conversation no longer contains the turn). seq equals the conversation
// index (Save persists msgs[i] at seq i; the system prompt is never
// persisted). Used by rewind: the clipped tail is deleted from disk but kept
// in memory for forward travel.
func (s *Store) DeleteFrom(id string, from int) error {
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM messages WHERE session_id=? AND seq>=?`, id, from)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(context.Background(), `DELETE FROM snapshots WHERE session_id=? AND seq>=?`, id, from)
	return err
}

// SetSnapshot records the workspace snapshot ref for the turn starting at
// conversation index seq ("" deletes: the turn's files were restored away).
func (s *Store) SetSnapshot(id string, seq int, ref string) error {
	if ref == "" {
		_, err := s.db.ExecContext(context.Background(), `DELETE FROM snapshots WHERE session_id=? AND seq=?`, id, seq)
		return err
	}
	_, err := s.db.ExecContext(context.Background(), `INSERT OR REPLACE INTO snapshots (session_id, seq, ref, created_at) VALUES (?,?,?,?)`,
		id, seq, ref, now())
	return err
}

// Snapshots returns the session's workspace snapshot refs keyed by
// conversation index.
func (s *Store) Snapshots(id string) map[int]string {
	rows, err := s.db.QueryContext(context.Background(), `SELECT seq, ref FROM snapshots WHERE session_id=?`, id)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	out := map[int]string{}
	for rows.Next() {
		var seq int
		var ref string
		if rows.Scan(&seq, &ref) == nil {
			out[seq] = ref
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

// Schedule is one scheduled task's durable record.
type Schedule struct {
	ID       int
	Schedule string    // '@every 10m' | '@at <rfc3339>'
	Prompt   string    // the machine-authored turn submitted on fire
	Anchor   time.Time // grid origin
	LastFire time.Time // zero = never fired
}

// AddSchedule records a scheduled task and returns its id.
func (s *Store) AddSchedule(sessionID, schedule, prompt string, anchor time.Time) (int, error) {
	var id int
	err := s.db.QueryRowContext(context.Background(), `INSERT INTO schedules (session_id, id, schedule, prompt, anchor, created_at)
		SELECT ?, COALESCE(MAX(id),0)+1, ?, ?, ?, ? FROM schedules WHERE session_id=? RETURNING id`,
		sessionID, schedule, prompt, anchor.UTC().Format(time.RFC3339), now(), sessionID).Scan(&id)
	return id, err
}

// Schedules returns a session's scheduled tasks, id order.
func (s *Store) Schedules(sessionID string) []Schedule {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, schedule, prompt, anchor, last_fire FROM schedules WHERE session_id=? ORDER BY id`, sessionID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var out []Schedule
	for rows.Next() {
		var sc Schedule
		var anchor, lastFire string
		if rows.Scan(&sc.ID, &sc.Schedule, &sc.Prompt, &anchor, &lastFire) != nil {
			continue
		}
		sc.Anchor, _ = time.Parse(time.RFC3339, anchor)
		sc.LastFire, _ = time.Parse(time.RFC3339, lastFire)
		out = append(out, sc)
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

// MarkFired stamps a task's last fire (a fired one-shot stays listed but
// never fires again).
func (s *Store) MarkFired(sessionID string, id int, at time.Time) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE schedules SET last_fire=? WHERE session_id=? AND id=?`,
		at.UTC().Format(time.RFC3339), sessionID, id)
	return err
}

// DeleteSchedule removes a scheduled task.
func (s *Store) DeleteSchedule(sessionID string, id int) error {
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM schedules WHERE session_id=? AND id=?`, sessionID, id)
	return err
}

// ClearSnapshots drops all of a session's workspace snapshot rows (compaction
// re-seqs messages, so the keys stop mapping to turns).
func (s *Store) ClearSnapshots(id string) error {
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM snapshots WHERE session_id=?`, id)
	return err
}

// Compaction is one recorded compaction event.
type Compaction struct {
	Seq     int    // generation (1-based)
	Cutoff  int    // raw-log seq the summary replaces
	Summary string // the generated summary text
}

// RecordCompaction appends a compaction event. The raw messages stay.
func (s *Store) RecordCompaction(id string, cutoff int, summary string) error {
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO compactions (session_id, seq, cutoff, summary, created_at)
		SELECT ?, COALESCE(MAX(seq),0)+1, ?, ?, ? FROM compactions WHERE session_id=?`,
		id, cutoff, summary, now(), id)
	return err
}

// Compactions returns a session's compaction events, oldest first.
func (s *Store) Compactions(id string) []Compaction {
	rows, err := s.db.QueryContext(context.Background(), `SELECT seq, cutoff, summary FROM compactions WHERE session_id=? ORDER BY seq`, id)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var out []Compaction
	for rows.Next() {
		var c Compaction
		if rows.Scan(&c.Seq, &c.Cutoff, &c.Summary) == nil {
			out = append(out, c)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

// DeleteCompaction removes one compaction event by generation (retry drops
// the bad event before re-compacting from the raw log).
func (s *Store) DeleteCompaction(id string, seq int) error {
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM compactions WHERE session_id=? AND seq=?`, id, seq)
	return err
}

// RawMessages returns the full stored log (no compaction view applied) —
// the inspection/retry surface for compactions.
func (s *Store) RawMessages(id string) []llm.Message {
	rows, err := s.db.QueryContext(context.Background(), `SELECT content FROM messages WHERE session_id=? ORDER BY seq`, id)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var msgs []llm.Message
	for rows.Next() {
		var data string
		if rows.Scan(&data) != nil {
			continue
		}
		var m llm.Message
		if json.Unmarshal([]byte(data), &m) == nil {
			msgs = append(msgs, m)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return msgs
}

// SetTitle retitles a session (/rename).
func (s *Store) SetTitle(id, title string) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE sessions SET title=? WHERE id=?`, title, id)
	return err
}

// Fork copies a session's stored rows with seq <= uptoSeq (pass len(msgs)
// for a full copy — one past the last row) into a new session titled title,
// carrying over cwd/model/provider/goal, and returns the new id. seq equals
// the conversation index (the system prompt is never persisted). The source
// session is untouched. The rows are cloned in one INSERT…SELECT, so the DB
// does the copy; nothing round-trips through Go.
func (s *Store) Fork(srcID string, uptoSeq int, title string) (string, error) {
	b := make([]byte, 4)
	rand.Read(b)
	newID := hex.EncodeToString(b)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO sessions (id, created_at, updated_at, cwd, model, provider, title, goal, forked_from, fork_seq, effort)
		SELECT ?, ?, ?, cwd, model, provider, ?, goal, ?, ?, effort FROM sessions WHERE id=?`,
		newID, now(), now(), title, srcID, uptoSeq, srcID); err != nil {
		return "", err
	}
	if uptoSeq > 0 {
		if _, err := tx.ExecContext(context.Background(), `INSERT INTO messages (session_id, seq, role, content)
			SELECT ?, seq, role, content FROM messages WHERE session_id=? AND seq <= ?`,
			newID, srcID, uptoSeq); err != nil {
			return "", err
		}
	}
	return newID, tx.Commit()
}

// SetTags replaces a session's label set (comma-separated storage).
func (s *Store) SetTags(id string, tags []string) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE sessions SET tags=? WHERE id=?`, strings.Join(tags, ","), id)
	return err
}

// SetPinned marks a session pinned (sorts first in /resume, kept by cleanup).
func (s *Store) SetPinned(id string, pinned bool) error {
	v := 0
	if pinned {
		v = 1
	}
	_, err := s.db.ExecContext(context.Background(), `UPDATE sessions SET pinned=? WHERE id=?`, v, id)
	return err
}

// ForksOf lists sessions forked from id, newest first — the session tree's
// children of one node.
func (s *Store) ForksOf(id string) ([]Meta, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, title, model, provider, cwd, goal, forked_from, fork_seq, tags, pinned, effort, usage_in, usage_cached, usage_out, task_id, updated_at
		FROM sessions WHERE forked_from=? ORDER BY updated_at DESC`, id)
	if err != nil {
		return nil, err
	}
	return scanMetas(rows)
}

// ForkTitle derives the default fork name: "<title> (fork #N)" with N
// incremented past any existing fork of the same base (opencode's
// getForkedTitle — packages/opencode/src/session/session.ts:162). Falls back
// to "session (fork #1)" for untitled sessions.
func (s *Store) ForkTitle(base string) (string, error) {
	if base == "" {
		base = "session"
	}
	// unwrap an existing "(fork #N)" suffix so forks of forks increment
	// instead of nesting: "x (fork #2)" → "x (fork #3)", not "x (fork #2) (fork #1)"
	base = strings.TrimSpace(base)
	if i := strings.LastIndex(base, " (fork #"); i > 0 {
		var n0 int
		var rest string
		n, err := fmt.Sscanf(base[i:], " (fork #%d)%s", &n0, &rest)
		if n0 > 0 && rest == "" && (err == nil || errors.Is(err, io.EOF)) && n >= 1 {
			base = base[:i]
		}
	}
	rows, err := s.db.QueryContext(context.Background(), `SELECT title FROM sessions WHERE title = ? OR title LIKE ? ESCAPE '\'`,
		base, likeEscape(base)+` (fork #%)`)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return "", err
		}
		var num int
		var rest string
		// exact suffix match only: a manually renamed "x (fork #9) notes"
		// must not inflate the numbering
		if nf, err := fmt.Sscanf(t, base+" (fork #%d)%s", &num, &rest); num > n && rest == "" && nf >= 1 && (err == nil || errors.Is(err, io.EOF)) {
			n = num
		}
	}
	return fmt.Sprintf("%s (fork #%d)", base, n+1), rows.Err()
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func scanMetas(rows *sql.Rows) ([]Meta, error) {
	defer func() { _ = rows.Close() }()
	var out []Meta
	for rows.Next() {
		var m Meta
		var updated, tags string
		var pinned int
		if err := rows.Scan(&m.ID, &m.Title, &m.Model, &m.Provider, &m.CWD, &m.Goal,
			&m.ForkedFrom, &m.ForkSeq, &tags, &pinned, &m.Effort,
			&m.UsageIn, &m.UsageCached, &m.UsageOut, &m.TaskID, &updated); err != nil {
			return nil, err
		}
		if tags != "" {
			m.Tags = strings.Split(tags, ",")
		}
		m.Pinned = pinned != 0
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, m)
	}
	return out, rows.Err()
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}
