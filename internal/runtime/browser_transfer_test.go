package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browserhost"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func spawnBrowserCall(owner session.Session, cell session.Cell, attachment browserhost.Attachment) tool.Invocation {
	return tool.Invocation{SessionID: owner.ID, CellID: cell.ID, RequestID: "transfer", Module: "agents", Name: "spawn", Arguments: map[string]any{"prompt": "child task", "browser_attachments": []string{attachment.Scope.AttachmentID}}}
}

func TestBrowserTransferCommitsOneChildAndDisablesParent(t *testing.T) {
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	fake := startBrowserFixture(t, r, owner)
	attachment := attachBrowserFixture(t, r, owner, cell)
	call := spawnBrowserCall(owner, cell, attachment)
	value, id, err := r.tools.Call(t.Context(), call)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		SessionID session.SessionID `json:"session_id"`
		InputID   session.InputID   `json:"input_id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.SessionID != store.TransferChildID(id) || result.InputID == "" {
		t.Fatal(string(raw))
	}
	op, err := r.store.Operation(t.Context(), id)
	if err != nil || op.State != session.OperationSucceeded {
		t.Fatal(op, err)
	}
	entries, err := r.BrowserAttachments(t.Context(), result.SessionID)
	if err != nil || len(entries) != 1 || entries[0].Scope.Resource() != attachment.Scope.Resource() {
		t.Fatal(entries, err)
	}
	if _, err := r.browser.Resolve(t.Context(), attachment.Owner, "run", browserhost.Arguments{AttachmentID: attachment.Scope.AttachmentID}); !errors.Is(err, browserhost.ErrStale) {
		t.Fatal("parent retained execution", err)
	}
	if _, _, err := r.tools.Call(t.Context(), call); err == nil {
		t.Fatal("guest request replayed")
	}
	if fake.commands.Load() != 2 {
		t.Fatal("native effect repeated", fake.commands.Load())
	}
	grants, err := r.Grants(t.Context(), owner.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if grant.Capability == "browser.control" {
			if _, err := r.RevokeGrant(t.Context(), grant.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if entries, err := r.BrowserAttachments(t.Context(), result.SessionID); err != nil || len(entries) != 0 {
		t.Fatal("lineage revocation left child control", entries, err)
	}
}

func TestBrowserTransferUnknownDeliveryRetiresBothAndNeverSpawns(t *testing.T) {
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	fake := startBrowserFixture(t, r, owner)
	attachment := attachBrowserFixture(t, r, owner, cell)
	fake.transferBlock.Store(true)
	call := spawnBrowserCall(owner, cell, attachment)
	done := make(chan error, 1)
	go func() { _, _, err := r.tools.Call(t.Context(), call); done <- err }()
	select {
	case <-fake.transferObserved:
	case <-time.After(5 * time.Second):
		t.Fatal("transfer did not reach native boundary")
	}
	if _, err := r.store.Session(t.Context(), store.TransferChildID(call.OperationID())); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("native ACK was not required", err)
	}
	fake.peer.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("lost native ACK succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lost native ACK not joined")
	}
	op, err := r.store.Operation(t.Context(), call.OperationID())
	if err != nil || op.State != session.OperationUncertain {
		t.Fatal(op, err)
	}
	if _, err := r.store.Session(t.Context(), store.TransferChildID(call.OperationID())); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("uncertain transfer published child", err)
	}
	if entries, err := r.BrowserAttachments(t.Context(), owner.ID); err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	if _, _, err := r.tools.Call(t.Context(), call); err == nil {
		t.Fatal("uncertain effect replayed")
	}
	if fake.commands.Load() != 2 {
		t.Fatal(fake.commands.Load())
	}
}

func TestBrowserTransferRevocationAfterNativeAcknowledgementRollsBackChild(t *testing.T) {
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	fake := startBrowserFixture(t, r, owner)
	attachment := attachBrowserFixture(t, r, owner, cell)
	fake.transferBlock.Store(true)
	call := spawnBrowserCall(owner, cell, attachment)
	done := make(chan error, 1)
	go func() { _, _, err := r.tools.Call(t.Context(), call); done <- err }()
	var command *browserhost.Command
	select {
	case command = <-fake.transferObserved:
	case <-time.After(5 * time.Second):
		t.Fatal("no transfer")
	}
	// Simulate revocation racing native completion. No retry may restore parent.
	grants, err := r.Grants(t.Context(), owner.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		if g.Capability == "browser.control" {
			if _, err := r.store.RevokeGrant(t.Context(), g.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := fake.peer.Settle(browserhost.CommandResult{CommandID: command.CommandID, RootID: command.Identity.RootID, ProviderEpoch: command.Scope.ProviderEpoch, AttachmentGeneration: command.Scope.AttachmentGeneration, DocumentRevision: "doc1", URL: "https://example.test", Title: "page", Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revoked authority committed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("commit did not finish")
	}
	if _, err := r.store.Session(t.Context(), store.TransferChildID(call.OperationID())); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("partial child survived", err)
	}
	if entries, err := r.BrowserAttachments(t.Context(), owner.ID); err != nil || len(entries) != 0 {
		t.Fatal("parent control restored", entries, err)
	}
}

func TestBothEnginesBrowserTransferCapturedChildAndRestart(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{"attach": `a=browser.attach(tab_id="human-tab"); print(a["tab_id"])`, "transfer": `child=agents.spawn(prompt="child",browser_attachments=[a["attachment_id"]]); print("transferred")`, "read": `print(child["session_id"])`}
			if engine == session.QuickJS {
				codes = map[string]string{"attach": `var a=await browser.attach({tab_id:"human-tab"}); console.log(a.tab_id)`, "transfer": `var child=await agents.spawn({prompt:"child",browser_attachments:[a.attachment_id]}); console.log("transferred")`, "read": `console.log(child.session_id)`}
			}
			base := cellProvider(codes)
			childEntered := make(chan struct{})
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.Messages[0].Parts[0].Text == "child" {
					close(childEntered)
					<-ctx.Done()
					return model.Response{}, ctx.Err()
				}
				// The script selects an explicit fixture input or its tool
				// result. Completion mail can follow the opening input after
				// restart; it is context, not a key in this test's code table.
				for i, message := range slices.Backward(request.Messages) {
					if len(message.Parts) == 0 {
						continue
					}
					_, scripted := codes[message.Parts[0].Text]
					if message.Role == session.Tool || message.Role == session.User && scripted {
						request.Messages = request.Messages[:i+1]
						return base(ctx, request)
					}
				}
				return model.Response{}, errors.New("missing browser-transfer fixture input")
			})
			directory := t.TempDir()
			r := openEngineTest(t, directory, provider)
			root := createEngineSession(t, r, engine)
			fake := startBrowserFixture(t, r, root)
			if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: root.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
				t.Fatal(err)
			}
			runCellTurn(t, r, root.ID, "attach", "human-tab\n")
			runCellTurn(t, r, root.ID, "transfer", "transferred\n")
			latest, err := r.store.LatestCell(t.Context(), root.ID)
			if err != nil {
				t.Fatal(err)
			}
			ops, err := r.Operations(t.Context(), latest.TurnID, "", 100)
			if err != nil || len(ops) != 1 {
				t.Fatal(ops, err)
			}
			child := store.TransferChildID(ops[0].ID)
			attachments, err := r.BrowserAttachments(t.Context(), child)
			if err != nil || len(attachments) != 1 {
				t.Fatal(attachments, err)
			}
			// Force the child interruption/report ordering that can otherwise race
			// the parent's next input when the host restarts.
			awaitMailSignal(t, childEntered)
			fake.peer.Close()
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			restarted := openEngineTest(t, directory, provider)
			completion := awaitDetachedCompletion(t, restarted, root.ID)
			if completion.Source != (session.MailSource{Kind: "completion", ID: string(child)}) {
				t.Fatal("wrong child report", completion.Source)
			}
			runCellTurn(t, restarted, root.ID, "read", fmt.Sprintln(child))
			assertMailState(t, restarted, root.ID, string(completion.ID), session.MailDelivered)
			if entries, err := restarted.BrowserAttachments(t.Context(), child); err != nil || len(entries) != 0 {
				t.Fatal("restart restored native ownership", entries, err)
			}
		})
	}
}

func TestBrowserTransferCommittedChildWaitsWhileSchedulerAlreadyAwake(t *testing.T) {
	childEntered := make(chan struct{})
	childAcquired := make(chan error, 1)
	var runtime *Runtime
	provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
		// This actual scheduler worker starts after the child SQL commit but before
		// its committing transfer owner has received the callback acknowledgement.
		owner := request.SessionID
		entries, err := runtime.BrowserAttachments(ctx, owner)
		if err != nil || len(entries) != 1 {
			childAcquired <- fmt.Errorf("provisional child attachment unavailable (%d): %w", len(entries), err)
			return model.Response{}, errors.New("missing attachment")
		}
		capture, err := runtime.browser.Resolve(ctx, entries[0].Owner, "run", browserhost.Arguments{AttachmentID: entries[0].Scope.AttachmentID})
		if err != nil {
			childAcquired <- err
			return model.Response{}, err
		}
		close(childEntered)
		lease, err := capture.Acquire(ctx)
		if err == nil {
			lease.Close()
		}
		childAcquired <- err
		return model.Response{Parts: []session.Part{{Type: "text", Text: "child"}}}, err
	})
	r, owner, cell := modelHelperFixture(t, provider)
	runtime = r
	fake := startBrowserFixture(t, r, owner)
	attachment := attachBrowserFixture(t, r, owner, cell)
	call := spawnBrowserCall(owner, cell, attachment)
	parent := attachment.Owner
	child := browserhost.Identity{RootID: parent.RootID, AgentID: string(store.TransferChildID(call.OperationID()))}
	capture, err := r.browser.PrepareTransfer(t.Context(), parent, child, []string{attachment.Scope.AttachmentID})
	if err != nil {
		t.Fatal(err)
	}
	request, err := parseSpawn(owner.ID, call.Arguments)
	if err != nil {
		t.Fatal(err)
	}
	intent := store.ChildTransferIntent{Request: request, ChildID: session.SessionID(child.AgentID), Parents: transferScopes(capture.Parents()), Children: transferScopes(capture.Attachments())}
	raw, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	spec := session.OperationSpec{ID: call.OperationID(), CellID: cell.ID, RequestID: call.RequestID, Capability: "agents.spawn", Resource: string(owner.TreeID), Arguments: raw}
	if _, err := r.store.AdmitOperation(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	lease, err := capture.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if allowed, err := r.store.DispatchOperation(t.Context(), spec.ID); err != nil || !allowed {
		t.Fatal(allowed, err)
	}
	// The fixture already owns a live parent cell. Start the ordinary scheduler
	// loop without startup recovery interrupting that deliberately held cell.
	r.mu.Lock()
	ctx, cancel := context.WithCancel(t.Context())
	r.cancel = cancel
	r.started = true
	r.mu.Unlock()
	go r.run(ctx)
	defer cancel()
	paused := make(chan struct{})
	releaseCommit := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := lease.Execute(ctx, string(spec.ID), func(ctx context.Context) error { return r.store.CheckChildTransfer(ctx, spec.ID, true) }, func(ctx context.Context, children []browserhost.Attachment) error {
			if _, err := r.store.CommitChildTransfer(ctx, spec.ID, transferScopes(children)); err != nil {
				return err
			}
			close(paused)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-releaseCommit:
				return nil
			}
		})
		done <- err
	}()
	select {
	case <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("commit not reached")
	}
	select {
	case <-childEntered:
	case err := <-childAcquired:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("already running scheduler did not claim child")
	}
	select {
	case err := <-childAcquired:
		t.Fatal("child crossed held transfer reservation", err)
	default:
	}
	close(releaseCommit)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-childAcquired:
		t.Fatal("child crossed unclosed transfer reservation", err)
	default:
	}
	lease.Close()
	select {
	case err := <-childAcquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not resume after activation")
	}
	if fake.commands.Load() != 2 {
		t.Fatal("unexpected native dispatch", fake.commands.Load())
	}
}

func TestBrowserTransferDurableChildSurvivesLostCommitAckAndActivationRevocation(t *testing.T) {
	for _, lostAck := range []bool{false, true} {
		t.Run(map[bool]string{false: "revoked-before-activation", true: "lost-SQL-acknowledgement"}[lostAck], func(t *testing.T) {
			r, owner, cell := modelHelperFixture(t, model.Scripted{})
			fake := startBrowserFixture(t, r, owner)
			attached := attachBrowserFixture(t, r, owner, cell)
			call := spawnBrowserCall(owner, cell, attached)
			child := browserhost.Identity{RootID: attached.Owner.RootID, AgentID: string(store.TransferChildID(call.OperationID()))}
			capture, err := r.browser.PrepareTransfer(t.Context(), attached.Owner, child, []string{attached.Scope.AttachmentID})
			if err != nil {
				t.Fatal(err)
			}
			request, err := parseSpawn(owner.ID, call.Arguments)
			if err != nil {
				t.Fatal(err)
			}
			intent := store.ChildTransferIntent{Request: request, ChildID: session.SessionID(child.AgentID), Parents: transferScopes(capture.Parents()), Children: transferScopes(capture.Attachments())}
			raw, err := json.Marshal(intent)
			if err != nil {
				t.Fatal(err)
			}
			spec := session.OperationSpec{ID: call.OperationID(), CellID: cell.ID, RequestID: call.RequestID, Capability: "agents.spawn", Resource: string(owner.TreeID), Arguments: raw}
			if _, err := r.store.AdmitOperation(t.Context(), spec); err != nil {
				t.Fatal(err)
			}
			lease, err := capture.Acquire(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			if allowed, err := r.store.DispatchOperation(t.Context(), spec.ID); err != nil || !allowed {
				t.Fatal(allowed, err)
			}
			var committed store.ChildAdmission
			_, err = lease.Execute(t.Context(), string(spec.ID), func(ctx context.Context) error { return r.store.CheckChildTransfer(ctx, spec.ID, true) }, func(ctx context.Context, children []browserhost.Attachment) error {
				var err error
				committed, err = r.store.CommitChildTransfer(ctx, spec.ID, transferScopes(children))
				if err != nil {
					return err
				}
				if lostAck {
					return errors.New("SQL commit acknowledgement lost")
				}
				r.browser.RevokeOwner(child)
				return nil
			})
			if !errors.Is(err, browserhost.ErrUnknown) {
				t.Fatal("post-commit authority loss was hidden", err)
			}
			lease.Close()
			operation, err := r.Operation(t.Context(), spec.ID)
			if err != nil || operation.State != session.OperationSucceeded {
				t.Fatal("known commit was relabelled", operation, err)
			}
			receipt, err := r.Admission(t.Context(), session.RequestIdentity{ClientID: "operation", RequestID: string(spec.ID)})
			if err != nil || receipt.Input.ID != committed.Admission.Input.ID {
				t.Fatal("committed child receipt lost", receipt, err)
			}
			for _, identity := range []browserhost.Identity{attached.Owner, child} {
				if entries := r.browser.Attachments(identity); len(entries) != 0 {
					t.Fatal("uncertain activation retained controls", entries)
				}
			}
			if fake.commands.Load() != 2 {
				t.Fatal("commit recovery replayed native handoff", fake.commands.Load())
			}
		})
	}
}
