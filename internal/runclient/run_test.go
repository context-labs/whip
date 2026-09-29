package runclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func TestRunClientWorker(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		return
	}
	if err := process.WorkerMain(os.Args[separator+1:], os.Stdin, os.Stdout, nil); err != nil {
		t.Fatal(err)
	}
}

type providerFunc func(context.Context, model.Request, func(model.Chunk)) (model.Response, error)

func (f providerFunc) Prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	prepared, err := (model.Scripted{}).Prepare(ctx, request)
	if err == nil {
		prepared.Execute = func(ctx context.Context, emit func(model.Chunk)) (model.Response, error) {
			return f(ctx, request, emit)
		}
	}
	return prepared, err
}

func fixture(t *testing.T, p providerFunc) (*runtime.Runtime, *client.Client) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "whip-run-") //nolint:usetesting // Keep Unix socket paths below the macOS limit.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := runtime.Open(t.Context(), dir, p, runtime.Options{PollInterval: time.Millisecond, EngineCommand: []string{executable, "-test.run=^TestRunClientWorker$", "--"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	authority := r.HostConfiguration()
	snapshot, err := authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = authority.Update(t.Context(), snapshot.Revision, func(host *config.Host) error {
		host.Defaults.Model = session.ModelSelection{Provider: "scripted", Name: "model", Effort: "low"}
		host.Providers["scripted"] = config.Provider{Kind: "openai-chat", BaseURL: "http://127.0.0.1:1", CredentialSource: "none"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
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
	c, err := client.Connect(t.Context(), r.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r, c
}

func response(text string) model.Response {
	return model.Response{Parts: []session.Part{{Type: "text", Text: text}}}
}

func output(t *testing.T) (*Output, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	o, err := NewOutput("json", true, &buf, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	return o, &buf
}

func idle(t *testing.T, c *client.Client, id protocol.ID) {
	t.Helper()
	s, err := c.Session(id)
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
			return
		}
		select {
		case <-deadline:
			t.Fatal("session did not become idle")
		case <-ticker.C:
		}
	}
}

func TestRunNativeResumeConfigurationBudgetsAndExactRecovery(t *testing.T) {
	var mu sync.Mutex
	var requests []model.Request
	r, c := fixture(t, func(_ context.Context, request model.Request, _ func(model.Chunk)) (model.Response, error) {
		if request.Purpose != "automatic_title" {
			mu.Lock()
			requests = append(requests, request)
			mu.Unlock()
		}
		return response("hello"), nil
	})
	o, buf := output(t)
	var records []client.InputRecord
	options := Options{WorkingDirectory: t.TempDir(), Prompt: "first", Effort: "high", CostNanoUSD: 123000, Tokens: 10000, Configuration: protocol.RunConfiguration{System: "one-run system", CacheKey: "stable"}, Record: func(record client.InputRecord) error { records = append(records, record); return nil }}
	result, err := Run(t.Context(), c, options, o)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Finish(nil); err != nil {
		t.Fatal(err)
	}
	if result.Text != "hello" || len(records) != 2 || records[0].Accepted || !records[1].Accepted || !strings.Contains(buf.String(), `"type":"done"`) {
		t.Fatal(result, records, buf.String())
	}
	current, err := r.Session(t.Context(), session.SessionID(result.SessionID))
	if err != nil || current.Definition.ID != "coding" || current.Config.Model.Effort != "high" || current.Config.Run.CacheKey != "stable" || !current.Config.Run.Headless {
		t.Fatal(current, err)
	}
	snapshot, err := r.HostConfiguration().Snapshot(t.Context())
	if err != nil || snapshot.Host.Defaults.Model.Effort != "low" {
		t.Fatal(snapshot, err)
	}
	budgets, err := r.Budgets(t.Context(), current.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range budgets {
		if b.Kind == session.BudgetModelCostNanoUSD && (b.Limit == nil || *b.Limit != 123000) {
			t.Fatal(b)
		}
	}
	idle(t, c, result.SessionID)
	o2, buf2 := output(t)
	second, err := Run(t.Context(), c, Options{Resume: result.SessionID, Prompt: "second", Effort: "off"}, o2)
	if err != nil || second.SessionID != result.SessionID || second.Text != "hello" {
		t.Fatal(second, err)
	}
	if len(events(t, buf2.String())) != 1 {
		t.Fatal("reprinted historical output", buf2.String())
	}
	mu.Lock()
	if len(requests) != 2 || requests[0].Selection.Effort != "high" || requests[1].Selection.Effort != "off" || !strings.Contains(requests[0].Instructions, "one-run system") || requests[0].CacheKey != "stable" || requests[1].CacheKey != string(result.SessionID) || len(requests[1].Messages) != 3 {
		t.Fatal(requests)
	}
	mu.Unlock()
	raw, err := json.Marshal(result.Record)
	if err != nil {
		t.Fatal(err)
	}
	replay, buf3 := output(t)
	recovered, err := Recover(t.Context(), c, raw, false, replay)
	if err != nil || recovered.Admission.Turn.ID != result.Admission.Turn.ID || recovered.Text != "hello" || len(events(t, buf3.String())) != 1 {
		t.Fatal(recovered, err, buf3.String())
	}
	mu.Lock()
	if len(requests) != 2 {
		t.Fatal("recovery executed provider", len(requests))
	}
	mu.Unlock()
}

func TestRunObserverAbortDoesNotCancelAndExplicitCancelTargetsSavedInput(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	r, c := fixture(t, func(ctx context.Context, request model.Request, _ func(model.Chunk)) (model.Response, error) {
		if request.Purpose == "automatic_title" {
			return response("title"), nil
		}
		close(started)
		<-ctx.Done()
		close(cancelled)
		return model.Response{}, ctx.Err()
	})
	ctx, abort := context.WithCancel(t.Context())
	var result Result
	var runErr error
	done := make(chan struct{})
	o, _ := output(t)
	go func() {
		result, runErr = Run(ctx, c, Options{WorkingDirectory: t.TempDir(), Prompt: "wait"}, o)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	abort()
	<-done
	if !errors.Is(runErr, context.Canceled) || result.Record == nil {
		t.Fatal(result, runErr)
	}
	select {
	case <-cancelled:
		t.Fatal("observer abort cancelled execution")
	default:
	}
	busyOutput, _ := output(t)
	if _, err := Run(t.Context(), c, Options{Resume: result.SessionID, Prompt: "must not change an active run", Effort: "high"}, busyOutput); err == nil {
		t.Fatal("busy run control was accepted")
	}
	owner, err := r.Session(t.Context(), session.SessionID(result.SessionID))
	if err != nil || owner.Config.Model.Effort != "low" {
		t.Fatal("busy run changed model", owner, err)
	}
	if err := Cancel(t.Context(), c, result); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("explicit cancel did not cancel provider")
	}
	raw, err := json.Marshal(result.Record)
	if err != nil {
		t.Fatal(err)
	}
	recovered, _ := output(t)
	if _, err := Recover(t.Context(), c, raw, false, recovered); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatal(err)
	}
}

func TestRunBothEnginesCanonicalToolsAndFinalNoToolsCap(t *testing.T) {
	for _, engine := range []string{"starlark", "quickjs"} {
		t.Run(engine, func(t *testing.T) {
			code := "print(42)"
			if engine == "quickjs" {
				code = "console.log(42)"
			}
			var mu sync.Mutex
			calls := 0
			_, c := fixture(t, func(_ context.Context, request model.Request, _ func(model.Chunk)) (model.Response, error) {
				if request.Purpose == "automatic_title" {
					return response("title"), nil
				}
				mu.Lock()
				defer mu.Unlock()
				calls++
				if len(request.Tools) == 0 {
					return response("capped answer"), nil
				}
				raw, _ := json.Marshal(map[string]string{"code": code})
				return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "exec", Name: "execute", Arguments: raw}}}}, nil
			})
			o, buf := output(t)
			result, err := Run(t.Context(), c, Options{WorkingDirectory: t.TempDir(), Prompt: "use tools", Engine: engine, Configuration: protocol.RunConfiguration{MaxTurns: 1}}, o)
			if err != nil || result.Text != "capped answer" {
				t.Fatal(result, err)
			}
			got := events(t, buf.String())
			if len(got) != 3 || got[0]["type"] != "tool_start" || got[1]["type"] != "tool_end" || !strings.Contains(got[1]["result"], "42") || got[2]["delta"] != "capped answer" {
				t.Fatal(got)
			}
			mu.Lock()
			if calls != 2 {
				t.Fatal(calls)
			}
			mu.Unlock()
			idle(t, c, result.SessionID)
			wrong := "quickjs"
			if engine == wrong {
				wrong = "starlark"
			}
			rejected, _ := output(t)
			if _, err := Run(t.Context(), c, Options{Resume: result.SessionID, Prompt: "wrong", Engine: wrong}, rejected); err == nil || !strings.Contains(err.Error(), "cannot resume") {
				t.Fatal(err)
			}
		})
	}
}

