package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestWorkspaceControlBothEnginesPreservesREPLAndHistoricalDirectory(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{"before": "saved = 41\nprint(saved)", "after": "print(saved + 1)"}
			if engine == session.QuickJS {
				codes = map[string]string{"before": "var saved = 41; console.log(saved)", "after": "console.log(saved + 1)"}
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(codes))
			root := createEngineSession(t, r, engine)
			runCellTurn(t, r, root.ID, "before", "41\n")
			old, err := r.store.LatestCell(t.Context(), root.ID)
			if err != nil {
				t.Fatal(err)
			}
			directory := filepath.Join(t.TempDir(), "new directory ")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			directory, err = filepath.EvalSymlinks(directory)
			if err != nil {
				t.Fatal(err)
			}
			request := session.WorkspaceSetRequest{ID: "cd", SessionID: root.ID, ExpectedRevision: root.ConfigRevision, Path: directory}
			result, err := r.SetWorkingDirectory(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if result.Session.WorkingDirectory != directory {
				t.Fatal(result)
			}
			runCellTurn(t, r, root.ID, "after", "42\n")
			next, err := r.store.LatestCell(t.Context(), root.ID)
			if err != nil {
				t.Fatal(err)
			}
			oldTurn, _ := r.store.Turn(t.Context(), old.TurnID)
			nextTurn, _ := r.store.Turn(t.Context(), next.TurnID)
			prior, err := r.store.ConfigurationSession(t.Context(), root.ID, oldTurn.ConfigRevision)
			if err != nil || prior.WorkingDirectory != root.WorkingDirectory {
				t.Fatal(prior, err)
			}
			current, err := r.store.ConfigurationSession(t.Context(), root.ID, nextTurn.ConfigRevision)
			if err != nil || current.WorkingDirectory != directory {
				t.Fatal(current, err)
			}
			if err := os.Remove(directory); err != nil {
				t.Fatal(err)
			}
			retry, err := r.SetWorkingDirectory(t.Context(), request)
			if err != nil || retry.Revision != result.Revision {
				t.Fatal("retry reopened deleted path", retry, err)
			}
		})
	}
}

