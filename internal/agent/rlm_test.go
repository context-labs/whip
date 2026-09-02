package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

func TestExclusiveToolSurfaceCannotBeWidenedByMCP(t *testing.T) {
	agent := New(llm.New("https://example.test", "key"), "model", 100, "system")
	runtimeTool := tools.Tool{Def: llm.NewTool("rlm_exec", "runtime", `{}`), Run: func(context.Context, json.RawMessage) (string, error) { return "", nil }}
	agent.SetExclusiveTool(runtimeTool, "rlm")
	agent.SetMCPTools([]tools.Tool{{Def: llm.NewTool("ambient", "bad", `{}`)}})
	all := agent.AllTools()
	if len(all) != 1 || all[0].Def.Function.Name != "rlm_exec" || agent.toolClientID != "rlm" {
		t.Fatalf("exclusive tools = %#v", all)
	}

	classic := New(llm.New("https://example.test", "key"), "model", 100, "system")
	if len(classic.AllTools()) < 2 {
		t.Fatal("Classic tool surface unexpectedly empty")
	}
}
