package client_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runtime"
)

type blockingProvider struct {
	started chan struct{}
	once    sync.Once
}

func (p *blockingProvider) Prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	prepared, err := (model.Scripted{}).Prepare(ctx, request)
	if err != nil {
		return prepared, err
	}
	if request.Messages[len(request.Messages)-1].Parts[0].Text == "cancel me" {
		prepared.Execute = func(ctx context.Context, _ func(model.Chunk)) (model.Response, error) {
			p.once.Do(func() { close(p.started) })
			<-ctx.Done()
			return model.Response{}, ctx.Err()
		}
	}
	return prepared, nil
}

func TestNativeGoSessionUsesRealRuntimeReceiptsHistoryAndExplicitCancellation(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "whip-go-runtime-") //nolint:usetesting // Unix sockets must fit the macOS path limit.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	provider := &blockingProvider{started: make(chan struct{})}
	r, err := runtime.Open(t.Context(), directory, provider, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	server, err := rpc.Listen(r, rpc.HostServices{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-serverDone; err != nil {
			t.Error(err)
		}
	})
	c, err := client.Connect(t.Context(), r.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	var created protocol.CreateTreeResult
	if err := c.Call(t.Context(), "trees.create", protocol.CreateTreeParams{CreationID: "go_create", Engine: "starlark", Definition: c.Builtins()[0], WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{ReportMode: new("message"), Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}}}, &created); err != nil {
		t.Fatal(err)
	}
	s, err := c.Session(created.Root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutContent(t.Context(), "content", "text/plain", []byte("fixture evidence")); err != nil {
		t.Fatal(err)
	}
	_, body, err := s.ReadContent(t.Context(), "content")
	if err != nil || string(body) != "fixture evidence" {
		t.Fatal(string(body), err)
	}
	input := protocol.SubmitParams{Source: "user", Identity: protocol.RequestIdentity{ClientID: "go", RequestID: "first"}, Parts: []protocol.Part{{Type: "text", Text: "hello"}, {Type: "content", ReferenceID: "content"}}}
	command, err := s.Submission(input)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := command.Send(t.Context())
	if err != nil || accepted.Input.State != "queued" {
		t.Fatal(accepted, err)
	}
	activity, err := s.Activity(t.Context())
	if err != nil || activity.QueuedInputCount != 1 {
		t.Fatal(activity, err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	wait, cancelWait := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelWait()
	done, err := command.Wait(wait)
	if err != nil || done.Turn.State != "succeeded" {
		t.Fatal(done, err)
	}
	observer, err := s.Observer(client.ObservationCursor{})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := observer.Next(t.Context())
	if err != nil || len(observation.Messages) != 2 || observation.Messages[0].InputID == nil || *observation.Messages[0].InputID != done.Input.ID {
		t.Fatal(observation, err)
	}
	page, err := s.History(t.Context(), protocol.HistoryPageParams{Direction: "backward", Limit: 1})
	if err != nil || len(page.Messages) != 1 || page.NextCursor == nil {
		t.Fatal(page, err)
	}
	input.Identity.RequestID = "cancel"
	input.Parts = []protocol.Part{{Type: "text", Text: "cancel me"}}
	interrupted, err := s.Submission(input)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := interrupted.Send(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.started:
	case <-wait.Done():
		t.Fatal(wait.Err())
	}
	if _, err := s.CancelInput(t.Context(), pending.Input.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, err := interrupted.Wait(wait)
	if err != nil || cancelled.Turn.State != "cancelled" {
		t.Fatal(cancelled, err)
	}
	raw, err := json.Marshal(command.Record())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := c.RestoreInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := restored.Retry(wait)
	if err != nil || retry.Input.ID != done.Input.ID || retry.Turn.ID != done.Turn.ID {
		t.Fatal("native recovery duplicated work", retry, err)
	}
}