func TestWorkspaceControlRejectsShellReservationsPinsAndLeavesGrants(t *testing.T) {
	r := workspaceRuntimeTest(t, t.TempDir())
	root := workspaceOwnerTest(t, r, workspaceRepoTest(t))
	target := t.TempDir()
	request := session.WorkspaceSetRequest{ID: "cd", SessionID: root.ID, ExpectedRevision: root.ConfigRevision, Path: target}
	scope, err := r.shells.Capture(string(root.ID))
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := scope.Reserve(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.SetWorkingDirectory(t.Context(), request); !errors.Is(err, store.ErrBusy) {
		t.Fatal("shell reservation ignored", err)
	}
	reservation.Release()
	snapshotRequest := session.WorkspaceRequest{ID: "capture", SnapshotID: "snapshot", SessionID: root.ID}
	if _, err := r.CaptureWorkspace(t.Context(), snapshotRequest); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SetWorkingDirectory(t.Context(), request); !errors.Is(err, store.ErrBusy) {
		t.Fatal("retained snapshot scope changed", err)
	}
	snapshotRequest.ID = "release"
	if _, err := r.ReleaseWorkspace(t.Context(), snapshotRequest); err != nil {
		t.Fatal(err)
	}
	grant, err := r.CreateGrant(t.Context(), session.Grant{ID: "old-directory", SessionID: root.ID, Capability: "files.read", Resource: root.WorkingDirectory})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.SetWorkingDirectory(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	grants, err := r.Grants(t.Context(), root.ID, "", 100)
	if err != nil || len(grants) != 1 || grants[0].Resource != grant.Resource {
		t.Fatal("directory edit remapped authority", grants, err)
	}
	if scope.Context().Err() == nil {
		t.Fatal("old shell generation survived cwd change")
	}
	if _, err := scope.Reserve(false); err == nil {
		t.Fatal("retired shell generation reused")
	}
}

func TestWorkspaceControlGateDoesNotBlockOtherTrees(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	first := createTest(t, r)
	other := createTest(t, r)
	submitTest(t, r, first.ID, "first")
	submitTest(t, r, other.ID, "other")
	gate, drop := r.controlGate(first.TreeID)
	defer drop()
	gate.mu.Lock()
	if _, err := r.claimControlled(t.Context(), first.ID); !errors.Is(err, store.ErrBusy) {
		t.Fatal(err)
	}
	if _, err := r.claimControlled(t.Context(), other.ID); err != nil {
		t.Fatal("other tree stalled", err)
	}
	gate.mu.Unlock()
	if _, err := r.claimControlled(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRunControlInstructionsAndRestart(t *testing.T) {
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	root := createTest(t, r)
	request := session.RunConfigureRequest{ID: "run", SessionID: root.ID, ExpectedRevision: 1, Configuration: session.RunConfiguration{System: "exact root override", Headless: true, CacheKey: "stable", MaxTurns: 2}}
	configured, err := r.ConfigureRun(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, root.ID, "first")
	claim, err := r.store.Claim(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	instructions, err := r.Instructions(t.Context(), claim.Turn, claim.Configuration.Instructions)
	if err != nil || instructions != request.Configuration.System {
		t.Fatal(instructions, err)
	}
	if _, err := r.ConfigureRun(t.Context(), session.RunConfigureRequest{ID: "busy", SessionID: root.ID, ExpectedRevision: 2}); !errors.Is(err, store.ErrBusy) {
		t.Fatal(err)
	}
	if _, err := r.store.Finish(t.Context(), claim.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, directory, model.Scripted{})
	again, err := reopened.ConfigureRun(t.Context(), request)
	if err != nil || again.Revision != configured.Revision || again.Session.Config.Run.CacheKey != "stable" {
		t.Fatal(again, err)
	}
	cleared, err := reopened.ConfigureRun(t.Context(), session.RunConfigureRequest{ID: "clear", SessionID: root.ID, ExpectedRevision: 2, Configuration: session.RunConfiguration{}})
	if err != nil {
		t.Fatal(err)
	}
	submitTest(t, reopened, root.ID, "second")
	second, err := reopened.store.Claim(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	instructions, err = reopened.Instructions(t.Context(), second.Turn, cleared.Session.Config.Instructions)
	if err != nil || instructions == request.Configuration.System || instructions == "" {
		t.Fatal(instructions, err)
	}
}

func TestWorkspaceControlCloseCancelsWaitingAction(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	root := createTest(t, r)
	entry, err := r.controlMCPRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	release, err := takeMCPSlot(t.Context(), entry.actions)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	finished := make(chan error, 1)
	target := t.TempDir()
	go func() {
		_, err := r.SetWorkingDirectory(t.Context(), session.WorkspaceSetRequest{ID: "close", SessionID: root.ID, ExpectedRevision: 1, Path: target})
		finished <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(r.workspaceSlots) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("control did not start")
		}
		time.Sleep(time.Millisecond)
	}
	closed := make(chan error, 1)
	go func() { closed <- r.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not join cancelled control")
	}
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestWorkspaceControlRetiresMCPRootWithoutRebindingOldResources(t *testing.T) {
	isolateMCP(t)
	url, _, _ := mcpHTTPFixture(t)
	r := openTest(t, t.TempDir(), model.Scripted{})
	root := createTest(t, r)
	configureMCPFixture(t, r, url)
	if _, err := r.MCPRefresh(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	old := awaitMCPReady(t, r, root)
	entry, err := r.mcpRoot(t.Context(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.SetWorkingDirectory(t.Context(), session.WorkspaceSetRequest{ID: "cd", SessionID: root.ID, ExpectedRevision: root.ConfigRevision, Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !entry.retired || r.mcpManager(entry) != nil {
		t.Fatal("old MCP generation still accessible")
	}
	if current, err := r.mcpRoot(t.Context(), *result.Session, false); err != nil || current != nil {
		t.Fatal("cwd change implicitly started MCP", current, err)
	}
	if _, err := r.MCPRefresh(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if next := awaitMCPReady(t, r, *result.Session); next == old {
		t.Fatal("old manager reused after cwd change")
	}
}
