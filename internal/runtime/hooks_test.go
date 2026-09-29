package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/executor"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func hookRoot(t *testing.T, r *Runtime, engine session.Engine, document session.DefinitionDocument) (session.Session, session.DefinitionRevision) {
	t.Helper()
	definition, err := r.RegisterDefinition(t.Context(), document)
	if err != nil {
		t.Fatal(err)
	}
	_, root, err := r.CreateTree(t.Context(), store.CreateTree{Engine: engine, Definition: definition.Ref, WorkingDirectory: t.TempDir(), Overrides: session.ConfigPatch{Model: &session.ModelSelection{Provider: "test", Name: "test"}}})
	if err != nil {
		t.Fatal(err)
	}
	return root, definition
}

func settleHook(t *testing.T, peer *executor.Peer, event executor.Event, result executor.Result) {
	t.Helper()
	if event.Invocation == nil || event.Invocation.Kind != executor.Hook {
		t.Fatal("not a hook invocation", event)
	}
	if err := peer.Settle(event.Epoch, event.Generation, event.ID, executor.Hook, result); err != nil {
		t.Fatal(err)
	}
}

func TestBothEnginesHooksRewriteBeforePermissionAndGateCustomTools(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := "print(files.read(path=\"a\")[\"output\"])\ntools.lookup(id=\"item\")"
			if engine == session.QuickJS {
				code = "console.log((await files.read({path:'a'})).output); await tools.lookup({id:'item'});"
			}
			base := cellProvider(map[string]string{"run": code})
			seen, resume := make(chan model.Request, 1), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(resume) })
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.Messages[len(request.Messages)-1].Role == session.Tool {
					seen <- request
					select {
					case <-resume:
					case <-ctx.Done():
						return model.Response{}, ctx.Err()
					}
				}
				return base(ctx, request)
			})
			r := openEngineTest(t, t.TempDir(), provider)
			t.Cleanup(unblock)
			document := executorDefinition()
			document.Defaults.Modules = []string{"files"}
			document.Defaults.Hooks = map[string]session.HookDeclaration{"turn_start": {}, "before_tool": {Operations: []string{"files.read", "tools.lookup"}}}
			root, definition := hookRoot(t, r, engine, document)
			peer, _ := executorPeer(t, r, definition)
			if err := os.WriteFile(filepath.Join(root.WorkingDirectory, "b"), []byte("rewritten"), 0o600); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, root.ID, "run")
			start := executorNext(t, peer)
			if start.Invocation.Name != "turn_start" || start.Invocation.Request.Input != "run" {
				t.Fatal("wrong start hook", start)
			}
			settleHook(t, peer, start, executor.Result{Decision: "deny", Context: "exact start contribution"})
			before := executorNext(t, peer)
			if before.Invocation.Name != "before_tool" || before.Invocation.Request.Operation != "files.read" || before.Invocation.Request.PermissionMode != "prompt" {
				t.Fatal("wrong tool hook", before)
			}
			settleHook(t, peer, before, executor.Result{Arguments: json.RawMessage(`{"path":"b"}`), Reason: "choose existing file"})
			permission := awaitRuntimeFilePermission(t, r, root.ID, "run", "files.read")
			if !strings.Contains(string(permission.Arguments), `"path":"b"`) {
				t.Fatal("permission did not cover rewritten intent", permission)
			}
			if _, err := r.ResolvePermission(t.Context(), permission.ID, true); err != nil {
				t.Fatal(err)
			}
			custom := executorNext(t, peer)
			if custom.Invocation.Kind != executor.Hook || custom.Invocation.Request.Operation != "tools.lookup" {
				t.Fatal("custom tool bypassed hook", custom)
			}
			settleHook(t, peer, custom, executor.Result{Decision: "deny", Reason: "custom denied"})
			var request model.Request
			select {
			case request = <-seen:
			case <-time.After(30 * time.Second):
				t.Fatal("next model call missing")
			}
			if !strings.Contains(request.Instructions, "exact start contribution") || !strings.Contains(request.Instructions, "Hook before_tool rewrote files.read") {
				t.Fatal("ephemeral hook context missing", request.Instructions)
			}
			activity, err := r.ExecutorActivity(t.Context(), root.ID)
			if err != nil || activity == nil || len(activity.Decisions) != 2 || activity.Decisions[0].Decision != "rewrite" || activity.Decisions[1].Decision != "deny" || activity.Decisions[1].InvocationID != custom.ID {
				t.Fatal("hook decision observation incorrect", activity, err)
			}
			activity.Decisions[0].Reason = "mutated"
			again, _ := r.ExecutorActivity(t.Context(), root.ID)
			if again.Decisions[0].Reason == "mutated" {
				t.Fatal("activity aliases owner")
			}
			operations, err := r.Operations(t.Context(), permission.TurnID, "", 100)
			if err != nil || len(operations) != 1 || operations[0].State != session.OperationSucceeded {
				t.Fatal("hook denial admitted effect", operations, err)
			}
			unblock()
			waitTestWithin(t, r, "run", terminal, 30*time.Second)
			if activity, err := r.ExecutorActivity(t.Context(), root.ID); err != nil || activity != nil {
				t.Fatal("terminal turn retained hook audit", activity, err)
			}
		})
	}
}

