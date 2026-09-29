package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browserhost"
	"github.com/context-labs/whip/internal/executor"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func publicBrowserFixture(t *testing.T) (*Runtime, session.Session, *browserFixture, browserhost.Attachment) {
	t.Helper()
	r, _ := directRuntime(t)
	root := createTest(t, r)
	fake := startBrowserFixture(t, r, root)
	if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: root.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
		t.Fatal(err)
	}
	admitDirect(t, r, root.ID, "attach", "browser", "attach", `{"tab_id":"human-tab"}`)
	if got := waitTest(t, r, "attach", terminal); got.Turn.State != session.Succeeded {
		t.Fatal(got)
	}
	entries, err := r.BrowserAttachments(t.Context(), root.ID)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	return r, root, fake, entries[0]
}

func transferPublicRequest(owner session.Session, attachment browserhost.Attachment) store.ChildRequest {
	return store.ChildRequest{ParentID: owner.ID, Parts: []session.Part{{Type: "text", Text: "child task"}}, BrowserAttachments: []string{attachment.Scope.AttachmentID}}
}

func settleTransferFixture(t *testing.T, fake *browserFixture, command *browserhost.Command) {
	t.Helper()
	if err := fake.peer.Settle(browserhost.CommandResult{CommandID: command.CommandID, RootID: command.Identity.RootID, ProviderEpoch: command.Scope.ProviderEpoch, AttachmentGeneration: command.Scope.AttachmentGeneration, DocumentRevision: "doc1", URL: "https://example.test", Title: "page", Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
}

func TestPublicBrowserTransferReadCancellationAndLostAcknowledgement(t *testing.T) {
	r, owner, fake, attachment := publicBrowserFixture(t)
	fake.transferBlock.Store(true)
	request := transferPublicRequest(owner, attachment)
	identity := session.RequestIdentity{ClientID: "public", RequestID: "transfer"}
	observer, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := r.SpawnChild(observer, identity, request); done <- err }()
	var command *browserhost.Command
	select {
	case command = <-fake.transferObserved:
	case <-time.After(5 * time.Second):
		t.Fatal("public transfer not dispatched")
	}
	if _, err := r.MatchChild(t.Context(), identity, request); !errors.Is(err, store.ErrBusy) {
		t.Fatal("pending public receipt claimed missing", err)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled observer returned child before ACK")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("observer cancellation blocked")
	}
	op, err := r.Operation(t.Context(), session.OperationID(command.OperationID))
	if err != nil || op.State != session.OperationDispatched || op.DirectTurnID == "" || op.CellID != "" {
		t.Fatal("observer cancelled runtime work", op, err)
	}
	if _, err := r.store.Session(t.Context(), store.TransferChildID(op.ID)); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("child created before ACK", err)
	}
	settleTransferFixture(t, fake, command)
	created, err := r.SpawnChild(t.Context(), identity, request)
	if err != nil || created.Session == nil || created.Admission.Receipt.RequestIdentity != identity {
		t.Fatal(created, err)
	}
	if created.Session.ID != store.TransferChildID(op.ID) {
		t.Fatal(created)
	}
	entries, err := r.BrowserAttachments(t.Context(), created.Session.ID)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	again, err := r.SpawnChild(t.Context(), identity, request)
	if err != nil || again.Session.ID != created.Session.ID || again.Admission.Input.ID != created.Admission.Input.ID {
		t.Fatal(again, err)
	}
	history, err := r.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) > 1 {
		t.Fatal("unexpected parent history", history, err)
	}
	// The admitted child may already have failed and delivered its completion
	// before this read. That canonical mail is independent of the direct transfer;
	// the transfer itself must not invent a parent prompt or tool conversation.
	for _, message := range history {
		if message.TurnID == op.DirectTurnID || message.InputID != nil || message.Mail == nil {
			t.Fatal("public transfer fabricated parent conversation", message)
		}
		mail, err := r.ReadMail(t.Context(), owner.ID, message.Mail.ID)
		if err != nil || mail.Source.Kind != "completion" || mail.Source.ID != string(created.Session.ID) || mail.RecipientID != owner.ID || mail.Revision != message.Mail.Revision {
			t.Fatal("parent message is not the admitted child's completion", mail, err)
		}
	}
	changed := request
	changed.Parts = []session.Part{{Type: "text", Text: "changed"}}
	if _, err := r.SpawnChild(t.Context(), identity, changed); !errors.Is(err, store.ErrConflict) {
		t.Fatal("changed request replayed", err)
	}
	if fake.commands.Load() != 2 {
		t.Fatal("public retry repeated native effect", fake.commands.Load())
	}
}

