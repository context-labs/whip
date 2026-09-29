package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"golang.org/x/sys/unix"
)

type nativeCLIProvider func(context.Context, model.Request, func(model.Chunk)) (model.Response, error)

func (f nativeCLIProvider) Prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	p, err := (model.Scripted{}).Prepare(ctx, request)
	if err == nil {
		p.Execute = func(ctx context.Context, emit func(model.Chunk)) (model.Response, error) {
			return f(ctx, request, emit)
		}
	}
	return p, err
}

func TestNativeCLIWorker(t *testing.T) {
	index := slices.Index(os.Args, "--")
	if index < 0 {
		return
	}
	if err := process.WorkerMain(os.Args[index+1:], os.Stdin, os.Stdout, nil); err != nil {
		t.Fatal(err)
	}
}

type runRequests struct {
	mu    sync.Mutex
	items []model.Request
}

func (r *runRequests) list() []model.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.items)
}

func runFixture(t *testing.T, reply string, requests *runRequests) *runtime.Runtime {
	t.Helper()
	return nativeRunFixture(t, func(_ context.Context, request model.Request, _ func(model.Chunk)) (model.Response, error) {
		if requests != nil && request.Purpose != "automatic_title" {
			requests.mu.Lock()
			requests.items = append(requests.items, request)
			requests.mu.Unlock()
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: reply}}}, nil
	})
}

func nativeRunFixture(t *testing.T, p nativeCLIProvider) *runtime.Runtime {
	t.Helper()
	return nativeRunFixtureConfigured(t, p, config.Default())
}

func nativeRunFixtureConfigured(t *testing.T, p runner.Provider, host config.Host) *runtime.Runtime {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "whip-cli-") //nolint:usetesting // Unix socket paths must fit macOS.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(home); err != nil {
			t.Error(err)
		}
	})
	t.Setenv("WHIPCODE_HOME", home)
	paths, err := nativeRuntimePaths()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	host.Defaults.Model = session.ModelSelection{Provider: "testprov", Name: "test"}
	if _, ok := host.Providers["testprov"]; !ok {
		host.Providers["testprov"] = config.Provider{Kind: "openai-chat", BaseURL: "http://127.0.0.1:1", CredentialSource: "none"}
	}
	if err := config.Save(paths.Directory, host); err != nil {
		t.Fatal(err)
	}

	r, err := runtime.Open(t.Context(), paths.Directory, p, runtime.Options{PollInterval: time.Millisecond, EngineCommand: []string{executable, "-test.run=^TestNativeCLIWorker$", "--"}})
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
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	previous := connectNativeRuntime
	connectNativeRuntime = func(ctx context.Context) (*client.Client, error) { return client.Connect(ctx, r.SocketPath(), nil) }
	t.Cleanup(func() { connectNativeRuntime = previous })
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r
}

func runCapture(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(stdin), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	oldIn, oldOut := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	os.Stdin = in
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outW
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&buf, outR); close(done) }()
	runErr := runCLI(args)
	_ = outW.Close()
	<-done
	_ = outR.Close()
	return buf.String(), runErr
}

func nativeSession(t *testing.T) *protocol.Session {
	t.Helper()
	c, err := connectNativeRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	var trees protocol.ListTreesResult
	if err := c.Call(t.Context(), "trees.list", protocol.ListTreesParams{Limit: 100}, &trees); err != nil {
		t.Fatal(err)
	}
	if len(trees.Items) == 0 {
		return nil
	}
	if len(trees.Items) != 1 {
		t.Fatal(trees)
	}
	s, err := c.Session(trees.Items[0].RootID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		activity, err := s.Activity(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if activity.ActiveTurn == nil && activity.QueuedInputCount == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("session not idle")
		case <-ticker.C:
		}
	}
	current, err := s.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return &current
}

func TestRunTextOutput(t *testing.T) {
	runFixture(t, "hello world", nil)
	out, err := runCapture(t, "", "say hi")
	if err != nil || out != "hello world\n" {
		t.Fatal(out, err)
	}
}

func TestRunJSONStream(t *testing.T) {
	runFixture(t, "all done", nil)
	out, err := runCapture(t, "", "--format", "json", "go")
	if err != nil {
		t.Fatal(err)
	}
	var text, done bool
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		var event map[string]string
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		text = text || event["type"] == "text"
		done = done || event["type"] == "done" && event["text"] == "all done"
	}
	if !text || !done {
		t.Fatal(out)
	}
}

func TestRunStdinAppendsToPrompt(t *testing.T) {
	var requests runRequests
	runFixture(t, "ok", &requests)
	if _, err := runCapture(t, "piped context\n", "summarize this"); err != nil {
		t.Fatal(err)
	}
	values := requests.list()
	if len(values) != 1 || values[0].Messages[0].Parts[0].Text != "summarize this\n\npiped context" {
		t.Fatal(values)
	}
}