func TestRequiredOptionalAndCancelledHooksNeverInventAuthority(t *testing.T) {
	for _, outcome := range []string{"required", "optional", "deny_optional", "cancel_optional", "timeout"} {
		t.Run(outcome, func(t *testing.T) {
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"run": "files.read(path=\"a\")"}))
			document := session.DefinitionDocument{ID: "hooked", Name: "Hooked", Defaults: session.ConfigPatch{Modules: []string{"files"}, Hooks: map[string]session.HookDeclaration{"before_tool": {Optional: strings.Contains(outcome, "optional")}}}}
			if outcome == "timeout" {
				document.Defaults.Hooks["before_tool"] = session.HookDeclaration{TimeoutMillis: 20}
			}
			root, definition := hookRoot(t, r, session.Starlark, document)
			peer, _ := executorPeer(t, r, definition)
			if err := os.WriteFile(filepath.Join(root.WorkingDirectory, "a"), []byte("value"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "read", SessionID: root.ID, Capability: "files.read", Resource: root.WorkingDirectory}); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, root.ID, "run")
			event := executorNext(t, peer)
			switch outcome {
			case "required", "optional":
				settleHook(t, peer, event, executor.Result{Failure: "handler failure"})
			case "deny_optional":
				settleHook(t, peer, event, executor.Result{Decision: "deny"})
			case "cancel_optional":
				if _, err := r.CancelTurn(t.Context(), event.Invocation.Request.TurnID); err != nil {
					t.Fatal(err)
				}
			}
			admission := waitTestWithin(t, r, "run", terminal, 30*time.Second)
			operations, err := r.Operations(t.Context(), admission.Turn.ID, "", 100)
			want := 0
			if outcome == "optional" {
				want = 1
			}
			if err != nil || len(operations) != want {
				t.Fatal("wrong hook effect disposition", outcome, operations, err)
			}
			if outcome == "cancel_optional" && admission.Turn.State != session.Cancelled {
				t.Fatal("optional hook ignored cancellation", admission.Turn)
			}
			if outcome == "timeout" || outcome == "cancel_optional" {
				if event := executorNext(t, peer); event.Type != "cancel" {
					t.Fatal("missing hook cancellation", event)
				}
			}
		})
	}
}

func TestSpawnHookSeesResolvedChildAndRewriteCannotWiden(t *testing.T) {
	r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"run": "agents.spawn(prompt=\"child\", grant_ids=[])"}))
	document := session.DefinitionDocument{ID: "parent", Name: "Parent", Defaults: session.ConfigPatch{Modules: []string{"agents"}, Hooks: map[string]session.HookDeclaration{"before_spawn": {}}}}
	root, definition := hookRoot(t, r, session.Starlark, document)
	peer, _ := executorPeer(t, r, definition)
	submitTest(t, r, root.ID, "run")
	event := executorNext(t, peer)
	var preview struct {
		Resolved store.ChildPreview `json:"resolved"`
	}
	if err := json.Unmarshal(event.Invocation.Request.Spawn, &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Resolved.Configuration.HooksDefinition == nil || *preview.Resolved.Configuration.HooksDefinition != definition.Ref || len(preview.Resolved.GrantIDs) != 0 {
		t.Fatal("spawn hook saw wrong effective child", preview)
	}
	settleHook(t, peer, event, executor.Result{Spawn: json.RawMessage(`{"prompt":"child","grant_ids":[],"overrides":{"modules":["agents","shell"]}}`)})
	admission := waitTestWithin(t, r, "run", terminal, 30*time.Second)
	operations, err := r.Operations(t.Context(), admission.Turn.ID, "", 100)
	if err != nil || len(operations) != 0 {
		t.Fatal("widened spawn reached admission", operations, err)
	}
	children, err := r.Sessions(t.Context(), root.TreeID, "", 100)
	if err != nil || len(children) != 1 {
		t.Fatal("hook created unauthorized child", children, err)
	}
}

