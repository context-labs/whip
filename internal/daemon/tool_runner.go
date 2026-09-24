package daemon

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	sessionstore "github.com/context-labs/whip/internal/session"
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
	r.services.SetMCPProvider(root.mcpProvider)
	r.services.SetDesktopBrowserProvider(root.desktopBrowserProvider)
	// Tool hosts own their content; never borrow a model root or provider identity.
	r.services.SetMCPAttachmentStore(func(ctx context.Context, mime string, data []byte) (string, error) {
		value, err := root.StoreContent(ctx, root.authority.AgentID, sessionstore.RuntimePayload{Data: data, MediaType: mime, Source: "MCP tool result"})
		if err != nil {
			return "", err
		}
		return value.ReferenceID, nil
	})
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

func (r *toolRunner) permissionServices() *tools.Services { return r.services }

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
	r.services.SetMCPAutomatic(!enabled)
	r.services.SetHeadlessPermissions(false)
}

func (r *toolRunner) ExternalPermissionsEnabled() bool {
	return r.services.ExternalPermissionsEnabled()
}

func (r *toolRunner) ResolvePermission(permissionID string, decision capability.Decision) error {
	return r.services.ResolvePermission(permissionID, decision)
}
