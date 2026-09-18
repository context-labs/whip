package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"net"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

type browserWire struct {
	t         *testing.T
	transport messageTransport
	next      int
	queued    []rpcMessage
}

func (w *browserWire) read() rpcMessage {
	w.t.Helper()
	_ = w.transport.SetReadDeadline(time.Now().Add(5 * time.Second))
	raw, err := w.transport.ReadMessage()
	if err != nil {
		w.t.Fatal(err)
	}
	var message rpcMessage
	if err = json.Unmarshal(raw, &message); err != nil {
		w.t.Fatal(err)
	}
	return message
}
func (w *browserWire) rpc(method string, params any) rpcMessage {
	w.t.Helper()
	w.next++
	id, _ := json.Marshal(w.next)
	raw, _ := json.Marshal(params)
	if err := writeTransportMessage(w.transport, rpcMessage{ID: id, Method: method, Params: raw}); err != nil {
		w.t.Fatal(err)
	}
	for {
		message := w.read()
		if string(message.ID) == string(id) {
			return message
		}
		w.queued = append(w.queued, message)
	}
}
func (w *browserWire) command() protocol.BrowserCommand {
	w.t.Helper()
	for {
		var message rpcMessage
		if len(w.queued) > 0 {
			message = w.queued[0]
			w.queued = w.queued[1:]
		} else {
			message = w.read()
		}
		if message.Method != "browser.command" {
			continue
		}
		var command protocol.BrowserCommand
		if err := json.Unmarshal(message.Params, &command); err != nil {
			w.t.Fatal(err)
		}
		return command
	}
}
func (w *browserWire) settle(command protocol.BrowserCommand, result any) rpcMessage {
	w.t.Helper()
	raw, _ := json.Marshal(result)
	return w.rpc("browser.command.result", protocol.BrowserCommandResultParams{CommandID: command.CommandID, RootID: command.RootID, ProviderEpoch: command.ProviderEpoch, AttachmentGeneration: command.Scope.AttachmentGeneration, DocumentRevision: "doc-1", Result: raw})
}
func browserHarness(t *testing.T) (*Daemon, *Server, string) {
	t.Helper()
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	root := createRoot(t, store)
	d, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.EnsureAuthority(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(d, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close(); _ = d.Close(); _ = store.Close() })
	return d, server, root
}
func connectBrowserWire(t *testing.T, s *Server, willing bool) *browserWire {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() { defer close(done); s.serveConn(server) }()
	w := &browserWire{t: t, transport: newUnixMessageTransport(client)}
	t.Cleanup(func() {
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("browser provider connection leaked")
		}
	})
	caps := []string{}
	if willing {
		caps = append(caps, "desktop-browser-v1", "desktop-browser-v2")
	}
	reply := w.rpc("initialize", protocol.InitializeParams{ProtocolMajor: ProtocolMajor, ClientID: "same-client-id", ClientKind: "test", Capabilities: caps})
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	return w
}
func browserOffer(root string) protocol.BrowserProviderBindParams {
	return protocol.BrowserProviderBindParams{RootID: root, Version: 1, DesktopID: "desktop", WindowID: "window", OfferRevision: "offer-1", CreateProfileID: "profile", OfferedTabs: []protocol.BrowserOfferedTab{{TabID: "tab-1", TabGeneration: "tab-gen", ProfileID: "profile", DocumentRevision: "doc-1"}, {TabID: "tab-2", TabGeneration: "tab-gen-2", ProfileID: "profile", DocumentRevision: "doc-1"}}, OfferedPreviewHosts: []capability.BrowserPreviewScope{}}
}
func bindBrowserWire(t *testing.T, w *browserWire, offer protocol.BrowserProviderBindParams) protocol.BrowserProviderBindResult {
	t.Helper()
	reply := w.rpc("browser.provider.bind", offer)
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	raw, _ := json.Marshal(reply.Result)
	var result protocol.BrowserProviderBindResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

type desktopOutcome struct {
	result browser.DesktopResult
	err    error
}

func executeBrowserAsync(ctx context.Context, p *browserProviders, identity browser.DesktopIdentity, op string, call capability.BrowserCall) <-chan desktopOutcome {
	result := make(chan desktopOutcome, 1)
	go func() {
		out, err := p.Execute(ctx, browser.DesktopRequest{Identity: identity, OperationID: browserID(), Operation: op, Call: call}, nil)
		result <- desktopOutcome{out, err}
	}()
	return result
}
func awaitBrowser(t *testing.T, result <-chan desktopOutcome) desktopOutcome {
	t.Helper()
	select {
	case out := <-result:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("browser operation did not settle")
		return desktopOutcome{}
	}
}
func attachBrowser(t *testing.T, p *browserProviders, w *browserWire, identity browser.DesktopIdentity, tab string) browser.DesktopResult {
	t.Helper()
	call, err := p.Resolve(t.Context(), identity, "browser.attach", browser.DesktopArguments{TabID: tab})
	if err != nil {
		t.Fatal(err)
	}
	outcome := executeBrowserAsync(t.Context(), p, identity, "browser.attach", call)
	command := w.command()
	if command.Kind != "attach" {
		t.Fatal(command.Kind)
	}
	if reply := w.settle(command, browser.DesktopResult{URL: "https://example.test/", Title: "test"}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	result := awaitBrowser(t, outcome)
	if result.err != nil {
		t.Fatal(result.err)
	}
	return result.result
}
func TestBrowserProviderExplicitAssociationAndEpochReplacement(t *testing.T) {
	d, s, root := browserHarness(t)
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	unw := connectBrowserWire(t, s, false)
	if reply := unw.rpc("browser.provider.bind", browserOffer(root)); reply.Error == nil {
		t.Fatal("unwilling connection selected")
	}
	first := connectBrowserWire(t, s, true)
	second := connectBrowserWire(t, s, true)
	if _, err := d.browserProviders.Resolve(t.Context(), identity, "browser.open", browser.DesktopArguments{URL: "https://example.test"}); err == nil {
		t.Fatal("initialize selected provider implicitly")
	}
	bound := bindBrowserWire(t, first, browserOffer(root))
	if reply := second.rpc("browser.provider.bind", browserOffer(root)); reply.Error == nil {
		t.Fatal("newest connection stole provider")
	}
	attachment := attachBrowser(t, d.browserProviders, first, identity, "tab-1")
	old, err := d.browserProviders.Resolve(t.Context(), identity, "browser.run", browser.DesktopArguments{AttachmentID: attachment.AttachmentID})
	if err != nil {
		t.Fatal(err)
	}
	offer := browserOffer(root)
	offer.ExpectedProviderEpoch = "wrong"
	if reply := second.rpc("browser.provider.bind", offer); reply.Error == nil {
		t.Fatal("stale epoch replaced provider")
	}
	offer.ExpectedProviderEpoch = bound.ProviderEpoch
	replacement := bindBrowserWire(t, second, offer)
	if replacement.ProviderEpoch == bound.ProviderEpoch {
		t.Fatal("epoch reused")
	}
	if _, err = d.browserProviders.CallContext(old); err == nil {
		t.Fatal("old attachment survived replacement")
	}
	if err = d.store.AuthorizeBrowser(t.Context(), root, root, old.Grant, "browser.run", old.Scope); err == nil {
		t.Fatal("old persisted grant survived replacement")
	}
}
func TestBrowserProviderRejectsForeignLateAndWrongGenerationResults(t *testing.T) {
	d, s, root := browserHarness(t)
	first := connectBrowserWire(t, s, true)
	foreign := connectBrowserWire(t, s, true)
	bindBrowserWire(t, first, browserOffer(root))
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	call, err := d.browserProviders.Resolve(t.Context(), identity, "browser.attach", browser.DesktopArguments{TabID: "tab-1"})
	if err != nil {
		t.Fatal(err)
	}
	outcome := executeBrowserAsync(t.Context(), d.browserProviders, identity, "browser.attach", call)
	command := first.command()
	if reply := foreign.settle(command, map[string]any{}); reply.Error == nil {
		t.Fatal("same client id but different pointer settled command")
	}
	altered := command
	altered.Scope.AttachmentGeneration = "wrong"
	if reply := first.settle(altered, map[string]any{}); reply.Error == nil {
		t.Fatal("wrong generation settled")
	}
	altered = command
	altered.ProviderEpoch = "wrong"
	if reply := first.settle(altered, map[string]any{}); reply.Error == nil {
		t.Fatal("wrong epoch settled")
	}
	if reply := first.settle(command, map[string]any{}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	if out := awaitBrowser(t, outcome); out.err != nil {
		t.Fatal(out.err)
	}
	if reply := first.settle(command, map[string]any{}); reply.Error == nil {
		t.Fatal("duplicate result accepted")
	}
	if _, err = d.browserProviders.Resolve(t.Context(), browser.DesktopIdentity{RootID: root, AgentID: "child"}, "browser.run", browser.DesktopArguments{AttachmentID: call.Scope.AttachmentID}); err == nil {
		t.Fatal("copied handle authorized foreign agent")
	}
}
func TestBrowserProviderCancellationAndEventGapRevoke(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	bindBrowserWire(t, w, browserOffer(root))
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	ctx, cancel := context.WithCancel(t.Context())
	call, err := d.browserProviders.Resolve(ctx, identity, "browser.attach", browser.DesktopArguments{TabID: "tab-1"})
	if err != nil {
		t.Fatal(err)
	}
	outcome := executeBrowserAsync(ctx, d.browserProviders, identity, "browser.attach", call)
	command := w.command()
	cancel()
	out := awaitBrowser(t, outcome)
	var routine *browser.DesktopError
	if !errors.As(out.err, &routine) || routine.Kind != "outcome_unknown" {
		t.Fatalf("cancel result: %v", out.err)
	}
	if reply := w.settle(command, map[string]any{}); reply.Error == nil {
		t.Fatal("cancelled command settled late")
	}
	attachment := attachBrowser(t, d.browserProviders, w, identity, "tab-2")
	run, err := d.browserProviders.Resolve(t.Context(), identity, "browser.run", browser.DesktopArguments{AttachmentID: attachment.AttachmentID})
	if err != nil {
		t.Fatal(err)
	}
	event := protocol.BrowserProviderEventParams{RootID: root, ProviderEpoch: run.Scope.ProviderEpoch, TabID: run.Scope.TabID, TabGeneration: run.Scope.TabGeneration, AttachmentID: run.Scope.AttachmentID, AttachmentGeneration: run.Scope.AttachmentGeneration, Sequence: 2, DocumentRevision: "doc-2", Kind: "document"}
	if reply := w.rpc("browser.provider.event", event); reply.Error == nil {
		t.Fatal("event gap accepted")
	}
	if _, err = d.browserProviders.CallContext(run); err == nil {
		t.Fatal("event gap retained executable attachment")
	}
}
func TestBrowserTabQueueOneActiveFourQueued(t *testing.T) {
	tab := &browserTab{}
	release, err := tab.acquire(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	contexts := make([]context.Context, 4)
	cancels := make([]context.CancelFunc, 4)
	outcomes := make([]chan error, 4)
	for i := range 4 {
		contexts[i], cancels[i] = context.WithCancel(t.Context())
		outcomes[i] = make(chan error, 1)
		go func(i int) {
			done, err := tab.acquire(contexts[i], false)
			if done != nil {
				done()
			}
			outcomes[i] <- err
		}(i)
	}
	deadline := time.Now().Add(time.Second)
	for {
		tab.mu.Lock()
		queued := len(tab.queue)
		tab.mu.Unlock()
		if queued == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queue did not fill")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := tab.acquire(t.Context(), false); err == nil {
		t.Fatal("fifth queued batch accepted")
	}
	if _, err := tab.acquire(t.Context(), true); err == nil {
		t.Fatal("busy transfer accepted")
	}
	cancels[1]()
	select {
	case err := <-outcomes[1]:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued cancellation stuck")
	}
	release()
	for _, i := range []int{0, 2, 3} {
		select {
		case err := <-outcomes[i]:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("queue did not drain")
		}
		cancels[i]()
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.active || len(tab.queue) != 0 {
		t.Fatal("queue not released")
	}
}
func TestBrowserTransferAtomicAcknowledgmentAndAncestry(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	bindBrowserWire(t, w, browserOffer(root))
	parent := browser.DesktopIdentity{RootID: root, AgentID: root}
	child := browser.DesktopIdentity{RootID: root, AgentID: "child"}
	if _, err := d.store.AdmitAgent(t.Context(), session.AgentAdmission{RootID: root, ParentAgentID: root, ChildAgentID: child.AgentID, CWD: t.TempDir(), Model: "model", Provider: "provider"}); err != nil {
		t.Fatal(err)
	}
	a := attachBrowser(t, d.browserProviders, w, parent, "tab-1")
	b := attachBrowser(t, d.browserProviders, w, parent, "tab-2")
	outcome := make(chan error, 1)
	go func() {
		_, err := d.browserProviders.Transfer(t.Context(), parent, child, []string{a.AttachmentID, b.AttachmentID})
		outcome <- err
	}()
	command := w.command()
	if command.Kind != "transfer" {
		t.Fatal(command.Kind)
	}
	var transfer protocol.BrowserTransferArguments
	if err := json.Unmarshal(command.Arguments, &transfer); err != nil {
		t.Fatal(err)
	}
	if len(transfer.Attachments) != 2 || transfer.ChildAgentID != child.AgentID {
		t.Fatal("transfer was partial")
	}
	if got := d.browserProviders.Attachments(t.Context(), child); len(got) != 0 {
		t.Fatal("child activated before native ACK")
	}
	if reply := w.settle(command, map[string]any{}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	select {
	case err := <-outcome:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("transfer stuck")
	}
	if got := d.browserProviders.Attachments(t.Context(), child); len(got) != 2 {
		t.Fatalf("child attachment count %d", len(got))
	}
	if _, err := d.browserProviders.Resolve(t.Context(), parent, "browser.run", browser.DesktopArguments{AttachmentID: a.AttachmentID}); err == nil {
		t.Fatal("parent executable after handoff")
	}
	revoker, err := d.browserProviders.Resolve(t.Context(), parent, "browser.detach", browser.DesktopArguments{AttachmentID: a.AttachmentID})
	if err != nil {
		t.Fatal(err)
	}
	detach := executeBrowserAsync(t.Context(), d.browserProviders, parent, "browser.detach", revoker)
	command = w.command()
	if reply := w.settle(command, map[string]any{}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	if out := awaitBrowser(t, detach); out.err != nil {
		t.Fatal(out.err)
	}
	remaining := d.browserProviders.Attachments(t.Context(), child)
	if len(remaining) != 1 || !slices.Contains([]string{"tab-1", "tab-2"}, remaining[0].TabID) {
		t.Fatalf("cascade: %+v", remaining)
	}
}

func TestBrowserRunWholeBatchUsesInjectedCDPAndOneTarget(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	bindBrowserWire(t, w, browserOffer(root))
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	attached := attachBrowser(t, d.browserProviders, w, identity, "tab-1")
	call, err := d.browserProviders.Resolve(t.Context(), identity, "browser.run", browser.DesktopArguments{AttachmentID: attached.AttachmentID, ExpectedDocument: "doc-1"})
	if err != nil {
		t.Fatal(err)
	}
	outcome := make(chan desktopOutcome, 1)
	go func() {
		out, err := d.browserProviders.Execute(t.Context(), browser.DesktopRequest{Identity: identity, OperationID: "whole-batch", Operation: "browser.run", Call: call}, func(ctx context.Context, backend browser.Backend) (string, error) {
			tabs, err := backend.Tabs(ctx)
			if err != nil {
				return "", err
			}
			if len(tabs) != 1 || tabs[0].TargetID != "tab-1" {
				return "", errors.New("target escaped attachment")
			}
			if err := backend.UseTab(ctx, "foreign"); err == nil {
				return "", errors.New("foreign target accepted")
			}
			return "typed", backend.TypeText(ctx, "hello")
		})
		outcome <- desktopOutcome{out, err}
	}()
	ids := map[string]bool{}
	kinds := []string{}
	insertions := 0
	for {
		command := w.command()
		if command.OperationID != "whole-batch" || ids[command.CommandID] {
			t.Fatal("command identity is not unique within batch")
		}
		ids[command.CommandID] = true
		kinds = append(kinds, command.Kind)
		if command.Kind == "cdp" {
			var args struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal(command.Arguments, &args)
			if args.Method == "Input.insertText" {
				insertions++
			}
			if args.Method == "Target.getTargets" {
				t.Fatal("target enumeration escaped daemon shim")
			}
		}
		if reply := w.settle(command, map[string]any{}); reply.Error != nil {
			t.Fatal(reply.Error)
		}
		if command.Kind == "end" {
			break
		}
	}
	out := awaitBrowser(t, outcome)
	if out.err != nil || out.result.Output != "typed" || insertions != 1 || kinds[0] != "begin" {
		t.Fatalf("batch = %+v %v kinds=%v inserts=%d", out.result, out.err, kinds, insertions)
	}
}

func TestBrowserDisconnectCancelsWithoutReplay(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	bound := bindBrowserWire(t, w, browserOffer(root))
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	call, err := d.browserProviders.Resolve(t.Context(), identity, "browser.open", browser.DesktopArguments{URL: "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	outcome := executeBrowserAsync(t.Context(), d.browserProviders, identity, "browser.open", call)
	_ = w.command()
	_ = w.transport.Close()
	if out := awaitBrowser(t, outcome); out.err == nil {
		t.Fatal("disconnected operation succeeded")
	}
	replacement := connectBrowserWire(t, s, true)
	fresh := bindBrowserWire(t, replacement, browserOffer(root))
	if fresh.ProviderEpoch == bound.ProviderEpoch {
		t.Fatal("reconnect reused authority epoch")
	}
	if _, err := d.browserProviders.CallContext(call); err == nil {
		t.Fatal("reconnect restored old executable attachment")
	}
	if got := d.browserProviders.Attachments(t.Context(), identity); len(got) != 0 {
		t.Fatal("reconnect replayed attachment")
	}
}

func uploadBrowserJPEG(t *testing.T, w *browserWire, root string, data []byte) protocol.ContentHandle {
	t.Helper()
	sum := sha256.Sum256(data)
	id := browserID()
	begin := protocol.UploadBeginParams{UploadID: id, RootID: root, AgentID: root, ExpectedDigest: hex.EncodeToString(sum[:]), Size: int64(len(data)), MediaType: "image/jpeg", Source: "browser screenshot"}
	if reply := w.rpc("upload.begin", begin); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	for offset := 0; offset < len(data); offset += MaxContentChunk {
		end := min(offset+MaxContentChunk, len(data))
		if reply := w.rpc("upload.chunk", protocol.UploadChunkParams{UploadID: id, Offset: int64(offset), Data: data[offset:end]}); reply.Error != nil {
			t.Fatal(reply.Error)
		}
	}
	reply := w.rpc("upload.finish", protocol.UploadFinishParams{UploadID: id})
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	raw, _ := json.Marshal(reply.Result)
	var handle protocol.ContentHandle
	if err := json.Unmarshal(raw, &handle); err != nil {
		t.Fatal(err)
	}
	return handle
}
func TestBrowserScreenshotRequiresSameHolderBoundedContent(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	other := connectBrowserWire(t, s, true)
	bindBrowserWire(t, w, browserOffer(root))
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	attached := attachBrowser(t, d.browserProviders, w, identity, "tab-1")
	call, err := d.browserProviders.Resolve(t.Context(), identity, "browser.run", browser.DesktopArguments{AttachmentID: attached.AttachmentID})
	if err != nil {
		t.Fatal(err)
	}
	d.browserProviders.mu.Lock()
	a, err := d.browserProviders.findLocked(call)
	d.browserProviders.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	client := &browserCDP{providers: d.browserProviders, attachment: a, operationID: "screenshot"}
	type capture struct {
		raw []byte
		err error
	}
	outcome := make(chan capture, 1)
	go func() {
		raw, err := client.Call(t.Context(), call.Scope.AttachmentID, "Page.captureScreenshot", map[string]string{"format": "jpeg"})
		outcome <- capture{raw, err}
	}()
	command := w.command()
	data := bytes.Repeat([]byte{1, 2, 3, 4}, MaxContentChunk/2)
	foreign := uploadBrowserJPEG(t, other, root, data)
	result := protocol.BrowserCommandResultParams{CommandID: command.CommandID, RootID: root, ProviderEpoch: command.ProviderEpoch, AttachmentGeneration: command.Scope.AttachmentGeneration, DocumentRevision: "doc-1", Screenshot: &foreign}
	if reply := w.rpc("browser.command.result", result); reply.Error == nil {
		t.Fatal("foreign-connection upload accepted")
	}
	own := uploadBrowserJPEG(t, w, root, data)
	bad := own
	bad.Size = 8<<20 + 1
	result.Screenshot = &bad
	if reply := w.rpc("browser.command.result", result); reply.Error == nil {
		t.Fatal("oversized screenshot accepted")
	}
	result.Screenshot = &own
	if reply := w.rpc("browser.command.result", result); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	select {
	case out := <-outcome:
		if out.err != nil {
			t.Fatal(out.err)
		}
		var result struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(out.raw, &result); err != nil {
			t.Fatal(err)
		}
		got, err := base64.StdEncoding.DecodeString(result.Data)
		if err != nil || !bytes.Equal(data, got) {
			t.Fatal("screenshot bytes changed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("screenshot transport stalled")
	}
}

func TestBrowserPreviewApprovalUsesImmutableCurrentScope(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	offer := browserOffer(root)
	preview := capability.BrowserPreviewScope{HostID: "host", HostIdentity: "runtime", ConnectionGeneration: "ssh-1", EnvironmentID: "env", Loopback: "127.0.0.1", Ports: []int{3000}}
	offer.OfferedTabs[0].Preview = &preview
	bindBrowserWire(t, w, offer)
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	attached := attachBrowser(t, d.browserProviders, w, identity, "tab-1")
	expansion, err := d.browserProviders.Resolve(t.Context(), identity, "browser.allow_preview_port", browser.DesktopArguments{AttachmentID: attached.AttachmentID, Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(expansion.Scope.Preview.Ports, []int{3000}) {
		t.Fatal("new port widened admitted current scope")
	}
	if err = d.store.AuthorizeBrowser(t.Context(), root, root, expansion.Grant, "browser.allow_preview_port", expansion.Scope); err != nil {
		t.Fatal(err)
	}
	outcome := executeBrowserAsync(t.Context(), d.browserProviders, identity, "browser.allow_preview_port", expansion)
	command := w.command()
	if command.Kind != "allow_preview_port" || !slices.Equal(command.Scope.Preview.Ports, []int{3000}) {
		t.Fatal("command did not preserve consented scope")
	}
	if reply := w.settle(command, map[string]any{}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	if out := awaitBrowser(t, outcome); out.err != nil {
		t.Fatal(out.err)
	}
	refreshed, err := d.browserProviders.Resolve(t.Context(), identity, "browser.run", browser.DesktopArguments{AttachmentID: attached.AttachmentID})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Grant == expansion.Grant || !slices.Equal(refreshed.Scope.Preview.Ports, []int{3000, 8080}) {
		t.Fatal("approved port did not replace grant")
	}
	if _, err = d.browserProviders.CallContext(expansion); err == nil {
		t.Fatal("stale permission envelope survived scope refresh")
	}
	if err = d.store.AuthorizeBrowser(t.Context(), root, root, expansion.Grant, "browser.run", expansion.Scope); err == nil {
		t.Fatal("old resource grant survived scope refresh")
	}
}

func TestBrowserLiveRevokeCancelsInFlightCommand(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	bindBrowserWire(t, w, browserOffer(root))
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	attached := attachBrowser(t, d.browserProviders, w, identity, "tab-1")
	call, err := d.browserProviders.Resolve(t.Context(), identity, "browser.run", browser.DesktopArguments{AttachmentID: attached.AttachmentID})
	if err != nil {
		t.Fatal(err)
	}
	d.browserProviders.mu.Lock()
	a, err := d.browserProviders.findLocked(call)
	d.browserProviders.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	client := &browserCDP{providers: d.browserProviders, attachment: a, operationID: "pending-effect"}
	pending := make(chan error, 1)
	go func() {
		_, err := client.Call(t.Context(), call.Scope.AttachmentID, "Input.insertText", map[string]string{"text": "x"})
		pending <- err
	}()
	delivered := w.command()
	if _, err := d.store.RevokeCapabilityFor(t.Context(), root, root, call.Grant.ID); err != nil {
		t.Fatal(err)
	}
	completed := make(chan struct{})
	go func() { d.browserProviders.invalidateRevoked(t.Context(), root); close(completed) }()
	detach := w.command()
	if detach.Kind != "detach" {
		t.Fatal(detach.Kind)
	}
	if reply := w.settle(detach, map[string]any{}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("revoke cleanup stuck")
	}
	select {
	case err := <-pending:
		if err == nil {
			t.Fatal("revoked delivered effect succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("revoke did not cancel command")
	}
	if reply := w.settle(delivered, map[string]any{}); reply.Error == nil {
		t.Fatal("revoked late result accepted")
	}
}

func TestBrowserRejectedAtomicTransferRollsBackTentativeGrants(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	bindBrowserWire(t, w, browserOffer(root))
	parent := browser.DesktopIdentity{RootID: root, AgentID: root}
	child := browser.DesktopIdentity{RootID: root, AgentID: "child"}
	if _, err := d.store.AdmitAgent(t.Context(), session.AgentAdmission{RootID: root, ParentAgentID: root, ChildAgentID: child.AgentID, CWD: t.TempDir(), Model: "model", Provider: "provider"}); err != nil {
		t.Fatal(err)
	}
	a := attachBrowser(t, d.browserProviders, w, parent, "tab-1")
	b := attachBrowser(t, d.browserProviders, w, parent, "tab-2")
	outcome := make(chan error, 1)
	go func() {
		_, err := d.browserProviders.Transfer(t.Context(), parent, child, []string{a.AttachmentID, b.AttachmentID})
		outcome <- err
	}()
	command := w.command()
	var transfer protocol.BrowserTransferArguments
	if err := json.Unmarshal(command.Arguments, &transfer); err != nil {
		t.Fatal(err)
	}
	reply := w.rpc("browser.command.result", protocol.BrowserCommandResultParams{CommandID: command.CommandID, RootID: root, ProviderEpoch: command.ProviderEpoch, AttachmentGeneration: command.Scope.AttachmentGeneration, DocumentRevision: "doc-1", Error: &browser.DesktopError{Kind: "browser_busy", Message: "atomic handoff rejected"}})
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	select {
	case err := <-outcome:
		if err == nil {
			t.Fatal("rejected transfer succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rejected transfer stalled")
	}
	if len(d.browserProviders.Attachments(t.Context(), parent)) != 2 || len(d.browserProviders.Attachments(t.Context(), child)) != 0 {
		t.Fatal("atomic rejection partially changed controller ownership")
	}
	for _, mapping := range transfer.Attachments {
		if _, err := d.browserProviders.Resolve(t.Context(), child, "browser.run", browser.DesktopArguments{AttachmentID: mapping.ChildScope.AttachmentID}); err == nil {
			t.Fatal("tentative child handle remained executable")
		}
	}
}

func TestBrowserTransferEntireEightTabCapacity(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	offer := browserOffer(root)
	offer.OfferedTabs = nil
	for range maxBrowserAttachments {
		offer.OfferedTabs = append(offer.OfferedTabs, protocol.BrowserOfferedTab{TabID: browserID(), TabGeneration: browserID(), ProfileID: "profile", DocumentRevision: "doc-1"})
	}
	bindBrowserWire(t, w, offer)
	parent := browser.DesktopIdentity{RootID: root, AgentID: root}
	child := browser.DesktopIdentity{RootID: root, AgentID: "child"}
	if _, err := d.store.AdmitAgent(t.Context(), session.AgentAdmission{RootID: root, ParentAgentID: root, ChildAgentID: child.AgentID, CWD: t.TempDir(), Model: "model", Provider: "provider"}); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, maxBrowserAttachments)
	for _, tab := range offer.OfferedTabs {
		ids = append(ids, attachBrowser(t, d.browserProviders, w, parent, tab.TabID).AttachmentID)
	}
	outcome := make(chan error, 1)
	go func() { _, err := d.browserProviders.Transfer(t.Context(), parent, child, ids); outcome <- err }()
	command := w.command()
	var transfer protocol.BrowserTransferArguments
	if err := json.Unmarshal(command.Arguments, &transfer); err != nil {
		t.Fatal(err)
	}
	if command.Kind != "transfer" || len(transfer.Attachments) != 8 {
		t.Fatal("all eight tabs must transfer atomically")
	}
	if reply := w.settle(command, map[string]any{}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	select {
	case err := <-outcome:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("transfer stuck")
	}
	owned := d.browserProviders.Attachments(t.Context(), child)
	if len(owned) != 8 {
		t.Fatal(len(owned))
	}
	if _, err := d.browserProviders.Resolve(t.Context(), parent, "browser.open", browser.DesktopArguments{URL: "https://example.test/"}); err == nil {
		t.Fatal("capacity exceeded")
	}
	call, err := d.browserProviders.Resolve(t.Context(), child, "browser.detach", browser.DesktopArguments{AttachmentID: owned[0].AttachmentID})
	if err != nil {
		t.Fatal(err)
	}
	detached := executeBrowserAsync(t.Context(), d.browserProviders, child, "browser.detach", call)
	command = w.command()
	if reply := w.settle(command, map[string]any{}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	if result := awaitBrowser(t, detached); result.err != nil {
		t.Fatal(result.err)
	}
	if _, err = d.browserProviders.Resolve(t.Context(), parent, "browser.open", browser.DesktopArguments{URL: "https://example.test/"}); err != nil {
		t.Fatalf("delegation-only ancestors consumed tab capacity: %v", err)
	}
}

func TestBrowserToolHostScreenshotStoredWithOwnAuthority(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID, err := store.Create(session.SessionKindToolHost, t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	services := tools.NewServices()
	services.SetGate(func(context.Context, tools.GateRequest) (tools.GateDecision, string) { return tools.GateAllowOnce, "" })
	d, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: NewToolRunner(services)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(d, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close(); _ = d.Close(); _ = store.Close() })
	root, err := d.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	w := connectBrowserWire(t, server, true)
	bindBrowserWire(t, w, browserOffer(rootID))
	identity := browser.DesktopIdentity{RootID: rootID, AgentID: rootID}
	attached := attachBrowser(t, d.browserProviders, w, identity, "tab-1")
	var data bytes.Buffer
	if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 4, 3)), nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan desktopOutcome, 1)
	go func() {
		args, _ := json.Marshal(map[string]any{"attachment_id": attached.AttachmentID, "code": "screenshot()"})
		raw, err := root.runner.(*toolRunner).CallTool(t.Context(), "browser_run", args)
		var result browser.DesktopResult
		if err == nil {
			err = json.Unmarshal([]byte(raw), &result)
		}
		done <- desktopOutcome{result, err}
	}()
	captured := false
	for {
		command := w.command()
		if command.RootID != rootID || command.AgentID != rootID {
			t.Fatal("tool host borrowed browser authority")
		}
		var args struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(command.Arguments, &args)
		if command.Kind == "cdp" && args.Method == "Page.captureScreenshot" {
			captured = true
			handle := uploadBrowserJPEG(t, w, rootID, data.Bytes())
			reply := w.rpc("browser.command.result", protocol.BrowserCommandResultParams{CommandID: command.CommandID, RootID: rootID, ProviderEpoch: command.ProviderEpoch, AttachmentGeneration: command.Scope.AttachmentGeneration, DocumentRevision: "doc-1", Screenshot: &handle})
			if reply.Error != nil {
				t.Fatal(reply.Error)
			}
		} else {
			response := map[string]any{}
			if args.Method == "Runtime.evaluate" {
				response = map[string]any{"result": map[string]any{"type": "string", "value": `{"url":"","title":"fixture"}`}}
			}
			if reply := w.settle(command, response); reply.Error != nil {
				t.Fatal(reply.Error)
			}
		}
		if command.Kind == "end" {
			break
		}
	}
	out := awaitBrowser(t, done)
	if out.err != nil || out.result.Error != nil || !captured || len(out.result.Media) != 1 {
		t.Fatalf("tool-host media: %+v %v", out.result, out.err)
	}
	got, metadata, err := store.ReadContent(t.Context(), out.result.Media[0], rootID, rootID, 0, MaxContentChunk)
	if err != nil || metadata.MediaType != "image/jpeg" || !bytes.Equal(got, data.Bytes()) {
		t.Fatalf("stored screenshot: %v %+v", err, metadata)
	}
	foreign := createRoot(t, store)
	if _, _, err := store.ReadContent(t.Context(), out.result.Media[0], foreign, foreign, 0, MaxContentChunk); err == nil {
		t.Fatal("foreign root read tool-host media")
	}
}

func TestBrowserExecutionDeadlineIncludesBeginAndBackendConstruction(t *testing.T) {
	for _, stage := range []string{"begin", "connect"} {
		t.Run(stage, func(t *testing.T) {
			d, s, root := browserHarness(t)
			w := connectBrowserWire(t, s, true)
			bindBrowserWire(t, w, browserOffer(root))
			identity := browser.DesktopIdentity{RootID: root, AgentID: root}
			attached := attachBrowser(t, d.browserProviders, w, identity, "tab-1")
			call, err := d.browserProviders.Resolve(t.Context(), identity, "browser.run", browser.DesktopArguments{AttachmentID: attached.AttachmentID, Timeout: .15})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan desktopOutcome, 1)
			invoked := make(chan struct{}, 1)
			started := time.Now()
			go func() {
				result, err := d.browserProviders.Execute(t.Context(), browser.DesktopRequest{Identity: identity, OperationID: "deadline", Operation: "browser.run", Call: call}, func(context.Context, browser.Backend) (string, error) { invoked <- struct{}{}; return "", nil })
				done <- desktopOutcome{result, err}
			}()
			command := w.command()
			if command.Kind != "begin" {
				t.Fatal(command.Kind)
			}
			if command.DeadlineMillis > started.Add(time.Second).UnixMilli() {
				t.Fatal("native begin omitted execution deadline")
			}
			if stage == "connect" {
				if reply := w.settle(command, map[string]any{}); reply.Error != nil {
					t.Fatal(reply.Error)
				}
				command = w.command()
				if command.Kind != "cdp" {
					t.Fatal(command.Kind)
				}
				if command.DeadlineMillis > started.Add(time.Second).UnixMilli() {
					t.Fatal("backend construction omitted execution deadline")
				}
				end := w.command()
				if end.Kind != "end" {
					t.Fatal(end.Kind)
				}
				// Deliberately withhold end ACK: cancellation cleanup must itself be bounded.
			}
			out := awaitBrowser(t, done)
			if out.err == nil || time.Since(started) > 4*time.Second {
				t.Fatalf("unbounded %s: %v", stage, out.err)
			}
			select {
			case <-invoked:
				t.Fatal("helper ran after begin/connect deadline")
			default:
			}
			if reply := w.settle(command, map[string]any{}); reply.Error == nil {
				t.Fatal("late expired command accepted")
			}
		})
	}
}

func TestBrowserPreviewInitialURLPort(t *testing.T) {
	for _, tc := range []struct {
		url, loopback string
		port          int
	}{
		{"http://127.0.0.1:5173/", "127.0.0.1", 5173},
		{"http://127.0.0.1/", "127.0.0.1", 80},
		{"https://127.0.0.1/", "127.0.0.1", 443},
		{"http://[::1]:5173/", "::1", 5173},
		{"https://[::1]/", "::1", 443},
		{"http://localhost:5173/", "127.0.0.1", 0},
		{"http://127.1:5173/", "127.0.0.1", 0},
		{"http://127.0.0.1.evil.test:5173/", "127.0.0.1", 0},
		{"http://127.0.0.1.:5173/", "127.0.0.1", 0},
		{"http://[::1]:5173/", "127.0.0.1", 0},
		{"http://127.0.0.1:5173/", "::1", 0},
		{"http://user:password@127.0.0.1:5173/", "127.0.0.1", 0},
		{"http://127.0.0.1:0/", "127.0.0.1", 0},
		{"http://127.0.0.1:65536/", "127.0.0.1", 0},
		{"http://127.0.0.1:bad/", "127.0.0.1", 0},
		{"file://127.0.0.1/path", "127.0.0.1", 0},
		{"ws://127.0.0.1:5173/", "127.0.0.1", 0},
		{"//127.0.0.1:5173/", "127.0.0.1", 0},
	} {
		t.Run(tc.url+"_"+tc.loopback, func(t *testing.T) {
			port, err := browserPreviewURLPort(tc.url, tc.loopback)
			if (err != nil) != (tc.port == 0) || port != tc.port {
				t.Fatalf("port=%d err=%v", port, err)
			}
		})
	}
}

func TestBrowserPreviewInitialPortIsResolvedBeforeConsentWithoutMutatingOffer(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	offer := browserOffer(root)
	preview := capability.BrowserPreviewScope{HostID: "host", HostIdentity: "runtime", ConnectionGeneration: "ssh-1", EnvironmentID: "env", Loopback: "127.0.0.1", Ports: []int{3000, 8080}}
	empty := preview
	empty.HostID = "empty-host"
	empty.Ports = []int{}
	offer.OfferedPreviewHosts = []capability.BrowserPreviewScope{preview, empty}
	bindBrowserWire(t, w, offer)
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	call, err := d.browserProviders.Resolve(t.Context(), identity, "browser.open", browser.DesktopArguments{URL: "http://127.0.0.1:5173/", PreviewHostID: "host"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(call.Scope.Preview.Ports, []int{3000, 5173, 8080}) || call.Grant.ID != "" {
		t.Fatal("initial port was not part of pre-consent inert scope")
	}
	d.browserProviders.mu.Lock()
	retained := slices.Clone(d.browserProviders.roots[root].offer.OfferedPreviewHosts[0].Ports)
	pending := len(d.browserProviders.pending)
	d.browserProviders.mu.Unlock()
	if !slices.Equal(retained, []int{3000, 8080}) || pending != 0 {
		t.Fatal("resolving mutated offer or sent native command before consent")
	}
	initial, err := d.browserProviders.Resolve(t.Context(), identity, "browser.open", browser.DesktopArguments{URL: "http://127.0.0.1:5173/", PreviewHostID: "empty-host"})
	if err != nil || !slices.Equal(initial.Scope.Preview.Ports, []int{5173}) {
		t.Fatalf("empty preview offer initial route: %+v %v", initial, err)
	}
	duplicate, err := d.browserProviders.Resolve(t.Context(), identity, "browser.open", browser.DesktopArguments{URL: "https://127.0.0.1:3000/", PreviewHostID: "host"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(duplicate.Scope.Preview.Ports, []int{3000, 8080}) {
		t.Fatal("initial port not deduplicated")
	}
	outcome := executeBrowserAsync(t.Context(), d.browserProviders, identity, "browser.open", call)
	command := w.command()
	if command.Kind != "open" || !slices.Equal(command.Scope.Preview.Ports, []int{3000, 5173, 8080}) {
		t.Fatal("native open omitted consented initial route")
	}
	if reply := w.settle(command, browser.DesktopResult{URL: "http://127.0.0.1:5173/"}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	if result := awaitBrowser(t, outcome); result.err != nil || !slices.Equal(result.result.Network.Ports, []int{3000, 5173, 8080}) {
		t.Fatalf("initial preview route: %+v", result)
	}
}

func (w *browserWire) revoked() protocol.BrowserProviderRevoked {
	w.t.Helper()
	decode := func(message rpcMessage) protocol.BrowserProviderRevoked {
		var result protocol.BrowserProviderRevoked
		if err := json.Unmarshal(message.Params, &result); err != nil {
			w.t.Fatal(err)
		}
		return result
	}
	for i, message := range w.queued {
		if message.Method == "browser.provider.revoked" {
			w.queued = slices.Delete(w.queued, i, i+1)
			return decode(message)
		}
	}
	for {
		message := w.read()
		if message.Method == "browser.provider.revoked" {
			return decode(message)
		}
		w.queued = append(w.queued, message)
	}
}

func TestBrowserProviderIdleReplacementAndUnbindNotifyExactLease(t *testing.T) {
	d, s, root := browserHarness(t)
	first := connectBrowserWire(t, s, true)
	second := connectBrowserWire(t, s, true)
	original := bindBrowserWire(t, first, browserOffer(root))
	replacementOffer := browserOffer(root)
	replacementOffer.ExpectedProviderEpoch = original.ProviderEpoch
	replacement := bindBrowserWire(t, second, replacementOffer)
	revoked := first.revoked()
	if revoked.RootID != root || revoked.ProviderID != original.ProviderID || revoked.ProviderEpoch != original.ProviderEpoch || revoked.Reason != "replaced" {
		t.Fatalf("wrong idle replacement notification: %+v", revoked)
	}
	for _, attempt := range []struct {
		wire  *browserWire
		epoch string
	}{{first, original.ProviderEpoch}, {first, replacement.ProviderEpoch}, {second, original.ProviderEpoch}, {second, ""}} {
		if reply := attempt.wire.rpc("browser.provider.unbind", protocol.BrowserProviderUnbindParams{RootID: root, ProviderEpoch: attempt.epoch}); reply.Error == nil {
			t.Fatal("foreign same-client-ID or stale epoch unbound replacement")
		}
	}
	d.browserProviders.mu.Lock()
	active := d.browserProviders.roots[root]
	unchanged := active != nil && active.id == replacement.ProviderID && active.ctx.Err() == nil && len(active.attachments) == 0 && len(d.browserProviders.pending) == 0
	d.browserProviders.mu.Unlock()
	if !unchanged {
		t.Fatal("idle replacement affected by rejected unbind")
	}
	params := protocol.BrowserProviderUnbindParams{RootID: root, ProviderEpoch: replacement.ProviderEpoch}
	if reply := second.rpc("browser.provider.unbind", params); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	revoked = second.revoked()
	if revoked.RootID != root || revoked.ProviderID != replacement.ProviderID || revoked.ProviderEpoch != replacement.ProviderEpoch || revoked.Reason != "unbound" {
		t.Fatalf("wrong idle unbind notification: %+v", revoked)
	}
	if _, err := d.browserProviders.Resolve(t.Context(), browser.DesktopIdentity{RootID: root, AgentID: root}, "browser.attach", browser.DesktopArguments{TabID: "tab-1"}); err == nil {
		t.Fatal("unbound provider still resolves")
	}
	if reply := second.rpc("browser.provider.unbind", params); reply.Error != nil {
		t.Fatalf("matching explicit unbind was not idempotent: %v", reply.Error)
	}
	if reply := first.rpc("browser.provider.unbind", params); reply.Error == nil {
		t.Fatal("foreign pointer reused released epoch")
	}
	fresh := bindBrowserWire(t, second, browserOffer(root))
	if reply := second.rpc("browser.provider.unbind", params); reply.Error != nil {
		t.Fatalf("matching released epoch was not idempotent across replacement: %v", reply.Error)
	}
	d.browserProviders.mu.Lock()
	active = d.browserProviders.roots[root]
	unchanged = active != nil && active.id == fresh.ProviderID && active.ctx.Err() == nil
	d.browserProviders.mu.Unlock()
	if !unchanged || len(second.queued) != 0 {
		t.Fatal("stale unbind affected new native binding")
	}
	freshParams := protocol.BrowserProviderUnbindParams{RootID: root, ProviderEpoch: fresh.ProviderEpoch}
	if reply := second.rpc("browser.provider.unbind", freshParams); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	if revoked := second.revoked(); revoked.ProviderEpoch != fresh.ProviderEpoch {
		t.Fatal("wrong new unbind notification")
	}
	if reply := second.rpc("browser.provider.unbind", params); reply.Error == nil {
		t.Fatal("retained more than last explicitly released epoch for connection/root")
	}
}

func TestBrowserProviderUnbindCancelsAuthorityAndRejectsLateResult(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	bound := bindBrowserWire(t, w, browserOffer(root))
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	attached := attachBrowser(t, d.browserProviders, w, identity, "tab-1")
	call, err := d.browserProviders.Resolve(t.Context(), identity, "browser.run", browser.DesktopArguments{AttachmentID: attached.AttachmentID})
	if err != nil {
		t.Fatal(err)
	}
	callContext, err := d.browserProviders.CallContext(call)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan desktopOutcome, 1)
	go func() {
		result, err := d.browserProviders.Execute(t.Context(), browser.DesktopRequest{Identity: identity, OperationID: "unbind-active", Operation: "browser.run", Call: call}, func(context.Context, browser.Backend) (string, error) { return "must not execute", nil })
		done <- desktopOutcome{result, err}
	}()
	command := w.command()
	if command.Kind != "begin" {
		t.Fatal(command.Kind)
	}
	if reply := w.rpc("browser.provider.unbind", protocol.BrowserProviderUnbindParams{RootID: root, ProviderEpoch: bound.ProviderEpoch}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	if revoked := w.revoked(); revoked.ProviderID != bound.ProviderID || revoked.Reason != "unbound" {
		t.Fatalf("wrong teardown: %+v", revoked)
	}
	if callContext.Err() == nil {
		t.Fatal("call context survived unbind")
	}
	if out := awaitBrowser(t, done); out.err == nil {
		t.Fatal("active command survived unbind")
	}
	if reply := w.settle(command, map[string]any{}); reply.Error == nil {
		t.Fatal("late command settled after unbind")
	}
	if err := d.store.AuthorizeBrowser(t.Context(), root, root, call.Grant, "browser.run", call.Scope); err == nil {
		t.Fatal("persisted grant survived unbind")
	}
}

func TestBrowserProviderReleaseTombstonesBoundedAndClearedOnDisconnect(t *testing.T) {
	p := newBrowserProviders(nil)
	holder := &serverConn{}
	oldest := browserReleaseKey{holder: holder, rootID: "oldest"}
	p.mu.Lock()
	p.rememberReleaseLocked(oldest, "old")
	var latest browserReleaseKey
	for range maxBrowserReleases {
		latest = browserReleaseKey{holder: holder, rootID: browserID()}
		p.rememberReleaseLocked(latest, "latest")
	}
	if len(p.released) != maxBrowserReleases || len(p.releaseOrder) != maxBrowserReleases {
		t.Fatal("release history is unbounded")
	}
	if _, ok := p.released[oldest]; ok {
		t.Fatal("oldest release not evicted")
	}
	p.rememberReleaseLocked(latest, "newest")
	if len(p.released) != maxBrowserReleases || len(p.releaseOrder) != maxBrowserReleases || p.released[latest] != "newest" {
		t.Fatal("last released epoch not replaced")
	}
	p.mu.Unlock()
	if err := p.unbind(holder, protocol.BrowserProviderUnbindParams{RootID: latest.rootID, ProviderEpoch: "newest"}); err != nil {
		t.Fatal(err)
	}
	if err := p.unbind(&serverConn{}, protocol.BrowserProviderUnbindParams{RootID: latest.rootID, ProviderEpoch: "newest"}); err == nil {
		t.Fatal("foreign pointer reused release record")
	}
	p.disconnect(holder)
	p.mu.Lock()
	cleared := len(p.released) == 0 && len(p.releaseOrder) == 0
	p.mu.Unlock()
	if !cleared {
		t.Fatal("connection teardown retained release history")
	}
	if err := p.unbind(holder, protocol.BrowserProviderUnbindParams{RootID: latest.rootID, ProviderEpoch: "newest"}); err == nil {
		t.Fatal("disconnected holder reused release record")
	}
}

func TestBrowserProviderDaemonShutdownNotifiesIdleLease(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	bound := bindBrowserWire(t, w, browserOffer(root))
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	revoked := w.revoked()
	if revoked.RootID != root || revoked.ProviderID != bound.ProviderID || revoked.ProviderEpoch != bound.ProviderEpoch || revoked.Reason != "shutdown" {
		t.Fatalf("wrong shutdown notification: %+v", revoked)
	}
	d.browserProviders.mu.Lock()
	closed := d.browserProviders.closed && len(d.browserProviders.roots) == 0 && len(d.browserProviders.released) == 0
	d.browserProviders.mu.Unlock()
	if !closed {
		t.Fatal("shutdown did not clear broker registries")
	}
}

func browserEventForScope(root string, scope capability.BrowserScope) protocol.BrowserProviderEventParams {
	return protocol.BrowserProviderEventParams{RootID: root, ProviderEpoch: scope.ProviderEpoch, TabID: scope.TabID, TabGeneration: scope.TabGeneration, AttachmentID: scope.AttachmentID, AttachmentGeneration: scope.AttachmentGeneration, Sequence: 1, DocumentRevision: "doc-1", Kind: "state"}
}

func TestBrowserRetiredEventIsTypedStaleWithoutAffectingOtherAttachment(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	bound := bindBrowserWire(t, w, browserOffer(root))
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	attachedA := attachBrowser(t, d.browserProviders, w, identity, "tab-1")
	attachedB := attachBrowser(t, d.browserProviders, w, identity, "tab-2")
	a, err := d.browserProviders.Resolve(t.Context(), identity, "browser.run", browser.DesktopArguments{AttachmentID: attachedA.AttachmentID})
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.browserProviders.Resolve(t.Context(), identity, "browser.run", browser.DesktopArguments{AttachmentID: attachedB.AttachmentID})
	if err != nil {
		t.Fatal(err)
	}
	retired := browserEventForScope(root, a.Scope)
	retired.Kind = "closed"
	if reply := w.rpc("browser.provider.event", retired); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	late := retired
	late.Sequence = 2
	late.Kind = "document"
	late.DocumentRevision = "must-not-apply"
	late.Title = "must-not-apply"
	assertStale := func(event protocol.BrowserProviderEventParams) {
		t.Helper()
		reply := w.rpc("browser.provider.event", event)
		if reply.Error == nil || reply.Error.Code != -32009 || reply.Error.Data == nil || reply.Error.Data.Kind != "browser_event_stale" {
			t.Fatalf("expected narrowly typed stale event: %+v", reply.Error)
		}
	}
	assertStale(late)
	missing := late
	missing.AttachmentID = "missing-attachment"
	assertStale(missing)
	staleB := browserEventForScope(root, b.Scope)
	staleB.AttachmentGeneration = "old-generation"
	assertStale(staleB)
	staleB = browserEventForScope(root, b.Scope)
	staleB.TabGeneration = "old-tab-generation"
	assertStale(staleB)
	currentB := browserEventForScope(root, b.Scope)
	currentB.DocumentRevision = "doc-b-current"
	currentB.Title = "B still valid"
	if reply := w.rpc("browser.provider.event", currentB); reply.Error != nil {
		t.Fatalf("A retirement or stale B generation affected live B: %v", reply.Error)
	}
	if err := d.store.AuthorizeBrowser(t.Context(), root, root, b.Grant, "browser.run", b.Scope); err != nil {
		t.Fatalf("B authority changed: %v", err)
	}
	if err := d.store.AuthorizeBrowser(t.Context(), root, root, a.Grant, "browser.run", a.Scope); err == nil {
		t.Fatal("late event restored A grant")
	}
	if _, err := d.browserProviders.CallContext(a); err == nil {
		t.Fatal("late event restored A context")
	}
	d.browserProviders.mu.Lock()
	lease := d.browserProviders.roots[root]
	recordA := lease.attachments[a.Scope.AttachmentID]
	recordB := lease.attachments[b.Scope.AttachmentID]
	unchanged := lease.id == bound.ProviderID && lease.epoch == bound.ProviderEpoch && len(lease.attachments) == 2 && !recordA.live && recordA.ctx.Err() != nil && recordA.sequence == 1 && recordA.result.DocumentRevision == "doc-1" && recordA.result.Title != "must-not-apply" && recordB.live && recordB.grant == b.Grant && recordB.sequence == 1 && recordB.result.DocumentRevision == "doc-b-current"
	d.browserProviders.mu.Unlock()
	if !unchanged {
		t.Fatal("stale events minted/rebound authority, mutated retired attachment, or disturbed live B")
	}
	owned := d.browserProviders.Attachments(t.Context(), identity)
	if len(owned) != 1 || owned[0].AttachmentID != b.Scope.AttachmentID {
		t.Fatal("retired A became visible again")
	}
}

func TestBrowserEventStaleKindDoesNotHideRealProviderFailures(t *testing.T) {
	d, s, root := browserHarness(t)
	w := connectBrowserWire(t, s, true)
	foreign := connectBrowserWire(t, s, true)
	bindBrowserWire(t, w, browserOffer(root))
	identity := browser.DesktopIdentity{RootID: root, AgentID: root}
	attached := attachBrowser(t, d.browserProviders, w, identity, "tab-1")
	call, err := d.browserProviders.Resolve(t.Context(), identity, "browser.run", browser.DesktopArguments{AttachmentID: attached.AttachmentID})
	if err != nil {
		t.Fatal(err)
	}
	event := browserEventForScope(root, call.Scope)
	realFailure := func(wire *browserWire, event protocol.BrowserProviderEventParams) {
		t.Helper()
		reply := wire.rpc("browser.provider.event", event)
		if reply.Error == nil || reply.Error.Data == nil || reply.Error.Data.Kind == "browser_event_stale" {
			t.Fatalf("real provider failure classified stale: %+v", reply.Error)
		}
	}
	realFailure(foreign, event)
	wrong := event
	wrong.ProviderEpoch = "wrong-epoch"
	wrong.AttachmentID = "missing"
	realFailure(w, wrong)
	wrong = event
	wrong.RootID = "wrong-root"
	wrong.AttachmentID = "missing"
	realFailure(w, wrong)
	wrong = event
	wrong.TabID = "wrong-tab"
	realFailure(w, wrong)
	wrong = event
	wrong.Kind = "unknown-kind"
	wrong.AttachmentID = "missing"
	realFailure(w, wrong)
	wrong = event
	wrong.Title = string(bytes.Repeat([]byte{'x'}, 4097))
	wrong.AttachmentID = "missing"
	realFailure(w, wrong)
	wrong = event
	wrong.Sequence = 0
	wrong.AttachmentID = "missing"
	realFailure(w, wrong)
	wrong = event
	wrong.Sequence = 2
	realFailure(w, wrong)
	if _, err := d.browserProviders.CallContext(call); err == nil {
		t.Fatal("live sequence gap failed to revoke attachment")
	}
}