func TestRunReasoningEffortIsSessionScoped(t *testing.T) {
	var requests runRequests
	r := runFixture(t, "done", &requests)
	if _, err := runCapture(t, "", "--effort", "high", "think"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := r.HostConfiguration().Snapshot(t.Context())
	if err != nil || snapshot.Host.Defaults.Model.Effort != "" {
		t.Fatal(snapshot, err)
	}
	if _, err := runCapture(t, "", "--effort", "off", "reply directly"); err != nil {
		t.Fatal(err)
	}
	values := requests.list()
	if len(values) != 2 || values[0].Selection.Effort != "high" || values[1].Selection.Effort != "off" {
		t.Fatal(values)
	}
}

func TestRunResume(t *testing.T) {
	var requests runRequests
	runFixture(t, "reply", &requests)
	if _, err := runCapture(t, "", "first question"); err != nil {
		t.Fatal(err)
	}
	owner := nativeSession(t)
	if _, err := runCapture(t, "", "--resume", string(owner.ID), "follow up"); err != nil {
		t.Fatal(err)
	}
	values := requests.list()
	if len(values) != 2 || len(values[1].Messages) != 3 || values[1].Messages[0].Parts[0].Text != "first question" {
		t.Fatal(values)
	}
}

func TestRunResumeUnknown(t *testing.T) {
	runFixture(t, "x", nil)
	if _, err := runCapture(t, "", "--resume", "nosuchsession", "hi"); err == nil || !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Fatal(err)
	}
}

