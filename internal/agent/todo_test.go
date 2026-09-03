package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

func callTodowrite(t *testing.T, a *Agent, todosJSON string) string {
	t.Helper()
	for _, tool := range a.Tools {
		if tool.Def.Function.Name == "todowrite" {
			out, err := tool.Run(t.Context(), json.RawMessage(todosJSON))
			if err != nil {
				return "Error: " + err.Error()
			}
			return out
		}
	}
	t.Fatal("todowrite tool not registered on agent")
	return ""
}

func TestTodowriteFullRewriteAndInjection(t *testing.T) {
	a := newTestAgent(nil, "m", 0, "sys")

	out := callTodowrite(t, a, `{"todos":[
		{"content":"read the code","status":"completed"},
		{"content":"add the tool","status":"in_progress"},
		{"content":"write tests","status":"pending"}]}`)
	if !strings.Contains(out, "3 item(s), 2 open") {
		t.Fatalf("unexpected result: %s", out)
	}

	block := a.todoBlock()
	if !strings.Contains(block, "add the tool") || !strings.Contains(block, "write tests") {
		t.Fatalf("open items missing from injection:\n%s", block)
	}
	if strings.Contains(block, "read the code") {
		t.Fatalf("completed item should not be injected:\n%s", block)
	}

	// Second full-list call replaces the first; completing everything empties the block.
	callTodowrite(t, a, `{"todos":[
		{"id":"t2","content":"add the tool","status":"completed"},
		{"id":"t3","content":"write tests","status":"completed"}]}`)
	if b := a.todoBlock(); b != "" {
		t.Fatalf("all-completed plan should inject nothing:\n%s", b)
	}
}

func TestTodowriteValidation(t *testing.T) {
	a := newTestAgent(nil, "m", 0, "sys")
	cases := []string{
		`{"todos":[{"content":"a","status":"in_progress"},{"content":"b","status":"in_progress"}]}`,           // two in_progress
		`{"todos":[{"content":"a","status":"bogus"}]}`,                                                        // bad status
		`{"todos":[{"content":"","status":"pending"}]}`,                                                       // empty content
		`{"todos":[{"id":"x","content":"a","status":"pending"},{"id":"x","content":"b","status":"pending"}]}`, // dup id
	}
	for _, c := range cases {
		if out := callTodowrite(t, a, c); !strings.HasPrefix(out, "Error:") {
			t.Fatalf("expected rejection, got %q for %s", out, c)
		}
	}
}

// An oversized list, malformed arguments, and the clear-the-plan call.
func TestTodowriteEdges(t *testing.T) {
	a := newTestAgent(nil, "m", 0, "sys")

	var big strings.Builder
	big.WriteString(`{"todos":[`)
	for i := range maxTodos + 1 {
		if i > 0 {
			big.WriteString(",")
		}
		big.WriteString(`{"content":"step","status":"pending"}`)
	}
	big.WriteString(`]}`)
	if out := callTodowrite(t, a, big.String()); !strings.Contains(out, "consolidate steps") {
		t.Errorf("oversized list: %q", out)
	}
	if out := callTodowrite(t, a, `{"todos":`); !strings.HasPrefix(out, "Error:") {
		t.Errorf("malformed args: %q", out)
	}

	callTodowrite(t, a, `{"todos":[{"content":"do it","status":"pending"}]}`)
	if out := callTodowrite(t, a, `{"todos":[]}`); out != "Plan cleared." {
		t.Errorf("clear: %q", out)
	}
	if a.todoBlock() != "" || a.TodosJSON() != "" {
		t.Error("cleared plan should inject and serialize nothing")
	}
}

// End-to-end: a plan set via the tool reaches the model request as an
// ephemeral system message each round, and a.Messages stays clean.
func TestTodowriteEndToEnd(t *testing.T) {
	var sawBlock bool
	srv := textServer(t, func(n int, req llm.Request) string {
		for _, m := range req.Messages {
			if m.Role == "system" && strings.Contains(m.Content, "Your current plan") {
				sawBlock = true
				if !strings.Contains(m.Content, "write tests") {
					t.Errorf("request plan block missing open item:\n%s", m.Content)
				}
				if strings.Contains(m.Content, "read the code") {
					t.Errorf("request plan block carried a completed item:\n%s", m.Content)
				}
			}
		}
		return "done"
	})
	defer srv.Close()

	a := newTestAgent(llm.New(srv.URL, "k"), "m", 100, "sys")
	callTodowrite(t, a, `{"todos":[
		{"content":"read the code","status":"completed"},
		{"content":"write tests","status":"in_progress"}]}`)
	if _, err := a.Turn(t.Context(), "go", Events{}); err != nil {
		t.Fatal(err)
	}
	if !sawBlock {
		t.Fatal("request never carried the plan block")
	}
	for _, m := range a.Messages {
		if strings.Contains(m.Content, "Your current plan") {
			t.Fatal("plan block must not persist into the conversation transcript")
		}
	}
}

func TestTodosPersistenceRoundTrip(t *testing.T) {
	a := newTestAgent(nil, "m", 0, "sys")
	callTodowrite(t, a, `{"todos":[{"content":"ship it","status":"in_progress"}]}`)

	saved := a.TodosJSON()
	if saved == "" {
		t.Fatal("TodosJSON should serialize a non-empty plan")
	}

	b := newTestAgent(nil, "m", 0, "sys")
	b.LoadTodosJSON(saved)
	if !strings.Contains(b.todoBlock(), "ship it") {
		t.Fatalf("restored plan should inject open item:\n%s", b.todoBlock())
	}

	// Corrupt/empty blobs load as an empty plan, never a crash.
	c := newTestAgent(nil, "m", 0, "sys")
	c.LoadTodosJSON("{not json")
	c.LoadTodosJSON("")
	if c.TodosJSON() != "" {
		t.Fatal("corrupt load should leave an empty plan")
	}
}
