package runtime

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func awaitReload(t *testing.T, r *Runtime, owner session.SessionID, id string) session.ReloadEdit {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		result, err := r.ReloadEdit(ctx, owner, id)
		if err != nil {
			t.Fatal(err)
		}
		if result.State != session.ReloadPending {
			return result
		}
		select {
		case <-ctx.Done():
			t.Fatal("reload pending", r.Err())
		case <-tick.C:
		}
	}
}

func TestBothEnginesReloadCapturesBusyChildSettingsBeforeRestartAndPreservesREPL(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{"warm": "saved = 41\nprint(saved)", "after": "print(saved + 1)", "busy": "print(7)"}
			if engine == session.QuickJS {
				codes = map[string]string{"warm": "var saved = 41; console.log(saved)", "after": "console.log(saved + 1)", "busy": "console.log(7)"}
			}
			entered := make(chan struct{})
			var once sync.Once
			scripted := cellProvider(codes)
			base := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				last := request.Messages[len(request.Messages)-1]
				if last.Role == session.User {
					for _, candidate := range slices.Backward(request.Messages) {
						if candidate.Role == session.User && len(candidate.Parts) > 0 {
							if _, known := codes[candidate.Parts[0].Text]; known {
								request.Messages = append(slices.Clip(request.Messages), candidate)
								break
							}
						}
					}
				}
				return scripted(ctx, request)
			})
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				last := request.Messages[len(request.Messages)-1]
				if last.Role == session.User && last.Parts[0].Text == "busy" {
					once.Do(func() { close(entered) })
					<-ctx.Done()
					return model.Response{}, ctx.Err()
				}
				return base(ctx, request)
			})
			directory := t.TempDir()
			r := openEngineTest(t, directory, provider)
			root := createEngineSession(t, r, engine)
			runCellTurn(t, r, root.ID, "warm", "41\n")
			child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "busy"}, store.ChildRequest{ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "busy"}}})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(30 * time.Second):
				t.Fatal("child did not run")
			}
			mode := setRuntimeMode(t, r, root.ID, "automatic", 1, session.PermissionAutomatic)
			deny, err := r.SetPermissionDenial(t.Context(), session.PermissionDenialRequest{ID: "deny", SessionID: root.ID, ExpectedRevision: mode.Policy.Revision, DenyInteractive: true})
			if err != nil {
				t.Fatal(err)
			}
			grant, err := r.CreateGrant(t.Context(), session.Grant{ID: "kept", SessionID: root.ID, Capability: "files.read", Resource: root.WorkingDirectory})
			if err != nil {
				t.Fatal(err)
			}
			setHostForTest(t, r, func(host *config.Host) { host.Defaults.Compaction.ThresholdPercent = 75 })
			capture, err := r.ReloadSession(t.Context(), session.ReloadRequest{ID: "Reload.Mixed", SessionID: root.ID, ExpectedRevision: 1})
			if err != nil || capture.State != session.ReloadPending || capture.Configuration.Compaction.ThresholdPercent != 75 {
				t.Fatal(capture, err)
			}
			setHostForTest(t, r, func(host *config.Host) { host.Defaults.Compaction.ThresholdPercent = 90 })
			if _, err := r.ReloadSession(t.Context(), session.ReloadRequest{ID: "second", SessionID: root.ID, ExpectedRevision: 1}); !errors.Is(err, store.ErrBusy) {
				t.Fatal(err)
			}
			retry, err := r.ReloadSession(t.Context(), capture.ReloadRequest)
			if err != nil || !reflect.DeepEqual(retry, capture) {
				t.Fatal(retry, err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			r = openEngineTest(t, directory, base)
			applied := awaitReload(t, r, root.ID, capture.ID)
			if applied.State != session.ReloadApplied || *applied.Revision != 2 || applied.HostRevision != capture.HostRevision {
				t.Fatal(applied)
			}

			runCellTurn(t, r, root.ID, "after", "42\n")
			actual, err := r.Session(t.Context(), root.ID)
			if err != nil || actual.Config.Compaction.ThresholdPercent != 75 || actual.WorkingDirectory != root.WorkingDirectory || !actual.Config.Model.Equal(root.Config.Model) {
				t.Fatal(actual, err)
			}
			retained, err := r.Session(t.Context(), child.Session.ID)
			if err != nil || retained.ConfigRevision != 1 || retained.Config.Compaction.ThresholdPercent != 50 {
				t.Fatal(retained, err)
			}
			policy, err := r.PermissionPolicy(t.Context(), root.ID)
			if err != nil || policy != deny.Policy {
				t.Fatal(policy, err)
			}
			grants, err := r.Grants(t.Context(), root.ID, "", 10)
			if err != nil || len(grants) != 1 || grants[0].ID != grant.ID || grants[0].RevokedAt != nil {
				t.Fatal(grants, err)
			}
			retry, err = r.ReloadSession(t.Context(), capture.ReloadRequest)
			if err != nil || !reflect.DeepEqual(retry, applied) {
				t.Fatal(retry, err)
			}
		})
	}
}

