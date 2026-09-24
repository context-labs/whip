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
	"github.com/context-labs/whip/internal/agentdef"
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
	ExpectedAnswer    string  `json:"-"`
	Filler            string  `json:"filler"`
	RootContextTokens int     `json:"root_context_tokens"`
	MaxOutputTokens   int     `json:"max_output_tokens"`
	MaxModelCalls     int     `json:"max_model_calls"`
	InputPrice        float64 `json:"input_price"`
	OutputPrice       float64 `json:"output_price"`
}

type evaluationMetrics struct {
	Correct                      bool    `json:"correct"`
	Error                        string  `json:"error,omitempty"`
	DurationMillis               int64   `json:"duration_ms"`
	ModelCalls                   int     `json:"model_calls"`
	ModelFanout                  int     `json:"model_fan_out"`
	HostCalls                    int     `json:"host_calls"`
	PromptTokens                 int     `json:"cumulative_prompt_tokens"`
	CompletionTokens             int     `json:"cumulative_completion_tokens"`
	CumulativeTokens             int     `json:"cumulative_tokens"`
	PeakCallPromptTokens         int     `json:"peak_call_prompt_tokens"`
	PeakCallPromptTokensEstimate int64   `json:"peak_call_prompt_tokens_estimate"`
	CostUSD                      float64 `json:"cost_usd"`
	UnknownCostCalls             int     `json:"unknown_cost_calls"`
	EstimatedCostCalls           int     `json:"estimated_cost_calls"`
}

type comparisonReport struct {
	ExecutionEngine   string            `json:"execution_engine"`
	Language          string            `json:"language"`
	UsageSource       string            `json:"usage_source"`
	PricingSource     string            `json:"pricing_source"`
	Fixture           string            `json:"fixture"`
	Model             string            `json:"model"`
	Provider          string            `json:"provider"`
	RootContextTokens int               `json:"root_context_tokens"`
	MaxModelCalls     int               `json:"max_model_calls"`
	Runtime           evaluationMetrics `json:"runtime"`
}

type evaluationBudget struct {
	mu                           sync.Mutex
	maxCalls                     int
	maxCallEstimate              int64
	peakCallPromptTokens         int
	peakCallPromptTokensEstimate int64
	calls                        int
	usage                        llm.Usage
	costMicros                   int64
	unknownCostCalls             int
	estimatedCostCalls           int
}

func (budget *evaluationBudget) BeginModelAttempt(_ context.Context, attempt llm.ModelAttempt) (llm.ModelPermit, error) {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.calls >= budget.maxCalls {
		return llm.ModelPermit{}, errors.New("evaluation model-call budget exhausted")
	}
	if attempt.InputTokens > budget.maxCallEstimate-int64(attempt.MaxTokens) {
		return llm.ModelPermit{}, fmt.Errorf("evaluation context estimate exceeds %d", budget.maxCallEstimate)
	}
	budget.calls++
	budget.peakCallPromptTokensEstimate = max(budget.peakCallPromptTokensEstimate, attempt.InputTokens)
	var once sync.Once
	var settleErr error
	return llm.ModelPermit{MaxTokens: attempt.MaxTokens, Timeout: attempt.Timeout, Settle: func(result llm.ModelAttemptResult) error {
		once.Do(func() {
			if !result.Dispatched {
				return
			}
			cost, known, err := attempt.Pricing.ActualCost(result.Usage)
			if err != nil {
				settleErr = err
				return
			}
			if !known && !result.Usage.HasUsage() && attempt.Pricing.Known() {
				cost, err = attempt.Pricing.ReserveCost(attempt.InputTokens, int64(attempt.MaxTokens))
				if err != nil {
					settleErr = err
					return
				}
				known = true
			}
			budget.mu.Lock()
			defer budget.mu.Unlock()
			budget.peakCallPromptTokens = max(budget.peakCallPromptTokens, result.Usage.PromptTokens)
			budget.usage.PromptTokens += result.Usage.PromptTokens
			budget.usage.CompletionTokens += result.Usage.CompletionTokens
			budget.costMicros += cost
			if !known {
				budget.unknownCostCalls++
			} else if result.Usage.Cost == nil {
				budget.estimatedCostCalls++
			}
		})
		return settleErr
	}}, nil
}

func (budget *evaluationBudget) Calls() int {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.calls
}

