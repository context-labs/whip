package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
)

type desktopBrowserAuthorizer interface {
	AuthorizeCapability(context.Context, string, string, capability.Reference, string, string) error
	AuthorizeBrowser(context.Context, string, string, capability.Reference, string, capability.BrowserScope) error
}

func (s *Services) SetDesktopBrowserProvider(resolve func() browser.DesktopProvider) {
	s.mu.Lock()
	s.desktopBrowserProvider = resolve
	s.mu.Unlock()
}

func (s *Services) desktopProvider() (browser.DesktopProvider, error) {
	s.mu.RLock()
	resolve := s.desktopBrowserProvider
	s.mu.RUnlock()
	if resolve != nil {
		if provider := resolve(); provider != nil {
			return provider, nil
		}
	}
	return nil, &browser.DesktopError{Kind: "desktop_unavailable", Message: "select a desktop browser provider for this conversation"}
}

func desktopOperation(operation string) string {
	if strings.HasPrefix(operation, "browser.") {
		return operation
	}
	return "browser." + operation
}

func isDesktopBrowserOperation(operation string) bool {
	switch operation {
	case "browser.list_tabs", "browser.open", "browser.attach", "browser.run", "browser.detach", "browser.allow_preview_port":
		return true
	}
	return false
}

func desktopNeedsConsent(operation string) bool {
	return operation == "browser.open" || operation == "browser.attach" || operation == "browser.allow_preview_port"
}

func desktopFailure(result browser.DesktopResult, err error) browser.DesktopResult {
	if result.Network.Ports == nil {
		result.Network.Ports = []int{}
	}
	if result.SupportedOperations == nil {
		result.SupportedOperations = []string{}
	}
	if result.Media == nil {
		result.Media = []string{}
	}
	if err == nil {
		return result
	}
	var known *browser.DesktopError
	if errors.As(err, &known) {
		result.Error = known
		return result
	}
	kind := "browser_failed"
	if errors.Is(err, capability.ErrDenied) {
		kind = "permission_denied"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		kind = "cancelled"
	}
	result.Error = &browser.DesktopError{Kind: kind, Message: err.Error()}
	return result
}

func (s *Services) desktopIdentity() (browser.DesktopIdentity, capability.Authority, capability.Ledger) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return browser.DesktopIdentity{RootID: s.authority.RootID, AgentID: s.authority.AgentID}, s.authority, s.permissionLedger
}

// RunDesktopBrowser resolves offered resources before digest/admission. It never
// consults the legacy Manager, even when the selected desktop is unavailable.
func (s *Services) RunDesktopBrowser(ctx context.Context, operation string, arguments json.RawMessage) (browser.DesktopResult, error) {
	operation = desktopOperation(operation)
	fail := func(err error) (browser.DesktopResult, error) {
		return desktopFailure(browser.DesktopResult{}, err), nil
	}
	if !isDesktopBrowserOperation(operation) {
		return fail(&browser.DesktopError{Kind: "unsupported_operation", Message: "unknown desktop browser operation"})
	}
	args, err := decodeDesktopArguments(operation, arguments)
	if err != nil {
		return fail(&browser.DesktopError{Kind: "invalid_arguments", Message: err.Error()})
	}
	identity, authority, ledger := s.desktopIdentity()
	authorizer, ok := ledger.(desktopBrowserAuthorizer)
	if !ok {
		return fail(capability.ErrDenied)
	}
	// Module admission remains necessary even with a copied/live resource ID.
	if err := authorizer.AuthorizeCapability(ctx, identity.RootID, identity.AgentID, authority.Shell, operation, ""); err != nil {
		return fail(err)
	}
	provider, err := s.desktopProvider()
	if err != nil {
		return fail(err)
	}
	if operation == "browser.list_tabs" {
		inventory, ok := provider.(browser.DesktopInventoryProvider)
		if !ok {
			return fail(&browser.DesktopError{Kind: "unsupported_operation", Message: "Desktop provider does not support tab discovery"})
		}
		result, err := inventory.ListTabs(ctx, identity)
		return desktopFailure(result, err), nil
	}
	// Each inert reservation belongs to this invocation, including denied prompts.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	call, err := provider.Resolve(ctx, identity, operation, args)
	if err != nil {
		return fail(err)
	}
	lifetime, err := provider.CallContext(call)
	if err != nil {
		return fail(err)
	}
	if lifetime == nil {
		return fail(&browser.DesktopError{Kind: "attachment_revoked", Message: "provider returned no attachment lifetime"})
	}
	stop := context.AfterFunc(lifetime, cancel)
	defer stop()
	if err := lifetime.Err(); err != nil {
		return fail(err)
	}
	envelope, err := json.Marshal(call)
	if err != nil {
		return fail(err)
	}
	output, err := s.run(ctx, operation, envelope)
	var result browser.DesktopResult
	if output != "" {
		if decodeErr := json.Unmarshal([]byte(output), &result); decodeErr != nil {
			return fail(decodeErr)
		}
	}
	return desktopFailure(result, err), nil
}

