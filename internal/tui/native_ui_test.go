package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/client"
	hostmodel "github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

type nativeUIProvider struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *nativeUIProvider) Prepare(ctx context.Context, request hostmodel.Request) (hostmodel.Prepared, error) {
	prepared, err := (hostmodel.Scripted{}).Prepare(ctx, request)
	if err != nil {
		return prepared, err
	}
	prepared.Execute = func(ctx context.Context, emit func(hostmodel.Chunk)) (hostmodel.Response, error) {
		last := request.Messages[len(request.Messages)-1]
		text := last.Parts[0].Text
		emit(hostmodel.Chunk{Text: "answer: "})
		if text == "hold" {
			p.once.Do(func() { close(p.entered) })
			select {
			case <-ctx.Done():
				return hostmodel.Response{}, ctx.Err()
			case <-p.release:
			}
		}
		return hostmodel.Response{Parts: []session.Part{{Type: "text", Text: "answer: " + text}}}, nil
	}
	return prepared, nil
}

func nativeUIFixture(t *testing.T) (*nativeModel, *nativeUIProvider) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "whip-tui-v4-") //nolint:usetesting // Bounded macOS Unix socket path.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	p := &nativeUIProvider{entered: make(chan struct{}), release: make(chan struct{})}
	host, err := runtime.Open(t.Context(), dir, p, runtime.Options{PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	server, err := rpc.Listen(host, rpc.HostServices{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	c, err := client.Connect(t.Context(), host.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	var root protocol.CreateTreeResult
	if err := c.Call(t.Context(), "trees.create", protocol.CreateTreeParams{CreationID: "native_tui", Engine: "starlark", Definition: c.Builtins()[0], WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{AutomaticTitle: new(false), GoalsEnabled: new(false), Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}}}, &root); err != nil {
		t.Fatal(err)
	}
	if err := host.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	m, err := newNativeModel(t.Context(), c, *root.Root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.work.close)
	m.Update(m.Init()())
	if !m.ready {
		t.Fatal(m.status)
	}
	return m, p
}

func nativeUISubmit(t *testing.T, m *nativeModel, text string) nativeSubmission {
	t.Helper()
	m.input.SetValue(text)
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal("submission not prepared", m.status)
	}
	result := command().(nativeSubmission)
	m.Update(result)
	if result.err != nil || result.admission.Input == nil {
		t.Fatal(result.err, m.status)
	}
	return result
}

func nativeUIRead(t *testing.T, m *nativeModel) {
	t.Helper()
	command := m.read()
	if command == nil {
		t.Fatal("read was not scheduled")
	}
	m.Update(command())
	if !m.ready {
		t.Fatal(m.status)
	}
}

func TestNativeUIRealHostPromptSteeringAndExactCancellation(t *testing.T) {
	m, p := nativeUIFixture(t)
	first := nativeUISubmit(t, m, "hello")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := first.command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	nativeUIRead(t, m)
	if !strings.Contains(m.View().Content, "answer: hello") || len(m.history.messages) != 2 || m.history.messages[0].InputID == nil || *m.history.messages[0].InputID != first.admission.Input.ID {
		t.Fatal("canonical transcript absent", m.View().Content)
	}
	held := nativeUISubmit(t, m, "hold")
	select {
	case <-p.entered:
	case <-ctx.Done():
		t.Fatal("host turn did not start")
	}
	nativeUIRead(t, m)
	if m.activity.ActiveTurn == nil {
		t.Fatal("missing active turn")
	}
	target := m.activity.ActiveTurn.ID
	steer := nativeUISubmit(t, m, "original steering text")
	if steer.admission.Input.Steering == nil || steer.admission.Input.Steering.TurnID != target || steer.admission.Input.Parts[0].Text != "original steering text" {
		t.Fatal(steer.admission.Input)
	}
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if command == nil {
		t.Fatal("cancel was not prepared")
	}
	cancelled := command().(nativeCancelled)
	m.Update(cancelled)
	if cancelled.err != nil || cancelled.turn != target {
		t.Fatal(cancelled)
	}
	value, err := held.command.Wait(ctx)
	if err != nil || value.Turn.State != "cancelled" {
		t.Fatal(value, err)
	}
	// Untaken steering stays an original queued input; cancellation never
	// retargets that subsequent turn.
	value, err = steer.command.Wait(ctx)
	if err != nil || value.Turn.State != "succeeded" || value.Turn.ID == target || value.Input.ID != steer.admission.Input.ID {
		t.Fatal(value, err)
	}
	nativeUIRead(t, m)
	if !strings.Contains(m.View().Content, "answer: original steering text") {
		t.Fatal(m.View().Content)
	}
}

func TestNativeUIDetachJoinsReadersWithoutCancellingHostTurn(t *testing.T) {
	m, p := nativeUIFixture(t)
	held := nativeUISubmit(t, m, "hold")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-p.entered:
	case <-ctx.Done():
		t.Fatal("host turn did not start")
	}
	m.work.close()
	value, found, err := held.command.Check(ctx)
	if err != nil || !found || value.Turn == nil || value.Turn.State != "running" {
		t.Fatal("detach cancelled host execution", value, err)
	}
	close(p.release)
	value, err = held.command.Wait(ctx)
	if err != nil || value.Turn.State != "succeeded" {
		t.Fatal(value, err)
	}
	if _, _, err := m.work.begin(); err == nil {
		t.Fatal("detached UI started another reader")
	}
}

func TestNativeUIWorkCapacityAndCloseJoin(t *testing.T) {
	ctx, stop := context.WithCancel(t.Context())
	w := nativeWork{ctx: ctx, stop: stop}
	var releases []func()
	for range 4 {
		_, done, err := w.begin()
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, done)
	}
	if _, _, err := w.begin(); err == nil {
		t.Fatal("unbounded UI requests")
	}
	done := make(chan struct{})
	go func() { w.close(); close(done) }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("close did not cancel owned readers")
	}
	select {
	case <-done:
		t.Fatal("close did not join accepted readers")
	default:
	}
	for _, release := range releases {
		release()
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close failed to join")
	}
}