func TestTurnStartReleasesWorkerAndLateReplyCannotAttachToNextTurn(t *testing.T) {
	r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"first": "print(1)", "second": "print(2)", "plain": "print(3)"}))
	document := session.DefinitionDocument{ID: "start", Name: "Start", Defaults: session.ConfigPatch{Hooks: map[string]session.HookDeclaration{"turn_start": {}}}}
	root, definition := hookRoot(t, r, session.Starlark, document)
	peer, lease := executorPeer(t, r, definition)
	plain := createEngineSession(t, r, session.Starlark)
	submitTest(t, r, root.ID, "first")
	first := executorNext(t, peer)
	// openEngineTest uses one worker. Another session must make progress while
	// the first turn waits outside any live cell for its contribution.
	runCellTurn(t, r, plain.ID, "plain", "3\n")
	if _, err := r.CancelTurn(t.Context(), first.Invocation.Request.TurnID); err != nil {
		t.Fatal(err)
	}
	if event := executorNext(t, peer); event.Type != "cancel" {
		t.Fatal(event)
	}
	waitTestWithin(t, r, "first", terminal, 30*time.Second)
	submitTest(t, r, root.ID, "second")
	second := executorNext(t, peer)
	if err := peer.Settle(lease.Epoch, lease.Generation, first.ID, executor.Hook, executor.Result{Context: "stale contribution"}); !errors.Is(err, executor.ErrConflict) {
		t.Fatal("late start reply accepted", err)
	}
	settleHook(t, peer, second, executor.Result{Context: "current contribution"})
	admission := waitTestWithin(t, r, "second", terminal, 30*time.Second)
	if admission.Turn.State != session.Succeeded {
		t.Fatal("next turn failed", admission.Turn)
	}
}

func TestSpawnHookRewriteIsRecheckedAfterPermissionAndGrantRevocation(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "revoked"}[revoke], func(t *testing.T) {
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"run": "agents.spawn(prompt=\"original\", grant_ids=[\"read\"])", "rewritten": "print(1)"}))
			document := session.DefinitionDocument{ID: "parent", Name: "Parent", Defaults: session.ConfigPatch{Modules: []string{"agents", "files"}, Hooks: map[string]session.HookDeclaration{"before_spawn": {}}}}
			root, definition := hookRoot(t, r, session.Starlark, document)
			peer, _ := executorPeer(t, r, definition)
			if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "read", SessionID: root.ID, Capability: "files.read", Resource: root.WorkingDirectory}); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, root.ID, "run")
			event := executorNext(t, peer)
			settleHook(t, peer, event, executor.Result{Spawn: json.RawMessage(`{"prompt":"rewritten","grant_ids":["read"]}`)})
			permission := awaitRuntimeFilePermission(t, r, root.ID, "run", "agents.spawn")
			if !strings.Contains(string(permission.Arguments), "rewritten") {
				t.Fatal("permission lost rewrite", permission)
			}
			if revoke {
				if _, err := r.RevokeGrant(t.Context(), "read"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.ResolvePermission(t.Context(), permission.ID, true); err != nil {
				t.Fatal(err)
			}
			waitTestWithin(t, r, "run", terminal, 30*time.Second)
			children, err := r.Sessions(t.Context(), root.TreeID, "", 100)
			if err != nil {
				t.Fatal(err)
			}
			expected := 2
			if revoke {
				expected = 1
			}
			if len(children) != expected {
				t.Fatal("transaction did not recheck grant", children)
			}
		})
	}
}

func TestHookPolicyIsCapturedEvenWhenNextConfigurationRemovesIt(t *testing.T) {
	r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"run": "files.read(path=\"a\")\nprint(files.read(path=\"a\"))"}))
	document := session.DefinitionDocument{ID: "captured-hooks", Name: "Captured", Defaults: session.ConfigPatch{Modules: []string{"files"}, Hooks: map[string]session.HookDeclaration{"before_tool": {Optional: true}}}}
	root, definition := hookRoot(t, r, session.Starlark, document)
	peer, _ := executorPeer(t, r, definition)
	if err := os.WriteFile(filepath.Join(root.WorkingDirectory, "a"), []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "read", SessionID: root.ID, Capability: "files.read", Resource: root.WorkingDirectory}); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, root.ID, "run")
	first := executorNext(t, peer)
	if _, err := r.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Hooks: map[string]session.HookDeclaration{}}); err != nil {
		t.Fatal(err)
	}
	settleHook(t, peer, first, executor.Result{})
	second := executorNext(t, peer)
	settleHook(t, peer, second, executor.Result{Decision: "deny"})
	admitted := waitTestWithin(t, r, "run", terminal, 30*time.Second)
	operations, err := r.Operations(t.Context(), admitted.Turn.ID, "", 100)
	if err != nil || len(operations) != 1 {
		t.Fatal("captured hook disappeared", operations, err)
	}
}

