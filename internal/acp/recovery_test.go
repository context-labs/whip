package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestBridgeLostAcknowledgementKeepsOneOriginalInput(t *testing.T) {
	f := nativeFixture(t, nil, nil)
	socket, submissions := dropSubmitAcknowledgement(t, f.host.SocketPath())
	c, err := client.Connect(t.Context(), socket, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	f.bridge.client = c
	// Close observers before shutting down their deliberately faulty proxy.
	t.Cleanup(f.bridge.CloseAll)
	id := f.newSession(t)
	response := f.prompt(t, id, "exact original")
	if response.StopReason != acpsdk.StopReasonEndTurn || submissions.Load() != 1 {
		t.Fatalf("response=%+v sends=%d", response, submissions.Load())
	}
	handle, err := f.native.Session(protocol.ID(id))
	if err != nil {
		t.Fatal(err)
	}
	page, err := handle.Inputs(t.Context(), "all", nil, 100)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("canonical inputs=%+v %v", page, err)
	}
	input, err := handle.Input(t.Context(), page.Items[0].ID)
	if err != nil || len(input.Parts) != 1 || input.Parts[0].Text != "exact original" {
		t.Fatalf("original payload=%+v %v", input, err)
	}
}

// Forward to the actual disposable host, then discard exactly the submission
// acknowledgement. Every recovery request still reaches canonical storage.
func dropSubmitAcknowledgement(t *testing.T, target string) (string, *atomic.Int32) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "whip-acp-proxy-") //nolint:usetesting // macOS socket bound.
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "host.sock")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	var workers sync.WaitGroup
	accepted := make(chan struct{})
	var submissions atomic.Int32
	go func() {
		defer close(accepted)
		for {
			peer, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Go(func() {
				defer peer.Close()
				closePeer := context.AfterFunc(ctx, func() { _ = peer.Close() })
				defer closePeer()
				upstream, err := (&net.Dialer{}).DialContext(ctx, "unix", target)
				if err != nil {
					return
				}
				defer upstream.Close()
				closeUpstream := context.AfterFunc(ctx, func() { _ = upstream.Close() })
				defer closeUpstream()
				_ = peer.SetDeadline(time.Now().Add(10 * time.Second))
				_ = upstream.SetDeadline(time.Now().Add(10 * time.Second))
				incoming, response := bufio.NewScanner(peer), bufio.NewScanner(upstream)
				incoming.Buffer(make([]byte, 4096), protocol.MaxFrameBytes+1)
				response.Buffer(make([]byte, 4096), protocol.MaxFrameBytes+1)
				for incoming.Scan() {
					var request protocol.Request
					if json.Unmarshal(incoming.Bytes(), &request) != nil {
						return
					}
					if _, err := upstream.Write(append(incoming.Bytes(), '\n')); err != nil || !response.Scan() {
						return
					}
					if request.Method == "sessions.submit" && submissions.Add(1) == 1 {
						return
					}
					if _, err := peer.Write(append(response.Bytes(), '\n')); err != nil {
						return
					}
				}
			})
		}
	}()
	t.Cleanup(func() { stop(); _ = listener.Close(); <-accepted; workers.Wait(); _ = os.RemoveAll(dir) })
	return socket, &submissions
}

