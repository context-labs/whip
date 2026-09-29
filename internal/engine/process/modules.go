package process

import (
	"context"
	"fmt"
	"regexp"
	"slices"

	"github.com/context-labs/whip/internal/hostmodule"
)

// Host is the only authority visible to a kernel worker. Implementations live
// in the daemon process and route calls through daemon APIs or the shared
// capability dispatcher.
type Host interface {
	Call(context.Context, string, string, map[string]any) (any, error)
}

type HostFunc func(context.Context, string, string, map[string]any) (any, error)

func (f HostFunc) Call(ctx context.Context, module, operation string, args map[string]any) (any, error) {
	return f(ctx, module, operation, args)
}

var moduleRegistry = hostmodule.Operations()

func Modules() map[string][]string { return hostmodule.Operations() }

// ToolsModule is the reserved module name for an agent definition's custom
// tools. It is never in the registry: its operations are the tool names the
// kernel was started with, so the model sees exactly the tools it may call.
const ToolsModule = "tools"

var toolName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// validateHostOperation accepts registry operations and, for the reserved tools
// module, the kernel's own tool names and nothing else.
func validateHostOperation(module, operation string, tools []string) error {
	if module == ToolsModule {
		if slices.Contains(tools, operation) {
			return nil
		}
		return fmt.Errorf("unknown RLM operation %s.%s", module, operation)
	}
	return validateModuleOperation(module, operation)
}

// validateTools checks a kernel's tool names: valid identifiers, no duplicates.
func validateTools(tools []string) error {
	for i, name := range tools {
		if !toolName.MatchString(name) {
			return fmt.Errorf("invalid RLM tool name %q", name)
		}
		if slices.Contains(tools[:i], name) {
			return fmt.Errorf("duplicate RLM tool %q", name)
		}
	}
	return nil
}

func validateModuleOperation(module, operation string) error {
	operations, ok := moduleRegistry[module]
	if !ok {
		return fmt.Errorf("unknown RLM module %q", module)
	}
	if slices.Contains(operations, operation) {
		return nil
	}
	return fmt.Errorf("unknown RLM operation %s.%s", module, operation)
}
