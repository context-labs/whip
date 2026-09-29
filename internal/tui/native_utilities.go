package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/context-labs/whip/internal/protocol"
)

const nativeHelp = `Native terminal commands

Conversation: /sessions · /resume <owner> · /rename <title> · /status
History: /older · /newer · /latest · /export [local path]
Edits: /stop · /start · /clear · /rewind <sequence> · /fork <title> · /fork-at <sequence> <title>
Input: /queue <text> · /steer <text> · !<shell command>
Images: /attach <client-local path> · /attach clipboard|check|retry|discard · Ctrl+V reads a clipboard image
Recovery: /check · /retry · /rejected restore|discard
Configuration: /model · /model-for-session · /effort [level|default] · /setup · /settings · /theme
Context: /context-doctor [attempt ID] · /compact · /compact log|retry|off|model <model>|provider <provider>
Goals: /goal [text|status|resume|clear] · /goal-from-context [2..100]
Schedules: /schedule list [cursor] · /schedule @every <duration> <text> · /schedule @at <RFC3339 time> <text> · /schedule cancel <ID>
Instructions: /me · /memory · /permissions
Integrations: /lsp · /mcp · /browser · /computer · /pwd · /cd <path>
Agents: /agents [list|open <ID>|stop <ID>|delete <child ID>|revoke <grant ID>]
Display: /sidebar · /dock · /repl [older|latest|turn <ID>|focus] · /tools expand|collapse · /reasoning on|off · /report
Exit: /quit (accepted host work continues)

Enter sends; Ctrl+J/Shift+Enter inserts a newline; Escape cancels the exact active input.
Ctrl+T focuses the agent tree; arrows select, Enter opens, Escape returns to its root.
Ctrl+R toggles REPL; PageUp/PageDown scroll the focused pane; Ctrl+C twice exits.
Ctrl+P opens commands; Ctrl+O toggles live reasoning. Tab completes commands, @host files, $authorized skills, or host paths.
Ctrl+X then M/L/N/B/R/T/C/G/S opens model/sessions/clear/sidebar/REPL/theme/compact/rewind or stops a selected child.
Paste collapse is opt-in in /settings; original text is restored before sending.
Commands act on the displayed owner. Export writes a private local file. Direct shell uses the host's normal permission and receipt path.`

func (m *nativeModel) directShell(command string) tea.Cmd {
	if len(m.liveImages(command)) > 0 || m.attachmentBusy || m.attachment != nil {
		m.status = "Direct shell does not accept image attachments; remove the chips or submit them as a prompt."
		return nil
	}
	command, err := m.expandPastes(command)
	if err != nil {
		m.status = err.Error()
		return nil
	}
	if !m.nativeAdmissionAvailable() {
		return nil
	}
	if strings.TrimSpace(command) == "" {
		m.status = "usage: !<shell command>"
		return nil
	}
	arguments, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		m.status = err.Error()
		return nil
	}
	input, err := m.connection.PrepareInput("tool.call", protocol.CallHostToolParams{
		Identity:  protocol.RequestIdentity{ClientID: "tui", RequestID: protocol.ID(uuid.NewString())},
		SessionID: m.owner.ID,
		Operation: protocol.DirectHostInput{Module: "shell", Name: "run", ArgumentsBase64: base64.StdEncoding.EncodeToString(arguments)},
	})
	if err != nil {
		m.status = err.Error()
		return nil
	}
	return m.submitPreparedInput(input)
}

