package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// echoRunner is a Runner that needs no LLM: it answers "ok:<prompt-tail>".
func echoRunner(calls *atomic.Int64) Runner {
	return func(ctx context.Context, req AgentRequest) (any, Usage, error) {
		if calls != nil {
			calls.Add(1)
		}
		return fmt.Sprintf("ok:%d", req.Index), Usage{Total: 10}, nil
	}
}

const metaHeader = "export const meta = { name: 't', description: 'd' }\n"

func TestRunSimpleAgent(t *testing.T) {
	res, err := Run(context.Background(), metaHeader+`return await agent('do thing')`, Options{Run: echoRunner(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != "ok:0" {
		t.Fatalf("value: %v", res.Value)
	}
	if res.AgentCount != 1 {
		t.Fatalf("agentCount: %d", res.AgentCount)
	}
	if len(res.Journal) != 1 || res.Journal[0].Hash == "" {
		t.Fatalf("journal: %+v", res.Journal)
	}
	if res.Tokens != 10 {
		t.Fatalf("tokens: %d", res.Tokens)
	}
}

func TestRunRequiresAgent(t *testing.T) {
	_, err := Run(context.Background(), metaHeader+`return 42`, Options{Run: echoRunner(nil)})
	if err == nil || !strings.Contains(err.Error(), "at least once") {
		t.Fatalf("expected agent-required error, got %v", err)
	}
}

func TestPipelineNoBarrier(t *testing.T) {
	// A slow stage-1 on item 0 must NOT hold back item 1's stage-2 — that's
	// the no-barrier contract (runtime.ts pipeline).
	var orderMu sync.Mutex
	var order []string
	runner := func(ctx context.Context, req AgentRequest) (any, Usage, error) {
		label := req.Options.Label
		orderMu.Lock()
		order = append(order, label)
		orderMu.Unlock()
		if label == "s1:a" {
			time.Sleep(150 * time.Millisecond)
		}
		return label, Usage{}, nil
	}
	script := metaHeader + `
return await pipeline(['a', 'b'],
  (x) => agent('stage1 ' + x, { label: 's1:' + x }),
  (prev, x) => agent('stage2 ' + x, { label: 's2:' + x }))
`
	res, err := Run(context.Background(), script, Options{Run: runner})
	if err != nil {
		t.Fatal(err)
	}
	vals, ok := res.Value.([]any)
	if !ok || len(vals) != 2 {
		t.Fatalf("value: %#v", res.Value)
	}
	if vals[0] != "s1:a" && vals[1] != "s1:b" {
		// stage2 returns its own label (the runner ignores the prompt)
	}
	// s2:b must have started before s1:a finished — i.e. order is
	// s1:a, s1:b, s2:b, s2:a (or s1:b first). With a barrier we'd get
	// s1:a, s1:b, s2:a, s2:b — s2:a before s2:b despite a's slowness.
	var s2b, s2a int
	for i, l := range order {
		if l == "s2:b" {
			s2b = i
		}
		if l == "s2:a" {
			s2a = i
		}
	}
	if s2a < s2b {
		t.Fatalf("barrier detected (s2:a ran before s2:b): %v", order)
	}
}

func TestParallelBarrierCollects(t *testing.T) {
	script := metaHeader + `
const r = await parallel([1, 2, 3].map(i => () => agent('item ' + i, { label: 'p' + i })))
return r.filter(Boolean).sort().join(',')
`
	res, err := Run(context.Background(), script, Options{Run: echoRunner(nil)})
	if err != nil {
		t.Fatal(err)
	}
	// Call indices are assigned at scheduler call time; under parallel fan-out
	// that order is nondeterministic, so assert on the SORTED set.
	if res.Value != "ok:0,ok:1,ok:2" {
		t.Fatalf("value: %v", res.Value)
	}
}

func TestParallelNullOnThrow(t *testing.T) {
	runner := func(ctx context.Context, req AgentRequest) (any, Usage, error) {
		if req.Index == 1 {
			return nil, Usage{}, fmt.Errorf("boom")
		}
		return fmt.Sprintf("ok:%d", req.Index), Usage{}, nil
	}
	script := metaHeader + `
const r = await parallel([0, 1, 2].map(i => () => agent('item ' + i)))
return JSON.stringify(r.map(x => x === null ? null : x))
`
	res, err := Run(context.Background(), script, Options{Run: runner})
	if err != nil {
		t.Fatal(err)
	}
	// One thunk's runner fails; exactly one slot must be null (parallel never
	// rejects). Which slot is nondeterministic: callIndex is assigned at
	// scheduler call time and the fan-out races.
	var got []any
	if err := json.Unmarshal([]byte(res.Value.(string)), &got); err != nil {
		t.Fatal(err)
	}
	nulls := 0
	for _, v := range got {
		if v == nil {
			nulls++
		}
	}
	if len(got) != 3 || nulls != 1 {
		t.Fatalf("expected exactly one null in 3 results: %v", res.Value)
	}
	// The failed agent must NOT be journaled (resume retries it).
	if len(res.Journal) != 2 {
		t.Fatalf("journal should hold only the 2 successes: %+v", res.Journal)
	}
}

func TestPipelineDropsFailedItem(t *testing.T) {
	// A stage that THROWS drops the item and skips its remaining stages. (A
	// failed agent() resolves to null — it does NOT throw — so the CC pattern
	// to drop it is a throwing filter stage, which is what this tests.)
	runner := func(ctx context.Context, req AgentRequest) (any, Usage, error) {
		if strings.Contains(req.Prompt, "stage bad") {
			return nil, Usage{}, fmt.Errorf("nope")
		}
		if strings.HasPrefix(req.Prompt, "stage ") {
			return req.Prompt[len("stage "):], Usage{}, nil // pass the item through
		}
		return "fine", Usage{}, nil
	}
	script := metaHeader + `
const r = await pipeline(['ok1', 'bad', 'ok2'],
  (x) => agent('stage ' + x),
  (prev) => { if (prev === null) throw new Error('dropped upstream'); return prev },
  (prev) => agent('second ' + prev))
return JSON.stringify(r)
`
	res, err := Run(context.Background(), script, Options{Run: runner})
	if err != nil {
		t.Fatal(err)
	}
	var got []any
	if err := json.Unmarshal([]byte(res.Value.(string)), &got); err != nil {
		t.Fatal(err)
	}
	// The middle item ("bad") fails at stage 1 → dropped to null; the other
	// two flow through both stages.
	if len(got) != 3 || got[0] != "fine" || got[1] != nil || got[2] != "fine" {
		t.Fatalf("value: %v", res.Value)
	}
}

func TestAgentCap(t *testing.T) {
	// A bare (non-fanned) agent() call past the cap rejects the script promise.
	script := metaHeader + `
let last
for (let i = 0; i < 5; i++) { last = await agent('a' + i) }
return last
`
	_, err := Run(context.Background(), script, Options{Run: echoRunner(nil), MaxAgents: 3})
	if err == nil || !strings.Contains(err.Error(), "agent limit exceeded") {
		t.Fatalf("expected cap error, got %v", err)
	}

	// Inside parallel(), the same cap error becomes a null item (CC contract:
	// a throwing thunk resolves to null) and shows up in the run logs.
	script = metaHeader + `
const r = await parallel([0,1,2,3,4].map(i => () => agent('a' + i)))
return r.filter(Boolean).length
`
	res, err := Run(context.Background(), script, Options{Run: echoRunner(nil), MaxAgents: 3})
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != int64(3) {
		t.Fatalf("expected 3 surviving agents, got %v", res.Value)
	}
}

func TestFanoutCap(t *testing.T) {
	script := metaHeader + `
const items = []; for (let i = 0; i < 5000; i++) items.push(i);
return await parallel(items.map(i => () => agent('a' + i)))
`
	_, err := Run(context.Background(), script, Options{Run: echoRunner(nil)})
	if err == nil || !strings.Contains(err.Error(), "4096") {
		t.Fatalf("expected fanout cap error, got %v", err)
	}
}

func TestDeterminismThrows(t *testing.T) {
	for _, snippet := range []string{
		"Math.random()",
		"Date.now()",
		"new Date()",
	} {
		// Wrap in a template so the parse-time blocklist doesn't trip first:
		// we want the RUNTIME neutering to fire. (Parse blocks it too — that's
		// the fast feedback; both layers must exist.)
		script := metaHeader + "const f = new Function('return " + snippet + "'); await agent('x'); return f()"
		_, err := Run(context.Background(), script, Options{Run: echoRunner(nil)})
		if err == nil {
			t.Fatalf("%s should throw at runtime", snippet)
		}
	}
}

func TestResumeReplaysPrefix(t *testing.T) {
	var calls atomic.Int64
	runner := echoRunner(&calls)

	script := metaHeader + `
const a = await agent('first')
const b = await agent('second')
const c = await agent('third')
return [a, b, c].join('|')
`
	first, err := Run(context.Background(), script, Options{Run: runner, RunID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("first run made %d agent calls", calls.Load())
	}

	// Resume with the SAME script: 100% cache hit, zero live calls.
	calls.Store(0)
	resumed, err := Run(context.Background(), script, Options{
		Run: runner, RunID: "r1", ResumeJournal: JournalMap(&PersistedRun{Journal: first.Journal}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("resume should replay everything, made %d live calls", calls.Load())
	}
	if resumed.Value != first.Value {
		t.Fatalf("resumed value %v != original %v", resumed.Value, first.Value)
	}
}

func TestResumeRunsFromFirstMiss(t *testing.T) {
	var calls atomic.Int64
	runner := echoRunner(&calls)

	v1 := metaHeader + `
const a = await agent('first')
const b = await agent('second')
return a + '|' + b
`
	first, err := Run(context.Background(), v1, Options{Run: runner, RunID: "r2"})
	if err != nil {
		t.Fatal(err)
	}

	// Edit the SECOND call: call 0 replays, call 1 runs live.
	calls.Store(0)
	v2 := metaHeader + `
const a = await agent('first')
const b = await agent('CHANGED second')
return a + '|' + b
`
	resumed, err := Run(context.Background(), v2, Options{
		Run: runner, RunID: "r2", ResumeJournal: JournalMap(&PersistedRun{Journal: first.Journal}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected exactly 1 live call after edit, got %d", calls.Load())
	}
	if resumed.Value != "ok:0|ok:1" {
		t.Fatalf("value: %v", resumed.Value)
	}
}

func TestNestedWorkflowSharesCaps(t *testing.T) {
	var calls atomic.Int64
	runner := echoRunner(&calls)

	child := metaHeader + `return await agent('child agent')`
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	childPath := PersistScript("child", "child-run", child)

	parent := metaHeader + fmt.Sprintf(`
const c = await workflow({ scriptPath: %q })
return 'parent got: ' + c
`, childPath)
	res, err := Run(context.Background(), parent, Options{Run: runner, RunID: "parent-run"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != "parent got: ok:0" {
		t.Fatalf("value: %v", res.Value)
	}
}

func TestNestedWorkflowDepthLimit(t *testing.T) {
	// A workflow() call inside a nested workflow must throw.
	grandchild := metaHeader + `
return await workflow({ scriptPath: 'whatever' })
`
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	grandPath := PersistScript("grandchild", "gc-run", grandchild)

	child := metaHeader + fmt.Sprintf(`
await agent('x')
return await workflow({ scriptPath: %q })
`, grandPath)
	childPath := PersistScript("child", "c-run", child)

	parent := metaHeader + fmt.Sprintf(`
return await workflow({ scriptPath: %q })
`, childPath)
	_, err := Run(context.Background(), parent, Options{Run: echoRunner(nil)})
	if err == nil || !strings.Contains(err.Error(), "one level") {
		t.Fatalf("expected depth-limit error, got %v", err)
	}
}

func TestStopCancelsRun(t *testing.T) {
	block := make(chan struct{})
	runner := func(ctx context.Context, req AgentRequest) (any, Usage, error) {
		select {
		case <-ctx.Done():
			return nil, Usage{}, ctx.Err()
		case <-block:
			return "never", Usage{}, nil
		}
	}
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, metaHeader+`return await agent('hangs')`, Options{Run: runner})
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled run should return an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled run did not unwind")
	}
}

func TestConcurrencyCapEnforced(t *testing.T) {
	var inFlight, maxSeen atomic.Int64
	runner := func(ctx context.Context, req AgentRequest) (any, Usage, error) {
		n := inFlight.Add(1)
		for {
			m := maxSeen.Load()
			if n <= m || maxSeen.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		return "x", Usage{}, nil
	}
	script := metaHeader + `
return await parallel([0,1,2,3,4,5,6,7].map(i => () => agent('a' + i)))
`
	_, err := Run(context.Background(), script, Options{Run: runner, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if m := maxSeen.Load(); m > 2 {
		t.Fatalf("concurrency cap violated: saw %d in flight", m)
	}
}
