package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/legacy/session"
	"github.com/context-labs/whip/internal/llm"
)

// A rejected effort save must leave the runner, the saved row and every
// reader at the previous level: the store is the authority, so it is written
// before the runner is touched.
func TestEffortPersistsBeforeItApplies(t *testing.T) {
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createRoot(t, store)
	runner := &controlSurfaceRunner{fakeRunner: &fakeRunner{}}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if result := clientCommand(t, root, "tui", "effort-high", "session.effort", map[string]any{"effort": "high"}); result.Status != "succeeded" || runner.effort != "high" {
		t.Fatalf("initial effort = %+v runner=%q", result, runner.effort)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_effort BEFORE UPDATE OF effort ON sessions
		BEGIN SELECT RAISE(ABORT,'effort rejected'); END`); err != nil {
		t.Fatal(err)
	}
	result := clientCommand(t, root, "tui", "effort-low", "session.effort", map[string]any{"effort": "low"})
	if result.Status != "failed" || !strings.Contains(result.Error, "effort rejected") {
		t.Fatalf("rejected effort = %+v", result)
	}
	meta, _, err := store.Load(rootID)
	if err != nil {
		t.Fatal(err)
	}
	get := clientCommand(t, root, "tui", "effort-get", "session.effort.get", map[string]any{})
	if runner.effort != "high" || meta.Effort != "high" || get.Output != "high" {
		t.Fatalf("failed save changed effort: runner=%q saved=%q get=%+v", runner.effort, meta.Effort, get)
	}
}

type blockingGoalRunner struct {
	*fakeRunner
	started, release chan struct{}
}

func (r *blockingGoalRunner) FormGoal(context.Context, int) (string, llm.Usage, error) {
	close(r.started)
	<-r.release
	return "formulated goal", llm.Usage{}, nil
}

// goal.set and goal.run share the busy guard with effort, model and workspace,
// so a goal formulation cannot be overwritten by, or overwrite, a concurrent
// explicit goal.
func TestGoalCommandsRefuseWhileGoalFormulationRuns(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &blockingGoalRunner{
		fakeRunner: &fakeRunner{turn: func(context.Context, string, bool) (string, error) { return "GOAL_MET — verified", nil }},
		started:    make(chan struct{}), release: make(chan struct{}),
	}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		result CommandResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		raw, _ := json.Marshal(map[string]any{"window": 4})
		digest, err := requestDigest("root", root.ID(), "goal.from-context", raw)
		if err != nil {
			done <- outcome{err: err}
			return
		}
		result, err := root.ClientCommand(context.Background(), session.CommandAdmission{
			ClientID: "tui", CommandID: "goal-context", RequestDigest: digest,
			Payload: session.RuntimePayload{Data: raw, MediaType: "application/json", Source: "goal.from-context"},
		}, "goal.from-context", raw)
		done <- outcome{result: result, err: err}
	}()
	<-runner.started
	set := clientCommand(t, root, "tui", "goal-set-during", "goal.set", map[string]any{"text": "user goal"})
	if set.Status != "failed" || !strings.Contains(set.Error, "goal cannot change") {
		t.Fatalf("goal.set during formulation = %+v", set)
	}
	close(runner.release)
	formed := <-done
	if formed.err != nil || formed.result.Status != "succeeded" || formed.result.Output != "formulated goal" {
		t.Fatalf("formulation = %+v %v", formed.result, formed.err)
	}
	meta, _, err := store.Load(rootID)
	if err != nil || meta.Goal != "formulated goal" {
		t.Fatalf("saved goal = %q %v", meta.Goal, err)
	}
}