func TestRunRecordFailurePreventsAdmissionAndValidationPreventsWrites(t *testing.T) {
	r, c := fixture(t, func(context.Context, model.Request, func(model.Chunk)) (model.Response, error) {
		t.Error("provider executed")
		return response("unexpected"), nil
	})
	o, _ := output(t)
	failure := errors.New("client record storage unavailable")
	result, err := Run(t.Context(), c, Options{WorkingDirectory: t.TempDir(), Prompt: "never send", Record: func(client.InputRecord) error { return failure }}, o)
	if !errors.Is(err, failure) || result.Record == nil {
		t.Fatal(result, err)
	}
	history, err := r.History(t.Context(), session.SessionID(result.SessionID), 0, 100)
	if err != nil || len(history) != 0 {
		t.Fatal(history, err)
	}
	for _, options := range []Options{{Prompt: "", WorkingDirectory: t.TempDir()}, {Prompt: "x", Engine: "invalid"}, {Prompt: "x", PermissionMode: "invalid"}, {Prompt: "x", Tokens: -1}, {Prompt: "x", Configuration: protocol.RunConfiguration{MaxTurns: -1}}} {
		if result, err := Run(t.Context(), c, options, o); err == nil || result.SessionID != "" {
			t.Fatal(result, err)
		}
	}
}

