package runtime

import (
	"context"
	"encoding/json"
	"slices"
	"sort"

	"github.com/context-labs/whip/internal/session"
)

type HostToolSchema struct {
	Module      string
	Name        string
	Description string
	InputSchema json.RawMessage
}

// HostToolSchemas is declaration metadata only. Listing does not acquire a
// resource, bind an executor, prepare an operation or grant permission.
func (r *Runtime) HostToolSchemas(ctx context.Context, id session.SessionID) ([]HostToolSchema, error) {
	owner, err := r.store.Session(ctx, id)
	if err != nil {
		return nil, err
	}
	result := []HostToolSchema{}
	for _, value := range directSchemas {
		if slices.Contains(owner.Config.Modules, value.Module) {
			value.InputSchema = append(json.RawMessage(nil), value.InputSchema...)
			result = append(result, value)
		}
	}
	for name, declaration := range owner.Config.Tools {
		result = append(result, HostToolSchema{Module: "tools", Name: name, Description: declaration.Description, InputSchema: append(json.RawMessage(nil), declaration.InputSchema...)})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Module != result[j].Module {
			return result[i].Module < result[j].Module
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

var directSchemas = []HostToolSchema{
	{"shell", "run", "Run a bounded shell command in this session workspace.", json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","minLength":1,"maxLength":65536},"timeout":{"type":"number","minimum":0.001,"maximum":120},"interactive":{"type":"boolean"}},"required":["command"],"additionalProperties":false}`)},
	{"files", "read", "Read a bounded range of lines in this workspace.", json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer","minimum":1,"maximum":262144},"limit":{"type":"integer","minimum":1,"maximum":262144}},"required":["path"],"additionalProperties":false}`)},
	{"files", "write", "Write bounded text in this workspace.", json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)},
	{"files", "patch", "Replace exact text in a workspace file.", json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old_text":{"type":"string","minLength":1},"new_text":{"type":"string"},"replace_all":{"type":"boolean"}},"required":["path","old_text","new_text"],"additionalProperties":false}`)},
	{"files", "list", "List bounded workspace paths without following symlinks.", json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":2000}},"additionalProperties":false}`)},
	{"files", "search", "Search bounded workspace files for literal text.", json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"query":{"type":"string","minLength":1,"maxLength":4096},"limit":{"type":"integer","minimum":1,"maximum":100}},"required":["query"],"additionalProperties":false}`)},
	{"files", "diagnostics", "Read language-server diagnostics for one captured file.", json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
}
