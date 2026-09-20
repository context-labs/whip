package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
)

func TestDesktopArgumentBoundariesRejectBeforeProvider(t *testing.T) {
	s, ledger, provider, _ := desktopTestServices(t)
	for _, tc := range []struct{ operation, args string }{
		{"open", strings.Repeat(" ", (128<<10)+1)},
		{"open", `{"url":"https://example.com"} {}`},
		{"open", `null`},
		{"open", `{"url":"https://example.com","attachment_id":"other"}`},
		{"attach", `{"tab_id":"tab","preview_host_id":"other"}`},
		{"run", `{"attachment_id":"attachment","code":"` + strings.Repeat("x", (64<<10)+1) + `"}`},
		{"detach", `{"attachment_id":"attachment","code":"not allowed"}`},
		{"allow_preview_port", `{"attachment_id":"attachment","port":65536}`},
		{"allow_preview_port", `{"attachment_id":"attachment","port":0}`},
		{"attach", `{"tab_id":"tab","expected_document":"stale"}`},
		{"detach", `{"attachment_id":"attachment","port":1234}`},
		{"list_tabs", `{"tab_id":"tab"}`},
	} {
		t.Run(tc.operation+tc.args[:min(len(tc.args), 40)], func(t *testing.T) {
			result := callDesktop(t, s, tc.operation, tc.args)
			if result.Error == nil || result.Error.Kind != "invalid_arguments" {
				t.Fatalf("invalid input reached provider: %+v", result)
			}
		})
	}
	if provider.resolves.Load() != 0 || provider.executes.Load() != 0 || ledger.begins.Load() != 0 {
		t.Fatal("invalid arguments must not reserve a target or enter admission")
	}
	args, err := decodeDesktopArguments("browser.run", json.RawMessage(`{"attachment_id":"a","code":"1","timeout":999}`))
	if err != nil || args.Timeout != 120 {
		t.Fatalf("timeout must be bounded: %+v, %v", args, err)
	}
}

type failingDesktopProvider struct {
	browser.DesktopProvider
	resolveErr      error
	contextErr      error
	lifetime        context.Context
	invalidEnvelope bool
}

func (p failingDesktopProvider) Resolve(ctx context.Context, identity browser.DesktopIdentity, op string, args browser.DesktopArguments) (capability.BrowserCall, error) {
	if p.resolveErr != nil {
		return capability.BrowserCall{}, p.resolveErr
	}
	call, err := p.DesktopProvider.Resolve(ctx, identity, op, args)
	if p.invalidEnvelope {
		call.Arguments = json.RawMessage("not json")
	}
	return call, err
}

func (p failingDesktopProvider) CallContext(capability.BrowserCall) (context.Context, error) {
	return p.lifetime, p.contextErr
}

func TestDesktopProviderFailuresHaveNoNativeEffects(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		name     string
		provider failingDesktopProvider
		kind     string
	}{
		{"resolve", failingDesktopProvider{resolveErr: errors.New("resolution failed")}, "browser_failed"},
		{"context", failingDesktopProvider{contextErr: capability.ErrDenied}, "permission_denied"},
		{"missing lifetime", failingDesktopProvider{}, "attachment_revoked"},
		{"cancelled lifetime", failingDesktopProvider{lifetime: cancelled}, "cancelled"},
		{"invalid envelope", failingDesktopProvider{lifetime: t.Context(), invalidEnvelope: true}, "browser_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, ledger, original, _ := desktopTestServices(t)
			provider := tc.provider
			provider.DesktopProvider = original
			s.SetDesktopBrowserProvider(func() browser.DesktopProvider { return provider })
			result := callDesktop(t, s, "open", `{"url":"https://example.com"}`)
			if result.Error == nil || result.Error.Kind != tc.kind {
				t.Fatalf("result=%+v, want %s", result, tc.kind)
			}
			if original.executes.Load() != 0 || ledger.begins.Load() != 0 {
				t.Fatal("invalid provider reservation entered admission or native execution")
			}
		})
	}
}

func TestDesktopAttachmentHelpersDenyUnboundAndUnavailable(t *testing.T) {
	unbound := NewServices()
	if result := callDesktop(t, unbound, "run", `{"attachment_id":"copied","code":"1"}`); result.Error == nil || result.Error.Kind != "permission_denied" {
		t.Fatalf("unbound service must not authorize copied attachment: %+v", result)
	}
	if _, err := unbound.TransferDesktopAttachments(t.Context(), "child", []string{"copied"}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("unbound transfer: %v", err)
	}
	if result := callDesktop(t, unbound, "unknown", `{}`); result.Error == nil || result.Error.Kind != "unsupported_operation" {
		t.Fatalf("unknown operation: %+v", result)
	}
	s, ledger, provider, authority := desktopTestServices(t)
	s.SetDesktopBrowserProvider(nil)
	if attachments := s.DesktopAttachments(t.Context()); len(attachments) != 0 {
		t.Fatalf("unavailable provider returned attachments: %+v", attachments)
	}
	if _, err := s.TransferDesktopAttachments(t.Context(), "child", []string{"copied"}); err == nil {
		t.Fatal("unavailable provider must not transfer attachments")
	}
	authority.AgentID = "unrelated-child"
	if err := s.BindDispatcher(ledger, provider.store.Workspaces(), provider.store.Processes(), authority); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransferDesktopAttachments(t.Context(), "child", []string{"copied"}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("ungranted child transfer: %v", err)
	}
}