func TestPublicBrowserTransferDefiniteFailureAndUncertaintyAreNonReplayable(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed-preparation", true: "lost-native-ACK"}[unknown], func(t *testing.T) {
			r, owner, fake, attachment := publicBrowserFixture(t)
			request := transferPublicRequest(owner, attachment)
			identity := session.RequestIdentity{ClientID: "public", RequestID: "failure"}
			want := store.ErrTransferFailed
			if unknown {
				fake.transferBlock.Store(true)
				done := make(chan error, 1)
				go func() { _, err := r.SpawnChild(t.Context(), identity, request); done <- err }()
				select {
				case <-fake.transferObserved:
				case <-time.After(5 * time.Second):
					t.Fatal("no transfer")
				}
				fake.peer.Close()
				want = store.ErrTransferUncertain
				select {
				case err := <-done:
					if !errors.Is(err, want) {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("lost ACK pending")
				}
			} else {
				request.GrantIDs = []session.GrantID{}
				if _, err := r.SpawnChild(t.Context(), identity, request); !errors.Is(err, want) {
					t.Fatal(err)
				}
			}
			before := fake.commands.Load()
			for range 2 {
				if _, err := r.SpawnChild(t.Context(), identity, request); !errors.Is(err, want) {
					t.Fatal("terminal retry changed", err)
				}
				if _, err := r.MatchChild(t.Context(), identity, request); !errors.Is(err, want) {
					t.Fatal("terminal match became missing", err)
				}
			}
			if fake.commands.Load() != before {
				t.Fatal("terminal retry sent effect")
			}
			sessions, err := r.Sessions(t.Context(), owner.TreeID, "", 100)
			if err != nil || len(sessions) != 1 {
				t.Fatal("failed handoff left child", sessions, err)
			}
		})
	}
}

func TestPublicBrowserTransferRequiredHookUsesDirectCapturedTurnAndPreservesOriginalReceipt(t *testing.T) {
	r, _ := directRuntime(t)
	root, definition := hookRoot(t, r, session.Starlark, session.DefinitionDocument{ID: "transfer-hook", Name: "Transfer hook", Defaults: session.ConfigPatch{Modules: []string{"agents", "browser"}, Hooks: map[string]session.HookDeclaration{"before_spawn": {}}}})
	peer, _ := executorPeer(t, r, definition)
	fake := startBrowserFixture(t, r, root)
	if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: root.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
		t.Fatal(err)
	}
	admitDirect(t, r, root.ID, "attach", "browser", "attach", `{"tab_id":"human-tab"}`)
	if got := waitTest(t, r, "attach", terminal); got.Turn.State != session.Succeeded {
		t.Fatal(got)
	}
	entries, err := r.BrowserAttachments(t.Context(), root.ID)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	request := transferPublicRequest(root, entries[0])
	identity := session.RequestIdentity{ClientID: "public", RequestID: "hook-transfer"}
	completed := make(chan store.ChildAdmission, 1)
	failed := make(chan error, 1)
	go func() {
		result, err := r.SpawnChild(t.Context(), identity, request)
		if err != nil {
			failed <- err
		} else {
			completed <- result
		}
	}()
	events := make(chan executor.Event, 1)
	observe, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		event, err := peer.Next(observe)
		if err != nil {
			failed <- err
		} else {
			events <- event
		}
	}()
	var event executor.Event
	select {
	case event = <-events:
	case err := <-failed:
		accepted, _ := r.store.BeginChildTransfer(t.Context(), identity, request)
		t.Fatalf("hook missing: %v turn=%+v", err, accepted.Turn)
	}
	if event.Invocation.Name != "before_spawn" || !event.Invocation.Request.HostOperation || event.Invocation.Request.CellID != "" {
		t.Fatal("wrong direct hook provenance", event)
	}
	var preview struct {
		Request  map[string]any     `json:"request"`
		Resolved store.ChildPreview `json:"resolved"`
	}
	if err := json.Unmarshal(event.Invocation.Request.Spawn, &preview); err != nil {
		t.Fatal(err)
	}
	if _, exists := preview.Request["identity"]; exists {
		t.Fatal("delivery identity exposed to hook rewrite")
	}
	if _, exists := preview.Request["parent_id"]; exists {
		t.Fatal("owner exposed to hook rewrite")
	}
	if len(preview.Resolved.GrantIDs) == 0 {
		t.Fatal("hook missed delegated scope")
	}
	preview.Request["parts"] = []session.Part{{Type: "text", Text: "rewritten child"}}
	rewritten, err := json.Marshal(preview.Request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Modules: []string{"agents"}}); err != nil {
		t.Fatal(err)
	}
	settleHook(t, peer, event, executor.Result{Spawn: rewritten})
	var child store.ChildAdmission
	select {
	case child = <-completed:
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("rewritten transfer pending")
	}
	if child.Admission.Input.Parts[0].Text != "rewritten child" || !slices.Contains(child.Session.Config.Modules, "browser") {
		t.Fatal("lost captured configuration or rewrite", child)
	}
	if got, err := r.MatchChild(t.Context(), identity, request); err != nil || got.Input.ID != child.Admission.Input.ID {
		t.Fatal("original receipt lost", got, err)
	}
	changed := request
	changed.Parts = []session.Part{{Type: "text", Text: "rewritten child"}}
	if _, err := r.MatchChild(t.Context(), identity, changed); !errors.Is(err, store.ErrConflict) {
		t.Fatal("rewritten payload falsely matched original", err)
	}
	if fake.commands.Load() != 2 {
		t.Fatal("hook replayed native effect", fake.commands.Load())
	}
}
