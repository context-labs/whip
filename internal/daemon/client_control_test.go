package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func clientCommand(t *testing.T, root *Session, clientID, commandID, operation string, payload any) CommandResult {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := requestDigest("root", root.ID(), operation, raw)
	if err != nil {
		t.Fatal(err)
	}
	result, err := root.ClientCommand(t.Context(), session.CommandAdmission{
		ClientID: clientID, CommandID: commandID, RequestDigest: digest,
		Payload: session.RuntimePayload{Data: raw, MediaType: "application/json", Source: operation},
	}, operation, raw)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestClientControlCommandsAreActorOwnedAndIdempotent(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}

	goal := clientCommand(t, root, "tui", "goal", "goal.set", map[string]string{"args": "ship it"})
	if goal.Status != "succeeded" || goal.Output != "ship it" {
		t.Fatalf("goal command = %+v", goal)
	}
	retry := clientCommand(t, root, "tui", "goal", "goal.set", map[string]string{"args": "ship it"})
	if retry != goal {
		t.Fatalf("goal retry = %+v, want %+v", retry, goal)
	}
	meta, _, err := store.Load(rootID)
	if err != nil || meta.Goal != "ship it" {
		t.Fatalf("stored goal = %q, %v", meta.Goal, err)
	}
	if err := store.Save(rootID, 1, []llm.Message{{Role: "system"}, {Role: "user", Content: "listed"}}, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	renamed := clientCommand(t, root, "tui", "rename", "session.rename", map[string]string{"args": "Daemon session"})
	if renamed.Status != "succeeded" || renamed.Output != "Daemon session" {
		t.Fatalf("rename command = %+v", renamed)
	}
	listedSessions := clientCommand(t, root, "tui", "sessions", "session.list", map[string]string{})
	if listedSessions.Status != "succeeded" || !strings.Contains(listedSessions.Output, "Daemon session") {
		t.Fatalf("session list = %+v", listedSessions)
	}
	opened := clientCommand(t, root, "tui", "open", "session.open", map[string]string{"args": rootID[:8]})
	if opened.Status != "succeeded" || opened.Output != rootID {
		t.Fatalf("session open = %+v", opened)
	}

	created := clientCommand(t, root, "tui", "schedule-create", "schedule.manage", map[string]string{"args": "@every 10m inspect CI"})
	if created.Status != "succeeded" || created.Output != "1" {
		t.Fatalf("schedule create = %+v", created)
	}
	listed := clientCommand(t, root, "tui", "schedule-list", "schedule.manage", map[string]string{"args": "list"})
	if listed.Status != "succeeded" || !strings.Contains(listed.Output, "inspect CI") {
		t.Fatalf("schedule list = %+v", listed)
	}
	cancelled := clientCommand(t, root, "tui", "schedule-cancel", "schedule.manage", map[string]string{"args": "cancel 1"})
	if cancelled.Status != "succeeded" || len(store.Schedules(rootID)) != 0 {
		t.Fatalf("schedule cancel = %+v schedules=%+v", cancelled, store.Schedules(rootID))
	}

	replaced := clientCommand(t, root, "tui", "new-model", "session.model", map[string]string{"args": "other provider"})
	if replaced.Status != "succeeded" || replaced.Output != "other @ provider" {
		t.Fatalf("model control outcome = %+v", replaced)
	}
	replacedRetry := clientCommand(t, root, "tui", "new-model", "session.model", map[string]string{"args": "other provider"})
	if replacedRetry != replaced {
		t.Fatalf("model retry = %+v, want %+v", replacedRetry, replaced)
	}
	meta, _, err = store.Load(rootID)
	if err != nil || meta.Model != "other" || meta.Provider != "provider" {
		t.Fatalf("stored model = %q @ %q, %v", meta.Model, meta.Provider, err)
	}

	replay, _, err := store.ReplayEvents(t.Context(), rootID, 0, session.MaxEventReplay)
	if err != nil {
		t.Fatal(err)
	}
	var queued, terminal bool
	for _, event := range replay {
		queued = queued || event.Kind == "command.control.queued"
		terminal = terminal || event.Kind == "command.succeeded"
	}
	if !queued || !terminal {
		t.Fatalf("control command events missing: %+v", replay)
	}
}

func TestClientCancelStopsOnlyCurrentTurn(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	started := make(chan struct{})
	var block atomic.Bool
	block.Store(true)
	runner := &fakeRunner{turn: func(ctx context.Context, input string, _ bool) (string, error) {
		if !block.Load() {
			return input, nil
		}
		select {
		case <-started:
		default:
			close(started)
		}
		<-ctx.Done()
		return "", ctx.Err()
	}}
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
	receipt, err := root.Submit(t.Context(), "first")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("turn did not start")
	}
	cancelled := clientCommand(t, root, "tui", "cancel", "cancel", map[string]string{})
	if cancelled.Status != "succeeded" {
		t.Fatalf("cancel command = %+v", cancelled)
	}
	completion := waitReceipt(t, receipt)
	if !errors.Is(completion.Err, context.Canceled) {
		t.Fatalf("turn completion error = %v", completion.Err)
	}

	block.Store(false)
	next, err := root.Submit(t.Context(), "second")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, next); completion.Err != nil || completion.Output != "second" {
		t.Fatalf("next turn = %+v", completion)
	}
}

