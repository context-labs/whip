package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/browser"
)

const maxSpawnBrowserAttachments = 4

func (host *recursiveHost) browser(ctx context.Context, operation string, arguments map[string]any) (any, error) {
	_, attachment := arguments["attachment_id"]
	_, session := arguments["session"]
	if session && attachment {
		return nil, errors.New("browser.run accepts either session or attachment_id, never both")
	}
	if operation == "run" && !attachment {
		return host.invoke(ctx, "browser_exec", arguments)
	}
	switch operation {
	case "list_tabs", "open", "attach", "run", "detach", "allow_preview_port":
	default:
		return nil, fmt.Errorf("unknown browser operation %q", operation)
	}
	if session {
		return nil, errors.New("session is only valid for legacy browser.run")
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		return nil, err
	}
	return host.session.agent.Services.RunDesktopBrowser(ctx, "browser."+operation, raw)
}

func validateSpawnBrowserAttachments(parent *AgentSession, definition agentdef.Definition, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > maxSpawnBrowserAttachments {
		return errors.New("browser_attachments accepts at most four attachment IDs")
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || strings.TrimSpace(id) != id || len(id) > 256 || seen[id] {
			return errors.New("browser_attachments requires unique, nonempty opaque attachment IDs")
		}
		seen[id] = true
	}
	if !slices.Contains(parent.effectiveDefinition().Modules, "browser") ||
		!slices.Contains(parent.capabilities, "browser") ||
		!slices.Contains(definition.Modules, "browser") || !slices.Contains(definition.Capabilities, "browser") {
		return errors.New("browser attachment transfer requires the browser module and capability on both agents")
	}
	return nil
}

// Attachment metadata is context, never page content or a transferable authority.
func boundedDesktopAttachments(results []browser.DesktopResult) []browser.DesktopResult {
	if len(results) > maxSpawnBrowserAttachments {
		results = results[:maxSpawnBrowserAttachments]
	}
	bounded := make([]browser.DesktopResult, 0, len(results))
	for _, result := range results {
		operations := make([]string, 0, min(len(result.SupportedOperations), 16))
		for _, operation := range result.SupportedOperations[:min(len(result.SupportedOperations), 16)] {
			operations = append(operations, utf8PrefixRuntime(operation, 64))
		}
		bounded = append(bounded, browser.DesktopResult{
			AttachmentID:     utf8PrefixRuntime(result.AttachmentID, 256),
			TabID:            utf8PrefixRuntime(result.TabID, 256),
			DocumentRevision: utf8PrefixRuntime(result.DocumentRevision, 256),
			URL:              utf8PrefixRuntime(result.URL, 2048), Title: utf8PrefixRuntime(result.Title, 512),
			Network: browser.DesktopNetwork{
				Kind: utf8PrefixRuntime(result.Network.Kind, 32), HostID: utf8PrefixRuntime(result.Network.HostID, 256),
				Ports: slices.Clone(result.Network.Ports[:min(len(result.Network.Ports), 16)]),
			},
			SupportedOperations: operations,
		})
	}
	return bounded
}

func (node *AgentSession) desktopAttachments(ctx context.Context) []browser.DesktopResult {
	if node.agent == nil || node.agent.Services == nil || !slices.Contains(node.capabilities, "browser") {
		return nil
	}
	return boundedDesktopAttachments(node.agent.Services.DesktopAttachments(ctx))
}

func (node *AgentSession) revokeDesktopAttachments() {
	if node.agent == nil || node.agent.Services == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := node.agent.Services.RevokeDesktopAttachments(ctx); err != nil && node.root != nil {
		node.root.supervisor.report("revoke browser attachments", err)
	}
}
