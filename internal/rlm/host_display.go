package rlm

import (
	"context"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// PresentedHostCall enriches an execution observation for retained clients.
type PresentedHostCall struct {
	HostCall
	Display *llm.OperationDisplay
}

// PresentHostCalls snapshots bounded display fields before entering host code.
func PresentHostCalls(started, completed func(PresentedHostCall)) HostObserver {
	return func(call HostCall, arguments map[string]any) func(HostCall, any) {
		presented := PresentedHostCall{HostCall: call, Display: hostDisplay(call.Module, call.Operation, arguments)}
		if started != nil {
			started(presented)
		}
		if completed == nil {
			return nil
		}
		return func(call HostCall, result any) {
			presented.HostCall = call
			presented.Display = hostResultDisplay(presented, result)
			completed(presented)
		}
	}
}

// ToolHost forwards retained dispatcher operation identities to the kernel.
func ToolHost(host Host) Host {
	if host == nil {
		return nil
	}
	return HostFunc(func(ctx context.Context, module, operation string, arguments map[string]any) (any, error) {
		return host.Call(tools.WithOperationObserver(ctx, func(id string) { ReportHostOperation(ctx, id) }), module, operation, arguments)
	})
}

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

func hostResultDisplay(call PresentedHostCall, result any) *llm.OperationDisplay {
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
