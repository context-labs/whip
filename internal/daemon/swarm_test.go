package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func TestDaemonBackgroundSubagentIsDurableAndSteerable(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	releaseChild := sync.OnceFunc(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		isChild := len(request.Messages) > 0 && strings.Contains(request.Messages[0].Content, "subagent inside whip")
		if isChild && len(request.Messages) == 2 {
			close(started)
			<-release
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"first"},"finish_reason":"stop"}]}`+"\n\n")
		} else if isChild {
			found := slices.ContainsFunc(request.Messages, func(message llm.Message) bool {
				return strings.Contains(message.Content, "change course")
			})
			if !found {
				t.Errorf("steer was absent from child request: %+v", request.Messages)
			}
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"guided"},"finish_reason":"stop"}]}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"root ack"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	defer releaseChild()

	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	ag := agent.New(llm.New(server.URL, "key"), "model", 100, "system")
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: NewAgentRunner(ag)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}

	task := ag.StartBackground("durable child", "begin", agent.SubModel{})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("child model call did not start")
	}
	childID := root.authority.AgentID + ":" + task.ID
	relatives, err := root.ListAgentRelatives(context.Background(), root.authority.AgentID)
	if err != nil || len(relatives.Children) != 1 || relatives.Children[0].ID != childID || relatives.Children[0].Status != "running" {
		t.Fatalf("live durable child=%+v err=%v", relatives.Children, err)
	}
	grant, err := root.InspectCapability(context.Background(), root.authority.AgentID, "child-files:"+childID)
	if err != nil || grant.AgentID != childID || grant.Status != "active" {
		t.Fatalf("child file authority=%+v err=%v", grant, err)
	}
	if err := ag.SteerTask(task.ID, "change course"); err != nil {
		t.Fatal(err)
	}
	queued, err := store.LoadQueuedInbox(context.Background(), rootID, childID, 0, 10)
	if err != nil || len(queued) != 0 {
		t.Fatalf("delivered child steer remained queued: %+v err=%v", queued, err)
	}
	releaseChild()
	select {
	case <-task.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not settle")
	}
	if task.Status != agent.TaskDone || task.Report != "guided" {
		t.Fatalf("settled task=%+v", task)
	}
	if output, err := ag.FollowupTask(context.Background(), task.ID, "follow up", agent.Events{}); err != nil || output != "guided" {
		t.Fatalf("durable child follow-up output=%q err=%v", output, err)
	}
	relatives, err = root.ListAgentRelatives(context.Background(), root.authority.AgentID)
	if err != nil || len(relatives.Children) != 1 || relatives.Children[0].Status != "succeeded" {
		t.Fatalf("settled durable child=%+v err=%v", relatives.Children, err)
	}
	messages, err := store.SubagentTranscript(rootID, task.ID)
	if err != nil || len(messages) < 6 {
		t.Fatalf("durable child transcript=%+v err=%v", messages, err)
	}
	budgets, err := root.InspectBudgets(context.Background(), root.authority.AgentID, root.authority.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range budgets {
		if budget.Kind == session.BudgetConcurrentChildTurns && budget.Reserved != 0 {
			t.Fatalf("child turn reservation leaked: %+v", budget)
		}
		if budget.Kind == session.BudgetTokens && (budget.Used == 0 || budget.Reserved != 0) {
			t.Fatalf("child token usage was not reconciled conservatively: %+v", budget)
		}
	}
	if cleared := ag.Tasks().ClearSettled(); cleared != 1 {
		t.Fatalf("cleared tasks=%d, want 1", cleared)
	}
	if err := root.routeControl(context.Background(), func(context.Context) error {
		if root.children[task.ID] != nil {
			return errors.New("cleared task retained live child")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRootReceivesQueuedPeerMessagesDuringTurn(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	runner := &fakeRunner{turn: func(ctx context.Context, _ string, _ bool) (string, error) {
		close(started)
		select {
		case <-release:
			return "done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	childID := root.AgentID() + ":messenger"
	if err := root.AdmitChild(context.Background(), root.AgentID(), childID, "exec-messenger"); err != nil {
		t.Fatal(err)
	}
	receipt, err := root.Submit(context.Background(), "hold")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("root turn did not start")
	}
	if _, err := root.SendAgentMessage(context.Background(), childID, root.AgentID(), session.AgentMessage{Delivery: session.DeliveryQueued, Body: "evidence ready"}); err != nil {
		t.Fatal(err)
	}
	messages, err := root.ReceiveAgentMessages(context.Background(), root.AgentID(), 8)
	if err != nil || len(messages) != 1 || messages[0].SenderAgentID != childID || messages[0].Body != "evidence ready" {
		t.Fatalf("received messages = %+v, %v", messages, err)
	}
	if messages, err := root.ReceiveAgentMessages(context.Background(), root.AgentID(), 8); err != nil || len(messages) != 0 {
		t.Fatalf("message replay = %+v, %v", messages, err)
	}
	if _, err := root.ReceiveAgentMessages(context.Background(), childID, 8); !errors.Is(err, session.ErrAgentAccess) {
		t.Fatalf("child polled root inbox: %v", err)
	}
	close(release)
	if completion := waitReceipt(t, receipt); completion.Err != nil {
		t.Fatal(completion.Err)
	}
}

func TestDurableChildConsumesQueuedInboxWhenStarted(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var call llm.Request
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		calls.Add(1)
		found := slices.ContainsFunc(call.Messages, func(message llm.Message) bool {
			return strings.Contains(message.Content, "queued evidence")
		})
		content := "first pass"
		if found {
			content = "saw queued evidence"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":\"stop\"}]}\n\n", content)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()

	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	child := agent.New(llm.New(provider.URL, "key"), "model", 100, "child")
	t.Cleanup(child.Close)
	if err := root.AdmitRLMSubagent(context.Background(), "reader", child, []string{"read"}, nil); err != nil {
		t.Fatal(err)
	}
	childID := root.AgentID() + ":reader"
	if _, err := root.SendAgentMessage(context.Background(), root.AgentID(), childID, session.AgentMessage{Delivery: session.DeliveryQueued, Body: "queued evidence"}); err != nil {
		t.Fatal(err)
	}
	if err := root.StartSubagent(context.Background(), "reader"); err != nil {
		t.Fatal(err)
	}
	queued, err := store.LoadQueuedInbox(context.Background(), rootID, childID, 0, 10)
	if err != nil || len(queued) != 1 {
		t.Fatalf("in-flight durable inbox = %+v, %v", queued, err)
	}
	output, err := child.Turn(context.Background(), "begin", agent.Events{})
	if err != nil || output != "saw queued evidence" || calls.Load() != 2 {
		t.Fatalf("child output = %q, calls=%d, err=%v", output, calls.Load(), err)
	}
	if err := root.FinishSubagent(context.Background(), "reader", agent.TaskDone); err != nil {
		t.Fatal(err)
	}
	queued, err = store.LoadQueuedInbox(context.Background(), rootID, childID, 0, 10)
	if err != nil || len(queued) != 0 {
		t.Fatalf("committed child inbox = %+v, %v", queued, err)
	}
	root.ReleaseSubagent("reader")
}

func TestDurableChildExposesOnlyDelegatedOptionalTools(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	parentServices := tools.NewServices()
	parentServices.SetBrowser(browser.NewManager(browser.ModeHeadless), false)
	parentServices.SetComputerPolicy(computer.NewPolicy(nil, nil, false))
	t.Cleanup(parentServices.Close)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	child := agent.NewWithServices(llm.New("http://127.0.0.1:1", "key"), "model", 100, "child", parentServices)
	if err := root.AdmitRLMSubagent(t.Context(), "operator", child, []string{"read", "browser", "computer"}, nil); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range child.Tools {
		names = append(names, tool.Def.Function.Name)
	}
	for _, want := range []string{"read", "browser_exec", "computer_exec"} {
		if !slices.Contains(names, want) {
			t.Errorf("delegated child tools %v omit %q", names, want)
		}
	}
	for _, denied := range []string{"subagent"} {
		if slices.Contains(names, denied) {
			t.Errorf("delegated child tools %v include %q", names, denied)
		}
	}
	root.ReleaseSubagent("operator")
}

func TestDaemonSubagentRespectsParentTokenBudget(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"should not run"},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	ag := agent.New(llm.New(server.URL, "key"), "model", 100, "system")
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: NewAgentRunner(ag)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.CapBudget(context.Background(), root.authority.AgentID, root.authority.AgentID, session.BudgetTokens, 1); err != nil {
		t.Fatal(err)
	}
	task := ag.StartBackground("bounded child", "begin", agent.SubModel{})
	select {
	case <-task.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("budget-rejected child did not settle")
	}
	if task.Status != agent.TaskError || calls.Load() != 0 {
		t.Fatalf("budget rejection status=%q report=%q model calls=%d", task.Status, task.Report, calls.Load())
	}
}

func TestDaemonSubagentCancellationTerminalizesDurableChild(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			return
		}
		if len(request.Messages) > 0 && strings.Contains(request.Messages[0].Content, "subagent inside whip") {
			close(started)
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"root ack"},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	ag := agent.New(llm.New(server.URL, "key"), "model", 100, "system")
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: NewAgentRunner(ag)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}

	task := ag.StartBackground("cancel child", "begin", agent.SubModel{})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("child model call did not start")
	}
	if !ag.Tasks().Cancel(task.ID) {
		t.Fatal("running child was not cancellable")
	}
	select {
	case <-task.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled child did not settle")
	}
	if task.Status != agent.TaskCancelled {
		t.Fatalf("cancelled task=%+v", task)
	}
	relatives, err := root.ListAgentRelatives(context.Background(), root.authority.AgentID)
	if err != nil || len(relatives.Children) != 1 || relatives.Children[0].Status != "stopped" {
		t.Fatalf("cancelled durable child=%+v err=%v", relatives.Children, err)
	}
}

func TestSwarmMutationsRouteThroughRootActor(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := root.AdmitChild(ctx, root.authority.AgentID, "tester", "exec-tester"); err != nil {
		t.Fatal(err)
	}
	if err := root.AdmitChild(ctx, root.authority.AgentID, "implementer", "exec-implementer"); err != nil {
		t.Fatal(err)
	}
	sequence, err := root.SendAgentMessage(ctx, "tester", "implementer", session.AgentMessage{
		Delivery: session.DeliveryQueued, Body: "actor routed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sequence.InboxSeq != 1 {
		t.Fatalf("message sequence = %+v", sequence)
	}
	relatives, err := root.ListAgentRelatives(ctx, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if relatives.Parent == nil || relatives.Parent.ID != root.authority.AgentID || len(relatives.Siblings) != 1 || relatives.Siblings[0].ID != "implementer" {
		t.Fatalf("actor relatives = %+v", relatives)
	}
	items, err := store.LoadQueuedInbox(ctx, rootID, "implementer", 0, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("actor message inbox=%+v err=%v", items, err)
	}
	var envelope session.AgentMessageEnvelope
	if err := json.Unmarshal(items[0].Payload.Inline, &envelope); err != nil || envelope.SenderAgentID != "tester" {
		t.Fatalf("actor message envelope=%+v err=%v", envelope, err)
	}
	if err := root.TerminalizeSubtree(ctx, root.authority.AgentID, "tester", "deleted"); err != nil {
		t.Fatal(err)
	}
	relatives, err = root.ListAgentRelatives(ctx, "tester")
	if err != nil || relatives.Parent == nil || relatives.Parent.ID != root.authority.AgentID {
		t.Fatalf("deleted lineage=%+v err=%v", relatives, err)
	}
}

func TestChildTurnWrappersAndSubagentGuards(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	parentID := root.authority.AgentID
	if err := root.AdmitChild(ctx, parentID, "wrapper-child", "wrapper-exec"); err != nil {
		t.Fatal(err)
	}
	if err := root.StartChildTurn(ctx, parentID, "wrapper-exec"); err != nil {
		t.Fatal(err)
	}
	if err := root.FinishChildTurn(ctx, parentID, "wrapper-exec", "succeeded"); err != nil {
		t.Fatal(err)
	}
	relatives, err := root.ListAgentRelatives(ctx, parentID)
	if err != nil || len(relatives.Children) != 1 || relatives.Children[0].Status != "succeeded" {
		t.Fatalf("finished child=%+v err=%v", relatives.Children, err)
	}

	child := agent.New(llm.New("http://unused", "key"), "model", 100, "system")
	t.Cleanup(child.Close)
	if err := root.AdmitSubagent(ctx, "", child); err == nil {
		t.Fatal("empty subagent task ID was accepted")
	}
	if err := root.AdmitSubagent(ctx, "task", nil); err == nil {
		t.Fatal("nil subagent was accepted")
	}
	if err := root.StartSubagent(ctx, "missing"); !errors.Is(err, session.ErrAgentAccess) {
		t.Fatalf("missing start error=%v", err)
	}
	if err := root.FinishSubagent(ctx, "missing", agent.TaskRunning); err == nil {
		t.Fatal("running completion status was accepted")
	}
	if err := root.FinishSubagent(ctx, "missing", agent.TaskDone); !errors.Is(err, session.ErrAgentAccess) {
		t.Fatalf("missing finish error=%v", err)
	}
	if err := root.SteerSubagent(ctx, "missing", "change"); !errors.Is(err, session.ErrAgentTerminal) {
		t.Fatalf("missing steer error=%v", err)
	}
	if _, err := (subagentModelBudget{root: root, taskID: "missing"}).ReserveModelCall(ctx, 1); !errors.Is(err, session.ErrAgentTerminal) {
		t.Fatalf("missing budget child error=%v", err)
	}

	if err := root.AdmitSubagent(ctx, "task", child); err != nil {
		t.Fatal(err)
	}
	if err := root.AdmitSubagent(ctx, "task", child); err == nil {
		t.Fatal("duplicate subagent was accepted")
	}
	if err := root.SteerSubagent(ctx, "task", "early"); !errors.Is(err, session.ErrAgentTerminal) {
		t.Fatalf("idle steer error=%v", err)
	}
	if err := root.StartSubagent(ctx, "task"); err != nil {
		t.Fatal(err)
	}
	if err := root.StartSubagent(ctx, "task"); err == nil {
		t.Fatal("second subagent start was accepted")
	}
	if err := root.FinishSubagent(ctx, "task", agent.TaskDone); err != nil {
		t.Fatal(err)
	}
	if err := root.FinishSubagent(ctx, "task", agent.TaskDone); err != nil {
		t.Fatalf("settled subagent finish=%v", err)
	}
}

func TestSwarmControlQueuedAfterFailureIsRejected(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	t.Cleanup(func() { _ = store.Close() })
	rootID := createRoot(t, store)
	meta, _, err := store.Load(rootID)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	root := newSession(store, meta, authority, Components{Runner: &fakeRunner{}})
	t.Cleanup(root.supervisor.stop)
	failure := errors.New("worker failed")
	reply := make(chan error, 1)
	ran := false
	root.supervisor.post(workerEnvelope{kind: "failed worker", err: failure})
	root.supervisor.post(workerEnvelope{kind: workerControl, reply: reply, control: func(context.Context) error {
		ran = true
		return nil
	}})
	if err := root.actor(); !errors.Is(err, failure) {
		t.Fatalf("actor error = %v", err)
	}
	if err := <-reply; !errors.Is(err, ErrStopped) || ran {
		t.Fatalf("control error=%v ran=%v", err, ran)
	}

	root = newSession(store, meta, authority, Components{Runner: &fakeRunner{}})
	t.Cleanup(root.supervisor.stop)
	taskReply := make(chan error, 1)
	reply = make(chan error, 1)
	ran = false
	root.supervisor.post(workerEnvelope{kind: workerTaskRecord, reply: taskReply})
	root.supervisor.post(workerEnvelope{kind: workerControl, reply: reply, control: func(context.Context) error {
		ran = true
		return nil
	}})
	if err := root.actor(); err == nil {
		t.Fatal("invalid task record should fail the actor")
	}
	if err := <-taskReply; err == nil {
		t.Fatal("invalid task record should return its error")
	}
	if err := <-reply; !errors.Is(err, ErrStopped) || ran {
		t.Fatalf("control after task failure error=%v ran=%v", err, ran)
	}
}
