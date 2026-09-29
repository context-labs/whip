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

	"github.com/context-labs/whip/internal/engine/process"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
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
	PeakCallPromptTokensEstimate *int64  `json:"peak_call_prompt_tokens_estimate"`
	PeakDeclaredInputTokenBound  *int64  `json:"peak_declared_input_token_bound"`
	CostUSD                      float64 `json:"cost_usd"`
	UnknownCostCalls             int     `json:"unknown_cost_calls"`
	UnknownUsageCalls            int     `json:"unknown_usage_calls"`
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
	RootContextTokens int               `json:"fixture_context_target_tokens"`
	MaxModelCalls     int               `json:"max_model_calls"`
	Runtime           evaluationMetrics `json:"runtime"`
}

type smokeHost struct {
	corpus      string
	handle      string
	evaluation  *nativeEvaluation
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
		return host.evaluation.batch(ctx, prompts)
	}
	return nil, fmt.Errorf("unsupported smoke operation %s.%s", module, operation)
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
	if engineID == process.EngineQuickJS {
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
	descriptor, err := process.ResolveExecutionEngine(engineID)
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
		var input chatRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if len(input.Messages) == 1 && input.Messages[0].Role == "user" {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"candidate found through a bounded corpus search\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n")
			return
		}
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
		fmt.Fprintf(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"rlm-1","type":"function","function":{"name":"execute","arguments":%q}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":800,"completion_tokens":75}}`+"\n\n", string(arguments))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
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

func TestEvalKernelWorker(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		return
	}
	if err := process.WorkerMain(os.Args[separator+1:], os.Stdin, os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func smokeKernel(t *testing.T, engineID string, host process.Host) *process.Kernel {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := process.NewKernel(process.KernelOptions{Command: []string{executable, "-test.run=TestEvalKernelWorker", "--"}, Engine: engineID, Host: host})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kernel.Close)
	return kernel
}

func TestOversizedCorpusStaysBehindFocusedReads(t *testing.T) {
	for _, engineID := range []string{process.EngineStarlark, process.EngineQuickJS} {
		t.Run(engineID, func(t *testing.T) {
			spec, corpus, _ := loadSmoke(t)
			host := &smokeHost{corpus: corpus}
			kernel := smokeKernel(t, engineID, host)
			want := corpusEvidence(corpus, "smoke-corpus", spec.Needle)
			code := fmt.Sprintf(`hits = context.search(query=%q)
excerpt = context.read(handle=hits["matches"][0]["handle"], offset=hits["matches"][0]["span"]["start"], length=%d)
{"text": excerpt["text"], "handle": excerpt["handle"], "citation": excerpt["span"]}`, spec.Needle, len(spec.Needle))
			if engineID == process.EngineQuickJS {
				code = fmt.Sprintf(`const hits = await context.search({query:%q});
const excerpt = await context.read({handle:hits.matches[0].handle,offset:hits.matches[0].span.start,length:%d});
({text:excerpt.text,handle:excerpt.handle,citation:excerpt.span})`, spec.Needle, len(spec.Needle))
			}
			result, err := kernel.Exec(t.Context(), process.Cell{Code: code})
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
			prompt := codingPrompt(t, engineID, "/workspace", &corpusHandle{ID: "smoke-corpus", Size: len(corpus)})
			if strings.Contains(prompt, spec.Needle) || len(prompt) >= len(corpus) {
				t.Fatal("corpus leaked into root prompt")
			}
		})
	}
}

func TestDeterministicRLMEvaluationReport(t *testing.T) {
	for _, engineID := range []string{process.EngineStarlark, process.EngineQuickJS} {
		t.Run(engineID, func(t *testing.T) {
			spec, corpus, task := loadComparison(t)
			evidence := comparisonEvidence(spec, corpus)
			server := comparisonServer(t, engineID, spec, evidence)
			defer server.Close()
			host := &smokeHost{corpus: corpus, handle: "comparison-corpus"}
			value := newNativeEvaluation(t, engineID, fixtureProvider(server.URL, spec), session.ModelSelection{Provider: "fixture", Name: "scripted"}, spec, host)
			metrics, output, err := value.evaluate(t.Context(), spec, task)
			if err != nil {
				t.Fatal(err)
			}
			descriptor, _ := process.ResolveExecutionEngine(engineID)
			report := comparisonReport{ExecutionEngine: engineID, Language: descriptor.Language, UsageSource: "synthetic_fixture", PricingSource: "synthetic_fixture", Fixture: "comparison", Model: "scripted", Provider: "httptest", RootContextTokens: spec.RootContextTokens, MaxModelCalls: spec.MaxModelCalls, Runtime: metrics}
			logEvaluationReport(t, report)
			if !report.Runtime.Correct || output != comparisonAnswer(spec, evidence) {
				t.Fatalf("runtime output=%q", output)
			}
			if metrics.ModelCalls != 4 || metrics.ModelFanout != 2 || !slices.Equal(host.calls, []string{"models.batch", "context.search", "context.read"}) {
				t.Fatalf("budget/calls=%+v host=%v", metrics, host.calls)
			}
			if metrics.CumulativeTokens != 2100 || metrics.PeakCallPromptTokens != 950 || metrics.PeakCallPromptTokensEstimate != nil || metrics.PeakDeclaredInputTokenBound == nil || *metrics.PeakDeclaredInputTokenBound != int64(spec.RootContextTokens) || metrics.UnknownUsageCalls != 0 || metrics.EstimatedCostCalls != 4 {
				t.Fatalf("usage metrics=%+v", metrics)
			}
			if host.maxRead != len(evidence.Text) || strings.Contains(value.prompt, spec.Expected) {
				t.Fatalf("corpus boundary max_read=%d", host.maxRead)
			}
		})
	}
}

func TestComparisonProviderRejectsInvalidEvidence(t *testing.T) {
	spec := comparisonSpec{Query: "target", Expected: "answer"}
	want := fixtureEvidence{Text: "target value=answer", Handle: "comparison-corpus", Citation: byteSpan{Start: 41, End: 60}}
	for _, engineID := range []string{process.EngineStarlark, process.EngineQuickJS} {
		t.Run(engineID, func(t *testing.T) {
			descriptor, _ := process.ResolveExecutionEngine(engineID)
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
					output, err := probeToolResult(t.Context(), server.URL, string(content))
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
		pricing     session.ModelPrices
		cost        float64
		unknown     int
	}{
		{"provider charge", `{"prompt_tokens":2,"completion_tokens":1,"cost":0.015}`, fixturePrices(1, 1), 0.015, 0},
		{"provider free", `{"prompt_tokens":2,"completion_tokens":1,"cost":0}`, fixturePrices(1, 1), 0, 0},
		{"unknown price", `{"prompt_tokens":2,"completion_tokens":1}`, session.ModelPrices{}, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}],\"usage\":%s}\n\n", tc.usage)
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer server.Close()
			spec := comparisonSpec{RootContextTokens: 100_000, MaxModelCalls: 1, MaxOutputTokens: 10, Expected: "answer"}
			provider := fixtureProvider(server.URL, spec)
			resolve := provider.Resolve
			provider.Resolve = func(ctx context.Context, selected session.ModelSelection) (model.Route, error) {
				route, err := resolve(ctx, selected)
				route.Prices = tc.pricing
				return route, err
			}
			value := newNativeEvaluation(t, process.EngineStarlark, provider, session.ModelSelection{Provider: "fixture", Name: "scripted"}, spec, nil)
			metrics, _, err := value.evaluate(t.Context(), spec, "question")
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
	smoke, corpus, task := loadSmoke(t)
	engineID := liveEvalEngine(t)
	provider, selected := liveProvider(t)
	host := &smokeHost{corpus: corpus}
	spec := comparisonSpec{RootContextTokens: 16384, MaxModelCalls: 8, MaxOutputTokens: 4096, ExpectedAnswer: evidenceAnswer(smoke.Needle, corpusEvidence(corpus, "smoke-corpus", smoke.Needle))}
	value := newNativeEvaluation(t, engineID, provider, selected, spec, host)
	metrics, output, err := value.evaluate(t.Context(), spec, task)
	if err != nil || !metrics.Correct {
		t.Fatalf("live answer=%q error=%v", output, err)
	}
	if !slices.Contains(host.calls, "context.search") || !slices.Contains(host.calls, "context.read") || host.maxRead > 8<<10 {
		t.Fatalf("calls=%v max_read=%d", host.calls, host.maxRead)
	}
	t.Logf("native live fixture: engine=%s corpus_bytes=%d usage=%+v", engineID, len(corpus), metrics)
}

func TestLiveRLMEvaluation(t *testing.T) {
	if os.Getenv("WHIP_RLM_LIVE_EVAL") != "1" {
		t.Skip("set WHIP_RLM_LIVE_EVAL=1 for the opt-in RLM evaluation")
	}
	spec, corpus, task := loadComparison(t)
	engineID := liveEvalEngine(t)
	provider, selected := liveProvider(t)
	host := &smokeHost{corpus: corpus, handle: "comparison-corpus"}
	value := newNativeEvaluation(t, engineID, provider, selected, spec, host)
	metrics, output, err := value.evaluate(t.Context(), spec, task)
	descriptor, _ := process.ResolveExecutionEngine(engineID)
	report := comparisonReport{ExecutionEngine: engineID, Language: descriptor.Language, UsageSource: "provider_reported", PricingSource: "provider_charge_or_declared_estimate", Fixture: "comparison-live", Model: selected.Name, Provider: selected.Provider, RootContextTokens: spec.RootContextTokens, MaxModelCalls: spec.MaxModelCalls, Runtime: metrics}
	data := logEvaluationReport(t, report)
	if path := os.Getenv("WHIP_RLM_EVAL_REPORT"); path != "" {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err != nil || !metrics.Correct {
		t.Fatalf("live output=%q error=%v", output, err)
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
	descriptor, err := process.ResolveExecutionEngine(os.Getenv("WHIP_RLM_EVAL_ENGINE"))
	if err != nil {
		t.Fatal(err)
	}
	return descriptor.ID
}

func TestLiveEvalEngineSelection(t *testing.T) {
	for _, test := range []struct{ name, value, want string }{
		{name: "default", value: "", want: process.EngineStarlark},
		{name: "explicit Starlark", value: process.EngineStarlark, want: process.EngineStarlark},
		{name: "explicit QuickJS", value: process.EngineQuickJS, want: process.EngineQuickJS},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("WHIP_RLM_EVAL_ENGINE", test.value)
			if got := liveEvalEngine(t); got != test.want {
				t.Fatalf("engine=%s want=%s", got, test.want)
			}
		})
	}
}

type corpusHandle struct {
	ID   string
	Size int
}

// The corpus module is deliberately a restricted evaluation fixture. Production
// context operations inspect canonical history, not this synthetic corpus.
func codingPrompt(tb testing.TB, engineID, workingDirectory string, handle *corpusHandle) string {
	tb.Helper()
	descriptor, err := process.ResolveExecutionEngine(engineID)
	if err != nil {
		tb.Fatal(err)
	}
	return fmt.Sprintf("Use execute to run %s in an isolated REPL. Workspace: %s. Synthetic corpus handle %s has %d bytes; its body is not in this prompt. Fixture context.inspect(handle), context.search(handle, query), context.read(handle, offset, length) return metadata, matches with exact byte spans, and bounded text with handle/span. Reads are at most8192bytes. models.batch(prompts, max_tokens=256) runs stateless reviewers. Starlark uses keyword arguments; JavaScript uses await and one object argument. Return the exact requested answer and citation.", descriptor.Language, workingDirectory, handle.ID, handle.Size)
}