func TestRunExplicitRetryMissingThenDeletedReceiptCannotExecuteAgain(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	_, c := fixture(t, func(_ context.Context, request model.Request, _ func(model.Chunk)) (model.Response, error) {
		if request.Purpose != "automatic_title" {
			mu.Lock()
			calls++
			mu.Unlock()
		}
		return response("retried"), nil
	})
	o, _ := output(t)
	result, err := Run(t.Context(), c, Options{WorkingDirectory: t.TempDir(), Prompt: "original bytes", Record: func(client.InputRecord) error { return errors.New("saved but did not send") }}, o)
	if err == nil || result.Record == nil {
		t.Fatal(result, err)
	}
	raw, err := json.Marshal(result.Record)
	if err != nil {
		t.Fatal(err)
	}
	recovery, _ := output(t)
	if _, err := Recover(t.Context(), c, raw, false, recovery); err == nil || !strings.Contains(err.Error(), "explicit retry") {
		t.Fatal(err)
	}
	retried, err := Recover(t.Context(), c, raw, true, recovery)
	if err != nil || retried.Text != "retried" {
		t.Fatal(retried, err)
	}
	idle(t, c, retried.SessionID)
	var deleted protocol.DeleteResult
	if err := c.Call(t.Context(), "sessions.delete", protocol.SessionParams{SessionID: retried.SessionID}, &deleted); err != nil {
		t.Fatal(err)
	}
	if _, err := Recover(t.Context(), c, raw, true, recovery); err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatal("recovery repeated accepted execution", calls)
	}
}

