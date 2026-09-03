package rlm_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/tools"
)

type smokeSpec struct {
	Records      int    `json:"records"`
	NeedleRecord int    `json:"needle_record"`
	Needle       string `json:"needle"`
	Filler       string `json:"filler"`
}

type comparisonSpec struct {
	Records           int     `json:"records"`
	NeedleRecord      int     `json:"needle_record"`
	Query             string  `json:"query"`
	Expected          string  `json:"expected"`
	Filler            string  `json:"filler"`
	RootContextTokens int     `json:"root_context_tokens"`
	MaxOutputTokens   int     `json:"max_output_tokens"`
	MaxModelCalls     int     `json:"max_model_calls"`
	InputPrice        float64 `json:"input_price"`
	OutputPrice       float64 `json:"output_price"`
}

type evaluationMetrics struct {
	Correct           bool    `json:"correct"`
	Error             string  `json:"error,omitempty"`
	DurationMillis    int64   `json:"duration_ms"`
	ModelCalls        int     `json:"model_calls"`
	ModelFanout       int     `json:"model_fan_out"`
	HostCalls         int     `json:"host_calls"`
	PromptTokens      int     `json:"prompt_tokens"`
	CompletionTokens  int     `json:"completion_tokens"`
	ContextTokensUsed int     `json:"context_tokens_used"`
	CostUSD           float64 `json:"cost_usd"`
}

type comparisonReport struct {
	Fixture           string            `json:"fixture"`
	Model             string            `json:"model"`
	Provider          string            `json:"provider"`
	RootContextTokens int               `json:"root_context_tokens"`
	MaxModelCalls     int               `json:"max_model_calls"`
	Runtime           evaluationMetrics `json:"runtime"`
}

type evaluationBudget struct {
	mu              sync.Mutex
	maxCalls        int
	maxCallEstimate int64
	calls           int
}

func (budget *evaluationBudget) ReserveModelCall(_ context.Context, estimate int64) (func(llm.Usage) error, error) {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.calls >= budget.maxCalls {
		return nil, errors.New("evaluation model-call budget exhausted")
	}
	if estimate > budget.maxCallEstimate {
		return nil, fmt.Errorf("evaluation context estimate %d exceeds %d", estimate, budget.maxCallEstimate)
	}
	budget.calls++
	return func(llm.Usage) error { return nil }, nil
}

func (budget *evaluationBudget) Calls() int {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.calls
}

type smokeHost struct {
	corpus       string
	handle       string
	client       *llm.Client
	model        string
	maxTokens    int
	budget       *evaluationBudget
	mu           sync.Mutex
	calls        []string
	maxRead      int
	modelFanout  int
	submodelUsed llm.Usage
}

func (host *smokeHost) contextHandle() string {
	if host.handle != "" {
		return host.handle
	}
	return "smoke-corpus"
}