type blockingShellRunner struct {
	*fakeRunner
	started chan struct{}
	release chan struct{}
}

func (r *blockingShellRunner) RunShell(ctx context.Context, command string) (string, error) {
	close(r.started)
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-r.release:
		return "shell:" + command, nil
	}
}

func TestShellCommandLeavesActorResponsiveAndQueuesTurns(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &blockingShellRunner{
		fakeRunner: &fakeRunner{}, started: make(chan struct{}), release: make(chan struct{}),
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

	payload := json.RawMessage(`{"command":"printf ready"}`)
	digest, err := requestDigest("root", rootID, "shell.run", payload)
	if err != nil {
		t.Fatal(err)
	}
	type commandOutcome struct {
		result CommandResult
		err    error
	}
	shellDone := make(chan commandOutcome, 1)
	go func() {
		result, commandErr := root.ClientCommand(context.Background(), session.CommandAdmission{
			ClientID: "tui", CommandID: "shell", RequestDigest: digest,
			Payload: session.RuntimePayload{Data: payload, MediaType: "application/json", Source: "shell.run"},
		}, "shell.run", payload)
		shellDone <- commandOutcome{result: result, err: commandErr}
	}()
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("shell worker did not start")
	}

	snapshotCtx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	if _, err := root.Snapshot(snapshotCtx); err != nil {
		t.Fatalf("snapshot blocked behind shell worker: %v", err)
	}
	goal := clientCommand(t, root, "tui", "goal-during-shell", "goal.set", map[string]string{"args": "stay responsive"})
	if goal.Status != "succeeded" {
		t.Fatalf("actor command during shell = %+v", goal)
	}
	receipt, err := root.Submit(t.Context(), "after shell")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(25 * time.Millisecond)
	if runner.calls.Load() != 0 {
		t.Fatal("queued turn ran concurrently with the root shell operation")
	}

	close(runner.release)
	select {
	case outcome := <-shellDone:
		if outcome.err != nil || outcome.result.Status != "succeeded" || outcome.result.Output != "shell:printf ready" {
			t.Fatalf("shell outcome = %+v, %v", outcome.result, outcome.err)
		}
	case <-time.After(time.Second):
		t.Fatal("shell command did not settle")
	}
	if completion := waitReceipt(t, receipt); completion.Err != nil || completion.Output != "after shell" {
		t.Fatalf("queued turn = %+v", completion)
	}
}

type compactingRunner struct {
	*fakeRunner
	replaced []llm.Message
}

func (r *compactingRunner) ReplaceHistory(history []llm.Message) {
	r.replaced = append([]llm.Message(nil), history...)
}

func (r *compactingRunner) CompactNow(context.Context) (clientCompaction, error) {
	return clientCompaction{
		summary: "durable summary", cutoff: 3, rawTailStart: 2, model: "summary-model",
		usage:  llm.Usage{PromptTokens: 30, CompletionTokens: 5},
		before: []llm.Message{{Role: "system", Content: "system"}, {Role: "user", Content: "before"}},
	}, nil
}

