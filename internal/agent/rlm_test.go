package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

func TestExclusiveToolSurfaceCannotBeWidenedByMCP(t *testing.T) {
	agent := NewRuntime(llm.New("https://example.test", "key"), "model", 100, "system", tools.NewServices())
	if len(agent.AllTools()) != 0 {
		t.Fatal("runtime constructor chose a model-facing tool surface")
	}
	runtimeTool := tools.Tool{Def: llm.NewTool("rlm_exec", "runtime", `{}`), Run: func(context.Context, json.RawMessage) (string, error) { return "", nil }}
	agent.SetExclusiveTool(runtimeTool, "rlm")
	agent.SetMCPTools([]tools.Tool{{Def: llm.NewTool("ambient", "bad", `{}`)}})
	all := agent.AllTools()
	if len(all) != 1 || all[0].Def.Function.Name != "rlm_exec" || agent.toolClientID != "rlm" {
		t.Fatalf("exclusive tools = %#v", all)
	}

}
