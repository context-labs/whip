package daemon

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// toolRunner hosts WHIP capabilities for protocol clients such as `whip mcp
// serve`. It is not a model session and deliberately has no agent loop.
type toolRunner struct {
	services *tools.Services
}

func NewToolRunner(services *tools.Services) Runner {
	return &toolRunner{services: services}
}

func (r *toolRunner) bind(root *Session) error {
	if r.services == nil {
		return errors.New("tool services are required")
	}
	return r.services.BindDispatcher(root.store, root.store.Workspaces(), root.store.Processes(), root.authority)
}

func (*toolRunner) Turn(context.Context, string, bool, func(), func(string)) (string, error) {
	return "", errors.New("tool host does not run model turns")
}

func (*toolRunner) Steer(string) bool      { return false }
func (*toolRunner) History() []llm.Message { return nil }
func (r *toolRunner) Close() {
	if r.services != nil {
		r.services.Close()
	}
}

func (r *toolRunner) ToolDefinitions(ctx context.Context) ([]llm.Tool, error) {
	return r.services.ToolDefinitions(ctx)
}

func (r *toolRunner) CallTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	return r.services.CallTool(ctx, name, arguments)
}

func (r *toolRunner) DenyToolPermissions() {
	r.services.SetExternalPermissions(false)
	r.services.SetGate(func(context.Context, tools.GateRequest) (tools.GateDecision, string) {
		return tools.GateReject, "this automation client cannot approve side effects"
	})
}

func (r *toolRunner) SetExternalPermissions(enabled bool) {
	r.services.SetExternalPermissions(enabled)
}

func (r *toolRunner) ExternalPermissionsEnabled() bool {
	return r.services.ExternalPermissionsEnabled()
}

func (r *toolRunner) ResolvePermission(permissionID string, decision capability.Decision) error {
	return r.services.ResolvePermission(permissionID, decision)
}