func (host *smokeHost) Call(ctx context.Context, module, operation string, arguments map[string]any) (any, error) {
	host.mu.Lock()
	host.calls = append(host.calls, module+"."+operation)
	host.mu.Unlock()
	if module == "context" && operation == "inspect" {
		return map[string]any{"handle": host.contextHandle(), "source": "fixture", "size": len(host.corpus), "media_type": "text/plain"}, nil
	}
	if module == "context" && operation == "search" {
		query, _ := arguments["query"].(string)
		if query == "" {
			return nil, errors.New("query is required")
		}
		index := strings.Index(host.corpus, query)
		if index < 0 {
			return map[string]any{"matches": []any{}}, nil
		}
		end := index + len(query)
		return map[string]any{"matches": []any{map[string]any{
			"handle": host.contextHandle(), "span": map[string]any{"start": index, "end": end},
			"source": "fixture", "text": host.corpus[max(0, index-32):min(len(host.corpus), end+64)],
		}}}, nil
	}
	if module == "context" && operation == "read" {
		offset := number(arguments["offset"])
		length := min(number(arguments["length"]), 8<<10)
		if offset < 0 || offset > len(host.corpus) || length < 0 {
			return nil, errors.New("invalid corpus range")
		}
		end := min(offset+length, len(host.corpus))
		host.mu.Lock()
		host.maxRead = max(host.maxRead, end-offset)
		host.mu.Unlock()
		return map[string]any{"text": host.corpus[offset:end], "handle": host.contextHandle(), "source": "fixture", "size": len(host.corpus), "span": map[string]any{"start": offset, "end": end}}, nil
	}
	if module == "models" && operation == "batch" {
		prompts, ok := arguments["prompts"].([]any)
		if !ok || len(prompts) == 0 {
			return nil, errors.New("prompts must be a non-empty list")
		}
		host.mu.Lock()
		host.modelFanout = max(host.modelFanout, len(prompts))
		host.mu.Unlock()
		results := make([]map[string]any, len(prompts))
		var calls sync.WaitGroup
		for index, item := range prompts {
			prompt, ok := item.(string)
			if !ok {
				results[index] = map[string]any{"error": "prompt is not a string"}
				continue
			}
			calls.Go(func() { results[index] = host.callModel(ctx, prompt) })
		}
		calls.Wait()
		return results, nil
	}
	return nil, fmt.Errorf("unsupported smoke operation %s.%s", module, operation)
}

func (host *smokeHost) callModel(ctx context.Context, prompt string) map[string]any {
	usage := llm.Usage{PromptTokens: 100, CompletionTokens: 10}
	output := "candidate found through a bounded corpus search"
	var err error
	if host.client != nil {
		maxTokens := host.maxTokens
		if maxTokens <= 0 {
			maxTokens = 256
		}
		var settle func(llm.Usage) error
		if host.budget != nil {
			settle, err = host.budget.ReserveModelCall(ctx, int64(agent.EstimateTokens([]llm.Message{{Role: "user", Content: prompt}})+maxTokens))
			if err != nil {
				return map[string]any{"error": err.Error()}
			}
		}
		output, usage, err = host.client.Complete(ctx, llm.Request{
			Model: host.model, Messages: []llm.Message{{Role: "user", Content: prompt}}, MaxTokens: maxTokens,
		})
		if settle != nil {
			if settleErr := settle(usage); err == nil {
				err = settleErr
			}
		}
	} else if host.budget != nil {
		settle, reserveErr := host.budget.ReserveModelCall(ctx, int64(agent.EstimateTokens([]llm.Message{{Role: "user", Content: prompt}})+256))
		if reserveErr != nil {
			return map[string]any{"error": reserveErr.Error()}
		}
		if settleErr := settle(usage); settleErr != nil {
			return map[string]any{"error": settleErr.Error()}
		}
	}
	host.mu.Lock()
	host.submodelUsed.PromptTokens += usage.PromptTokens
	host.submodelUsed.CompletionTokens += usage.CompletionTokens
	host.mu.Unlock()
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"output": output, "usage": usage}
}

