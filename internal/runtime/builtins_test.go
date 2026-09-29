package runtime

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestNativeBuiltinPersonasBothEnginesAndRestart(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{"read": "print(files.read(path=\"proof.txt\"))", "delegate": "agents.list()"}
			if engine == session.QuickJS {
				codes = map[string]string{"read": "console.log(await files.read({path:'proof.txt'}))", "delegate": "await agents.list({})"}
			}
			var mu sync.Mutex
			instructions := map[session.SessionID]string{}
			base := cellProvider(codes)
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				mu.Lock()
				instructions[request.SessionID] = request.Instructions
				mu.Unlock()
				return base(ctx, request)
			})
			directory := t.TempDir()
			r := openEngineTest(t, directory, provider)
			refs, err := r.Builtins()
			if err != nil {
				t.Fatal(err)
			}
			roots := map[string]session.Session{}
			for _, name := range []string{"coding", "junior-developer"} {
				index := slices.IndexFunc(refs, func(ref session.DefinitionRef) bool { return ref.ID == name })
				if index < 0 {
					t.Fatalf("missing %s", name)
				}
				working := t.TempDir()
				for filename, body := range map[string]string{"proof.txt": "native persona proof", "CLAUDE.md": "CLAUDE_PERSONA_RULE", "AGENTS.md": "AGENTS_PERSONA_RULE"} {
					if err := os.WriteFile(filepath.Join(working, filename), []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				_, root, err := r.CreateTree(t.Context(), store.CreateTree{Metadata: session.TreeMetadata{Title: new("fixture")}, Engine: engine, Definition: refs[index], WorkingDirectory: working, Overrides: session.ConfigPatch{Model: &session.ModelSelection{Provider: "scripted", Name: "scripted"}}})
				if err != nil {
					t.Fatal(err)
				}
				roots[name] = root
				// Defining syntax does not grant filesystem authority.
				grants, err := r.Grants(t.Context(), root.ID, "", 100)
				if err != nil || len(grants) != 0 {
					t.Fatal("builtin minted grants", grants, err)
				}
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID("read_" + name), SessionID: root.ID, Capability: "files.read", Resource: working}); err != nil {
					t.Fatal(err)
				}
				// Use distinct receipt IDs while giving the provider one concrete task.
				key := name + "_read"
				_, err = r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key}, store.Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "read"}}})
				if err != nil {
					t.Fatal(err)
				}
				admission := waitTestWithin(t, r, key, terminal, 30*time.Second)
				if admission.Turn.State != session.Succeeded {
					t.Fatalf("turn=%+v failure=%v", admission.Turn, *admission.Turn.Failure)
				}
				history, err := r.History(t.Context(), root.ID, 0, 100)
				if err != nil || !strings.Contains(history[len(history)-1].Parts[0].Text, "native persona proof") {
					t.Fatal("native read failed", history, err)
				}
				mu.Lock()
				guide := instructions[root.ID]
				mu.Unlock()
				if !strings.Contains(guide, root.Config.Instructions.Text) || !strings.Contains(guide, "CLAUDE_PERSONA_RULE") || !strings.Contains(guide, "AGENTS_PERSONA_RULE") {
					t.Fatal("persona or project instructions absent", guide)
				}
			}
			junior := roots["junior-developer"]
			bindingCellFailure(t, r, junior.ID, "delegate", "agents")
			if _, err := r.UpdateConfiguration(t.Context(), junior.ID, junior.ConfigRevision, session.ConfigPatch{Modules: append(slices.Clone(junior.Config.Modules), "agents")}); err == nil {
				t.Fatal("junior ceiling broadened")
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := openEngineTest(t, directory, provider)
			current, err := reopened.Session(t.Context(), junior.ID)
			if err != nil || current.Definition != junior.Definition || slices.Contains(current.Config.Modules, "agents") {
				t.Fatal("restart changed persona", current, err)
			}
			_, err = reopened.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "restart_delegate"}, store.Submission{SessionID: junior.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "delegate"}}})
			if err != nil {
				t.Fatal(err)
			}
			admission := waitTestWithin(t, reopened, "restart_delegate", terminal, 30*time.Second)
			if admission.Turn.State != session.Succeeded {
				t.Fatal(admission.Turn)
			}
			operations, err := reopened.store.Operations(t.Context(), admission.Turn.ID, "", 100)
			if err != nil || len(operations) != 0 {
				t.Fatal("restart enabled delegation", operations, err)
			}
		})
	}
}
