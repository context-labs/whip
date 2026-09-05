package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
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

func TestToolHostRunsOnlyRestrictedToolCommands(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "evidence.txt")
	if err := os.WriteFile(path, []byte("tool-host evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID, err := store.Create(session.SessionKindToolHost, workspace, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var agentConstructions atomic.Int32
	owner, err := New(store, func(_ context.Context, meta session.Meta, _ []llm.Message) (Components, error) {
		if meta.Kind != session.SessionKindToolHost {
			agentConstructions.Add(1)
			return Components{}, errors.New("unexpected model session")
		}
		return Components{Runner: NewToolRunner(tools.NewServices())}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := root.runner.(*toolRunner); !ok || agentConstructions.Load() != 0 {
		t.Fatalf("tool host runner=%T agent constructions=%d", root.runner, agentConstructions.Load())
	}
	if _, err := root.Submit(t.Context(), "model turn"); err == nil || !strings.Contains(err.Error(), "tool-host") {
		t.Fatalf("tool-host submit error=%v", err)
	}
	if _, err := root.Steer(t.Context(), "model steer"); err == nil || !strings.Contains(err.Error(), "tool-host") {
		t.Fatalf("tool-host steer error=%v", err)
	}

	schema := clientCommand(t, root, "mcp", "schema", "tool.schema", struct{}{})
	if schema.Status != "succeeded" || !strings.Contains(schema.Output, `"name":"read"`) {
		t.Fatalf("tool schema=%+v", schema)
	}
	call := clientCommand(t, root, "mcp", "read", "tool.call", map[string]any{
		"tool": "read", "arguments": json.RawMessage(fmt.Sprintf(`{"path":%q}`, path)),
	})
	if call.Status != "succeeded" || !strings.Contains(call.Output, "tool-host evidence") {
		t.Fatalf("tool call=%+v", call)
	}
	if _, err := root.ClientCommand(t.Context(), session.CommandAdmission{
		ClientID: "mcp", CommandID: "agents", RequestDigest: "irrelevant",
	}, "agents.list", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "tool-host") {
		t.Fatalf("agent command error=%v", err)
	}
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
	if got := root.rawCompactionCutoff(4, 0); got != 4 {
		t.Fatalf("empty-history raw cutoff = %d", got)
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
	if created.Status != "succeeded" || created.Output != "schedule 1 created" {
		t.Fatalf("schedule create = %+v", created)
	}
	listed := clientCommand(t, root, "tui", "schedule-list", "schedule.manage", map[string]string{"args": "list"})
	if listed.Status != "succeeded" || !strings.Contains(listed.Output, "inspect CI") {
		t.Fatalf("schedule list = %+v", listed)
	}
	cancelled := clientCommand(t, root, "tui", "schedule-cancel", "schedule.manage", map[string]string{"args": "cancel 1"})
	if cancelled.Status != "succeeded" || cancelled.Output != "schedule 1 cancelled" || len(store.Schedules(rootID)) != 0 {
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
	model := clientCommand(t, root, "tui", "model-during-turn", "session.model", map[string]string{"args": "other provider"})
	if model.Status != "failed" || !strings.Contains(model.Error, "root operation") {
		t.Fatalf("model command during turn = %+v", model)
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
	retry, err := root.ClientCommand(t.Context(), session.CommandAdmission{
		ClientID: "tui", CommandID: "shell", RequestDigest: digest,
		Payload: session.RuntimePayload{Data: payload, MediaType: "application/json", Source: "shell.run"},
	}, "shell.run", payload)
	if err != nil || retry.Status != "queued" && retry.Status != "running" {
		t.Fatalf("in-flight shell retry = %+v, %v", retry, err)
	}
	for i, test := range []struct {
		operation string
		payload   any
	}{
		{"shell.run", map[string]string{"command": "true"}},
		{"history.compact", map[string]string{}},
		{"goal.from-context", map[string]string{"args": "2"}},
		{"history.rewind", map[string]string{"args": "1"}},
		{"history.clear", map[string]string{}},
	} {
		result := clientCommand(t, root, "tui", fmt.Sprintf("busy-%d", i), test.operation, test.payload)
		if result.Status != "failed" || !strings.Contains(result.Error, "running") {
			t.Fatalf("busy %s = %+v", test.operation, result)
		}
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
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"},
		{Role: "assistant", Content: "four"},
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
	if got := root.rawCompactionCutoff(3, 0); got != 3 {
		t.Fatalf("nested raw cutoff = %d", got)
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
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "answer"},
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

type controlSurfaceRunner struct {
	*fakeRunner
	workingDirectory string
	effort           string
	terminalID       string
	terminalInput    string
	replaced         []llm.Message
}

func (r *controlSurfaceRunner) ResolveWorkingDirectory(path string) (string, error) {
	if path == "bad" {
		return "", errors.New("invalid workspace")
	}
	if path != "." {
		return filepath.Join(r.workingDirectory, path), nil
	}
	return r.workingDirectory, nil
}

func (r *controlSurfaceRunner) SetWorkingDirectory(path string) { r.workingDirectory = path }
func (r *controlSurfaceRunner) ReplaceHistory(history []llm.Message) {
	r.replaced = append([]llm.Message(nil), history...)
}

func (r *controlSurfaceRunner) StartTask(prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("task prompt required")
	}
	return "task-1", nil
}

func (r *controlSurfaceRunner) SendTerminalInput(id string, input []byte) error {
	r.terminalID, r.terminalInput = id, string(input)
	return nil
}
func (r *controlSurfaceRunner) SetEffort(effort string) { r.effort = effort }

type controlSurfaceMCP struct {
	fakeCloser
	actions []string
}

func (m *controlSurfaceMCP) Statuses() []mcp.Server {
	return []mcp.Server{{Name: "alpha", Status: mcp.StatusReady, Tools: 2}}
}
func (m *controlSurfaceMCP) Blocked() []mcp.Server      { return nil }
func (m *controlSurfaceMCP) Reconnect(name string) bool { return m.record("reconnect", name) }
func (m *controlSurfaceMCP) Enable(name string) bool    { return m.record("enable", name) }
func (m *controlSurfaceMCP) Disable(name string) bool   { return m.record("disable", name) }
func (m *controlSurfaceMCP) record(action, name string) bool {
	if name != "alpha" {
		return false
	}
	m.actions = append(m.actions, action)
	return true
}

func TestClientControlSurfaceDelegatesEveryAuthorityToDaemon(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &controlSurfaceRunner{fakeRunner: &fakeRunner{}, workingDirectory: t.TempDir()}
	mcpManager := &controlSurfaceMCP{}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner, MCP: mcpManager}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.AdmitAgent(t.Context(), session.AgentAdmission{ParentAgentID: root.AgentID(), ChildAgentID: "child", Name: "child"}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		id        string
		operation string
		args      string
		contains  string
	}{
		{"workspace-inspect", "workspace.inspect", "", runner.workingDirectory},
		{"workspace-set", "workspace.set", "nested", "nested"},
		{"effort", "session.effort", "high", "high"},
		{"model", "session.model", "", "model @ provider"},
		{"same-model", "session.model", "model provider", "model @ provider"},
		{"fork", "session.fork", "control fork", ""},
		{"agents", "agents.list", "", "child"},
		{"budget", "budget.cap", root.AgentID() + " tokens 100", `"Limit":100`},
		{"context", "context.audit", "", rootID},
		{"terminal", "terminal.input", "", "input delivered"},
		{"mcp-list", "mcp.control", "list", "alpha"},
		{"mcp-reconnect", "mcp.control", "alpha reconnect", "alpha: reconnect"},
		{"mcp-enable", "mcp.control", "alpha enable", "alpha: enable"},
		{"mcp-disable", "mcp.control", "alpha disable", "alpha: disable"},
		{"lsp", "lsp.control", "status", "[]"},
		{"browser", "browser.control", "status", "disabled"},
		{"computer", "computer.control", "status", "disabled"},
		{"clear-goal", "goal.set", "clear", ""},
	}
	for _, test := range tests {
		payload := map[string]any{"args": test.args}
		if test.operation == "terminal.input" {
			payload = map[string]any{"id": "terminal-7", "bytes": []byte("yes\n")}
		}
		result := clientCommand(t, root, "tui", test.id, test.operation, payload)
		if result.Status != "succeeded" || !strings.Contains(result.Output, test.contains) {
			t.Fatalf("%s = %+v, want output containing %q", test.operation, result, test.contains)
		}
	}
	if runner.effort != "high" || runner.terminalID != "terminal-7" || runner.terminalInput != "yes\n" {
		t.Fatalf("runner controls effort=%q terminal=%q %q", runner.effort, runner.terminalID, runner.terminalInput)
	}
	if strings.Join(mcpManager.actions, ",") != "reconnect,enable,disable" {
		t.Fatalf("MCP actions = %v", mcpManager.actions)
	}
	meta, _, err := store.Load(rootID)
	if err != nil || meta.CWD != runner.workingDirectory || meta.Effort != "high" {
		t.Fatalf("durable controls meta=%+v err=%v", meta, err)
	}
}

func TestClientControlRejectsRootRuntimeMutationDuringTurn(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &controlSurfaceRunner{fakeRunner: &fakeRunner{}, workingDirectory: t.TempDir()}
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
	if err := root.routeControl(t.Context(), func(context.Context) error {
		root.running = &rootTurn{seq: 1}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = root.routeControl(context.Background(), func(context.Context) error {
			root.running = nil
			return nil
		})
	})

	for index, test := range []struct {
		operation string
		args      string
		contains  string
	}{
		{"workspace.set", "nested", "working directory cannot change"},
		{"session.effort", "high", "effort cannot change"},
	} {
		result := clientCommand(t, root, "tui", fmt.Sprintf("busy-mutation-%d", index), test.operation, map[string]string{"args": test.args})
		if result.Status != "failed" || !strings.Contains(result.Error, test.contains) {
			t.Fatalf("%s while busy = %+v", test.operation, result)
		}
	}
	result := clientCommand(t, root, "tui", "busy-effort-read", "session.effort", map[string]string{"args": ""})
	if result.Status != "succeeded" {
		t.Fatalf("effort status while busy = %+v", result)
	}
}

func TestClientControlRejectsInvalidActionsDurably(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &controlSurfaceRunner{fakeRunner: &fakeRunner{}, workingDirectory: t.TempDir()}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner, MCP: &controlSurfaceMCP{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		operation string
		args      string
	}{
		{"workspace.set", "bad"},
		{"shell.run", ""},
		{"history.compact", ""},
		{"goal.from-context", "1"},
		{"goal.from-context", "2"},
		{"history.rewind", "not-a-number"},
		{"session.rename", ""},
		{"session.open", "missing"},
		{"schedule.manage", "cancel nope"},
		{"schedule.manage", "@every nope prompt"},
		{"schedule.manage", "invalid"},
		{"agent.control", ""},
		{"budget.cap", "missing"},
		{"budget.cap", root.AgentID() + " tokens nope"},
		{"capability.revoke", "missing"},
		{"terminal.input", ""},
		{"mcp.control", "missing reconnect"},
		{"mcp.control", "alpha explode"},
		{"unsupported", ""},
	}
	for i, test := range tests {
		result := clientCommand(t, root, "tui", fmt.Sprintf("invalid-%d", i), test.operation, map[string]string{"args": test.args})
		if result.Status != "failed" || result.Error == "" {
			t.Fatalf("%s %q = %+v", test.operation, test.args, result)
		}
	}
	for i, operation := range []string{"shell.run", "goal.from-context", "history.rewind"} {
		raw := json.RawMessage(`{`)
		result, err := root.ClientCommand(t.Context(), session.CommandAdmission{
			ClientID: "tui", CommandID: fmt.Sprintf("malformed-%d", i), RequestDigest: "malformed-payload",
			Payload: session.RuntimePayload{Data: raw, MediaType: "application/json", Source: operation},
		}, operation, raw)
		if err != nil || result.Status != "failed" {
			t.Fatalf("malformed %s = %+v err=%v", operation, result, err)
		}
	}
	raw := json.RawMessage(`{`)
	result, err := root.ClientCommand(t.Context(), session.CommandAdmission{
		ClientID: "tui", CommandID: "malformed-generic", RequestDigest: "malformed-generic",
		Payload: session.RuntimePayload{Data: raw, MediaType: "application/json", Source: "workspace.inspect"},
	}, "workspace.inspect", raw)
	if err != nil || result.Status != "failed" || !strings.Contains(result.Error, "invalid client action payload") {
		t.Fatalf("malformed generic action = %+v err=%v", result, err)
	}
}

func TestProtocolClientCommandsFailClosedWithoutOptionalRunnerCapabilities(t *testing.T) {
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
	for i, test := range []struct {
		operation string
		payload   any
	}{
		{"permission.mode", map[string]bool{"external_permissions": true}},
		{"tool.configure", map[string]bool{"deny_permissions": true}},
		{"tool.schema", struct{}{}},
		{"run.configure", map[string]int{"max_turns": -1}},
		{"run.configure", struct{}{}},
		{"workspace.set", map[string]string{"args": "nested"}},
		{"history.clear", struct{}{}},
	} {
		result := clientCommand(t, root, "client", fmt.Sprintf("unsupported-%d", i), test.operation, test.payload)
		if result.Status != "failed" || result.Error == "" {
			t.Fatalf("%s = %+v", test.operation, result)
		}
	}
	for i, raw := range []json.RawMessage{json.RawMessage(`{`), json.RawMessage(`{}`), json.RawMessage(`{"tool":"read","arguments":{}}`)} {
		result, err := root.ClientCommand(t.Context(), session.CommandAdmission{
			ClientID: "client", CommandID: fmt.Sprintf("tool-invalid-%d", i), RequestDigest: fmt.Sprintf("tool-invalid-%d", i),
			Payload: session.RuntimePayload{Data: raw, MediaType: "application/json", Source: "tool.call"},
		}, "tool.call", raw)
		if err != nil || result.Status != "failed" || result.Error == "" {
			t.Fatalf("tool call %q = %+v, %v", raw, result, err)
		}
	}
}

func TestClientControlReportsUnsupportedRunnerCapabilities(t *testing.T) {
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
	tests := []struct {
		operation string
		payload   map[string]any
	}{
		{"shell.run", map[string]any{"command": "true"}},
		{"history.compact", map[string]any{}},
		{"goal.from-context", map[string]any{"args": "2"}},
		{"history.rewind", map[string]any{"args": "1"}},
		{"workspace.set", map[string]any{"args": "."}},
		{"history.clear", map[string]any{}},
		{"terminal.input", map[string]any{"id": "terminal", "bytes": []byte("x")}},
	}
	for i, test := range tests {
		result := clientCommand(t, root, "tui", fmt.Sprintf("unsupported-%d", i), test.operation, test.payload)
		if result.Status != "failed" || result.Error == "" {
			t.Fatalf("%s = %+v", test.operation, result)
		}
	}
	if result := clientCommand(t, root, "tui", "idle-cancel", "cancel", map[string]any{}); result.Status != "succeeded" || result.Output != "already idle" {
		t.Fatalf("idle cancel = %+v", result)
	}
	if result := clientCommand(t, root, "tui", "workspace-fallback", "workspace.inspect", map[string]any{}); result.Output != root.meta.CWD {
		t.Fatalf("workspace fallback = %+v", result)
	}
	if result := clientCommand(t, root, "tui", "mcp-fallback", "mcp.control", map[string]any{}); result.Output != "[]" {
		t.Fatalf("MCP fallback = %+v", result)
	}
}

type failingAsyncRunner struct{ *fakeRunner }

func (r *failingAsyncRunner) RunShell(context.Context, string) (string, error) {
	return "partial", errors.New("shell failed")
}

func (r *failingAsyncRunner) CompactNow(context.Context) (clientCompaction, error) {
	return clientCompaction{}, errors.New("compact failed")
}

func (r *failingAsyncRunner) FormGoal(context.Context, int) (string, llm.Usage, error) {
	return "", llm.Usage{}, errors.New("goal failed")
}
func (r *failingAsyncRunner) ReplaceHistory([]llm.Message) {}
func (r *failingAsyncRunner) CaptureWorkspace(context.Context) string {
	return ""
}
func (r *failingAsyncRunner) WorkspaceClean(context.Context) bool { return false }
func (r *failingAsyncRunner) DropWorkspaceSnapshot(context.Context, string) {
}

func (r *failingAsyncRunner) RestoreWorkspace(context.Context, string) (int, error) {
	return 0, errors.New("restore failed")
}

func TestAsyncClientControlFailuresSettleDurably(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	if err := store.SetSnapshot(rootID, 1, "snapshot-ref"); err != nil {
		t.Fatal(err)
	}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &failingAsyncRunner{fakeRunner: &fakeRunner{}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		operation string
		payload   map[string]string
		want      string
	}{
		{"shell.run", map[string]string{"command": "false"}, "shell failed"},
		{"history.compact", map[string]string{}, "compact failed"},
		{"goal.from-context", map[string]string{"args": "2"}, "goal failed"},
		{"history.rewind", map[string]string{"args": "1"}, "restore failed"},
	}
	for i, test := range tests {
		result := clientCommand(t, root, "tui", fmt.Sprintf("async-failure-%d", i), test.operation, test.payload)
		if result.Status != "failed" || !strings.Contains(result.Error, test.want) {
			t.Fatalf("%s = %+v", test.operation, result)
		}
		retry := clientCommand(t, root, "tui", fmt.Sprintf("async-failure-%d", i), test.operation, test.payload)
		if retry != result {
			t.Fatalf("%s retry=%+v want=%+v", test.operation, retry, result)
		}
	}
}

type replaceBlockedRunner struct{ *fakeRunner }

func (*replaceBlockedRunner) CanReplace() error { return errors.New("child task is running") }

type reloadTestRuntime struct{ running atomic.Bool }

func (*reloadTestRuntime) Close()                   {}
func (r *reloadTestRuntime) HasRunningAgents() bool { return r.running.Load() }

func TestMCPImportDefersRuntimeReloadUntilChildrenAreIdle(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runtime := &reloadTestRuntime{}
	runtime.running.Store(true)
	constructions := 0
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		constructions++
		return Components{Runner: &fakeRunner{}, Runtime: runtime}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	result := clientCommand(t, root, "tui", "mcp-import", "mcp.control", map[string]string{"args": "import claude off"})
	if result.Status != "succeeded" || !strings.Contains(result.Output, "pending") || !root.reloadPending || constructions != 1 {
		t.Fatalf("busy import=%+v pending=%t constructions=%d", result, root.reloadPending, constructions)
	}
	cfg, err := config.Load()
	if err != nil || cfg.MCPImport == nil || cfg.MCPImport.Claude == nil || cfg.MCPImport.Claude.Enabled == nil || *cfg.MCPImport.Claude.Enabled {
		t.Fatalf("saved import policy=%+v err=%v", cfg.MCPImport, err)
	}
	runtime.running.Store(false)
	root.applyPendingReloadAfterAgent()
	if root.reloadPending || constructions != 2 {
		t.Fatalf("idle reload pending=%t constructions=%d", root.reloadPending, constructions)
	}
}

func TestEffortControlPreservesExplicitOffAndGlobalDefaultOnCompatibilityChange(t *testing.T) {
	t.Run("explicit off", func(t *testing.T) {
		t.Setenv("WHIP_HOME", t.TempDir())
		store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
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
		result := clientCommand(t, root, "tui", "effort-off", "session.effort", map[string]string{"args": "off", "persist_default": "true"})
		meta, _, loadErr := store.Load(rootID)
		cfg, configErr := config.Load()
		if result.Status != "succeeded" || runner.effort != "" || loadErr != nil || configErr != nil || meta.Effort != "off" || cfg.DefaultEffort != "off" {
			t.Fatalf("off result=%+v runner=%q meta=%q default=%q load=%v config=%v", result, runner.effort, meta.Effort, cfg.DefaultEffort, loadErr, configErr)
		}
	})

	t.Run("model compatibility is session only", func(t *testing.T) {
		t.Setenv("WHIP_HOME", t.TempDir())
		cfg := config.Default()
		cfg.DefaultEffort = "high"
		cfg.Providers["limited-provider"] = config.Provider{BaseURL: "https://example.test"}
		cfg.Models["limited-model"] = config.Model{ID: "limited-api", Providers: []string{"limited-provider"}}
		if err := cfg.Save(); err != nil {
			t.Fatal(err)
		}
		if err := config.SaveCatalogs(map[string]config.Catalog{"limited-provider": {
			Models: []config.ModelInfoLite{{ID: "limited-api", ReasoningEfforts: []string{"low"}}},
		}}); err != nil {
			t.Fatal(err)
		}
		store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
		rootID := createRoot(t, store)
		if err := store.SetEffort(rootID, "high"); err != nil {
			t.Fatal(err)
		}
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
		result := clientCommand(t, root, "tui", "model-session-only", "session.model", map[string]string{
			"args": "limited-model limited-provider", "persist_default": "false",
		})
		meta, _, loadErr := store.Load(rootID)
		saved, configErr := config.Load()
		if result.Status != "succeeded" || loadErr != nil || configErr != nil || meta.Effort != "off" || saved.DefaultEffort != "high" {
			t.Fatalf("compatibility result=%+v effort=%q default=%q load=%v config=%v", result, meta.Effort, saved.DefaultEffort, loadErr, configErr)
		}
	})
}

func TestModelReplacementRejectsUnsafeFactories(t *testing.T) {
	tests := []struct {
		name         string
		initial      Runner
		replacement  func() (Components, error)
		clearFactory bool
		want         string
	}{
		{name: "missing factory", initial: &fakeRunner{}, clearFactory: true, want: "cannot be rebuilt"},
		{name: "active child", initial: &replaceBlockedRunner{fakeRunner: &fakeRunner{}}, want: "child task is running"},
		{name: "factory error", initial: &fakeRunner{}, replacement: func() (Components, error) {
			return Components{}, errors.New("factory failed")
		}, want: "factory failed"},
		{name: "nil runner", initial: &fakeRunner{}, replacement: func() (Components, error) {
			return Components{MCP: &fakeCloser{}, Runtime: &fakeCloser{}}, nil
		}, want: "no replacement runner"},
		{name: "runner bind", initial: &fakeRunner{}, replacement: func() (Components, error) {
			return Components{Runner: &bindErrorRunner{err: errors.New("runner bind failed")}}, nil
		}, want: "runner bind failed"},
		{name: "component bind", initial: &fakeRunner{}, replacement: func() (Components, error) {
			return Components{Runner: &fakeRunner{}, Bind: func(*Session) error { return errors.New("component bind failed") }}, nil
		}, want: "component bind failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
			rootID := createRoot(t, store)
			calls := 0
			value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
				calls++
				if calls == 1 || test.replacement == nil {
					return Components{Runner: test.initial}, nil
				}
				return test.replacement()
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = value.Close() })
			root, err := value.Open(rootID)
			if err != nil {
				t.Fatal(err)
			}
			if test.clearFactory {
				root.factory = nil
			}
			result := clientCommand(t, root, "tui", "replace", "session.model", map[string]string{"args": "other provider"})
			if result.Status != "failed" || !strings.Contains(result.Error, test.want) {
				t.Fatalf("replacement = %+v, want %q", result, test.want)
			}
		})
	}
}

func TestProductionAgentRunnerControlAdapters(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"formed goal"}}],"usage":{"prompt_tokens":7,"completion_tokens":2}}`)
	}))
	defer server.Close()

	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	authority, err := store.EnsureAuthority(t.Context(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	services := tools.NewServices()
	services.SetBrowser(browser.NewManager(browser.ModeLive), false)
	services.SetComputerPolicy(computer.NewPolicy(nil, nil, false))
	services.SetDiagnostics(lsp.NewManager(nil))
	if err := services.BindDispatcher(store, store.Workspaces(), store.Processes(), authority); err != nil {
		t.Fatal(err)
	}
	meta, _, err := store.Load(rootID)
	if err != nil {
		t.Fatal(err)
	}
	agentValue := agent.NewRuntime(llm.New(server.URL, "key"), "model", 100, "system", services)
	agentValue.WorkingDir = meta.CWD
	runner := &AgentSession{agent: agentValue}

	if path, err := runner.ResolveWorkingDirectory("."); err != nil || !filepath.IsAbs(path) {
		t.Fatalf("resolved workspace=%q err=%v", path, err)
	}
	rootDirectory := agentValue.WorkingDir
	nested := filepath.Join(rootDirectory, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(rootDirectory)
	if err != nil {
		t.Fatal(err)
	}
	canonicalNested, err := filepath.EvalSymlinks(nested)
	if err != nil {
		t.Fatal(err)
	}
	runner.SetWorkingDirectory(nested)
	if path, err := runner.ResolveWorkingDirectory("."); err != nil || path != canonicalNested {
		t.Fatalf("resolved current session workspace=%q err=%v", path, err)
	}
	if path, err := runner.ResolveWorkingDirectory(".."); err != nil || path != canonicalRoot {
		t.Fatalf("resolved relative session workspace=%q err=%v", path, err)
	}
	if output, err := runner.RunShell(t.Context(), "printf adapter"); err != nil || output != "adapter" {
		t.Fatalf("shell output=%q err=%v", output, err)
	}
	services.SetExternalPermissions(true)
	runner.ConfigureRun("", 3, true)
	if services.ExternalPermissionsEnabled() || agentValue.MaxTurns != 3 {
		t.Fatalf("headless run policy external=%v maxTurns=%d",
			services.ExternalPermissionsEnabled(), agentValue.MaxTurns)
	}
	if _, err := runner.RunShell(t.Context(), "printf denied"); err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("headless shell should be denied, got %v", err)
	}
	runner.ReplaceHistory([]llm.Message{{Role: "user", Content: "question"}, {Role: "assistant", Content: "answer"}})
	goal, usage, err := runner.FormGoal(t.Context(), 2)
	if err != nil || goal != "formed goal" || usage.PromptTokens != 7 || usage.CompletionTokens != 2 {
		t.Fatalf("goal=%q usage=%+v err=%v", goal, usage, err)
	}
	if err := runner.CanReplace(); err != nil {
		t.Fatal(err)
	}
	if runner.browserManager() == nil || runner.lspManager() == nil || runner.computerPolicy() == nil {
		t.Fatal("configured services were absent from the production adapter")
	}
	controlSession := &Session{runner: runner}
	if output, err := controlSession.clientBrowser("status"); err != nil || output == "" {
		t.Fatalf("browser status=%q err=%v", output, err)
	}
	if output, err := controlSession.clientBrowser("driver chromedp"); err != nil || output != browser.DriverChromedp {
		t.Fatalf("browser driver=%q err=%v", output, err)
	}
	if _, err := controlSession.clientBrowser("invalid"); err == nil {
		t.Fatal("invalid browser driver was accepted")
	}
	if output, err := controlSession.clientComputer("allow Terminal"); err != nil || output != "Terminal: allow" {
		t.Fatalf("computer allow=%q err=%v", output, err)
	}
	if output, err := controlSession.clientComputer("deny Preview"); err != nil || output != "Preview: deny" {
		t.Fatalf("computer deny=%q err=%v", output, err)
	}
	if _, err := controlSession.clientComputer("allow"); err == nil {
		t.Fatal("invalid computer action was accepted")
	}
	lspPayload := json.RawMessage(`{"args":"status"}`)
	if output, err := controlSession.applyClientCommand(t.Context(), "lsp.control", lspPayload); err != nil || !strings.Contains(output, "[") {
		t.Fatalf("LSP status=%q err=%v", output, err)
	}
	if _, err := controlSession.applyClientCommand(t.Context(), "lsp.control", json.RawMessage(`{"args":"restart"}`)); err == nil {
		t.Fatal("invalid LSP action was accepted")
	}
	emptyRunner := &AgentSession{agent: &agent.Agent{}}
	if emptyRunner.browserManager() != nil || emptyRunner.lspManager() != nil || emptyRunner.computerPolicy() != nil {
		t.Fatal("agent without services exposed optional managers")
	}
	if _, err := runner.CompactNow(t.Context()); err == nil {
		t.Fatal("short history unexpectedly compacted")
	}
	emptyHistory := &AgentSession{agent: agent.NewRuntime(llm.New(server.URL, "key"), "model", 100, "system", tools.NewServices())}
	if _, _, err := emptyHistory.FormGoal(t.Context(), 2); err == nil {
		t.Fatal("goal formulation accepted an empty conversation")
	}
	runner.SetEffort("max")
	if agentValue.Effort != "max" {
		t.Fatalf("effort=%q", agentValue.Effort)
	}
	if err := runner.SendTerminalInput("missing", []byte("x")); err == nil {
		t.Fatal("terminal input succeeded before the daemon bridge was bound")
	}
	runner.interactive = newDaemonInteractiveRunner(func(string, StreamEvent) {})
	if err := runner.SendTerminalInput("stale", []byte("x")); err == nil {
		t.Fatal("stale terminal input was accepted")
	}
	runner.Close()
}

type recoveringCompactionRunner struct{ *snapshottingHistoryRunner }

func (*recoveringCompactionRunner) CompactNow(context.Context) (clientCompaction, error) {
	return clientCompaction{}, nil
}

func TestClientControlStoreFailuresAndAsyncRecovery(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	meta, _, err := store.Load(rootID)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(t.Context(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	runner := &controlSurfaceRunner{fakeRunner: &fakeRunner{}, workingDirectory: t.TempDir()}
	root := newSession(store, meta, authority, Components{Runner: runner})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		operation string
		payload   string
	}{
		{"goal.set", `{"args":"persist"}`},
		{"workspace.set", `{"args":"."}`},
		{"session.effort", `{"args":"high"}`},
		{"session.rename", `{"args":"title"}`},
		{"history.clear", `{}`},
		{"schedule.manage", `{"args":"list"}`},
		{"session.list", `{}`},
		{"context.audit", `{}`},
		{"budget.cap", fmt.Sprintf(`{"args":%q}`, root.AgentID()+" tokens 1")},
	} {
		if _, err := root.applyClientCommand(t.Context(), test.operation, json.RawMessage(test.payload)); err == nil {
			t.Fatalf("closed store accepted %s", test.operation)
		}
	}
	compactionRunner := &recoveringCompactionRunner{snapshottingHistoryRunner: &snapshottingHistoryRunner{fakeRunner: &fakeRunner{}}}
	root.runner = compactionRunner
	root.clientBusy = true
	completion := &clientCommandCompletion{
		clientID: "tui", commandID: "compact", operation: "history.compact", reply: make(chan clientCommandReply, 1),
		compact: &clientCompaction{summary: "summary", cutoff: 2, before: []llm.Message{{Role: "system"}, {Role: "user", Content: "question"}}},
	}
	if err := root.completeClientCommand(completion); err == nil || len(compactionRunner.replaced) != 1 {
		t.Fatalf("compaction recovery err=%v history=%+v", err, compactionRunner.replaced)
	}
	root.clientBusy = true
	completion = &clientCommandCompletion{
		clientID: "tui", commandID: "rewind", operation: "history.rewind", reply: make(chan clientCommandReply, 1),
		rewind: &clientRewind{cut: 1},
	}
	if err := root.completeClientCommand(completion); err == nil {
		t.Fatal("rewind completion ignored the closed store")
	}
	root.clientBusy = true
	completion = &clientCommandCompletion{
		clientID: "tui", commandID: "goal", operation: "goal.from-context", reply: make(chan clientCommandReply, 1),
		goal: &clientGoal{text: "goal"},
	}
	if err := root.completeClientCommand(completion); err == nil {
		t.Fatal("goal completion ignored the closed store")
	}
}
