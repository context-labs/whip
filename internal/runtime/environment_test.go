package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestNativeEnvironmentReachesBothEnginesAndKeepsCapturedOwner(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			requests := make(chan model.Request, 8)
			code := "print(42)"
			if engine == session.QuickJS {
				code = "console.log(42)"
			}
			base := cellProvider(map[string]string{"before": code, "after": code, "child": code})
			r := openEngineTest(t, t.TempDir(), providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				requests <- request
				return base(ctx, request)
			}))
			root := createEngineSession(t, r, engine)
			writeInstructionFile(t, filepath.Join(root.WorkingDirectory, "AGENTS.md"), "UNGRANTED_PROJECT_RULE")
			runCellTurn(t, r, root.ID, "before", "42\n")
			first, followup := nextInstructionRequest(t, requests), nextInstructionRequest(t, requests)
			turn, err := r.Turn(t.Context(), first.TurnID)
			if err != nil {
				t.Fatal(err)
			}
			for _, marker := range []string{"Identity: root agent", string(root.ID), string(root.TreeID), root.Definition.Revision, "Platform: " + runtime.GOOS + "/" + runtime.GOARCH, "User:", root.WorkingDirectory, turn.StartedAt.UTC().Format(time.RFC3339Nano)} {
				if !strings.Contains(first.Instructions, marker) {
					t.Fatal("missing environment fact", marker, first.Instructions)
				}
			}
			if first.Instructions != followup.Instructions || strings.Contains(first.Instructions, "UNGRANTED_PROJECT_RULE") {
				t.Fatal("environment changed within a turn or granted implicit project access")
			}
			manifest, err := r.InstructionManifest(t.Context(), first.TurnID)
			digest := sha256.Sum256([]byte(first.Instructions))
			if err != nil || manifest == nil || manifest.SHA256 != hex.EncodeToString(digest[:]) || len(manifest.Sources) != 0 {
				t.Fatal("environment missing from digest or invented file provenance", manifest, err)
			}
			directory := filepath.Join(t.TempDir(), "new <env>\n directory ")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			changed, err := r.SetWorkingDirectory(t.Context(), session.WorkspaceSetRequest{ID: "environment-directory", SessionID: root.ID, ExpectedRevision: root.ConfigRevision, Path: directory})
			if err != nil {
				t.Fatal(err)
			}
			runCellTurn(t, r, root.ID, "after", "42\n")
			after, afterFollowup := nextInstructionRequest(t, requests), nextInstructionRequest(t, requests)
			quoted, _ := json.Marshal(changed.Session.WorkingDirectory)
			if after.Instructions != afterFollowup.Instructions || !strings.Contains(after.Instructions, string(quoted)) || strings.Contains(after.Instructions, root.WorkingDirectory) || strings.Contains(after.Instructions, directory) {
				t.Fatal("new turn did not quote its newly captured directory", after.Instructions)
			}
			prior, err := r.store.ConfigurationSession(t.Context(), root.ID, turn.ConfigRevision)
			if err != nil || prior.WorkingDirectory != root.WorkingDirectory {
				t.Fatal("historical owner changed", prior, err)
			}
			child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "environment-child"}, store.ChildRequest{Name: "Workspace reviewer", ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "child"}}})
			if err != nil {
				t.Fatal(err)
			}
			if done := waitTestWithin(t, r, "environment-child", terminal, 30*time.Second); done.Turn.State != session.Succeeded {
				t.Fatal(done)
			}
			childRequest, childFollowup := nextInstructionRequest(t, requests), nextInstructionRequest(t, requests)
			if childRequest.Instructions != childFollowup.Instructions || !strings.Contains(childRequest.Instructions, `Identity: child agent "Workspace reviewer" (session `+string(child.Session.ID)) || !strings.Contains(childRequest.Instructions, "parent session "+string(root.ID)) || !strings.Contains(childRequest.Instructions, "Successful completion sends no automatic notice") || !strings.Contains(childRequest.Instructions, string(quoted)) {
				t.Fatal("child received the wrong identity or reporting behavior", childRequest.Instructions)
			}
		})
	}
}