type smokeHost struct {
	corpus      string
	handle      string
	client      *llm.Client
	model       string
	pricing     llm.Pricing
	maxTokens   int
	budget      *evaluationBudget
	mu          sync.Mutex
	calls       []string
	maxRead     int
	modelFanout int
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
	if module == "context" {
		if handle, ok := arguments["handle"].(string); ok && handle != host.contextHandle() {
			return nil, fmt.Errorf("unknown context handle %q", handle)
		}
	}
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
		var accounting *llm.CallAccounting
		if host.budget != nil {
			accounting = &llm.CallAccounting{Budget: host.budget, Purpose: "models.call", Pricing: host.pricing}
		}
		output, usage, err = host.client.Complete(ctx, llm.Request{
			Model: host.model, Messages: []llm.Message{{Role: "user", Content: prompt}}, MaxTokens: maxTokens, Accounting: accounting,
		})
	} else if host.budget != nil {
		permit, reserveErr := host.budget.BeginModelAttempt(ctx, llm.ModelAttempt{InputTokens: int64(llm.EstimateTokens([]llm.Message{{Role: "user", Content: prompt}})), MaxTokens: 256, Pricing: host.pricing})
		if reserveErr != nil {
			return map[string]any{"error": reserveErr.Error()}
		}
		if settleErr := permit.Settle(llm.ModelAttemptResult{Usage: usage, Dispatched: true}); settleErr != nil {
			return map[string]any{"error": settleErr.Error()}
		}
	}
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"output": output, "usage": usage}
}

