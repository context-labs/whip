package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

type subscriptionRuntimeTransport func(*http.Request) (*http.Response, error)

func (f subscriptionRuntimeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSubscriptionRecursiveRuntimeToolsHelpersTitleAndCompaction(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	providers := NewProviderService(t.Context(), "subscription-runtime")
	t.Cleanup(providers.Close)
	if err := providers.openAI.Install(t.Context(), providers.openAI.Generation(), openAITestCredentials()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := config.UpdateVersioned("", func(cfg *config.Config) error { return cfg.UpsertOpenAICodex() }); err != nil {
		t.Fatal(err)
	}
	client, err := providers.ModelClient(config.Provider{API: openaiauth.Provider, BaseURL: openaiauth.BaseURL})
	if err != nil {
		t.Fatal(err)
	}
	client.MaxRetries = 1
	var calledTool, replayed atomic.Bool
	var helpers, dispatches atomic.Int32
	client.HTTP.Transport = subscriptionRuntimeTransport(func(request *http.Request) (*http.Response, error) {
		dispatches.Add(1)
		if request.URL.String() != openaiauth.BaseURL+"/responses" || request.Header.Get("ChatGPT-Account-Id") != "account-id" {
			t.Error("runtime escaped the subscription route")
		}
		var body struct {
			Model  string            `json:"model"`
			Stream bool              `json:"stream"`
			Tools  []json.RawMessage `json:"tools"`
			Input  json.RawMessage   `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			return nil, err
		}
		if body.Model != "gpt-5.5" || !body.Stream {
			t.Error("runtime helper lost the subscription wire profile")
		}
		output := []any{map[string]any{"type": "message", "role": "assistant", "phase": "final_answer",
			"content": []any{map[string]string{"type": "output_text", "text": "subscription result"}}}}
		if len(body.Tools) == 0 {
			helpers.Add(1)
		} else if calledTool.CompareAndSwap(false, true) {
			args, err := json.Marshal(map[string]string{"code": "print(models.call(prompt=\"helper work\"))\nprint(models.batch(prompts=[\"batch one\", \"batch two\"]))"})
			if err != nil {
				return nil, err
			}
			output = []any{
				map[string]string{"type": "reasoning", "encrypted_content": "private-runtime-continuation"},
				map[string]string{"type": "function_call", "call_id": "runtime-cell", "name": "rlm_exec", "arguments": string(args)},
			}
		} else if strings.Contains(string(body.Input), "private-runtime-continuation") && strings.Contains(string(body.Input), "function_call_output") {
			replayed.Store(true)
		}
		event, err := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{
			"status": "completed", "output": output, "usage": map[string]int{"input_tokens": 10, "output_tokens": 5},
		}})
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader("data: " + string(event) + "\n\n"))}, nil
	})
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	var runtime *RecursiveRuntime
	owner, err := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
		value := agent.NewRuntime(client, "gpt-5.5", llm.SubscriptionOutputLimit("gpt-5.5"), "", tools.NewServices())
		value.ModelName, value.Provider, value.WorkingDir = "gpt-5.5", openaiauth.Provider, meta.CWD
		value.ContextLimit = 400000
		limits := rlm.DefaultLimits()
		var err error
		runtime, err = NewRecursiveRuntime(RecursiveRuntimeOptions{Agent: value, History: history, Limits: limits,
			Kernels: rlm.NewManager(4), KernelCommand: recursiveKernelCommand})
		if err != nil {
			return Components{}, err
		}
		return Components{Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind}, nil
	}, providers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	runs := &sync.Map{}
	runtime.setRunTurnHook(observeRunTurn(runs))
	for range 5 {
		receipt, err := root.Submit(t.Context(), "subscription root work")
		if err != nil {
			t.Fatal(err)
		}
		if completion := waitReceipt(t, receipt); completion.Err != nil || completion.Output != "subscription result" {
			t.Fatalf("subscription root turn: %+v", completion)
		}
	}
	if !replayed.Load() || helpers.Load() != 3 {
		t.Fatalf("tool continuation/helpers failed: replay=%v helpers=%d", replayed.Load(), helpers.Load())
	}
	spawned, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "child", "prompt": "child work", "report": "message"})
	if err != nil {
		t.Fatal(err)
	}
	childID := spawned.(map[string]any)["id"].(string)
	waitRunTurn(t, runs, childID, 1)
	child := runtime.agents[childID]
	waitAgentIdle(t, child)
	if child.agent.Provider != openaiauth.Provider || child.agent.Client.HTTP != client.HTTP {
		t.Fatal("child lost the shared subscription credential owner")
	}
	if title, _, err := runtime.rootNode.GenerateTitle(t.Context()); err != nil || title != "subscription result" {
		t.Fatalf("subscription title: %q %v", title, err)
	}
	if _, err := runtime.rootNode.CompactNow(t.Context()); err != nil {
		t.Fatalf("subscription compaction: %v", err)
	}
	if helpers.Load() != 5 {
		t.Fatalf("title/compaction bypassed the subscription adapter: helpers=%d", helpers.Load())
	}
	summary := modelAccountingSummary(t, store, root, "", true)
	if summary.UnknownCostCalls != int64(dispatches.Load()) || summary.ReportedCostMicros != 0 || summary.EstimatedCostMicros != 0 {
		t.Fatalf("subscription runtime accounting: %+v", summary)
	}
}