func TestRunSystemOverride(t *testing.T) {
	var requests runRequests
	runFixture(t, "ok", &requests)
	if _, err := runCapture(t, "", "--system", "You are a pirate.", "hi"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "system")
	if err := os.WriteFile(path, []byte("You are a poet."), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runCapture(t, "", "--system", "pirate", "--system-file", path, "hi"); err != nil {
		t.Fatal(err)
	}
	values := requests.list()
	if len(values) != 2 || values[0].Instructions != "You are a pirate." || values[1].Instructions != "You are a poet." {
		t.Fatal(values)
	}
}

func TestRunCacheKey(t *testing.T) {
	var requests runRequests
	runFixture(t, "ok", &requests)
	if _, err := runCapture(t, "", "--cache-key", "repo/reviewer", "hi"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCapture(t, "", "hi"); err != nil {
		t.Fatal(err)
	}
	values := requests.list()
	if len(values) != 2 || values[0].CacheKey != "repo/reviewer" || values[1].CacheKey != string(values[1].SessionID) {
		t.Fatal(values)
	}
}

func TestRunNoSession(t *testing.T) {
	runFixture(t, "ok", nil)
	if _, err := runCapture(t, "", "--no-session", "one-off"); err != nil {
		t.Fatal(err)
	}
	if nativeSession(t) != nil {
		t.Fatal("session retained")
	}
}

func TestRunQuietJSON(t *testing.T) {
	runFixture(t, "quiet reply", nil)
	notes := captureStderr(t, func() {
		out, err := runCapture(t, "", "--quiet", "--format", "json", "go")
		if err != nil || !strings.Contains(out, `"type":"done"`) {
			t.Fatal(out, err)
		}
	})
	if notes != "" {
		t.Fatal(notes)
	}
}

func TestRunArgValidation(t *testing.T) {
	for _, test := range []struct {
		want string
		args []string
	}{{"not defined", []string{"--nosuchflag"}}, {"unknown --format", []string{"--format", "xml", "hi"}}, {"no prompt given", nil}, {"requires --recover", []string{"--retry", "hi"}}, {"cannot be combined", []string{"--recover", "file", "hi"}}} {
		if _, err := runCapture(t, "", test.args...); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatal(test, err)
		}
	}
}

func TestRunResolveErrors(t *testing.T) {
	nativeRunFixture(t, func(context.Context, model.Request, func(model.Chunk)) (model.Response, error) {
		return model.Response{}, errors.New("provider is not configured")
	})
	if _, err := runCapture(t, "", "--p", "unknown", "hi"); err == nil {
		t.Fatal("unconfigured provider succeeded")
	}
	if _, err := runCapture(t, "", "--system-file", filepath.Join(t.TempDir(), "absent"), "hi"); err == nil || !strings.Contains(err.Error(), "-system-file") {
		t.Fatal(err)
	}
}

func TestRunUnreadableConfig(t *testing.T) {
	unusableHome(t)
	if _, err := runCapture(t, "", "hi"); err == nil {
		t.Fatal("invalid home accepted")
	}
}

func TestRunExecutionEngineSelectionAndResume(t *testing.T) {
	runFixture(t, "done", nil)
	if _, err := runCapture(t, "", "--rlm-engine", "quickjs", "--permission-mode", "automatic", "--max-tokens", "20000", "select"); err != nil {
		t.Fatal(err)
	}
	owner := nativeSession(t)
	if _, err := runCapture(t, "", "--resume", string(owner.ID), "--rlm-engine", "starlark", "wrong"); err == nil || !strings.Contains(err.Error(), "cannot resume") {
		t.Fatal(err)
	}
	if _, err := runCapture(t, "", "--resume", string(owner.ID), "--rlm-engine", "quickjs", "matching"); err != nil {
		t.Fatal(err)
	}
}

func TestRunAgentSelectionAndResume(t *testing.T) {
	runFixture(t, "done", nil)
	if _, err := runCapture(t, "", "--agent", "junior-developer", "--permission-mode", "automatic", "select"); err != nil {
		t.Fatal(err)
	}
	owner := nativeSession(t)
	if owner.Definition.ID != "junior-developer" {
		t.Fatal(owner)
	}
	if _, err := runCapture(t, "", "--resume", string(owner.ID), "--agent", "coding", "wrong"); err == nil || !strings.Contains(err.Error(), "cannot resume") {
		t.Fatal(err)
	}
	if _, err := runCapture(t, "", "--resume", string(owner.ID), "--agent", "junior-developer", "matching"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCapture(t, "", "--agent", "architect", "prompt"); err == nil || !strings.Contains(err.Error(), "unknown agent") {
		t.Fatal(err)
	}
}

func TestRunRejectsInvalidEngineAndLimits(t *testing.T) {
	for _, args := range [][]string{{"--rlm-engine", "node"}, {"--permission-mode", "yes"}, {"--max-cost", "NaN"}, {"--max-cost", "-1"}, {"--max-tokens", "-1"}, {"--max-turns", "-1"}, {"--max-turns", "1000001"}, {"--timeout", "-1s"}} {
		if _, err := runCapture(t, "", append(args, "prompt")...); err == nil {
			t.Fatal(args)
		}
	}
}

func TestRunRejectsUnrepresentableCostCaps(t *testing.T) {
	for _, test := range []struct{ value, want string }{{"0.0000001", "must be at least"}, {"0.0000009", "must be at least"}, {"9223372036854.775808", "too large"}, {"1e1000000", "too large"}, {"1e-1000000", "must be at least"}} {
		if _, err := runCapture(t, "", "--max-cost", test.value, "prompt"); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatal(test, err)
		}
	}
	for value, want := range map[string]protocol.Counter{"0": 0, "0e100000000": 0, "0.000001": 1000, "1.000000001": 1000000001, "1.0000000005": 1000000001, "1e-6": 1000} {
		if got, err := runCost(value); err != nil || got != want {
			t.Fatal(value, got, want, err)
		}
	}
}

func TestRunMaxTurnsAndJSONToolEvents(t *testing.T) {
	nativeRunFixture(t, func(_ context.Context, request model.Request, _ func(model.Chunk)) (model.Response, error) {
		if request.Purpose == "automatic_title" {
			return model.Response{Parts: []session.Part{{Type: "text", Text: "title"}}}, nil
		}
		if len(request.Tools) == 0 {
			return model.Response{Parts: []session.Part{{Type: "text", Text: "final answer"}}}, nil
		}
		return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{"code":"print(42)"}`)}}}}, nil
	})
	out, err := runCapture(t, "", "--quiet", "--format", "json", "--max-turns", "1", "go")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"type":"tool_start"`, `"type":"tool_end"`, `"text":"final answer"`, `"type":"done"`} {
		if !strings.Contains(out, expected) {
			t.Fatal(expected, out)
		}
	}
}

func TestRunTimeoutCancelsExactHostInput(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	nativeRunFixture(t, func(ctx context.Context, request model.Request, _ func(model.Chunk)) (model.Response, error) {
		if request.Purpose == "automatic_title" {
			return model.Response{Parts: []session.Part{{Type: "text", Text: "title"}}}, nil
		}
		close(started)
		<-ctx.Done()
		close(cancelled)
		return model.Response{}, ctx.Err()
	})
	out, err := runCapture(t, "", "--format", "json", "--timeout", "500ms", "wait")
	if err == nil || !strings.Contains(err.Error(), "timed out") || !strings.Contains(out, `"type":"error"`) || strings.Contains(out, `"type":"done"`) {
		t.Fatal(out, err)
	}
	select {
	case <-started:
	default:
		t.Fatal("provider never started")
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not cancel")
	}
	owner := nativeSession(t)
	if owner == nil {
		t.Fatal("timeout deleted retained session")
	}
}

func TestRunSavedRecordRecoverDoesNotResubmitAndNoSessionDeletes(t *testing.T) {
	var requests runRequests
	runFixture(t, "saved response", &requests)
	path := filepath.Join(t.TempDir(), "record.json")
	if _, err := runCapture(t, "", "--record", path, "original prompt"); err != nil {
		t.Fatal(err)
	}
	owner := nativeSession(t)
	if data, err := readRunRecord(path); err != nil || !strings.Contains(string(data), "original prompt") {
		t.Fatal(string(data), err)
	}
	if _, err := runCapture(t, "", "--record", path, "must not overwrite"); err == nil {
		t.Fatal("overwrote record")
	}
	out, err := runCapture(t, "", "--recover", path, "--retry", "--no-session")
	if err != nil || !strings.Contains(out, "saved response") || len(requests.list()) != 1 {
		t.Fatal(out, err, requests.list())
	}
	if _, err := runCapture(t, "", "--recover", path, "--retry"); err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatal(err)
	}
	c, err := connectNativeRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	s, _ := c.Session(owner.ID)
	if _, err := s.Get(t.Context()); err == nil {
		t.Fatal("-no-session did not delete recovered owner")
	}
}

func TestRunRecoveryFilesAndInputBounds(t *testing.T) {
	runFixture(t, "unused", nil)
	directory := t.TempDir()
	original := filepath.Join(directory, "original")
	if err := os.WriteFile(original, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(original, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readRunRecord(link); err == nil {
		t.Fatal("symlink recovery accepted")
	}
	if err := saveRunRecord(link, client.InputRecord{}); err == nil {
		t.Fatal("symlink recovery overwritten")
	}
	if err := os.Chmod(original, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readRunRecord(original); err == nil {
		t.Fatal("public recovery file accepted")
	}
	if _, err := runCapture(t, strings.Repeat("x", (1<<20)+1), "prompt"); err == nil {
		t.Fatal("oversized stdin accepted")
	}
	if err := os.WriteFile(original, bytes.Repeat([]byte("x"), (512<<10)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runCapture(t, "", "--system-file", original, "prompt"); err == nil {
		t.Fatal("oversized system file accepted")
	}
	fifo := filepath.Join(directory, "fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runCapture(t, "", "--system-file", fifo, "prompt"); err == nil {
		t.Fatal("FIFO system file accepted")
	}
	if nativeSession(t) != nil {
		t.Fatal("invalid preflight created a session")
	}
}

func TestRunCapturesExactBudgetAndPermissionFlags(t *testing.T) {
	r := runFixture(t, "configured", nil)
	if _, err := runCapture(t, "", "--quiet", "--max-cost", "1.000000001", "--max-tokens", "100000", "--permission-mode", "automatic", "--max-turns", "7", "configured"); err != nil {
		t.Fatal(err)
	}
	owner := nativeSession(t)
	if owner.Configuration.Run == nil || !owner.Configuration.Run.Headless || owner.Configuration.Run.MaxTurns != 7 {
		t.Fatal(owner)
	}
	budgets, err := r.Budgets(t.Context(), session.SessionID(owner.ID))
	if err != nil {
		t.Fatal(err)
	}
	for kind, want := range map[session.BudgetKind]int64{session.BudgetModelCostNanoUSD: 1000000001, session.BudgetModelTokens: 100000} {
		index := slices.IndexFunc(budgets, func(value session.Budget) bool { return value.Kind == kind })
		if index < 0 || budgets[index].Limit == nil || *budgets[index].Limit != want {
			t.Fatal(kind, budgets)
		}
	}
	policy, err := r.PermissionPolicy(t.Context(), session.SessionID(owner.ID))
	if err != nil || policy.Mode != session.PermissionAutomatic {
		t.Fatal(policy, err)
	}
}

// Models a socket poller observing the deadline before the context timer has
// published cancellation. Both are scheduled independently under real load.
type unpublishedRunDeadline struct {
	context.Context
	deadline time.Time
}

func (c unpublishedRunDeadline) Deadline() (time.Time, bool) { return c.deadline, true }

func TestRunCallerDeadlineDoesNotDependOnTimerPublication(t *testing.T) {
	overdue := unpublishedRunDeadline{Context: context.Background(), deadline: time.Now().Add(-time.Second)}
	if overdue.Err() != nil {
		t.Fatal("fixture cancellation is already published")
	}
	if err := runCancellation(overdue); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("elapsed caller deadline missed", err)
	}
	pending := unpublishedRunDeadline{Context: context.Background(), deadline: time.Now().Add(time.Hour)}
	if err := runCancellation(pending); err != nil {
		t.Fatal("earlier transport failure would gain cancellation authority", err)
	}
	if err := runCancellation(context.Background()); err != nil {
		t.Fatal("unlimited run was cancelled", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runCancellation(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal("explicit signal cancellation lost", err)
	}
}
