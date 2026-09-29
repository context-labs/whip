package rpc_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/jpeg"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

type browserSocket struct {
	*executorSocket
	events []protocol.BrowserEvent
}

func openBrowserSocket(t *testing.T, path string) *browserSocket {
	t.Helper()
	return &browserSocket{executorSocket: openExecutorSocket(t, path)}
}

func (s *browserSocket) read(t *testing.T) *protocol.Response {
	t.Helper()
	var raw json.RawMessage
	if err := s.decoder.Decode(&raw); err != nil {
		t.Fatal(err)
	}
	var header struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		t.Fatal(err)
	}
	if header.ID != "" {
		if err := protocol.Validate("Response", raw); err != nil {
			t.Fatal(err)
		}
		var response protocol.Response
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		return &response
	}
	if err := protocol.Validate("BrowserEvent", raw); err != nil {
		t.Fatal(string(raw), err)
	}
	var event protocol.BrowserEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	s.events = append(s.events, event)
	return nil
}

func (s *browserSocket) call(t *testing.T, method string, params any) protocol.Response {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.encoder.Encode(protocol.Request{JSONRPC: "2.0", ID: protocol.ID(method), Method: method, Params: raw}); err != nil {
		t.Fatal(err)
	}
	for {
		if r := s.read(t); r != nil {
			if r.ID != protocol.ID(method) {
				t.Fatal("response identity mismatch", r)
			}
			return *r
		}
	}
}

func (s *browserSocket) next(t *testing.T) protocol.BrowserEvent {
	t.Helper()
	for len(s.events) == 0 {
		if r := s.read(t); r != nil {
			t.Fatal("unexpected response", r)
		}
	}
	n := s.events[0]
	s.events = s.events[1:]
	return n
}

func browserOffer(root protocol.ID) protocol.BrowserProviderBindParams {
	return protocol.BrowserProviderBindParams{RootID: root, Version: 2, DesktopID: "desktop", WindowID: "window", OfferRevision: "offer", CreateProfileID: "profile", OfferedTabs: []protocol.BrowserOfferedTab{{TabID: "human-tab", TabGeneration: "tab-generation", ProfileID: "profile", DocumentRevision: "doc", URL: "about:blank", Title: "Human page"}}, OfferedPreviewHosts: []protocol.BrowserPreviewScope{}}
}

