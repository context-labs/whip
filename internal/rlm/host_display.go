package rlm

import "github.com/context-labs/whip/internal/llm"

// Explicit identifying fields only. Bodies, prompts, code and arbitrary tool
// arguments stay in their existing authorized content path.
func hostDisplay(module, operation string, arguments map[string]any) *llm.OperationDisplay {
	value := func(key string, limit int) string {
		text, _ := arguments[key].(string)
		return llm.PresentationExcerpt(text, limit)
	}
	display := &llm.OperationDisplay{}
	switch module {
	case "files":
		path, _ := arguments["path"].(string)
		if len(path) <= 4096 {
			display.Target = path
		}
		display.Query = value("pattern", 512)
	case "shell":
		display.Command = value("command", 512)
	case "browser":
		display.Target, display.Query = value("url", 1024), value("query", 512)
	case "agents":
		if operation == "spawn" {
			display.Label = value("name", 160)
		}
	}
	if *display == (llm.OperationDisplay{}) {
		return nil
	}
	return display
}

func hostResultDisplay(call HostCall, result any) *llm.OperationDisplay {
	if call.Module != "agents" || call.Operation != "spawn" {
		return call.Display
	}
	value, ok := result.(map[string]any)
	if !ok {
		return call.Display
	}
	display := llm.OperationDisplay{}
	if call.Display != nil {
		display = *call.Display
	}
	if id, ok := value["id"].(string); ok && len(id) <= 256 {
		display.ChildID = id
	}
	return &display
}