func TestRunRegisteredDefinitionsRequireExactAmbiguousRevision(t *testing.T) {
	r, c := fixture(t, func(context.Context, model.Request, func(model.Chunk)) (model.Response, error) {
		return response("custom"), nil
	})
	first, err := r.RegisterDefinition(t.Context(), session.DefinitionDocument{ID: "custom", Name: "Custom", Defaults: session.ConfigPatch{Instructions: &session.Instructions{Text: "first"}}})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := resolveDefinition(t.Context(), c, "custom")
	if err != nil || ref.Revision != first.Ref.Revision {
		t.Fatal(ref, err)
	}
	_, err = r.RegisterDefinition(t.Context(), session.DefinitionDocument{ID: "custom", Name: "Custom", Defaults: session.ConfigPatch{Instructions: &session.Instructions{Text: "second"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolveDefinition(t.Context(), c, "custom"); err == nil || !strings.Contains(err.Error(), "id@revision") {
		t.Fatal(err)
	}
	o, _ := output(t)
	result, err := Run(t.Context(), c, Options{WorkingDirectory: t.TempDir(), Agent: "custom@" + first.Ref.Revision, Prompt: "read first definition"}, o)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := r.Session(t.Context(), session.SessionID(result.SessionID))
	if err != nil || owner.Definition != first.Ref {
		t.Fatal(owner, err)
	}
	rejected, _ := output(t)
	if _, err := Run(t.Context(), c, Options{Resume: result.SessionID, Agent: "coding", Prompt: "wrong"}, rejected); err == nil || !strings.Contains(err.Error(), "cannot resume") {
		t.Fatal(err)
	}
}

func TestRunUncappedLoopsAndPermissionModeRemainHostAuthority(t *testing.T) {
	for _, engine := range []string{"starlark", "quickjs"} {
		for _, mode := range []string{"prompt", "automatic"} {
			t.Run(engine+"/"+mode, func(t *testing.T) {
				cwd := t.TempDir()
				code := `files.write(path="proof.txt", content="written")`
				if engine == "quickjs" {
					code = `await files.write({path:'proof.txt',content:'written'});`
				}
				var mu sync.Mutex
				rounds := 0
				_, c := fixture(t, func(_ context.Context, request model.Request, _ func(model.Chunk)) (model.Response, error) {
					if request.Purpose == "automatic_title" {
						return response("title"), nil
					}
					mu.Lock()
					defer mu.Unlock()
					rounds++
					if len(request.Tools) == 0 {
						return model.Response{}, errors.New("uncapped run removed tools")
					}
					if rounds == 3 {
						return response("done"), nil
					}
					raw, _ := json.Marshal(map[string]string{"code": code})
					return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: fmt.Sprintf("call%d", rounds), Name: "execute", Arguments: raw}}}}, nil
				})
				o, buf := output(t)
				result, err := Run(t.Context(), c, Options{WorkingDirectory: cwd, Prompt: "write proof", Engine: engine, PermissionMode: mode}, o)
				if err != nil || result.Text != "done" {
					t.Fatal(result, err)
				}
				data, err := os.ReadFile(filepath.Join(cwd, "proof.txt"))
				if mode == "automatic" && (err != nil || string(data) != "written") {
					t.Fatal(string(data), err)
				}
				if mode == "prompt" && (!os.IsNotExist(err) || !strings.Contains(buf.String(), "headless run cannot wait")) {
					t.Fatal("prompt bypassed approval", string(data), err, buf.String())
				}
				mu.Lock()
				defer mu.Unlock()
				if rounds != 3 {
					t.Fatal(rounds)
				}
			})
		}
	}
}
