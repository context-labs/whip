package rlm_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
)

type smokeSpec struct {
	Records      int    `json:"records"`
	NeedleRecord int    `json:"needle_record"`
	Needle       string `json:"needle"`
	Filler       string `json:"filler"`
}

type smokeHost struct {
	corpus  string
	mu      sync.Mutex
	calls   []string
	maxRead int
}

func (host *smokeHost) Call(_ context.Context, module, operation string, arguments map[string]any) (any, error) {
	host.mu.Lock()
	host.calls = append(host.calls, module+"."+operation)
	host.mu.Unlock()
	if module == "context" && operation == "search" {
		query, _ := arguments["query"].(string)
		index := strings.Index(host.corpus, query)
		if index < 0 {
			return map[string]any{"matches": []any{}}, nil
		}
		end := index + len(query)
		return map[string]any{"matches": []any{map[string]any{
			"handle": "smoke-corpus", "span": map[string]any{"start": index, "end": end},
			"text": host.corpus[max(0, index-32):min(len(host.corpus), end+64)],
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
		return map[string]any{"text": host.corpus[offset:end], "handle": "smoke-corpus", "span": map[string]any{"start": offset, "end": end}}, nil
	}
	if module == "answer" && operation == "submit" {
		return arguments, nil
	}
	return nil, fmt.Errorf("unsupported smoke operation %s.%s", module, operation)
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
answer.submit(text=excerpt["text"], citations=[excerpt["span"]])`, spec.Needle, index, len(spec.Needle))
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

func TestLiveOversizedContextSmoke(t *testing.T) {
	if os.Getenv("WHIP_RLM_LIVE_SMOKE") != "1" {
		t.Skip("set WHIP_RLM_LIVE_SMOKE=1 for the opt-in provider evaluation")
	}
	spec, corpus, task := loadSmoke(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	provider, model, apiID, err := cfg.Resolve("", "")
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
	ag := agent.New(client, apiID, maxOutput, rlm.BuildPrompt("/workspace", &rlm.ContextHandle{ReferenceID: "smoke-corpus", Size: int64(len(corpus)), Source: "fixture"}))
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

func TestSmokeFixtureNeedleIsUnique(t *testing.T) {
	spec, corpus, _ := loadSmoke(t)
	if count := strings.Count(corpus, spec.Needle); count != 1 {
		t.Fatalf("needle count = %d", count)
	}
	if len(corpus) < 500_000 {
		t.Fatalf("fixture expansion is only %s bytes", strconv.Itoa(len(corpus)))
	}
}