func decodeDesktopArguments(operation string, data json.RawMessage) (browser.DesktopArguments, error) {
	var args browser.DesktopArguments
	if len(data) > 128<<10 {
		return args, errors.New("browser arguments exceed limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return args, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return args, errors.New("browser arguments require one object")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return args, errors.New("browser arguments require an object")
	}
	switch operation {
	case "browser.list_tabs":
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 0 {
			return args, errors.New("list_tabs accepts no arguments")
		}
	case "browser.open":
		if args.URL == "" || args.TabID != "" || args.AttachmentID != "" || args.Code != "" {
			return args, errors.New("open requires url and optional preview_host_id only")
		}
	case "browser.attach":
		if args.TabID == "" || args.URL != "" || args.AttachmentID != "" || args.Code != "" || args.PreviewHostID != "" {
			return args, errors.New("attach requires tab_id only")
		}
	case "browser.run":
		if args.AttachmentID == "" || strings.TrimSpace(args.Code) == "" || len(args.Code) > 64<<10 || args.TabID != "" || args.URL != "" || args.PreviewHostID != "" || args.Port != 0 {
			return args, errors.New("run requires attachment_id and bounded code; session and desktop targets are mutually exclusive")
		}
		if args.Timeout <= 0 {
			args.Timeout = 60
		}
		if args.Timeout > 120 {
			args.Timeout = 120
		}
	case "browser.detach", "browser.allow_preview_port":
		if args.AttachmentID == "" || args.URL != "" || args.TabID != "" || args.Code != "" || args.PreviewHostID != "" {
			return args, errors.New("operation requires attachment_id")
		}
		if operation == "browser.allow_preview_port" && (args.Port < 1 || args.Port > 65535) {
			return args, errors.New("port must be between 1 and 65535")
		}
	}
	if operation != "browser.run" && (args.Timeout != 0 || args.ExpectedDocument != "") {
		return args, errors.New("timeout and expected_document apply to run only")
	}
	if operation != "browser.allow_preview_port" && args.Port != 0 {
		return args, errors.New("port applies to allow_preview_port only")
	}
	return args, nil
}

func (s *Services) desktopBrowserRegistrations(ledger capability.Ledger) []capability.Registration {
	registrations := make([]capability.Registration, 0, 5)
	for _, operation := range []string{"browser.open", "browser.attach", "browser.run", "browser.detach", "browser.allow_preview_port"} {
		registrations = append(registrations, capability.Registration{
			Operation: operation, Mutation: capability.MutationNone, Permission: desktopNeedsConsent(operation),
			Handler: func(ctx context.Context, dispatched capability.Call) (string, error) {
				identity, authority, _ := s.desktopIdentity()
				authorizer, ok := ledger.(desktopBrowserAuthorizer)
				if !ok {
					return "", capability.ErrDenied
				}
				if err := authorizer.AuthorizeCapability(ctx, identity.RootID, identity.AgentID, authority.Shell, operation, ""); err != nil {
					return "", err
				}
				var call capability.BrowserCall
				if err := json.Unmarshal(dispatched.Arguments, &call); err != nil {
					return "", err
				}
				if operation != "browser.open" && operation != "browser.attach" {
					if err := authorizer.AuthorizeBrowser(ctx, identity.RootID, identity.AgentID, call.Grant, operation, call.Scope); err != nil {
						return "", err
					}
				}
				s.mu.RLock()
				headless, sink, store := s.headlessPermissions, s.screenshotSink, s.mcpAttachmentStore
				s.mu.RUnlock()
				if headless && desktopNeedsConsent(operation) {
					return "", &capability.PermissionDeniedError{Reason: "headless execution cannot request browser consent"}
				}
				provider, err := s.desktopProvider()
				if err != nil {
					return "", err
				}
				args, err := decodeDesktopArguments(operation, call.Arguments)
				if err != nil {
					return "", err
				}
				var run func(context.Context, browser.Backend) (string, error)
				var media []string
				if operation == "browser.run" {
					run = func(ctx context.Context, backend browser.Backend) (string, error) {
						ctx, cancel := context.WithTimeout(ctx, secondsDuration(args.Timeout))
						defer cancel()
						var mediaErr error
						output, runErr := runBrowserCode(ctx, backend, args.Code, call.Scope.AttachmentID, true, func(shots [][]byte) {
							if sink != nil {
								sink(shots)
							}
							if store != nil {
								for _, shot := range shots {
									handle, err := store(ctx, "image/jpeg", shot)
									if err != nil {
										mediaErr = err
										return
									}
									media = append(media, handle)
								}
							}
						})
						return output, errors.Join(runErr, mediaErr)
					}
				}
				result, err := provider.Execute(ctx, browser.DesktopRequest{Identity: identity, OperationID: dispatched.Request.OperationID, Operation: operation, Call: call}, run)
				result.Media = append(result.Media, media...)
				result = desktopFailure(result, err)
				data, marshalErr := json.Marshal(result)
				return string(data), marshalErr
			},
		})
	}
	return registrations
}

func (s *Services) decideDesktopBrowser(ctx context.Context, prompt capability.PermissionPrompt) (capability.Decision, error) {
	s.mu.RLock()
	headless, automatic, external, gate := s.headlessPermissions, s.mcpAutomatic, s.externalPermissions, s.gate
	s.mu.RUnlock()
	if headless {
		return capability.Decision{PrincipalID: "headless", Reason: "headless execution cannot request browser consent"}, nil
	}
	// Automatic is an existing permission policy, not a resource selector.
	if automatic && gate == nil {
		return capability.Decision{Allow: true, PrincipalID: "automatic"}, nil
	}
	if external {
		return s.waitPermission(ctx, prompt.ID)
	}
	if gate == nil {
		return capability.Decision{PrincipalID: "unpaired", Reason: "browser consent requires an explicit approval UI"}, nil
	}
	command, _, _ := capability.PermissionRule(prompt.Operation, prompt.Arguments, "")
	decision, reason := gate(ctx, GateRequest{Tool: prompt.Operation, Command: command})
	return capability.Decision{Allow: decision == GateAllowOnce, PrincipalID: "local-human", Reason: reason}, nil
}

func (s *Services) TransferDesktopAttachments(ctx context.Context, childAgentID string, ids []string) ([]browser.DesktopResult, error) {
	identity, authority, ledger := s.desktopIdentity()
	authorizer, ok := ledger.(desktopBrowserAuthorizer)
	if !ok {
		return nil, capability.ErrDenied
	}
	if err := authorizer.AuthorizeCapability(ctx, identity.RootID, identity.AgentID, authority.Shell, "browser.run", ""); err != nil {
		return nil, err
	}
	provider, err := s.desktopProvider()
	if err != nil {
		return nil, err
	}
	return provider.Transfer(ctx, identity, browser.DesktopIdentity{RootID: identity.RootID, AgentID: childAgentID}, ids)
}

func (s *Services) DesktopAttachments(ctx context.Context) []browser.DesktopResult {
	identity, _, _ := s.desktopIdentity()
	provider, err := s.desktopProvider()
	if err != nil {
		return nil
	}
	return provider.Attachments(ctx, identity)
}

func (s *Services) RevokeDesktopAttachments(ctx context.Context) error {
	identity, _, _ := s.desktopIdentity()
	provider, err := s.desktopProvider()
	if err != nil {
		return nil
	}
	return provider.RevokeAgent(ctx, identity)
}

var desktopToolOperations = map[string]string{"browser_list_tabs": "browser.list_tabs", "browser_open": "browser.open", "browser_attach": "browser.attach", "browser_run": "browser.run", "browser_detach": "browser.detach", "browser_allow_preview_port": "browser.allow_preview_port"}

func desktopToolDefinitions() []llm.Tool {
	var defs []llm.Tool
	for _, name := range []string{"browser_list_tabs", "browser_open", "browser_attach", "browser_run", "browser_detach", "browser_allow_preview_port"} {
		var schema string
		switch name {
		case "browser_list_tabs":
			schema = `{"type":"object","properties":{},"additionalProperties":false}`
		case "browser_open":
			schema = `{"type":"object","properties":{"url":{"type":"string"},"preview_host_id":{"type":"string"}},"required":["url"],"additionalProperties":false}`
		case "browser_attach":
			schema = `{"type":"object","properties":{"tab_id":{"type":"string"}},"required":["tab_id"],"additionalProperties":false}`
		case "browser_run":
			schema = `{"type":"object","properties":{"attachment_id":{"type":"string"},"code":{"type":"string"},"expected_document":{"type":"string"},"timeout":{"type":"number"}},"required":["attachment_id","code"],"additionalProperties":false}`
		case "browser_detach":
			schema = `{"type":"object","properties":{"attachment_id":{"type":"string"}},"required":["attachment_id"],"additionalProperties":false}`
		case "browser_allow_preview_port":
			schema = `{"type":"object","properties":{"attachment_id":{"type":"string"},"port":{"type":"integer","minimum":1,"maximum":65535}},"required":["attachment_id","port"],"additionalProperties":false}`
		}
		defs = append(defs, llm.NewTool(name, fmt.Sprintf("%s on the explicitly selected desktop browser. Requires a paired provider and scoped permission; never falls back to another browser. Returns structured result/error.", desktopToolOperations[name]), schema))
	}
	return defs
}