func browserBind(t *testing.T, s *browserSocket, root protocol.ID) protocol.BrowserProviderBindResult {
	t.Helper()
	r := s.call(t, "browser.provider.bind", browserOffer(root))
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	var v protocol.BrowserProviderBindResult
	if err := json.Unmarshal(r.Result, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func waitBrowserTurn(t *testing.T, r *runtime.Runtime, key string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		a, err := r.Admission(t.Context(), session.RequestIdentity{ClientID: "browser-test", RequestID: key})
		if err != nil {
			t.Fatal(err)
		}
		if a.Turn != nil && a.Turn.FinishedAt != nil {
			if a.Turn.State != session.Succeeded {
				t.Fatal(a.Turn)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("browser turn not settled")
		}
		time.Sleep(time.Millisecond)
	}
}

func admitBrowser(t *testing.T, r *runtime.Runtime, root protocol.ID, key, name, args string) {
	t.Helper()
	_, err := r.AdmitHostOperation(t.Context(), session.RequestIdentity{ClientID: "browser-test", RequestID: key}, session.SessionID(root), session.HostOperation{Module: "browser", Name: name, Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBrowserSocketOfferInventoryAndPermissionBeforeExactHolderDispatch(t *testing.T) {
	r, c := fixture(t)
	tree := create(t, c)
	s := openBrowserSocket(t, r.SocketPath())
	binding := browserBind(t, s, tree.Root.ID)
	if entries := call[protocol.BrowserAttachmentsResult](t, c, "browser.attachments", protocol.SessionParams{SessionID: tree.Root.ID}); len(entries.Attachments) != 0 {
		t.Fatal(entries)
	}
	grants, err := r.Grants(t.Context(), session.SessionID(tree.Root.ID), "", 100)
	if err != nil || len(grants) != 0 {
		t.Fatal(grants, err)
	}
	listed := make(chan error, 1)
	var tabs protocol.BrowserTabsResult
	go func() {
		listed <- c.Call(t.Context(), "browser.tabs", protocol.SessionParams{SessionID: tree.Root.ID}, &tabs)
	}()
	request := s.next(t).Inventory
	if request == nil || len(request.Tabs) != 1 || request.ProviderEpoch != binding.ProviderEpoch {
		t.Fatal(request)
	}
	response := s.call(t, "browser.inventory.result", protocol.BrowserInventoryResultParams{RequestID: request.RequestID, RootID: tree.Root.ID, ProviderEpoch: binding.ProviderEpoch, Tabs: []protocol.BrowserTab{{TabID: "human-tab", TabGeneration: "tab-generation", DocumentRevision: "doc", URL: "about:blank", Title: "Human page", State: "available", Requestable: true}}})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	if err := <-listed; err != nil || len(tabs.Tabs) != 1 || !tabs.Tabs[0].Requestable {
		t.Fatal(tabs, err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	admitBrowser(t, r, tree.Root.ID, "attach", "attach", `{"tab_id":"human-tab"}`)
	deadline := time.Now().Add(10 * time.Second)
	var operation session.OperationID
	for {
		permissions, err := r.Permissions(t.Context(), session.SessionID(tree.Root.ID), "", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(permissions) > 0 {
			operation = permissions[0].OperationID
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("missing browser permission")
		}
		time.Sleep(time.Millisecond)
	}
	state, err := r.Operation(t.Context(), operation)
	if err != nil || state.State != session.OperationWaiting {
		t.Fatal(state, err)
	}
	if _, err := r.ResolvePermission(t.Context(), operation, true); err != nil {
		t.Fatal(err)
	}
	command := s.next(t).Command
	if command == nil || command.Kind != "attach" || command.OperationID != protocol.ID(operation) {
		t.Fatal(command)
	}
	state, err = r.Operation(t.Context(), operation)
	if err != nil || state.State != session.OperationDispatched {
		t.Fatal("native command before durable dispatch", state, err)
	}
	other := create(t, c)
	foreign := openBrowserSocket(t, r.SocketPath())
	browserBind(t, foreign, other.Root.ID)
	result := protocol.BrowserCommandResultParams{CommandID: command.CommandID, RootID: command.RootID, ProviderEpoch: command.ProviderEpoch, AttachmentGeneration: command.Scope.AttachmentGeneration, DocumentRevision: "doc", URL: "about:blank", Title: "Human page", Result: json.RawMessage(`{}`)}
	if response := foreign.call(t, "browser.command.result", result); response.Error == nil {
		t.Fatal("foreign holder settled command")
	}
	if response := s.call(t, "browser.command.result", result); response.Error != nil {
		t.Fatal(response.Error)
	}
	waitBrowserTurn(t, r, "attach")
	if response := s.call(t, "browser.command.result", result); response.Error == nil {
		t.Fatal("completed command accepted twice")
	}
	attachments := call[protocol.BrowserAttachmentsResult](t, c, "browser.attachments", protocol.SessionParams{SessionID: tree.Root.ID})
	if len(attachments.Attachments) != 1 || attachments.Attachments[0].Scope != command.Scope {
		t.Fatal(attachments)
	}
	grants, err = r.Grants(t.Context(), session.SessionID(tree.Root.ID), "", 100)
	if err != nil || len(grants) != 2 {
		t.Fatal(grants, err)
	}
	for _, method := range []string{"initialize", "shell.input", "executor.bind", "trees.list"} {
		if response := s.call(t, method, protocol.EmptyParams{}); response.Error == nil {
			t.Fatal("browser peer accepted unrelated method", method)
		}
	}
	// A snapshot is observation, never restored authority. Disconnect invalidates
	// every live control while leaving the human page outside runtime ownership.
	if err := s.conn.Close(); err != nil {
		t.Fatal(err)
	}
	for {
		entries, err := r.BrowserAttachments(t.Context(), session.SessionID(tree.Root.ID))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("disconnected peer retained authority")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBrowserSocketScreenshotChunksArePendingCommandAndOwnerScoped(t *testing.T) {
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
	cmd := s.next(t).Command
	if cmd == nil {
		t.Fatal("missing attach")
	}
	resultFor := func(cmd *protocol.BrowserCommand) protocol.BrowserCommandResultParams {
		return protocol.BrowserCommandResultParams{CommandID: cmd.CommandID, RootID: cmd.RootID, ProviderEpoch: cmd.ProviderEpoch, AttachmentGeneration: cmd.Scope.AttachmentGeneration, DocumentRevision: "doc", URL: "about:blank", Title: "Human page", Result: json.RawMessage(`{}`)}
	}
	if response := s.call(t, "browser.command.result", resultFor(cmd)); response.Error != nil {
		t.Fatal(response.Error)
	}
	waitBrowserTurn(t, r, "attach")
	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"attachment_id": string(cmd.Scope.AttachmentID), "code": "screenshot()"})
	admitBrowser(t, r, tree.Root.ID, "image", "run", string(args))
	captured := false
	for {
		cmd = s.next(t).Command
		if cmd == nil {
			t.Fatal("missing browser command")
		}
		result := resultFor(cmd)
		if cmd.Kind == "cdp" {
			var p struct {
				Method string `json:"method"`
			}
			if err := json.Unmarshal(cmd.Arguments, &p); err != nil {
				t.Fatal(err)
			}
			switch p.Method {
			case "Page.getFrameTree":
				result.Result = json.RawMessage(`{"frameTree":{"frame":{"id":"frame","url":"about:blank","securityOrigin":"null","mimeType":"text/html"}}}`)
			case "Runtime.evaluate":
				result.Result = json.RawMessage(`{"result":{"type":"string","value":"{\"url\":\"about:blank\",\"title\":\"page\",\"w\":2,\"h\":2}"}}`)
			case "Page.getLayoutMetrics":
				result.Result = json.RawMessage(`{"cssLayoutViewport":{"clientWidth":2,"clientHeight":2}}`)
			case "Page.captureScreenshot":
				captured = true
				chunk := protocol.BrowserScreenshotChunkParams{CommandID: cmd.CommandID, RootID: cmd.RootID, ProviderEpoch: cmd.ProviderEpoch, AttachmentGeneration: cmd.Scope.AttachmentGeneration, DataBase64: base64.StdEncoding.EncodeToString(jpegBytes.Bytes())}
				bad := chunk
				bad.AttachmentGeneration = "foreign"
				if response := s.call(t, "browser.screenshot.chunk", bad); response.Error == nil {
					t.Fatal("foreign attachment upload accepted")
				}
				bad = chunk
				bad.Offset = 9007199254740993
				if response := s.call(t, "browser.screenshot.chunk", bad); response.Error == nil {
					t.Fatal("large offset rounded or accepted")
				}
				if response := s.call(t, "browser.screenshot.chunk", chunk); response.Error != nil {
					t.Fatal(response.Error)
				}
				if response := s.call(t, "browser.screenshot.chunk", chunk); response.Error == nil {
					t.Fatal("upload offset replay accepted")
				}
				sum := sha256.Sum256(jpegBytes.Bytes())
				result.Screenshot = &protocol.BrowserScreenshot{Size: protocol.Counter(jpegBytes.Len()), Digest: hex.EncodeToString(sum[:]), MediaType: "image/jpeg"}
			}
		}
		if response := s.call(t, "browser.command.result", result); response.Error != nil {
			t.Fatal(response.Error)
		}
		if cmd.Kind == "end" {
			break
		}
	}
	if !captured {
		t.Fatal("no screenshot command")
	}
	waitBrowserTurn(t, r, "image")
	admission, err := r.Admission(t.Context(), session.RequestIdentity{ClientID: "browser-test", RequestID: "image"})
	if err != nil {
		t.Fatal(err)
	}
	operations, err := r.Operations(t.Context(), admission.Turn.ID, "", 10)
	if err != nil || len(operations) != 1 || len(operations[0].Result.ContentReferences) != 1 {
		t.Fatal(operations, err)
	}
	ref, _, err := r.ReadContent(t.Context(), session.SessionID(tree.Root.ID), operations[0].Result.ContentReferences[0], 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	if ref.SessionID != session.SessionID(tree.Root.ID) || ref.MediaType != "image/jpeg" || ref.Size != int64(jpegBytes.Len()) {
		t.Fatal(ref)
	}
}

func TestBrowserSocketDisconnectAfterDispatchDoesNotReplayOrRestore(t *testing.T) {
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
	admitBrowser(t, r, tree.Root.ID, "unknown-attach", "attach", `{"tab_id":"human-tab"}`)
	command := s.next(t).Command
	if command == nil || command.Kind != "attach" {
		t.Fatal(command)
	}
	if err := s.conn.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		op, err := r.Operation(t.Context(), session.OperationID(command.OperationID))
		if err != nil {
			t.Fatal(err)
		}
		if op.State == session.OperationUncertain {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lost native ack was not uncertain", op)
		}
		time.Sleep(time.Millisecond)
	}
	entries, err := r.BrowserAttachments(t.Context(), session.SessionID(tree.Root.ID))
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	// Exact receipt recovery cannot repeat an already-dispatched native command.
	admitBrowser(t, r, tree.Root.ID, "unknown-attach", "attach", `{"tab_id":"human-tab"}`)
	again := openBrowserSocket(t, r.SocketPath())
	browserBind(t, again, tree.Root.ID)
	entries, err = r.BrowserAttachments(t.Context(), session.SessionID(tree.Root.ID))
	if err != nil || len(entries) != 0 {
		t.Fatal("new offer restored old control", entries, err)
	}
	invalid := openBrowserSocket(t, r.SocketPath())
	response := invalid.call(t, "browser.provider.bind", browserOffer("missing-root"))
	if response.Error == nil {
		t.Fatal("missing root accepted")
	}
	var extra json.RawMessage
	if err := invalid.decoder.Decode(&extra); err == nil {
		t.Fatal("failed first bind retained a peer")
	}
}