func TestBridgeCallerContextDoesNotCancelHostInput(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	f := nativeFixture(t, func(ctx context.Context, _ model.Request, _ func(model.Chunk)) (model.Response, error) {
		close(entered)
		select {
		case <-release:
			return textResponse("finished independently"), nil
		case <-ctx.Done():
			return model.Response{}, ctx.Err()
		}
	}, nil)
	id := f.newSession(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		// Call the bridge boundary directly: ClientSideConnection.Prompt sends
		// an explicit session/cancel notification when its context ends.
		_, err := f.bridge.Prompt(ctx, acpsdk.PromptRequest{SessionId: id, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock("keep running")}})
		done <- err
	}()
	<-entered
	cancel()
	select {
	case err := <-done:
		t.Fatalf("attachment-owned observation ended with its caller: %v", err)
	default:
	}
	s := f.bridge.getSession(id)
	s.mu.Lock()
	command := s.current
	s.mu.Unlock()
	admission, found, err := command.Check(t.Context())
	if err != nil || !found || admission.Turn == nil || admission.Turn.State != "running" {
		t.Fatalf("observer abort cancelled execution: %+v %v", admission, err)
	}
	close(release)
	settled, err := command.Wait(t.Context())
	if err != nil || settled.Turn == nil || settled.Turn.State != "succeeded" {
		t.Fatalf("host did not complete independently: turn=%+v %v", settled.Turn, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	await(t, func() bool { return len(s.turnCh) == 0 })
}

func TestBridgeSDKCallerAbortSendsExplicitHostCancellation(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	f := nativeFixture(t, func(ctx context.Context, _ model.Request, _ func(model.Chunk)) (model.Response, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		return model.Response{}, ctx.Err()
	}, nil)
	id := f.newSession(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.conn.Prompt(ctx, acpsdk.PromptRequest{SessionId: id, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock("cancel through SDK")}})
		done <- err
	}()
	<-entered
	cancel()
	var cancelledRequest *acpsdk.RequestError
	if err := <-done; !errors.As(err, &cancelledRequest) || cancelledRequest.Code != -32800 {
		t.Fatalf("SDK caller abort: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("SDK session/cancel did not cancel provider")
	}
	s := f.bridge.getSession(id)
	s.mu.Lock()
	command := s.current
	s.mu.Unlock()
	settled, err := command.Wait(t.Context())
	if err != nil || settled.Turn == nil || settled.Turn.State != "cancelled" {
		t.Fatalf("explicit SDK cancellation: turn=%+v %v", settled.Turn, err)
	}
	await(t, func() bool { return len(s.turnCh) == 0 })
}

func TestBridgeLoadsExistingQuestionAndExternalPermissionMode(t *testing.T) {
	f := nativeFixture(t, codeProvider(`print(user.ask(questions=[{"question":"Pick","options":[{"label":"A"},{"label":"B"}]}]))`), &fakeACPClient{answer: "1"})
	id := f.newSession(t)
	if _, err := f.conn.CloseSession(t.Context(), acpsdk.CloseSessionRequest{SessionId: id}); err != nil {
		t.Fatal(err)
	}
	handle, _ := f.native.Session(protocol.ID(id))
	command, err := handle.Submission(protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "other", RequestID: "open-question"}, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "ask"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := command.Send(t.Context()); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool {
		values, err := f.host.Questions(t.Context(), session.SessionID(id), true, "", 100)
		return err == nil && len(values) == 1
	})
	if _, err := f.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: id, Cwd: f.cwd, McpServers: []acpsdk.McpServer{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := command.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	questions, err := f.host.Questions(t.Context(), session.SessionID(id), false, "", 100)
	if err != nil || len(questions) != 1 || questions[0].State != session.QuestionAnswered || questions[0].Answers[0].Answer[0] != "B" {
		t.Fatalf("loaded question=%+v %v", questions, err)
	}
	var policy protocol.PermissionPolicy
	if err := f.native.Call(t.Context(), "permissions.policy", protocol.SessionParams{SessionID: protocol.ID(id)}, &policy); err != nil {
		t.Fatal(err)
	}
	var changed protocol.PermissionModeEdit
	if err := f.native.Call(t.Context(), "permissions.set_mode", protocol.SetPermissionModeParams{SessionID: protocol.ID(id), EditID: "external-mode", ExpectedRevision: policy.Revision, Mode: "automatic"}, &changed); err != nil {
		t.Fatal(err)
	}
	await(t, func() bool {
		f.editor.mu.Lock()
		defer f.editor.mu.Unlock()
		for _, update := range f.editor.updates {
			if value := update.Update.CurrentModeUpdate; value != nil && value.CurrentModeId == ModeAuto {
				return true
			}
		}
		return false
	})
}

func TestBridgeImageIsOwnerScopedAndReplays(t *testing.T) {
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	request := make(chan model.Request, 1)
	f := nativeFixture(t, func(_ context.Context, r model.Request, _ func(model.Chunk)) (model.Response, error) {
		request <- r
		return textResponse("image received"), nil
	}, nil)
	id := f.newSession(t)
	if _, err := f.conn.Prompt(t.Context(), acpsdk.PromptRequest{SessionId: id, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock("image"), {Image: &acpsdk.ContentBlockImage{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(body.Bytes())}}}}); err != nil {
		t.Fatal(err)
	}
	actual := <-request
	if len(actual.Contents) != 1 {
		t.Fatalf("model content=%+v", actual.Contents)
	}
	var ref protocol.ID
	for id, content := range actual.Contents {
		ref = protocol.ID(id)
		if content.MediaType != "image/png" || !bytes.Equal(content.Data, body.Bytes()) {
			t.Fatal("model image differs from original normalized content")
		}
	}
	other := f.newSession(t)
	handle, _ := f.native.Session(protocol.ID(other))
	if _, _, err := handle.ReadContent(t.Context(), ref); err == nil {
		t.Fatal("another root read the image reference")
	}
	if _, err := f.conn.CloseSession(t.Context(), acpsdk.CloseSessionRequest{SessionId: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: id, Cwd: f.cwd, McpServers: []acpsdk.McpServer{}}); err != nil {
		t.Fatal(err)
	}
	f.editor.mu.Lock()
	defer f.editor.mu.Unlock()
	images := 0
	for _, update := range f.editor.updates {
		if value := update.Update.UserMessageChunk; value != nil && value.Content.Image != nil {
			images++
			if value.Content.Image.Data != base64.StdEncoding.EncodeToString(body.Bytes()) {
				t.Fatal("replayed image changed")
			}
		}
	}
	if images != 1 {
		t.Fatalf("replayed images=%d", images)
	}
}