func TestExecutorActivityBoundsAndExactTurnOwnership(t *testing.T) {
	r := &Runtime{active: map[session.SessionID]*execution{"owner": {turn: "current"}}}
	for range 20 {
		r.hookDecision("owner", "current", HookDecision{Hook: "before_tool", Decision: "skipped", Reason: strings.Repeat("😀", 1024)})
		r.hookNotice("owner", "current", strings.Repeat("😀", 1024))
	}
	active := r.active["owner"]
	if len(active.executorActivity.Decisions) != 8 || !active.executorActivity.Truncated || len(active.executorActivity.Decisions[0].Reason) > 512 {
		t.Fatal("unbounded hook audit", active.executorActivity)
	}
	if notice := r.TurnNotices(session.Turn{SessionID: "owner", ID: "current"}); len(notice) > 2048 || !utf8.ValidString(notice) {
		t.Fatal("unbounded hook context")
	}
	revision := active.executorActivity.Revision
	r.hookDecision("owner", "old", HookDecision{Decision: "deny"})
	r.hookNotice("owner", "old", "stale")
	r.executorProgress("owner", "old", &ExecutorProgress{Text: "stale"})
	if active.executorActivity.Revision != revision || active.executorActivity.Progress != nil {
		t.Fatal("late executor callback attached to another turn")
	}
}

func TestChildExecutesToolsAndHooksThroughTheirDistinctCanonicalOwners(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := "tools.lookup(id=\"item\")"
			if engine == session.QuickJS {
				code = "await tools.lookup({id:'item'})"
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"run": code}))
			document := executorDefinition()
			document.ID = "parent-source"
			document.Defaults.Hooks = map[string]session.HookDeclaration{"before_tool": {}}
			parent, parentDefinition := hookRoot(t, r, engine, document)
			hookPeer, _ := executorPeer(t, r, parentDefinition)
			childDocument := executorDefinition()
			childDocument.ID = "child-source"
			childDefinition, err := r.RegisterDefinition(t.Context(), childDocument)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "custom", SessionID: parent.ID, Capability: "tools.lookup", Resource: childDefinition.Ref.ID + "@" + childDefinition.Ref.Revision}); err != nil {
				t.Fatal(err)
			}
			spawned, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "run"}, store.ChildRequest{ParentID: parent.ID, Definition: &childDefinition.Ref, Parts: []session.Part{{Type: "text", Text: "run"}}, GrantIDs: []session.GrantID{"custom"}})
			if err != nil {
				t.Fatal(err)
			}
			child := *spawned.Session
			if child.Config.ToolsDefinition == nil || *child.Config.ToolsDefinition != childDefinition.Ref || child.Config.HooksDefinition == nil || *child.Config.HooksDefinition != parentDefinition.Ref {
				t.Fatal("mixed provenance lost", child.Config)
			}
			toolPeer, toolLease := executorPeer(t, r, childDefinition)
			hook := executorNext(t, hookPeer)
			if hook.Invocation.Request.SessionID != child.ID || hook.Invocation.Kind != executor.Hook {
				t.Fatal("hook dispatched to wrong owner", hook)
			}
			settleHook(t, hookPeer, hook, executor.Result{})
			custom := executorNext(t, toolPeer)
			if custom.Invocation.Lease.Definition != childDefinition.Ref || custom.Invocation.Request.SessionID != child.ID {
				t.Fatal("tool dispatched to inherited owner", custom)
			}
			if err := toolPeer.Settle(toolLease.Epoch, toolLease.Generation, custom.ID, executor.Tool, executor.Result{Value: json.RawMessage(`{"value":9007199254740993}`)}); err != nil {
				t.Fatal(err)
			}
			waitTestWithin(t, r, "run", terminal, 30*time.Second)
			operation, err := r.Operation(t.Context(), custom.Invocation.Request.OperationID)
			if err != nil || operation.State != session.OperationSucceeded || string(operation.Result.Value) != `{"value":9007199254740993}` {
				t.Fatal("exact result lost", operation, err)
			}
		})
	}
}
