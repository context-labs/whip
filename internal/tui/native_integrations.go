package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

func (m *nativeModel) lspCommand(args string) tea.Cmd {
	if args != "" && args != "status" && args != "list" {
		m.status = "usage: /lsp [status]"
		return nil
	}
	owner := m.owner.ID
	return m.control("Language servers", false, func(ctx context.Context) nativeControlResult {
		var result protocol.LanguageServersResult
		if err := m.connection.Call(ctx, "lsp.status", protocol.SessionParams{SessionID: owner}, &result); err != nil {
			return nativeControlResult{err: err}
		}
		lines := []string{"Language servers for " + string(owner) + " (status does not start servers)"}
		for _, row := range result.Items {
			line := row.Name + " · " + row.State
			if row.WorkspaceRoot != nil {
				line += " · " + *row.WorkspaceRoot
			}
			if row.Failure != nil {
				line += " · " + *row.Failure
			}
			lines = append(lines, line)
		}
		return nativeControlResult{notice: strings.Join(lines, "\n")}
	})
}

func (m *nativeModel) browserCommand(args string) tea.Cmd {
	fields := strings.Fields(args)
	if len(fields) > 0 && fields[0] == "external" {
		return m.externalBrowserCommand(strings.TrimSpace(strings.TrimPrefix(args, "external")))
	}
	if args == "tabs" {
		owner := m.owner.ID
		return m.control("Browser tabs", false, func(ctx context.Context) nativeControlResult {
			var value protocol.BrowserTabsResult
			if err := m.connection.Call(ctx, "browser.tabs", protocol.SessionParams{SessionID: owner}, &value); err != nil {
				return nativeControlResult{err: fmt.Errorf("browser inventory unavailable: %w", err)}
			}
			lines := []string{"Offered browser tabs for " + string(owner)}
			for _, tab := range value.Tabs {
				lines = append(lines, fmt.Sprintf("%s · %s · %s · %s", tab.TabID, tab.State, tab.Title, tab.URL))
			}
			return nativeControlResult{notice: strings.Join(lines, "\n")}
		})
	}
	if len(fields) == 0 || args == "status" {
		owner := m.owner.ID
		return m.control("Browser status", false, func(ctx context.Context) nativeControlResult {
			var driver protocol.HostBrowserDriver
			if err := m.connection.Call(ctx, "host.browser_driver", protocol.EmptyParams{}, &driver); err != nil {
				return nativeControlResult{err: err}
			}
			var attachments protocol.BrowserAttachmentsResult
			if err := m.connection.Call(ctx, "browser.attachments", protocol.SessionParams{SessionID: owner}, &attachments); err != nil {
				return nativeControlResult{err: err}
			}
			lines := []string{fmt.Sprintf("Browser driver: %s · configured %s · pinned %t", driver.Driver, driver.ConfiguredDriver, driver.Pinned)}
			for _, attachment := range attachments.Attachments {
				if attachment.AgentID != owner {
					return nativeControlResult{err: errors.New("browser attachment ownership mismatch")}
				}
				lines = append(lines, fmt.Sprintf("Attached %s · %s · %s", attachment.Scope.AttachmentID, attachment.Title, attachment.URL))
			}
			lines = append(lines, "Status does not contact a browser. /browser tabs explicitly reads offered tab inventory.")
			return nativeControlResult{notice: strings.Join(lines, "\n")}
		})
	}
	if len(fields) == 2 && fields[0] == "driver" {
		fields = fields[1:]
	}
	if len(fields) != 1 || fields[0] != "rod" && fields[0] != "chromedp" {
		m.status = "usage: /browser [status|tabs|driver rod|driver chromedp|external status|external list]"
		return nil
	}
	if m.owner.ParentID != nil {
		m.status = "Browser controls are read-only from a child. Select the root before changing host configuration."
		return nil
	}
	if !m.nativeAdmissionAvailable() {
		return nil
	}
	var params *protocol.SetBrowserDriverParams
	driver := fields[0]
	return m.control("Set browser driver", true, func(ctx context.Context) nativeControlResult {
		if params == nil {
			var current protocol.HostBrowserDriver
			if err := m.connection.Call(ctx, "host.browser_driver", protocol.EmptyParams{}, &current); err != nil {
				return nativeControlResult{err: err}
			}
			params = &protocol.SetBrowserDriverParams{ExpectedRevision: current.Revision, Driver: driver}
		}
		var value protocol.HostBrowserDriver
		err := m.connection.Call(ctx, "host.set_browser_driver", *params, &value)
		return nativeControlResult{notice: fmt.Sprintf("Browser driver: %s · configured %s · pinned %t", value.Driver, value.ConfiguredDriver, value.Pinned), err: err}
	})
}

func nativeComputerStatus(value protocol.ComputerStatus) string {
	return fmt.Sprintf("Computer: %s · enabled %t · helper configured %t · platform supported %t\nDefault deny: %t\nAllowed apps: %s\nDenied apps: %s\nStatus does not start the helper.", value.State, value.Configuration.Enabled, value.NativeConfigured, value.PlatformSupported, value.Configuration.DefaultDeny, strings.Join(value.Configuration.Allow, ", "), strings.Join(value.Configuration.Deny, ", "))
}

func (m *nativeModel) computerCommand(args string) tea.Cmd {
	fields := strings.Fields(args)
	if len(fields) == 0 || args == "status" {
		return m.control("Computer status", false, func(ctx context.Context) nativeControlResult {
			var value protocol.ComputerStatus
			err := m.connection.Call(ctx, "computer.status", protocol.EmptyParams{}, &value)
			return nativeControlResult{notice: nativeComputerStatus(value), err: err}
		})
	}
	if fields[0] != "allow" && fields[0] != "deny" {
		return m.prompt("The user asked for this task to be done with computer-use. Use the available computer module.\n\nTask: "+args, "auto")
	}
	if len(fields) < 2 {
		m.status = "usage: /computer [status|allow <app>|deny <app>|<task>]"
		return nil
	}
	if !m.nativeAdmissionAvailable() {
		return nil
	}
	app := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(args, fields[0])))
	allow := fields[0] == "allow"
	var params *protocol.ConfigureComputerParams
	return m.control("Update computer app policy", true, func(ctx context.Context) nativeControlResult {
		if params == nil {
			var current protocol.ComputerStatus
			if err := m.connection.Call(ctx, "computer.status", protocol.EmptyParams{}, &current); err != nil {
				return nativeControlResult{err: err}
			}
			policy := current.Configuration
			policy.Allow = slices.DeleteFunc(policy.Allow, func(value string) bool { return value == app })
			policy.Deny = slices.DeleteFunc(policy.Deny, func(value string) bool { return value == app })
			if allow {
				policy.Allow = append(policy.Allow, app)
			} else {
				policy.Deny = append(policy.Deny, app)
			}
			params = &protocol.ConfigureComputerParams{Revision: current.Revision, Configuration: policy}
		}
		var value protocol.ComputerStatus
		err := m.connection.Call(ctx, "computer.configure", *params, &value)
		return nativeControlResult{notice: nativeComputerStatus(value), err: err}
	})
}
