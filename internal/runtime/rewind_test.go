package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func rewindRuntimeRequest(t *testing.T, r *Runtime, owner session.SessionID, keep int64) session.RewindRequest {
	t.Helper()
	snapshot, err := r.store.HistorySnapshot(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	return session.RewindRequest{ID: "rewind", SessionID: owner, ExpectedRevision: snapshot.Revision, ObservedThrough: snapshot.ThroughSequence, KeepThrough: keep}
}

func TestRewindBothEnginesResetWithoutReplayAcrossDisposalAndRestart(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		for _, mode := range []string{"active_idle_partial", "immediate_partial", "delayed_keep_all", "restart_keep_none"} {
			t.Run(string(engine)+"/"+mode, func(t *testing.T) {
				codes := map[string]string{
					"first":  "x = 41\nfiles.write(path=\"effect.txt\", content=\"effect survives\")\nprint(x)",
					"second": "x += 1\nprint(x)", "probe": "print(x)", "new": "x = 7\nprint(x)", "after_retry": "x += 1\nprint(x)",
				}
				if engine == session.QuickJS {
					codes = map[string]string{
						"first":  "var x = 41; await files.write({path: 'effect.txt', content: 'effect survives'}); console.log(x)",
						"second": "x += 1; console.log(x)", "probe": "console.log(x)", "new": "var x = 7; console.log(x)", "after_retry": "x += 1; console.log(x)",
					}
				}
				entered, release := make(chan struct{}), make(chan struct{})
				base := cellProvider(codes)
				provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
					if request.Messages[len(request.Messages)-1].Role == session.Tool {
						for _, message := range slices.Backward(request.Messages) {
							if message.Role != session.User {
								continue
							}
							if message.Parts[0].Text == "new" {
								close(entered)
								select {
								case <-release:
								case <-ctx.Done():
									return model.Response{}, ctx.Err()
								}
							}
							break
						}
					}
					return base(ctx, request)
				})
				directory := t.TempDir()
				r := openEngineTest(t, directory, provider)
				owner := createEngineSession(t, r, engine)
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "write", SessionID: owner.ID, Capability: "files.write", Resource: owner.WorkingDirectory}); err != nil {
					t.Fatal(err)
				}
				runCellTurn(t, r, owner.ID, "first", "41\n")
				runCellTurn(t, r, owner.ID, "second", "42\n")
				oldCell, err := r.store.LatestCell(t.Context(), owner.ID)
				if err != nil {
					t.Fatal(err)
				}
				r.mu.Lock()
				oldKernel := r.kernels[owner.ID]
				r.mu.Unlock()
				if oldKernel == nil || oldKernel.historyRevision != 1 {
					t.Fatal("fixture has no original live kernel")
				}
				if mode != "active_idle_partial" {
					if _, err := r.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
						t.Fatal(err)
					}
				}
				keep := int64(4)
				switch mode {
				case "delayed_keep_all":
					keep = 8
				case "restart_keep_none":
					keep = 0
				}
				request := rewindRuntimeRequest(t, r, owner.ID, keep)
				var edit session.HistoryEdit
				if mode == "immediate_partial" || mode == "active_idle_partial" {
					edit, err = r.Rewind(t.Context(), request)
				} else {
					// Deliberately omit postcommit cache disposal. The durable revision
					// must independently guard both a live loader and process restart.
					edit, err = r.store.Rewind(t.Context(), request)
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, err := oldKernel.checkpoints.Load(t.Context()); !errors.Is(err, store.ErrConflict) {
					t.Fatal("old loader crossed history revision", err)
				}
				if cell, err := r.store.LatestCell(t.Context(), owner.ID); err != nil || cell != nil {
					t.Fatal("rewind exposed a previous checkpoint", cell, err)
				}
				if exact, err := r.Cell(t.Context(), oldCell.ID); err != nil || !reflect.DeepEqual(exact, *oldCell) {
					t.Fatal("rewind erased exact execution evidence", exact, err)
				}
				if mode == "restart_keep_none" {
					if err := r.Close(); err != nil {
						t.Fatal(err)
					}
					r = openEngineTest(t, directory, provider)
				}
				if _, err := r.SetLifecycle(t.Context(), owner.ID, session.Active); err != nil {
					t.Fatal(err)
				}
				submitTest(t, r, owner.ID, "probe")
				probe := waitTestWithin(t, r, "probe", terminal, 30*time.Second)
				if probe.Turn.HistoryRevision != edit.Revision {
					t.Fatal("resumed turn captured retired revision", probe.Turn)
				}
				cell, err := r.store.LatestCell(t.Context(), owner.ID)
				if err != nil || cell == nil || cell.State != session.CellFailed || cell.ResultMessageID == nil {
					t.Fatal("retired global x was restored", cell, err)
				}
				history, err := r.History(t.Context(), owner.ID, 8, 100)
				if err != nil || len(history) == 0 || history[0].Sequence != 9 {
					t.Fatal("resumed history reused retired coordinates", history, err)
				}
				found := false
				for _, message := range history {
					if message.ID != *cell.ResultMessageID {
						continue
					}
					var payload struct {
						Error string `json:"error"`
					}
					if err := json.Unmarshal([]byte(message.Parts[0].Result.Output), &payload); err != nil {
						t.Fatal(err)
					}
					found = strings.Contains(payload.Error, "x")
				}
				if !found {
					t.Fatal("reset probe did not observe missing global")
				}
				submitTest(t, r, owner.ID, "new")
				select {
				case <-entered:
				case <-time.After(30 * time.Second):
					t.Fatal("new revision failed to execute")
				}
				r.mu.Lock()
				newKernel := r.kernels[owner.ID]
				r.mu.Unlock()
				if newKernel == nil || newKernel == oldKernel || newKernel.historyRevision != edit.Revision {
					t.Fatal("runtime reused stale kernel")
				}
				r.applyHistoryEdit(edit)
				if retried, err := r.Rewind(t.Context(), request); err != nil || !reflect.DeepEqual(retried, edit) {
					t.Fatal("exact retry refused during new active execution", retried, err)
				}
				r.mu.Lock()
				retained := r.kernels[owner.ID] == newKernel
				r.mu.Unlock()
				if !retained {
					t.Fatal("delayed acknowledgement discarded resumed kernel")
				}
				close(release)
				if finished := waitTestWithin(t, r, "new", terminal, 30*time.Second); finished.Turn.State != session.Succeeded {
					t.Fatal(finished.Turn, r.Err())
				}
				runCellTurn(t, r, owner.ID, "after_retry", "8\n")
				assertRuntimeFileBytes(t, filepath.Join(owner.WorkingDirectory, "effect.txt"), "effect survives")
				original := waitTestWithin(t, r, "first", terminal, 30*time.Second)
				operations, err := r.Operations(t.Context(), original.Turn.ID, "", 100)
				if err != nil || len(operations) != 1 || operations[0].State != session.OperationSucceeded {
					t.Fatal("rewind replayed/lost external operation", operations, err)
				}
			})
		}
	}
}