func TestReloadWaitsForExistingShellAndControlLifetimeWithoutStartingResources(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	root := createTest(t, r)
	scope, err := r.shells.Capture(string(root.ID))
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := scope.Reserve(true)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := r.ReloadSession(t.Context(), session.ReloadRequest{ID: "reload", SessionID: root.ID, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	candidate := store.PendingReload{ID: capture.ID, SessionID: root.ID, TreeID: root.TreeID}
	if err := r.applyReload(t.Context(), candidate); !errors.Is(err, store.ErrBusy) {
		t.Fatal(err)
	}
	reservation.Release()
	if _, err := r.SetLifecycle(t.Context(), root.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	scope, err = r.shells.Capture(string(root.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.applyReload(t.Context(), candidate); err != nil {
		t.Fatal(err)
	}
	if scope.Context().Err() != nil {
		t.Fatal("reload retired unrelated shell ownership")
	}
	r.mcp.mu.Lock()
	retained := len(r.mcp.roots)
	r.mcp.mu.Unlock()
	if retained != 0 {
		t.Fatal("reload retained or started a manager", retained)
	}
	if result, err := r.CancelReload(t.Context(), root.ID, capture.ID); err != nil || result.State != session.ReloadApplied {
		t.Fatal(result, err)
	}
	if err := r.applyReload(t.Context(), candidate); err != nil {
		t.Fatal(err)
	}
}

func TestReloadJoinsLanguageServerBeforeNextClaimAndExactRetryKeepsNewLifetime(t *testing.T) {
	code := `print(files.write(path="main.go",content="package main"))`
	r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"first": code, "probe": "print(9)", "recreated": code}))
	setHostForTest(t, r, func(host *config.Host) {
		host.LSP = map[string]lsp.Config{"fixture": {Command: []string{os.Args[0], "-test.run=^TestLanguageServerFixture$"}, Extensions: []string{".go"}, RootMarkers: []string{"go.mod"}, Env: map[string]string{"WHIP_LSP_FIXTURE": "1"}}, "gopls": {Enabled: new(false)}}
	})
	root := createEngineSession(t, r, session.Starlark)
	setRuntimeMode(t, r, root.ID, "automatic", 1, session.PermissionAutomatic)
	run := func(key string) {
		t.Helper()
		submitTest(t, r, root.ID, key)
		result := waitTestWithin(t, r, key, terminal, 30*time.Second)
		if result.Turn.State != session.Succeeded {
			t.Fatal(result.Turn)
		}
	}
	status := func(want string) {
		t.Helper()
		values, err := r.LSPStatus(t.Context(), root.ID)
		if err != nil || len(values) != 1 || values[0].State != want {
			t.Fatal(values, err)
		}
	}
	run("first")
	status("connected")
	captured, err := r.ReloadSession(t.Context(), session.ReloadRequest{ID: "reload", SessionID: root.ID, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	applied := awaitReload(t, r, root.ID, captured.ID)
	if applied.State != session.ReloadApplied {
		t.Fatal(applied)
	}
	runCellTurn(t, r, root.ID, "probe", "9\n")
	status("not started")
	run("recreated")
	status("connected")
	if _, err := r.ReloadSession(t.Context(), captured.ReloadRequest); err != nil {
		t.Fatal(err)
	}
	status("connected")
}