func number(value any) int {
	switch value := value.(type) {
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func loadSmoke(t *testing.T) (smokeSpec, string, string) {
	t.Helper()
	data, err := os.ReadFile("fixtures/smoke/spec.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec smokeSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	var corpus strings.Builder
	for index := range spec.Records {
		value := spec.Filler
		if index == spec.NeedleRecord {
			value = spec.Needle
		}
		fmt.Fprintf(&corpus, "record=%05d value=%s\n", index, value)
	}
	task, err := os.ReadFile("fixtures/smoke/task.txt")
	if err != nil {
		t.Fatal(err)
	}
	return spec, corpus.String(), string(task)
}

func loadComparison(t *testing.T) (comparisonSpec, string, string) {
	t.Helper()
	data, err := os.ReadFile("fixtures/comparison/spec.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec comparisonSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	var corpus strings.Builder
	for index := range spec.Records {
		value := spec.Filler
		if index == spec.NeedleRecord {
			value = spec.Query + " value=" + spec.Expected
		}
		fmt.Fprintf(&corpus, "record=%05d %s\n", index, value)
	}
	task, err := os.ReadFile("fixtures/comparison/task.txt")
	if err != nil {
		t.Fatal(err)
	}
	return spec, corpus.String(), string(task)
}

func comparisonServer(t *testing.T, spec comparisonSpec) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input llm.Request
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if len(input.Messages) > 0 && input.Messages[len(input.Messages)-1].Role == "tool" {
			body, _ := json.Marshal("result=" + spec.Expected + " citation=fixture-span")
			fmt.Fprintf(w, `data: {"choices":[{"delta":{"content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":950,"completion_tokens":55}}`+"\n\n", body)
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		code := fmt.Sprintf(`candidates = models.batch(prompts=["locate %s", "verify %s"], max_tokens=256)
evidence = context.search(query=%q)
{"text": evidence["matches"][0]["text"], "citation": evidence["matches"][0]["span"]}`, spec.Query, spec.Query, spec.Query)
		arguments, _ := json.Marshal(map[string]string{"code": code})
		fmt.Fprintf(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"rlm-1","type":"function","function":{"name":"rlm_exec","arguments":%q}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":800,"completion_tokens":75}}`+"\n\n", string(arguments))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func evaluateAgent(ctx context.Context, spec comparisonSpec, task string, value *agent.Agent, budget *evaluationBudget, host *smokeHost) (evaluationMetrics, string, error) {
	value.ContextLimit = spec.RootContextTokens
	value.MaxTurns = spec.MaxModelCalls
	value.SetModelCallBudget(budget)
	started := time.Now()
	output, err := value.Turn(ctx, task, agent.Events{})
	duration := time.Since(started)
	usage := value.Usage()
	hostCalls, modelFanout := 0, 0
	if host != nil {
		host.mu.Lock()
		hostCalls = len(host.calls)
		modelFanout = host.modelFanout
		usage.PromptTokens += host.submodelUsed.PromptTokens
		usage.CompletionTokens += host.submodelUsed.CompletionTokens
		host.mu.Unlock()
	}
	metrics := evaluationMetrics{
		Correct: strings.Contains(output, spec.Expected), Error: errorString(err), DurationMillis: duration.Milliseconds(),
		ModelCalls: budget.Calls(), ModelFanout: modelFanout, HostCalls: hostCalls, PromptTokens: usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens, ContextTokensUsed: usage.PromptTokens + usage.CompletionTokens,
		CostUSD: float64(usage.PromptTokens)*spec.InputPrice + float64(usage.CompletionTokens)*spec.OutputPrice,
	}
	return metrics, output, err
}

func logEvaluationReport(t *testing.T, report comparisonReport) []byte {
	t.Helper()
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("RLM evaluation report:\n%s", data)
	return append(data, '\n')
}

func resolveLiveEvalRoute(cfg *config.Config) (config.Provider, config.Model, string, error) {
	return cfg.Resolve(os.Getenv("WHIP_RLM_EVAL_MODEL"), os.Getenv("WHIP_RLM_EVAL_PROVIDER"))
}

func TestEvalKernelWorker(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		return
	}
	if err := rlm.WorkerMain(os.Args[separator+1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func smokeKernel(t *testing.T, host rlm.Host) *rlm.Kernel {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := rlm.NewKernel(rlm.KernelOptions{Command: []string{executable, "-test.run=TestEvalKernelWorker", "--"}, Host: host})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kernel.Close)
	return kernel
}

func TestOversizedCorpusStaysBehindFocusedReads(t *testing.T) {
	spec, corpus, _ := loadSmoke(t)
	host := &smokeHost{corpus: corpus}
	kernel := smokeKernel(t, host)
	index := strings.Index(corpus, spec.Needle)
	code := fmt.Sprintf(`hits = context.search(query=%q)
excerpt = context.read(handle="smoke-corpus", offset=%d, length=%d)
{"text": excerpt["text"], "citation": excerpt["span"]}`, spec.Needle, index, len(spec.Needle))
	result, err := kernel.Exec(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result.Value)
	if !strings.Contains(string(encoded), spec.Needle) || host.maxRead > 8<<10 {
		t.Fatalf("result=%s max_read=%d", encoded, host.maxRead)
	}
	prompt := rlm.BuildPrompt("/workspace", &rlm.ContextHandle{ReferenceID: "smoke-corpus", Size: int64(len(corpus)), Source: "fixture"})
	if strings.Contains(prompt, spec.Needle) || len(prompt) >= len(corpus) {
		t.Fatal("corpus leaked into root prompt")
	}
}

func TestDeterministicRLMEvaluationReport(t *testing.T) {
	spec, corpus, task := loadComparison(t)
	server := comparisonServer(t, spec)
	defer server.Close()

	rlmBudget := &evaluationBudget{maxCalls: spec.MaxModelCalls, maxCallEstimate: int64(spec.RootContextTokens)}
	host := &smokeHost{corpus: corpus, handle: "comparison-corpus", budget: rlmBudget}
	kernel := smokeKernel(t, host)
	rlmAgent := agent.NewRuntime(llm.New(server.URL, "scripted"), "scripted", spec.MaxOutputTokens,
		rlm.BuildPrompt("/fixture", &rlm.ContextHandle{ReferenceID: "comparison-corpus", Size: int64(len(corpus)), Source: "fixture"}), tools.NewServices())
	rlmAgent.SetExclusiveTool(rlm.Tool(kernel), "rlm")
	rlmMetrics, rlmOutput, err := evaluateAgent(t.Context(), spec, task, rlmAgent, rlmBudget, host)
	if err != nil {
		t.Fatal(err)
	}

	report := comparisonReport{
		Fixture: "comparison", Model: "scripted", Provider: "httptest",
		RootContextTokens: spec.RootContextTokens, MaxModelCalls: spec.MaxModelCalls,
		Runtime: rlmMetrics,
	}
	logEvaluationReport(t, report)
	if !report.Runtime.Correct {
		t.Fatalf("runtime output = %q", rlmOutput)
	}
	if report.Runtime.ModelCalls > spec.MaxModelCalls || report.Runtime.HostCalls < 2 || report.Runtime.ModelFanout != 2 {
		t.Fatalf("evaluation budgets/host calls = %+v", report)
	}
	prompt := rlm.BuildPrompt("/fixture", &rlm.ContextHandle{ReferenceID: "comparison-corpus", Size: int64(len(corpus)), Source: "fixture"})
	if host.maxRead > 8<<10 || strings.Contains(prompt, spec.Expected) {
		t.Fatalf("oversized corpus crossed the root boundary: max_read=%d", host.maxRead)
	}
}

func TestLiveOversizedContextSmoke(t *testing.T) {
	if os.Getenv("WHIP_RLM_LIVE_SMOKE") != "1" {
		t.Skip("set WHIP_RLM_LIVE_SMOKE=1 for the opt-in provider evaluation")
	}
	spec, corpus, task := loadSmoke(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	provider, model, apiID, err := resolveLiveEvalRoute(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key, err := provider.ResolveKey()
	if err != nil || key == "" {
		t.Fatalf("resolve provider key: %v", err)
	}
	host := &smokeHost{corpus: corpus}
	kernel := smokeKernel(t, host)
	client := llm.New(provider.BaseURL, key)
	client.MaxRetries = cfg.MaxRetries
	maxOutput := model.MaxOut
	if maxOutput == 0 {
		maxOutput = 4_096
	}
	ag := agent.NewRuntime(client, apiID, maxOutput, rlm.BuildPrompt("/workspace", &rlm.ContextHandle{ReferenceID: "smoke-corpus", Size: int64(len(corpus)), Source: "fixture"}), tools.NewServices())
	ag.MaxTurns = 8
	ag.SetExclusiveTool(rlm.Tool(kernel), "rlm")
	output, err := ag.Turn(context.Background(), task, agent.Events{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, spec.Needle) {
		t.Fatalf("live answer %q does not contain %s", output, spec.Needle)
	}
	host.mu.Lock()
	calls, maxRead := append([]string(nil), host.calls...), host.maxRead
	host.mu.Unlock()
	if !slices.Contains(calls, "context.search") || !slices.Contains(calls, "context.read") || maxRead > 8<<10 {
		t.Fatalf("live module evidence calls=%v max_read=%d", calls, maxRead)
	}
	usage := ag.Usage()
	t.Logf("RLM live smoke: corpus_bytes=%d prompt_bytes=%d calls=%v max_read=%d input_tokens=%d output_tokens=%d", len(corpus), len(task), calls, maxRead, usage.PromptTokens, usage.CompletionTokens)
}

func TestLiveRLMEvaluation(t *testing.T) {
	if os.Getenv("WHIP_RLM_LIVE_EVAL") != "1" {
		t.Skip("set WHIP_RLM_LIVE_EVAL=1 for the opt-in RLM evaluation")
	}
	spec, corpus, task := loadComparison(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	provider, model, apiID, err := resolveLiveEvalRoute(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key, err := provider.ResolveKey()
	if err != nil || key == "" {
		t.Fatalf("resolve provider key: %v", err)
	}
	maxOutput := model.MaxOut
	if maxOutput <= 0 || maxOutput > spec.MaxOutputTokens {
		maxOutput = spec.MaxOutputTokens
	}
	if catalog, ok := config.LoadCatalogs()[provider.Name]; ok {
		if input, output, _, priced := catalog.Pricing(apiID); priced {
			spec.InputPrice, spec.OutputPrice = input, output
		}
	}
	newClient := func() *llm.Client {
		client := llm.New(provider.BaseURL, key)
		client.MaxRetries = cfg.MaxRetries
		return client
	}

	rlmBudget := &evaluationBudget{maxCalls: spec.MaxModelCalls, maxCallEstimate: int64(spec.RootContextTokens)}
	host := &smokeHost{corpus: corpus, handle: "comparison-corpus", client: newClient(), model: apiID, maxTokens: min(maxOutput, 256), budget: rlmBudget}
	kernel := smokeKernel(t, host)
	rlmAgent := agent.NewRuntime(newClient(), apiID, maxOutput,
		rlm.BuildPrompt("/fixture", &rlm.ContextHandle{ReferenceID: "comparison-corpus", Size: int64(len(corpus)), Source: "fixture"}), tools.NewServices())
	rlmAgent.SetExclusiveTool(rlm.Tool(kernel), "rlm")
	rlmMetrics, rlmOutput, err := evaluateAgent(t.Context(), spec, task, rlmAgent, rlmBudget, host)
	rlmErr := err

	report := comparisonReport{
		Fixture: "comparison-live", Model: apiID, Provider: provider.Name,
		RootContextTokens: spec.RootContextTokens, MaxModelCalls: spec.MaxModelCalls,
		Runtime: rlmMetrics,
	}
	data := logEvaluationReport(t, report)
	if path := os.Getenv("WHIP_RLM_EVAL_REPORT"); path != "" {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if rlmErr != nil || !report.Runtime.Correct {
		t.Fatalf("live evaluation output=%q error=%v", rlmOutput, rlmErr)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestSmokeFixtureNeedleIsUnique(t *testing.T) {
	spec, corpus, _ := loadSmoke(t)
	if count := strings.Count(corpus, spec.Needle); count != 1 {
		t.Fatalf("needle count = %d", count)
	}
	if len(corpus) < 500_000 {
		t.Fatalf("fixture expansion is only %s bytes", strconv.Itoa(len(corpus)))
	}
}
