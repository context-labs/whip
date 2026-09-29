package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

const nativeExternalBrowserHelp = `External Chrome is separate from offered Desktop tabs.
/browser external status · passive host configuration and driver pin
/browser external list · passive named connections owned by this session's root
/browser external configure <revision> <complete JSON configuration>
/browser external reconnect|disconnect <name> <displayed generation>
Configuration fields: mode (disabled/live/dedicated/headless/extension), executable (absolute host path for dedicated/headless), live_endpoint (literal loopback HTTP/WebSocket URL), live_profile (absolute host path), allow_private_urls (true/false). Live mode needs exactly one endpoint or profile. Include every field; unused source fields can be empty strings.
Only a selected root may change configuration or connections. Configuration and reconnect do not open Chrome, publish relay credentials or grant agent access. An agent operation needs its own permission; extension assets use whipcode browser install.
If an acknowledgement is lost, read status/list before another explicit edit; /retry never resends these controls.`

func (m *nativeModel) externalBrowserCommand(args string) tea.Cmd {
	fields := strings.Fields(args)
	if args == "" || args == "status" {
		child := m.owner.ParentID != nil
		return m.control("External Chrome status", false, func(ctx context.Context) nativeControlResult {
			var value protocol.ExternalBrowserStatus
			if err := m.connection.Call(ctx, "host.external_browser", protocol.EmptyParams{}, &value); err != nil {
				return nativeControlResult{err: err}
			}
			raw, err := json.Marshal(value.Configuration)
			notice := fmt.Sprintf("External Chrome: %s · driver %s · pinned %t\nHost revision: %s\nComplete declaration:\n/browser external configure %s %s\n", value.Configuration.Mode, value.Driver, value.DriverPinned, value.Revision, value.Revision, raw)
			if child {
				notice += "Selected child: read-only. Select the root for human controls.\n"
			}
			return nativeControlResult{notice: notice + nativeExternalBrowserHelp, err: err}
		})
	}
	if args == "list" {
		owner, child := m.owner.ID, m.owner.ParentID != nil
		return m.control("External Chrome connections", false, func(ctx context.Context) nativeControlResult {
			var value protocol.ExternalBrowserSessions
			if err := m.connection.Call(ctx, "browser.external_sessions", protocol.SessionParams{SessionID: owner}, &value); err != nil {
				return nativeControlResult{err: err}
			}
			lines := []string{fmt.Sprintf("External Chrome connections for %s (metadata only; child read-only: %t)", owner, child)}
			for _, row := range value.Items {
				lines = append(lines, fmt.Sprintf("%s · root %s · %s/%s · %s · generation %s", row.Name, row.RootID, row.Mode, row.Driver, row.State, row.Generation))
			}
			if len(value.Items) == 0 {
				lines = append(lines, "No prepared connections. An agent operation prepares the first named connection; browser access requires separate permission.")
			}
			return nativeControlResult{notice: strings.Join(lines, "\n") + "\n" + nativeExternalBrowserHelp}
		})
	}
	if m.owner.ParentID != nil {
		m.status = "External Chrome is read-only from a child. Select the root for configuration or connection controls."
		return nil
	}
	if !m.nativeAdmissionAvailable() {
		return nil
	}
	if len(fields) >= 2 && fields[0] == "configure" {
		if len(args) > 32<<10 {
			m.status = "External Chrome configuration exceeds 32 KiB."
			return nil
		}
		rest := strings.TrimSpace(strings.TrimPrefix(args, "configure"))
		revision := fields[1]
		raw := strings.TrimSpace(strings.TrimPrefix(rest, revision))
		encoded, err := json.Marshal(struct {
			ExpectedRevision string          `json:"expected_revision"`
			Configuration    json.RawMessage `json:"configuration"`
		}{revision, json.RawMessage(raw)})
		if err != nil {
			m.status = "Invalid external Chrome configuration: " + err.Error()
			return nil
		}
		if err := protocol.Validate("ConfigureExternalBrowserParams", encoded); err != nil {
			m.status = "Invalid complete external Chrome configuration: " + err.Error()
			return nil
		}
		var params protocol.ConfigureExternalBrowserParams
		if err := json.Unmarshal(encoded, &params); err != nil {
			m.status = "Invalid external Chrome configuration: " + err.Error()
			return nil
		}
		return m.control("Configure external Chrome", false, func(ctx context.Context) nativeControlResult {
			var value protocol.ExternalBrowserStatus
			if err := m.connection.Call(ctx, "host.set_external_browser", params, &value); err != nil {
				return nativeControlResult{mutation: true, err: fmt.Errorf("configuration not confirmed; /browser external status inspects current evidence before another explicit edit: %w", err)}
			}
			if value.Configuration != params.Configuration {
				return nativeControlResult{mutation: true, err: errors.New("external Chrome configuration acknowledgement mismatch; inspect /browser external status")}
			}
			return nativeControlResult{mutation: true, notice: fmt.Sprintf("External Chrome configuration saved at %s. No browser was opened or agent grant created.\n%s", value.Revision, nativeExternalBrowserHelp)}
		})
	}
	if len(fields) == 3 && (fields[0] == "reconnect" || fields[0] == "disconnect") {
		params := protocol.ExternalBrowserConnectionParams{RootID: m.owner.ID, Name: fields[1], Generation: protocol.ID(fields[2])}
		method := "browser." + fields[0] + "_external"
		return m.control("External Chrome connection action", false, func(ctx context.Context) nativeControlResult {
			var value protocol.ExternalBrowserSession
			if err := m.connection.Call(ctx, method, params, &value); err != nil {
				return nativeControlResult{mutation: true, err: fmt.Errorf("connection action not confirmed; /browser external list inspects generations before another explicit action: %w", err)}
			}
			if value.RootID != params.RootID || value.Name != params.Name {
				return nativeControlResult{mutation: true, err: errors.New("external Chrome connection acknowledgement ownership mismatch; inspect /browser external list")}
			}
			return nativeControlResult{mutation: true, notice: fmt.Sprintf("External Chrome %s: %s · generation %s. No browser was opened or operation replayed; agent access remains subject to permission checks for this generation.", value.Name, value.State, value.Generation)}
		})
	}
	m.status = "usage: /browser external status|list|configure <revision> <complete JSON>|reconnect|disconnect <name> <generation>"
	return nil
}