func TestNativeUIAuxiliaryFailureDoesNotDiscardObservedMessages(t *testing.T) {
	m := &nativeModel{history: nativeTranscript{owner: "owner"}, input: newInput(), width: 80, height: 24, reading: true}
	page := nativeObservation(1, nativeMessage(1, "assistant", "committed"))
	m.Update(nativeRead{observation: &page, evidenceError: errors.New("output unavailable")})
	if !m.ready || len(m.history.messages) != 1 || !strings.Contains(m.status, "unavailable") {
		t.Fatal(m.status, m.history.messages)
	}
	m.Update(nativeRead{observation: &page})
	if m.ready || m.observer != nil || !strings.Contains(m.status, "reload") {
		t.Fatal("invalid projection skipped without reload", m.status)
	}
}

func TestNativeUIUncertainInputIsNotResentAndDeletedReceiptIsShown(t *testing.T) {
	m := &nativeModel{ready: true, input: newInput(), uncertain: &client.InputCommand{}}
	m.input.SetValue("same original prompt")
	if cmd := m.submit(); cmd != nil || !strings.Contains(m.status, "uncertain") || m.input.Value() != "same original prompt" {
		t.Fatal("uncertain input resubmitted or draft lost", m.status)
	}
	m.Update(nativeSubmission{})
	if m.uncertain != nil || !strings.Contains(m.status, "deleted") {
		t.Fatal(m.status)
	}
}

func TestNativeUIRenderedRowsAndControlCharactersAreBounded(t *testing.T) {
	body := strings.Repeat("line\n", 70000) + "\x1b[2J\aend"
	m := &nativeModel{input: newInput(), width: 80, height: 24, history: nativeTranscript{messages: []protocol.Message{nativeMessage(1, "assistant", body)}}}
	m.refresh()
	if len(m.rows) != 65537 || !strings.Contains(m.rows[len(m.rows)-1], "Display row limit") || m.history.messages[0].Parts[0].Text != body {
		t.Fatal("render limit silently changed canonical body", len(m.rows))
	}
	if got := nativeDisplayText("before\x1b[2J\x1b]0;title\aafter\a\r"); got != "beforeafter��" {
		t.Fatalf("terminal control survived: %q", got)
	}
}

func TestNativeUIContextLabelKeepsEvidenceUnknownAndStale(t *testing.T) {
	if got := nativeContextLabel(protocol.ContextUsage{}); got != "Latest prefill: unavailable" {
		t.Fatal(got)
	}
	value := protocol.ContextUsage{Prefill: &protocol.ContextPrefill{InputTokens: 0, InputSource: "reported", Stale: true}}
	if got := nativeContextLabel(value); got != "Latest prefill: 0 tokens (reported) · capacity unknown · earlier history tail" {
		t.Fatal(got)
	}
	value.Prefill.InputSource, value.Prefill.Stale, value.Prefill.ContextWindowTokens = "estimated", false, new(protocol.Counter(1000))
	if got := nativeContextLabel(value); got != "Latest prefill: 0 tokens (estimated) · capacity 1000" {
		t.Fatal(got)
	}
}