func TestBridgeDetachDoesNotResolvePendingPermission(t *testing.T) {
	gate := make(chan struct{})
	f := nativeFixture(t, codeProvider(`files.write(path="approved-later.txt",content="once")`), &fakeACPClient{permissionGate: gate})
	id := f.newSession(t)
	done := make(chan error, 1)
	go func() {
		_, err := f.conn.Prompt(t.Context(), acpsdk.PromptRequest{SessionId: id, Prompt: []acpsdk.ContentBlock{acpsdk.TextBlock("write")}})
		done <- err
	}()
	await(t, func() bool { f.editor.mu.Lock(); defer f.editor.mu.Unlock(); return len(f.editor.perms) == 1 })
	s := f.bridge.getSession(id)
	s.mu.Lock()
	command := s.current
	s.mu.Unlock()
	if _, err := f.conn.CloseSession(t.Context(), acpsdk.CloseSessionRequest{SessionId: id}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("detach did not join prompt observation")
	}
	var pending protocol.PermissionsResult
	if err := f.native.Call(t.Context(), "permissions.list", protocol.PermissionsParams{SessionID: protocol.ID(id), PendingOnly: true, Limit: 100}, &pending); err != nil || len(pending.Items) != 1 {
		t.Fatalf("detach resolved pending permission: %+v %v", pending, err)
	}
	close(gate)
	if _, err := f.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: id, Cwd: f.cwd, McpServers: []acpsdk.McpServer{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := command.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(f.cwd, "approved-later.txt"))
	if err != nil || string(data) != "once" {
		t.Fatalf("later explicit decision lost effect: %q %v", data, err)
	}
}

func TestBridgeProjectionFailureDoesNotAdvancePastUndeliveredOutput(t *testing.T) {
	f := nativeFixture(t, nil, nil)
	id := f.newSession(t)
	original := f.bridge.getSession(id)
	if _, err := f.conn.CloseSession(t.Context(), acpsdk.CloseSessionRequest{SessionId: id}); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	observer, err := original.handle.Observer(client.ObservationCursor{})
	if err != nil {
		t.Fatal(err)
	}
	s := &acpSession{id: id, handle: original.handle, tree: original.tree, lifecycle: ctx, stop: stop, observer: observer, policy: original.policy, pending: map[protocol.ID]*decisionWork{}}
	s.presentation.emit = func(acpsdk.SessionUpdate) error { return errors.New("editor projection failed") }
	done := make(chan struct{})
	go func() { defer close(done); f.bridge.consume(s) }()
	t.Cleanup(func() { stop(); <-done })
	command, err := original.handle.Submission(protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "projection", RequestID: "failure"}, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "project"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := command.Send(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := command.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("failed projection kept advancing its observer")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure == nil || s.failure.Error() != "editor projection failed" {
		t.Fatalf("failure was hidden: %v", s.failure)
	}
	var snapshot protocol.HistorySnapshot
	if err := f.native.Call(t.Context(), "context.snapshot", protocol.SessionParams{SessionID: protocol.ID(id)}, &snapshot); err != nil || s.cursor.After >= snapshot.ThroughSequence {
		t.Fatalf("undelivered output acknowledged: cursor=%+v snapshot=%+v %v", s.cursor, snapshot, err)
	}
}
