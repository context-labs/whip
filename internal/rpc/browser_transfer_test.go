package rpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestBrowserSocketChildTransferPrivateAdmissionAndExactPublicRecovery(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "lost-native-ACK"}[unknown], func(t *testing.T) {
			r, c := fixture(t)
			tree := create(t, c)
			s := openBrowserSocket(t, r.SocketPath())
			browserBind(t, s, tree.Root.ID)
			if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: session.SessionID(tree.Root.ID), ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
				t.Fatal(err)
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			admitBrowser(t, r, tree.Root.ID, "attach", "attach", `{"tab_id":"human-tab"}`)
			attached := s.next(t).Command
			if attached == nil {
				t.Fatal("missing attach")
			}
			resultFor := func(cmd *protocol.BrowserCommand) protocol.BrowserCommandResultParams {
				return protocol.BrowserCommandResultParams{CommandID: cmd.CommandID, RootID: cmd.RootID, ProviderEpoch: cmd.ProviderEpoch, AttachmentGeneration: cmd.Scope.AttachmentGeneration, DocumentRevision: "doc", URL: "about:blank", Title: "page", Result: json.RawMessage(`{}`)}
			}
			if result := s.call(t, "browser.command.result", resultFor(attached)); result.Error != nil {
				t.Fatal(result.Error)
			}
			waitBrowserTurn(t, r, "attach")
			params := protocol.SpawnSessionParams{Identity: protocol.RequestIdentity{ClientID: "public", RequestID: "transfer"}, ParentID: tree.Root.ID, Parts: []protocol.Part{{Type: "text", Text: "child"}}, BrowserAttachments: []protocol.ID{protocol.ID(attached.Scope.AttachmentID)}}
			observe, cancel := context.WithCancel(t.Context())
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				var output protocol.SpawnSessionResult
				finished <- c.Call(observe, "sessions.spawn", params, &output)
			}()
			transfer := s.next(t).Command
			if transfer == nil || transfer.Kind != "transfer" {
				t.Fatal(transfer)
			}
			state, err := r.Operation(t.Context(), session.OperationID(transfer.OperationID))
			if err != nil || state.State != session.OperationDispatched || state.DirectTurnID == "" || state.CellID != "" {
				t.Fatal("native transfer preceded direct committed dispatch", state, err)
			}
			requireHistoryError(t, c, "receipts.match", matchParams(t, "sessions.spawn", params), "BUSY")
			changed := params
			changed.BrowserAttachments = nil
			requireHistoryError(t, c, "receipts.match", matchParams(t, "sessions.spawn", changed), "CONFLICT")
			cancel()
			select {
			case err := <-finished:
				if err == nil {
					t.Fatal("cancelled observer received child before native ACK")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("observer did not cancel")
			}
			if unknown {
				if err := s.conn.Close(); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(5 * time.Second)
				for {
					var output protocol.Admission
					err := c.Call(t.Context(), "receipts.match", matchParams(t, "sessions.spawn", params), &output)
					var remote *client.Error
					if errors.As(err, &remote) && remote.Kind == "TRANSFER_UNCERTAIN" {
						break
					}
					if !errors.As(err, &remote) || remote.Kind != "BUSY" || time.Now().After(deadline) {
						t.Fatal("lost ACK outcome", err)
					}
					time.Sleep(time.Millisecond)
				}
				requireHistoryError(t, c, "sessions.spawn", params, "TRANSFER_UNCERTAIN")
				return
			}
			if result := s.call(t, "browser.command.result", resultFor(transfer)); result.Error != nil {
				t.Fatal(result.Error)
			}
			created := call[protocol.SpawnSessionResult](t, c, "sessions.spawn", params)
			if created.Session == nil || created.Session.ParentID == nil || *created.Session.ParentID != tree.Root.ID || created.Admission.Receipt.Identity != params.Identity {
				t.Fatal(created)
			}
			matched := call[protocol.Admission](t, c, "receipts.match", matchParams(t, "sessions.spawn", params))
			if matched.Input.ID != created.Admission.Input.ID {
				t.Fatal(matched)
			}
			attachments := call[protocol.BrowserAttachmentsResult](t, c, "browser.attachments", protocol.SessionParams{SessionID: created.Session.ID})
			if len(attachments.Attachments) != 1 || attachments.Attachments[0].AgentID != created.Session.ID || attachments.Attachments[0].Scope.ControlLineage != attached.Scope.ControlLineage {
				t.Fatal(attachments)
			}
			retried := call[protocol.SpawnSessionResult](t, c, "sessions.spawn", params)
			if retried.Session.ID != created.Session.ID || retried.Admission.Input.ID != created.Admission.Input.ID {
				t.Fatal(retried)
			}
		})
	}
}