func TestExplicitCompactionRunsOffActorAndRecordsDurableSummary(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	if err := store.Save(rootID, 0, []llm.Message{
		{Role: "user", Content: "one"}, {Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"}, {Role: "assistant", Content: "four"},
	}, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	runner := &compactingRunner{fakeRunner: &fakeRunner{}}
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
	result := clientCommand(t, root, "tui", "compact", "history.compact", map[string]string{})
	if result.Status != "succeeded" || !strings.Contains(result.Output, "summary-model") {
		t.Fatalf("compaction result = %+v", result)
	}
	events := store.Compactions(rootID)
	if len(events) != 1 || events[0].Summary != "durable summary" || events[0].Cutoff != 2 {
		t.Fatalf("durable compactions = %+v", events)
	}
	meta, _, err := store.Load(rootID)
	if err != nil || meta.UsageIn != 30 || meta.UsageOut != 5 {
		t.Fatalf("compaction usage = %+v, %v", meta, err)
	}
}

type snapshottingHistoryRunner struct {
	*fakeRunner
	captures atomic.Int32
	restores atomic.Int32
	drops    atomic.Int32
	replaced []llm.Message
}

func (r *snapshottingHistoryRunner) CaptureWorkspace(context.Context) string {
	r.captures.Add(1)
	return "pre-turn-ref"
}

func (r *snapshottingHistoryRunner) WorkspaceClean(context.Context) bool { return false }
func (r *snapshottingHistoryRunner) DropWorkspaceSnapshot(context.Context, string) {
	r.drops.Add(1)
}
func (r *snapshottingHistoryRunner) RestoreWorkspace(context.Context, string) (int, error) {
	r.restores.Add(1)
	return 1, nil
}

func TestClearHistoryAtomicallyDropsDerivedStateAndReleasesSnapshots(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	if err := store.Save(rootID, 1, []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "question"}, {Role: "assistant", Content: "answer"},
	}, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSnapshot(rootID, 1, "pre-turn-ref"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordCompaction(rootID, 2, "summary"); err != nil {
		t.Fatal(err)
	}
	runner := &snapshottingHistoryRunner{fakeRunner: &fakeRunner{}}
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
	result := clientCommand(t, root, "tui", "clear", "history.clear", map[string]string{})
	if result.Status != "succeeded" || result.Output != "history cleared" {
		t.Fatalf("clear result = %+v", result)
	}
	if _, history, err := store.Load(rootID); err != nil || len(history) != 0 {
		t.Fatalf("cleared history = %+v, %v", history, err)
	}
	if len(store.Snapshots(rootID)) != 0 || len(store.Compactions(rootID)) != 0 || len(runner.replaced) != 0 {
		t.Fatalf("derived state snapshots=%v compactions=%v replacement=%v", store.Snapshots(rootID), store.Compactions(rootID), runner.replaced)
	}
	deadline := time.Now().Add(time.Second)
	for runner.drops.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if runner.drops.Load() != 1 {
		t.Fatalf("released workspace snapshots = %d", runner.drops.Load())
	}
}

type goalFormingRunner struct {
	*fakeRunner
	forms atomic.Int32
}

func (r *goalFormingRunner) FormGoal(context.Context, int) (string, llm.Usage, error) {
	r.forms.Add(1)
	return "ship the daemon-backed client", llm.Usage{PromptTokens: 12, CompletionTokens: 3}, nil
}

func TestGoalFromContextRunsOnceOffActorAndStartsGoal(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &goalFormingRunner{fakeRunner: &fakeRunner{
		turn: func(context.Context, string, bool) (string, error) { return "GOAL_MET — verified", nil },
	}}
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
	result := clientCommand(t, root, "tui", "goal-context", "goal.from-context", map[string]string{"args": "4"})
	if result.Status != "succeeded" || result.Output != "ship the daemon-backed client" {
		t.Fatalf("goal formulation = %+v", result)
	}
	retry := clientCommand(t, root, "tui", "goal-context", "goal.from-context", map[string]string{"args": "4"})
	if retry != result || runner.forms.Load() != 1 {
		t.Fatalf("goal retry=%+v formulations=%d", retry, runner.forms.Load())
	}
	deadline := time.Now().Add(time.Second)
	for runner.calls.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("formulated goal turns = %d", runner.calls.Load())
	}
}
func (r *snapshottingHistoryRunner) ReplaceHistory(history []llm.Message) {
	r.replaced = append([]llm.Message(nil), history...)
}

func TestTurnSnapshotAndRewindAreDaemonOwnedAndIdempotent(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &snapshottingHistoryRunner{fakeRunner: &fakeRunner{
		history: []llm.Message{{Role: "system", Content: "system"}},
	}}
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
	receipt, err := root.Submit(t.Context(), "change the workspace")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, receipt); completion.Err != nil {
		t.Fatal(completion.Err)
	}
	if snapshots := store.Snapshots(rootID); len(snapshots) != 1 || snapshots[1] != "pre-turn-ref" {
		t.Fatalf("committed workspace snapshots = %v", snapshots)
	}

	result := clientCommand(t, root, "tui", "rewind-one", "history.rewind", map[string]string{"args": "1"})
	if result.Status != "succeeded" || !strings.Contains(result.Output, "restored 1 workspace file") {
		t.Fatalf("rewind result = %+v", result)
	}
	if runner.restores.Load() != 1 || len(runner.replaced) != 0 {
		t.Fatalf("restore count=%d replacement=%+v", runner.restores.Load(), runner.replaced)
	}
	if snapshots := store.Snapshots(rootID); len(snapshots) != 0 {
		t.Fatalf("rewind left snapshot rows: %v", snapshots)
	}
	_, history, err := store.Load(rootID)
	if err != nil || len(history) != 0 {
		t.Fatalf("rewound history = %+v, %v", history, err)
	}

	retry := clientCommand(t, root, "tui", "rewind-one", "history.rewind", map[string]string{"args": "1"})
	if retry != result || runner.restores.Load() != 1 {
		t.Fatalf("stable retry=%+v restore count=%d", retry, runner.restores.Load())
	}
}

func TestGoalRunUsesOneControlCommandAndStartsTheGoalTurn(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &fakeRunner{turn: func(context.Context, string, bool) (string, error) { return "GOAL_MET — done", nil }}
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
	result := clientCommand(t, root, "tui", "goal-run", "goal.run", map[string]string{"args": "finish it"})
	if result.Status != "succeeded" || result.Output != "finish it" {
		t.Fatalf("goal command = %+v", result)
	}
	deadline := time.Now().Add(time.Second)
	for runner.calls.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("goal turn count = %d", runner.calls.Load())
	}
	deadline = time.Now().Add(time.Second)
	for {
		meta, _, loadErr := store.Load(rootID)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if meta.Goal == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("completed goal = %q", meta.Goal)
		}
		time.Sleep(time.Millisecond)
	}
}