func number(value any) int {
	switch value := value.(type) {
	case int64:
		return int(value)
	case json.Number:
		integer, err := value.Int64()
		if err == nil {
			return int(integer)
		}
		return 0
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
	if strings.Count(corpus.String(), spec.Query+" value="+spec.Expected) != 1 {
		t.Fatal("comparison target must be unique")
	}
	spec.ExpectedAnswer = comparisonAnswer(spec, comparisonEvidence(spec, corpus.String()))
	task, err := os.ReadFile("fixtures/comparison/task.txt")
	if err != nil {
		t.Fatal(err)
	}
	return spec, corpus.String(), string(task)
}

type byteSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type fixtureEvidence struct {
	Text     string   `json:"text"`
	Handle   string   `json:"handle"`
	Citation byteSpan `json:"citation"`
}

func corpusEvidence(corpus, handle, text string) fixtureEvidence {
	start := strings.Index(corpus, text)
	return fixtureEvidence{Text: text, Handle: handle, Citation: byteSpan{Start: start, End: start + len(text)}}
}

func comparisonEvidence(spec comparisonSpec, corpus string) fixtureEvidence {
	return corpusEvidence(corpus, "comparison-corpus", spec.Query+" value="+spec.Expected)
}

func evidenceAnswer(value string, evidence fixtureEvidence) string {
	return fmt.Sprintf("result=%s citation=%s:%d-%d", value, evidence.Handle, evidence.Citation.Start, evidence.Citation.End)
}

func comparisonAnswer(spec comparisonSpec, evidence fixtureEvidence) string {
	return evidenceAnswer(spec.Expected, evidence)
}

func comparisonCode(engineID string, spec comparisonSpec, evidence fixtureEvidence) string {
	if engineID == rlm.EngineQuickJS {
		return fmt.Sprintf(`const candidates = await models.batch({prompts:["locate %s", "verify %s"], max_tokens:256});
const evidence = await context.search({handle:%q, query:%q});
const excerpt = await context.read({handle:evidence.matches[0].handle, offset:evidence.matches[0].span.start, length:%d});
({text:excerpt.text, handle:excerpt.handle, citation:excerpt.span})`, spec.Query, spec.Query, evidence.Handle, spec.Query, len(evidence.Text))
	}
	return fmt.Sprintf(`candidates = models.batch(prompts=["locate %s", "verify %s"], max_tokens=256)
evidence = context.search(handle=%q, query=%q)
excerpt = context.read(handle=evidence["matches"][0]["handle"], offset=evidence["matches"][0]["span"]["start"], length=%d)
{"text": excerpt["text"], "handle": excerpt["handle"], "citation": excerpt["span"]}`, spec.Query, spec.Query, evidence.Handle, spec.Query, len(evidence.Text))
}

func validateComparisonToolResult(content, engineID string, want fixtureEvidence) error {
	var result struct {
		FormatVersion   int             `json:"format_version"`
		ExecutionEngine string          `json:"execution_engine"`
		Language        string          `json:"language"`
		HasValue        bool            `json:"has_value"`
		Value           fixtureEvidence `json:"value"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return fmt.Errorf("invalid tool result: %w", err)
	}
	descriptor, err := rlm.ResolveEngine(engineID)
	if err != nil {
		return err
	}
	if result.FormatVersion != 2 || result.ExecutionEngine != engineID || result.Language != descriptor.Language || !result.HasValue {
		return errors.New("missing or mismatched engine result")
	}
	if result.Value != want {
		return fmt.Errorf("evidence mismatch: got %+v, want %+v", result.Value, want)
	}
	return nil
}

func comparisonServer(t *testing.T, engineID string, spec comparisonSpec, evidence fixtureEvidence) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input llm.Request
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if len(input.Messages) > 0 && input.Messages[len(input.Messages)-1].Role == "tool" {
			answer := comparisonAnswer(spec, evidence)
			if err := validateComparisonToolResult(input.Messages[len(input.Messages)-1].Content, engineID, evidence); err != nil {
				answer = "fixture validation failed: " + err.Error()
			}
			body, _ := json.Marshal(answer)
			fmt.Fprintf(w, `data: {"choices":[{"delta":{"content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":950,"completion_tokens":55}}`+"\n\n", body)
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		arguments, _ := json.Marshal(map[string]string{"code": comparisonCode(engineID, spec, evidence)})
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
	budget.mu.Lock()
	usage := budget.usage
	peakPrompt, peakPromptEstimate := budget.peakCallPromptTokens, budget.peakCallPromptTokensEstimate
	costUSD, unknown, estimated := float64(budget.costMicros)/1e6, budget.unknownCostCalls, budget.estimatedCostCalls
	budget.mu.Unlock()
	hostCalls, modelFanout := 0, 0
	if host != nil {
		host.mu.Lock()
		hostCalls = len(host.calls)
		modelFanout = host.modelFanout
		host.mu.Unlock()
	}
	expected := spec.ExpectedAnswer
	if expected == "" {
		expected = spec.Expected
	}
	metrics := evaluationMetrics{
		Correct: output == expected, Error: errorString(err), DurationMillis: duration.Milliseconds(),
		ModelCalls: budget.Calls(), ModelFanout: modelFanout, HostCalls: hostCalls, PromptTokens: usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens, CumulativeTokens: usage.PromptTokens + usage.CompletionTokens,
		PeakCallPromptTokens: peakPrompt, PeakCallPromptTokensEstimate: peakPromptEstimate,
		CostUSD: costUSD, UnknownCostCalls: unknown, EstimatedCostCalls: estimated,
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

func smokeKernel(t *testing.T, engineID string, host rlm.Host) *rlm.Kernel {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := rlm.NewKernel(rlm.KernelOptions{Command: []string{executable, "-test.run=TestEvalKernelWorker", "--"}, Engine: engineID, Host: host})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kernel.Close)
	return kernel
}

func TestOversizedCorpusStaysBehindFocusedReads(t *testing.T) {
	for _, engineID := range []string{rlm.EngineStarlark, rlm.EngineQuickJS} {
		t.Run(engineID, func(t *testing.T) {
			spec, corpus, _ := loadSmoke(t)
			host := &smokeHost{corpus: corpus}
			kernel := smokeKernel(t, engineID, host)
			want := corpusEvidence(corpus, "smoke-corpus", spec.Needle)
			code := fmt.Sprintf(`hits = context.search(query=%q)
excerpt = context.read(handle=hits["matches"][0]["handle"], offset=hits["matches"][0]["span"]["start"], length=%d)
{"text": excerpt["text"], "handle": excerpt["handle"], "citation": excerpt["span"]}`, spec.Needle, len(spec.Needle))
			if engineID == rlm.EngineQuickJS {
				code = fmt.Sprintf(`const hits = await context.search({query:%q});
const excerpt = await context.read({handle:hits.matches[0].handle,offset:hits.matches[0].span.start,length:%d});
({text:excerpt.text,handle:excerpt.handle,citation:excerpt.span})`, spec.Needle, len(spec.Needle))
			}
			result, err := kernel.Exec(t.Context(), code)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(result.Value)
			if err != nil {
				t.Fatal(err)
			}
			var got fixtureEvidence
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if got != want || result.ExecutionEngine != engineID || !result.HasValue || host.maxRead != len(spec.Needle) {
				t.Fatalf("result=%+v want=%+v engine=%s max_read=%d", got, want, result.ExecutionEngine, host.maxRead)
			}
			if !slices.Equal(host.calls, []string{"context.search", "context.read"}) {
				t.Fatalf("host calls=%v", host.calls)
			}
			prompt := codingPrompt(t, engineID, "/workspace", &rlm.ContextHandle{ReferenceID: "smoke-corpus", Size: int64(len(corpus)), Source: "fixture"})
			if strings.Contains(prompt, spec.Needle) || len(prompt) >= len(corpus) {
				t.Fatal("corpus leaked into root prompt")
			}
		})
	}
}

func TestDeterministicRLMEvaluationReport(t *testing.T) {
	for _, engineID := range []string{rlm.EngineStarlark, rlm.EngineQuickJS} {
		t.Run(engineID, func(t *testing.T) {
			spec, corpus, task := loadComparison(t)
			evidence := comparisonEvidence(spec, corpus)
			server := comparisonServer(t, engineID, spec, evidence)
			defer server.Close()
			budget := &evaluationBudget{maxCalls: spec.MaxModelCalls, maxCallEstimate: int64(spec.RootContextTokens)}
			pricing := llm.Pricing{Prompt: strconv.FormatFloat(spec.InputPrice, 'g', -1, 64), Completion: strconv.FormatFloat(spec.OutputPrice, 'g', -1, 64)}
			host := &smokeHost{corpus: corpus, handle: "comparison-corpus", budget: budget, pricing: pricing}
			kernel := smokeKernel(t, engineID, host)
			prompt := codingPrompt(t, engineID, "/fixture", &rlm.ContextHandle{ReferenceID: "comparison-corpus", Size: int64(len(corpus)), Source: "fixture"})
			value := agent.NewRuntime(llm.New(server.URL, "scripted"), "scripted", spec.MaxOutputTokens, prompt, tools.NewServices())
			value.Pricing = pricing
			value.SetExclusiveTool(rlm.Tool(kernel), "rlm")
			metrics, output, err := evaluateAgent(t.Context(), spec, task, value, budget, host)
			if err != nil {
				t.Fatal(err)
			}
			descriptor, _ := rlm.ResolveEngine(engineID)
			report := comparisonReport{ExecutionEngine: engineID, Language: descriptor.Language, UsageSource: "synthetic_fixture", PricingSource: "synthetic_fixture", Fixture: "comparison", Model: "scripted", Provider: "httptest", RootContextTokens: spec.RootContextTokens, MaxModelCalls: spec.MaxModelCalls, Runtime: metrics}
			logEvaluationReport(t, report)
			if !report.Runtime.Correct || output != comparisonAnswer(spec, evidence) {
				t.Fatalf("runtime output=%q", output)
			}
			if metrics.ModelCalls != 4 || metrics.ModelFanout != 2 || !slices.Equal(host.calls, []string{"models.batch", "context.search", "context.read"}) {
				t.Fatalf("budget/calls=%+v host=%v", metrics, host.calls)
			}
			if metrics.CumulativeTokens != 2100 || metrics.PeakCallPromptTokens != 950 || metrics.PeakCallPromptTokensEstimate <= 0 || metrics.PeakCallPromptTokensEstimate > int64(spec.RootContextTokens) {
				t.Fatalf("usage metrics=%+v", metrics)
			}
			if host.maxRead != len(evidence.Text) || strings.Contains(prompt, spec.Expected) {
				t.Fatalf("corpus boundary max_read=%d", host.maxRead)
			}
		})
	}
}

func TestComparisonProviderRejectsInvalidEvidence(t *testing.T) {
	spec := comparisonSpec{Query: "target", Expected: "answer"}
	want := fixtureEvidence{Text: "target value=answer", Handle: "comparison-corpus", Citation: byteSpan{Start: 41, End: 60}}
	for _, engineID := range []string{rlm.EngineStarlark, rlm.EngineQuickJS} {
		t.Run(engineID, func(t *testing.T) {
			descriptor, _ := rlm.ResolveEngine(engineID)
			server := comparisonServer(t, engineID, spec, want)
			defer server.Close()
			for _, test := range []struct {
				name   string
				change func(map[string]any)
			}{
				{name: "wrong text with correct substring", change: func(result map[string]any) {
					evidence := want
					evidence.Text = "unrelated answer"
					result["value"] = evidence
				}},
				{name: "wrong handle", change: func(result map[string]any) {
					evidence := want
					evidence.Handle = "another-corpus"
					result["value"] = evidence
				}},
				{name: "wrong span", change: func(result map[string]any) { evidence := want; evidence.Citation.End--; result["value"] = evidence }},
				{name: "wrong engine", change: func(result map[string]any) { result["execution_engine"] = "other" }},
				{name: "missing value", change: func(result map[string]any) { result["has_value"] = false }},
			} {
				t.Run(test.name, func(t *testing.T) {
					result := map[string]any{"format_version": 2, "execution_engine": engineID, "language": descriptor.Language, "has_value": true, "value": want}
					test.change(result)
					content, err := json.Marshal(result)
					if err != nil {
						t.Fatal(err)
					}
					call := llm.ToolCall{ID: "rlm-1", Type: "function"}
					call.Function.Name, call.Function.Arguments = "rlm_exec", "{}"
					response, _, err := llm.New(server.URL, "scripted").Stream(t.Context(), llm.Request{Model: "scripted", MaxTokens: 256, Messages: []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{call}}, {Role: "tool", ToolCallID: call.ID, Content: string(content)}}}, nil, nil, nil)
					output := response.Content
					if err != nil || !strings.HasPrefix(output, "fixture validation failed:") {
						t.Fatalf("faux provider accepted invalid evidence: output=%q error=%v", output, err)
					}
				})
			}
			if err := validateComparisonToolResult("Error: failed cell", engineID, want); err == nil {
				t.Fatal("failed execution accepted as valid evidence")
			}
		})
	}
}

func TestEvaluationAccountsProviderChargesAndUnknownPrices(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		pricing     llm.Pricing
		cost        float64
		unknown     int
	}{
		{"provider charge", `{"prompt_tokens":2,"completion_tokens":1,"cost":0.015}`, llm.Pricing{Prompt: "1", Completion: "1"}, 0.015, 0},
		{"provider free", `{"prompt_tokens":2,"completion_tokens":1,"cost":0}`, llm.Pricing{Prompt: "1", Completion: "1"}, 0, 0},
		{"unknown price", `{"prompt_tokens":2,"completion_tokens":1}`, llm.Pricing{}, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}],\"usage\":%s}\n\n", tc.usage)
			}))
			defer server.Close()
			budget := &evaluationBudget{maxCalls: 1, maxCallEstimate: 100_000}
			value := agent.NewRuntime(llm.New(server.URL, "test"), "test", 10, "answer briefly", tools.NewServices())
			value.Pricing = tc.pricing
			metrics, _, err := evaluateAgent(t.Context(), comparisonSpec{RootContextTokens: 100_000, MaxModelCalls: 1, Expected: "answer"}, "question", value, budget, nil)
			if err != nil || metrics.CostUSD != tc.cost || metrics.UnknownCostCalls != tc.unknown || metrics.ModelCalls != 1 {
				t.Fatalf("metrics=%+v err=%v", metrics, err)
			}
		})
	}
}

func TestLiveOversizedContextSmoke(t *testing.T) {
	if os.Getenv("WHIP_RLM_LIVE_SMOKE") != "1" {
		t.Skip("set WHIP_RLM_LIVE_SMOKE=1 for the opt-in provider evaluation")
	}
	spec, corpus, task := loadSmoke(t)
	engineID := liveEvalEngine(t)
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
	kernel := smokeKernel(t, engineID, host)
	client := llm.New(provider.BaseURL, key)
	client.MaxRetries = cfg.MaxRetries
	maxOutput := model.MaxOut
	if maxOutput == 0 {
		maxOutput = 4_096
	}
	ag := agent.NewRuntime(client, apiID, maxOutput, codingPrompt(t, engineID, "/workspace", &rlm.ContextHandle{ReferenceID: "smoke-corpus", Size: int64(len(corpus)), Source: "fixture"}), tools.NewServices())
	ag.MaxTurns = 8
	ag.SetExclusiveTool(rlm.Tool(kernel), "rlm")
	output, err := ag.Turn(context.Background(), task, agent.Events{})
	if err != nil {
		t.Fatal(err)
	}
	expected := evidenceAnswer(spec.Needle, corpusEvidence(corpus, "smoke-corpus", spec.Needle))
	if output != expected {
		t.Fatalf("live answer=%q want=%q", output, expected)
	}
	host.mu.Lock()
	calls, maxRead := append([]string(nil), host.calls...), host.maxRead
	host.mu.Unlock()
	if !slices.Contains(calls, "context.search") || !slices.Contains(calls, "context.read") || maxRead > 8<<10 {
		t.Fatalf("live module evidence calls=%v max_read=%d", calls, maxRead)
	}
	usage := ag.Usage()
	t.Logf("RLM live smoke: engine=%s usage_source=provider_reported corpus_bytes=%d prompt_bytes=%d calls=%v max_read=%d input_tokens=%d output_tokens=%d", engineID, len(corpus), len(task), calls, maxRead, usage.PromptTokens, usage.CompletionTokens)
}

func TestLiveRLMEvaluation(t *testing.T) {
	if os.Getenv("WHIP_RLM_LIVE_EVAL") != "1" {
		t.Skip("set WHIP_RLM_LIVE_EVAL=1 for the opt-in RLM evaluation")
	}
	spec, corpus, task := loadComparison(t)
	engineID := liveEvalEngine(t)
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
	var pricing llm.Pricing
	if catalog, ok := config.LoadCatalogs()[provider.Name]; ok {
		pricing = catalog.ModelPricing(apiID)
	}
	newClient := func() *llm.Client {
		client := llm.New(provider.BaseURL, key)
		client.MaxRetries = cfg.MaxRetries
		return client
	}

	rlmBudget := &evaluationBudget{maxCalls: spec.MaxModelCalls, maxCallEstimate: int64(spec.RootContextTokens)}
	host := &smokeHost{corpus: corpus, handle: "comparison-corpus", client: newClient(), model: apiID, maxTokens: min(maxOutput, 256), budget: rlmBudget, pricing: pricing}
	kernel := smokeKernel(t, engineID, host)
	rlmAgent := agent.NewRuntime(newClient(), apiID, maxOutput,
		codingPrompt(t, engineID, "/fixture", &rlm.ContextHandle{ReferenceID: "comparison-corpus", Size: int64(len(corpus)), Source: "fixture"}), tools.NewServices())
	rlmAgent.Pricing = pricing
	rlmAgent.SetExclusiveTool(rlm.Tool(kernel), "rlm")
	rlmMetrics, rlmOutput, err := evaluateAgent(t.Context(), spec, task, rlmAgent, rlmBudget, host)
	rlmErr := err

	descriptor, _ := rlm.ResolveEngine(engineID)
	report := comparisonReport{
		ExecutionEngine: engineID, Language: descriptor.Language, UsageSource: "provider_reported", PricingSource: "provider_charge_or_catalog_estimate",
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

func liveEvalEngine(t *testing.T) string {
	t.Helper()
	descriptor, err := rlm.ResolveEngine(os.Getenv("WHIP_RLM_EVAL_ENGINE"))
	if err != nil {
		t.Fatal(err)
	}
	return descriptor.ID
}

func TestLiveEvalEngineSelection(t *testing.T) {
	for _, test := range []struct{ name, value, want string }{
		{name: "default", value: "", want: rlm.EngineStarlark},
		{name: "explicit Starlark", value: rlm.EngineStarlark, want: rlm.EngineStarlark},
		{name: "explicit QuickJS", value: rlm.EngineQuickJS, want: rlm.EngineQuickJS},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("WHIP_RLM_EVAL_ENGINE", test.value)
			if got := liveEvalEngine(t); got != test.want {
				t.Fatalf("engine=%s want=%s", got, test.want)
			}
		})
	}
}

// codingPrompt is the coding agent's standalone system prompt for one engine.
func codingPrompt(tb testing.TB, engine, workingDirectory string, handle *rlm.ContextHandle) string {
	tb.Helper()
	prompt, err := agentdef.Coding().SystemPrompt(engine, workingDirectory, handle)
	if err != nil {
		tb.Fatal(err)
	}
	return prompt
}
