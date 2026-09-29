package acp

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/protocol"
)

func unboundACPFixture(t *testing.T) (*Bridge, *acpsdk.AgentSideConnection, *fakeACPClient) {
	t.Helper()
	input, peerOutput, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	peerInput, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewBridge("startup", nil, Options{})
	editor := &fakeACPClient{}
	agent := acpsdk.NewAgentSideConnection(bridge, output, input)
	peer := acpsdk.NewClientSideConnection(editor, peerOutput, peerInput)
	t.Cleanup(func() {
		_ = input.Close()
		_ = peerOutput.Close()
		_ = peerInput.Close()
		_ = output.Close()
		bridge.CloseAll()
		<-agent.Done()
		<-peer.Done()
	})
	return bridge, agent, editor
}

func startupSession(b *Bridge) *acpSession {
	ctx, stop := context.WithCancel(b.lifecycle)
	s := &acpSession{id: "early", lifecycle: ctx, stop: stop, pending: map[protocol.ID]*decisionWork{}}
	b.mu.Lock()
	b.sessions[s.id] = s
	b.mu.Unlock()
	return s
}

func TestConnectionPublicationPreservesEarlyNotificationAndPermission(t *testing.T) {
	b, agent, editor := unboundACPFixture(t)
	if err := b.SetAgentConnection(nil); err == nil {
		t.Fatal("nil connection accepted")
	}
	s := startupSession(b)
	updateDone := make(chan error, 1)
	go func() {
		updateDone <- b.update(t.Context(), s.id, acpsdk.UpdateAgentMessageText("initial notification"))
	}()
	decisionEntered := make(chan struct{})
	decisionDone := make(chan error, 1)
	b.startDecision(s, "initial-permission", func(ctx context.Context) {
		close(decisionEntered)
		value, err := b.awaitDecision(ctx, acpsdk.RequestPermissionRequest{SessionId: s.id, ToolCall: acpsdk.ToolCallUpdate{ToolCallId: "initial-permission", Title: new("initial permission")}, Options: []acpsdk.PermissionOption{{OptionId: optAllowOnce, Name: "Allow once", Kind: acpsdk.PermissionOptionKindAllowOnce}}}, func(context.Context) bool { return true })
		if err == nil && (value.Outcome.Selected == nil || value.Outcome.Selected.OptionId != optAllowOnce) {
			err = errors.New("early permission result lost")
		}
		decisionDone <- err
	})
	select {
	case <-decisionEntered:
	case <-time.After(time.Second):
		t.Fatal("early permission was dropped before publication")
	}
	select {
	case err := <-updateDone:
		t.Fatalf("early notification did not await publication: %v", err)
	case err := <-decisionDone:
		t.Fatalf("early permission did not await publication: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := b.SetAgentConnection(agent); err != nil {
		t.Fatal(err)
	}
	for _, done := range []<-chan error{updateDone, decisionDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("early outbound action did not finish")
		}
	}
	deadline := time.Now().Add(time.Second)
	for {
		editor.mu.Lock()
		updates, permissions := len(editor.updates), len(editor.perms)
		var text string
		if updates == 1 && editor.updates[0].Update.AgentMessageChunk != nil {
			text = editor.updates[0].Update.AgentMessageChunk.Content.Text.Text
		}
		editor.mu.Unlock()
		if updates == 1 && permissions == 1 {
			if text != "initial notification" {
				t.Fatal(text)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("outbound actions were lost or duplicated", updates, permissions)
		}
		time.Sleep(time.Millisecond)
	}
	if err := b.SetAgentConnection(agent); err == nil {
		t.Fatal("connection rebound after publication")
	}
	if bound, err := b.connection(t.Context()); err != nil || bound != agent {
		t.Fatal(bound, err)
	}
}

func TestCloseBeforeConnectionPublicationJoinsPendingDecision(t *testing.T) {
	b := NewBridge("startup", nil, Options{})
	s := startupSession(b)
	entered := make(chan struct{})
	decisionDone := make(chan error, 1)
	b.startDecision(s, "unpublished", func(ctx context.Context) {
		close(entered)
		_, err := b.awaitDecision(ctx, acpsdk.RequestPermissionRequest{}, func(context.Context) bool { return true })
		decisionDone <- err
	})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("decision did not start")
	}
	updateDone := make(chan error, 1)
	go func() {
		updateDone <- b.update(t.Context(), s.id, acpsdk.UpdateAgentMessageText("must not be silently dropped"))
	}()
	closed := make(chan struct{})
	go func() { b.CloseAll(); close(closed) }()
	for _, done := range []<-chan error{decisionDone, updateDone} {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("unpublished outbound action did not cancel")
		}
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close did not join pending decision")
	}
	if err := b.SetAgentConnection(&acpsdk.AgentSideConnection{}); err == nil {
		t.Fatal("closed bridge was rebound")
	}
}