func (m *nativeModel) effortCommand(args string) tea.Cmd {
	owner, connection := m.owner, m.connection
	if args == "" {
		return m.control("Reasoning effort", false, func(ctx context.Context) nativeControlResult {
			var catalog protocol.ProviderCatalog
			err := connection.Call(ctx, "providers.catalog", protocol.ProviderParams{Provider: owner.Configuration.Model.Provider}, &catalog)
			if err == nil && catalog.Provider != owner.Configuration.Model.Provider {
				err = errors.New("effort catalog provider mismatch")
			}
			text := "Current effort: " + owner.Configuration.Model.Effort + ". /effort default uses the provider default."
			levels := ""
			for _, model := range catalog.Models {
				if model.ID == owner.Configuration.Model.Name {
					levels = strings.Join(model.ReasoningEfforts, ", ")
					break
				}
			}
			text += "\nAdvertised levels: " + levels
			return nativeControlResult{notice: text, err: err}
		})
	}
	if !m.nativeAdmissionAvailable() {
		return nil
	}
	if len(strings.Fields(args)) != 1 {
		m.status = "usage: /effort <advertised level|off|default>"
		return nil
	}
	selection := owner.Configuration.Model
	selection.Effort = args
	if args == "default" {
		selection.Effort = ""
	}
	params := protocol.UpdateConfigurationParams{SessionID: owner.ID, ExpectedRevision: owner.ConfigRevision, Patch: protocol.ConfigPatch{Model: &selection}}
	return m.control("Set reasoning effort", true, func(ctx context.Context) nativeControlResult {
		if args != "default" && args != "off" {
			var catalog protocol.ProviderCatalog
			if err := connection.Call(ctx, "providers.catalog", protocol.ProviderParams{Provider: selection.Provider}, &catalog); err != nil {
				return nativeControlResult{err: err}
			}
			if catalog.Provider != selection.Provider {
				return nativeControlResult{err: errors.New("effort catalog provider mismatch")}
			}
			advertised := false
			for _, model := range catalog.Models {
				advertised = advertised || model.ID == selection.Name && slices.Contains(model.ReasoningEfforts, args)
			}
			if !advertised {
				return nativeControlResult{err: errors.New("effort is not advertised for the selected model; inspect /effort or choose /model")}
			}
		}
		var changed protocol.Session
		err := connection.Call(ctx, "sessions.configure", params, &changed)
		if err == nil && (changed.ID != owner.ID || !reflect.DeepEqual(changed.Configuration.Model, selection)) {
			err = errors.New("model configuration ownership or selection mismatch")
		}
		return nativeControlResult{label: "Reasoning effort: " + args, owner: &changed, err: err}
	})
}

func (m *nativeModel) contextDoctor(args string) tea.Cmd {
	if len(strings.Fields(args)) > 1 {
		m.status = "usage: /context-doctor [attempt ID]"
		return nil
	}
	handle, connection := m.handle, m.connection
	return m.control("Captured model context", false, func(ctx context.Context) nativeControlResult {
		usage, err := handle.ContextUsage(ctx)
		if err != nil {
			return nativeControlResult{err: err}
		}
		text := nativeContextLabel(usage) + "\nInspection reads captured evidence; it does not prepare a provider request or estimate fresh injections."
		attempt := protocol.ID(args)
		if attempt == "" && usage.Prefill != nil {
			attempt = usage.Prefill.AttemptID
		}
		if attempt == "" {
			return nativeControlResult{notice: text + "\nNo captured attempt: " + usage.UnavailableReason}
		}
		var inspection protocol.ModelInspection
		err = connection.Call(ctx, "models.inspection", protocol.ModelInspectionParams{SessionID: handle.ID(), AttemptID: attempt}, &inspection)
		if err != nil {
			return nativeControlResult{err: err}
		}
		if inspection.SessionID != handle.ID() || inspection.AttemptID != attempt {
			return nativeControlResult{err: errors.New("captured model context ownership mismatch")}
		}
		text += fmt.Sprintf("\nAttempt %s · turn %s\nRequest digest: %s", attempt, inspection.TurnID, inspection.RequestDigest)
		if capture := inspection.Capture; capture != nil {
			text += fmt.Sprintf("\nInstructions: %d bytes (%s)\nNotices: %d bytes (%s)\nMessages: %d · tools: %d · complete capture: %t", capture.Instructions.Bytes, capture.Instructions.Status, capture.Notices.Bytes, capture.Notices.Status, len(capture.Messages), capture.ToolsCount, capture.ContextComplete)
		} else {
			text += "\nCapture is unavailable."
		}
		return nativeControlResult{notice: text}
	})
}

func (m *nativeModel) nativeReport() string {
	var text strings.Builder
	fmt.Fprintf(&text, "Whip %s\nSystem: %s/%s · %s\nTheme: %s\nTerminal size: %d×%d\n", Version, runtime.GOOS, runtime.GOARCH, runtime.Version(), CurrentTheme(), m.width, m.height)
	for _, key := range []string{"TERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION", "COLORTERM", "COLORFGBG", "LC_ALL", "LANG"} {
		if value := os.Getenv(key); value != "" {
			value, _ = nativeTextPrefix(nativeDisplayText(value), 256)
			fmt.Fprintf(&text, "%s: %s\n", key, strings.ReplaceAll(value, "\n", " "))
		}
	}
	summary := text.String()
	return summary + "\nOpen a prefilled issue: " + issueURL("```\n"+summary+"```\n") + "\nNothing has been submitted."
}
