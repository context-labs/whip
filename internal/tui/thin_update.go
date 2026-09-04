package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

// Update applies daemon state and presentation-only input. Model execution,
// persistence, tool routing, and process ownership never enter this package.
func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	defer m.layout()
	if probe, ok := message.(viewProbe); ok {
		probe.fn(m)
		return m, nil
	}

	switch msg := message.(type) {
	case clientUpdateMsg:
		var commands []tea.Cmd
		if msg.StateChanged {
			m.clientState, m.clientErr = msg.State, msg.Err
			if msg.State == ClientLive {
				m.clientErr = nil
				if !m.historyRequested {
					m.historyRequested = true
					_, command := m.submitClientAction("history.user.list", map[string]string{}, "")
					commands = append(commands, command)
				}
				if m.initialPrompt != "" {
					text := m.initialPrompt
					m.initialPrompt = ""
					_, command := m.submitClientAction("submit", map[string]string{"text": text}, text)
					commands = append(commands, command)
				}
			}
		}
		if msg.Snapshot != nil {
			m.applyClientSnapshot(*msg.Snapshot)
		}
		if msg.Event != nil {
			m.clientCursor = max(m.clientCursor, msg.Event.Seq)
			m.recordClientStream(*msg.Event)
			if handled, command := m.applyClientStream(msg.Event.Kind, msg.Event.Payload); handled {
				return m, tea.Batch(waitClientUpdate(m.client), command)
			}
			if handled, command := m.applyClientLifecycle(msg.Event.Kind, msg.Event.Payload); handled {
				return m, tea.Batch(waitClientUpdate(m.client), command)
			}
			return m, tea.Batch(waitClientUpdate(m.client), m.requestClientSnapshot())
		}
		if msg.closed {
			return m, nil
		}
		commands = append(commands, waitClientUpdate(m.client))
		return m, tea.Batch(commands...)

	case clientSnapshotMsg:
		if msg.err != nil {
			m.clientErr = msg.err
		} else {
			m.applyClientSnapshot(msg.snapshot)
		}
		return m, nil

	case clientCommandMsg:
		m.clientInFlight = max(m.clientInFlight-1, 0)
		succeeded := msg.err == nil && msg.result.Error == "" && msg.result.Status == "succeeded"
		if succeeded && msg.action.Operation == "session.list" {
			var metas []session.Meta
			if err := json.Unmarshal([]byte(msg.result.Output), &metas); err != nil {
				m.append(errStyle.Render("session list: " + err.Error()))
			} else if len(metas) == 0 {
				m.append(dimStyle.Render("(no previous sessions)"))
			} else {
				m.picker = &picker{metas: metas, previews: map[string][2]string{}}
				return m.submitClientAction("session.preview", map[string]string{"id": metas[0].ID}, "")
			}
			return m, nil
		}
		if succeeded && msg.action.Operation == "session.preview" {
			var preview daemon.SessionPreviewResult
			if err := json.Unmarshal([]byte(msg.result.Output), &preview); err != nil {
				m.append(errStyle.Render("session preview: " + err.Error()))
			} else if m.picker != nil {
				m.picker.previews[preview.RootID] = [2]string{preview.User, preview.Assistant}
			}
			return m, nil
		}
		if succeeded && msg.action.Operation == "history.user.list" {
			var history []string
			if err := json.Unmarshal([]byte(msg.result.Output), &history); err != nil {
				m.append(errStyle.Render("input history: " + err.Error()))
			} else {
				// histPrev walks backward from len(hist), so retain oldest-first.
				slices.Reverse(history)
				for _, local := range m.hist {
					if !slices.Contains(history, local) {
						history = append(history, local)
					}
				}
				m.hist = append(m.hist[:0], history...)
				m.histIdx = len(m.hist)
			}
			return m, nil
		}
		if succeeded && msg.action.Operation == "provider.catalogs" {
			var result daemon.ProviderCatalogsResult
			if err := json.Unmarshal([]byte(msg.result.Output), &result); err != nil {
				m.append(errStyle.Render("model catalogs: " + err.Error()))
			} else {
				m.updateCatalogs(result.Catalogs)
				m.append(dimStyle.Render(fmt.Sprintf("✓ refreshed %d provider catalog(s)", len(result.Catalogs))))
				for provider, message := range result.Errors {
					m.append(errStyle.Render(provider + ": " + message))
				}
			}
			if m.reloadAfterCatalogs {
				m.reloadAfterCatalogs = false
				return m.submitClientAction("session.reload", map[string]string{}, "")
			}
			return m, nil
		}
		if msg.action.Operation == "provider.catalogs" && !succeeded {
			m.reloadAfterCatalogs = false
		}
		if succeeded && msg.action.Operation == "agent.submit" {
			var result daemon.AgentSubmitResult
			if err := json.Unmarshal([]byte(msg.result.Output), &result); err != nil {
				m.append(errStyle.Render("agent submit: " + err.Error()))
			} else {
				kind := result.Kind
				if kind == "" {
					kind = "submit"
				}
				m.upsertInbox(result.AgentID, result.InboxSeq, kind, result.Status)
			}
		}
		if succeeded && msg.action.Operation == "agent.transcript" {
			var transcript daemon.AgentTranscriptResult
			if err := json.Unmarshal([]byte(msg.result.Output), &transcript); err != nil {
				m.append(errStyle.Render("agent transcript: " + err.Error()))
			} else if transcript.Agent.ParentID == "" {
				m.clientView.messages = append([]llm.Message{{Role: "system", Content: m.sysPrompt}}, transcript.Messages...)
				m.clientView.presentation = mergePresentation(transcript.Presentation, m.clientView.presentation, transcript.Cursor)
				m.replaceAgentInbox(transcript.Agent.ID, transcript.Inbox)
				m.rebuildClientTranscript()
				m.restoreTerminalMarker()
				m.refreshVP()
			} else {
				m.openAgent(transcript)
			}
			if transcript.Cursor > 0 && transcript.Cursor < m.clientCursor {
				return m, m.requestClientSnapshot()
			}
			return m, nil
		}
		if succeeded && msg.action.Operation == "workspace.set" {
			m.clientView.workingDir = msg.result.Output
		}
		if succeeded && strings.HasPrefix(strings.TrimSpace(msg.result.Output), "[") {
			var rendered string
			var renderErr error
			switch msg.action.Operation {
			case "mcp.control":
				rendered, renderErr = renderMCPStatus(msg.result.Output)
			case "lsp.control":
				rendered, renderErr = renderLSPStatus(msg.result.Output)
			case "schedule.manage":
				rendered, renderErr = renderSchedules(msg.result.Output)
			}
			if rendered != "" || renderErr != nil {
				if renderErr != nil {
					m.append(errStyle.Render(msg.action.Operation + ": " + renderErr.Error()))
				} else {
					m.append(dimStyle.Render(rendered))
				}
				return m, nil
			}
		}
		if succeeded && msg.action.Operation == "context.audit" {
			rendered, err := renderContextAudit(msg.result.Output)
			if err != nil {
				m.append(errStyle.Render("context audit: " + err.Error()))
			} else {
				m.append(dimStyle.Render(rendered))
			}
			return m, nil
		}
		if succeeded && msg.action.Operation == "history.compact.log" {
			rendered, err := renderCompactionLog(msg.result.Output)
			if err != nil {
				m.append(errStyle.Render("compaction log: " + err.Error()))
			} else {
				m.append(dimStyle.Render(rendered))
			}
			return m, nil
		}
		if succeeded && msg.action.Operation == "agents.list" {
			var values []session.RuntimeAgent
			if err := json.Unmarshal([]byte(msg.result.Output), &values); err != nil {
				m.append(errStyle.Render("agents: " + err.Error()))
			} else if len(values) == 0 {
				m.append(dimStyle.Render("(no descendant agents)"))
			} else {
				for _, value := range values {
					m.append(dimStyle.Render(runtimeAgentLine(value) + "  " + value.ID))
				}
			}
		}
		turnOperation := msg.action.Operation == "submit" || msg.action.Operation == "steer" || msg.action.Operation == "agent.submit"
		switch {
		case msg.err != nil:
			if turnOperation {
				m.recordTurnFailure(msg.action, msg.err.Error())
			} else {
				m.append(errStyle.Render(msg.action.Operation + ": " + msg.err.Error()))
			}
			if turnOperation {
				m.busy = false
				m.turnStart = time.Time{}
			}
		case msg.result.Error != "":
			if turnOperation {
				m.recordTurnFailure(msg.action, msg.result.Error)
			} else {
				m.append(errStyle.Render(msg.action.Operation + ": " + msg.result.Error))
			}
			if turnOperation {
				m.busy = false
				m.turnStart = time.Time{}
			}
		case msg.result.Status == "interrupted":
			m.append(dimStyle.Render("(interrupted — effects may be uncertain)"))
		case msg.action.Operation != "submit" && msg.action.Operation != "steer" &&
			msg.action.Operation != "agent.submit" && msg.action.Operation != "agent.turn.cancel" &&
			msg.action.Operation != "session.open" && msg.action.Operation != "agents.list" && msg.result.Output != "":
			m.append(dimStyle.Render(msg.result.Output))
		}
		if succeeded && (msg.action.Operation == "session.fork" || msg.action.Operation == "session.open") {
			m.clientState = ClientSnapshotting
			if err := m.client.SwitchRoot(msg.result.Output); err != nil {
				m.clientErr = err
			}
			return m, nil
		}
		if succeeded && msg.action.Operation == "session.rename" {
			m.sessTitle = msg.result.Output
		}
		if m.clientState == ClientLive && clientCommandNeedsSnapshot(msg.action.Operation) {
			return m, m.requestClientSnapshot()
		}
		return m, nil

	case clientPermissionMsg:
		m.clientInFlight = max(m.clientInFlight-1, 0)
		if m.permDialog != nil && m.permDialog.daemon != nil && m.permDialog.daemon.ID == msg.permissionID {
			m.permDialog.deciding = false
			if msg.err == nil {
				m.permDialog = nil
			}
		}
		if msg.err != nil {
			m.append(errStyle.Render("permission: " + msg.err.Error()))
		}
		if m.clientState == ClientLive {
			return m, m.requestClientSnapshot()
		}
		return m, nil

	case clientTerminalMsg:
		if msg.err != nil {
			m.append(errStyle.Render("terminal input: " + msg.err.Error()))
		} else if msg.result.Error != "" {
			m.append(errStyle.Render("terminal input: " + msg.result.Error))
		}
		return m, nil

	case cfgSyncTick:
		return m.cfgSync()
	case cfgSyncMsg:
		m.applyCfgSync(msg)
		return m, nil
	case tea.WindowSizeMsg:
		m.termWidth, m.height = msg.Width, msg.Height
		m.recalcWidth()
		return m, nil
	case themePollMsg:
		if m.cfg.Theme != "" {
			return m, themePollTick()
		}
		return m, tea.Batch(pollClientTheme, themePollTick())
	case themeSyncMsg:
		if !msg.ok || m.cfg.Theme != "" {
			return m, nil
		}
		mdMu.Lock()
		same := mdKnown && mdLight == msg.light
		mdMu.Unlock()
		if same {
			return m, nil
		}
		SetLightTheme(msg.light)
		m.applyOpencodeStyles()
		m.refreshVP()
		return m, nil
	case toastClearMsg:
		if msg.at.Equal(m.toastAt) {
			m.toast = ""
		}
		return m, nil
	case selScrollTick:
		return m, m.selEdgeScroll()
	case tea.BackgroundColorMsg:
		m.applyDetectedBackground(msg)
	case tea.ColorProfileMsg:
		setThemeProfile(msg.Profile)
		m.applyOpencodeStyles()
		m.refreshVP()
		return m, nil
	case tea.PasteMsg:
		m.sel = nil
		return m.thinPaste(msg)
	case tea.KeyPressMsg:
		m.sel = nil
		return m.thinKey(msg)
	case tea.MouseMsg:
		return m.thinMouse(msg)

	case textMsg:
		m.flushThink()
		m.current += string(msg)
		if i := strings.LastIndexByte(m.current, '\n'); i >= 0 {
			m.appendAssistant(m.current[:i])
			m.current = m.current[i+1:]
		}
		return m, nil
	case thinkMsg:
		if m.showThinking {
			// reasoning streams into one expandable "+ Thought" block: the
			// transient "+ Thinking…" line shows while it runs (viewBody) and
			// flushThink collapses the text with its duration when it ends
			m.flushCurrent()
			if m.thinkStart.IsZero() {
				m.thinkStart = m.nowFn()
			}
			m.ocThink += string(msg)
			m.inThink = true
		}
		return m, nil
	case toolCallMsg:
		row := dimStyle.Render("⋯ " + msg.name + " " + queuedSubject(msg.name, msg.args))
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == blockToolQueued && m.blocks[i].toolID == msg.id {
				m.blocks[i].text, m.blocks[i].stale = row, true
				m.refreshVP()
				return m, nil
			}
		}
		m.blocks = append(m.blocks, block{kind: blockToolQueued, text: row, toolID: msg.id, toolName: msg.name, toolArgs: msg.args})
		m.refreshVP()
		return m, nil
	case toolStartMsg:
		m.flushThink()
		m.flushCurrent()
		for i := range slices.Backward(m.blocks) {
			if m.blocks[i].kind == blockToolQueued && m.blocks[i].toolID == msg.id {
				m.blocks = slices.Delete(m.blocks, i, i+1)
				break
			}
		}
		row := toolStyle.Render("⚒ "+toolVerb(msg.name)+" ") + dimStyle.Render(msg.args)
		m.blocks = append(m.blocks, block{kind: blockToolRun, text: row, toolID: msg.id, toolRunning: true, toolName: msg.name, toolArgs: msg.args})
		m.refreshVP()
		return m, nil
	case toolOutputMsg:
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if block := &m.blocks[i]; block.kind == blockToolRun && block.toolRunning && block.toolID == msg.id {
				block.live, block.stale = lastLines(msg.text, 3), true
				m.refreshVP()
				break
			}
		}
		return m, nil
	case toolEndMsg:
		m.finishTool(msg)
		return m, nil

	case authResultMsg:
		if m.applyAuthResult(msg) {
			m.reloadAfterCatalogs = true
			return m.submitClientAction("provider.catalogs", map[string]string{}, "")
		}
		return m, nil
	case inferenceNetLoginMsg:
		m.applyInferenceNetLogin(msg)
		return m, nil
	case inferenceNetProjectsMsg:
		m.applyInferenceNetProjects(msg)
		return m, nil
	case inferenceNetProjectCreatedMsg:
		m.applyInferenceNetProjectCreated(msg)
		return m, nil
	case inferenceNetAuthMsg:
		if m.applyInferenceNetAuth(msg) {
			m.reloadAfterCatalogs = true
			return m.submitClientAction("provider.catalogs", map[string]string{}, "")
		}
		return m, nil
	case inferenceNetKeyMsg:
		if m.applyInferenceNetKey(msg) {
			m.reloadAfterCatalogs = true
			return m.submitClientAction("provider.catalogs", map[string]string{}, "")
		}
		return m, nil
	case meEditedMsg:
		if msg.err != nil {
			m.append(errStyle.Render("/me: editor failed: " + msg.err.Error()))
		} else {
			m.append(dimStyle.Render("✓ me.md saved — standing instructions updated"))
		}
		return m, nil
	case noticeMsg:
		m.append(dimStyle.Render(string(msg)))
		return m, nil
	case usageMsg:
		m.lastResp = llm.Usage(msg)
		return m, nil
	case quitArmMsg:
		m.quit1 = false
		return m, nil
	case escArmMsg:
		m.esc1, m.escClr = false, false
		return m, nil
	case imageMsg:
		switch {
		case msg.err != nil:
			m.append(errStyle.Render("image paste failed: " + msg.err.Error()))
		case msg.path == "":
			m.append(dimStyle.Render("(no image on clipboard)"))
		default:
			m.input.InsertString("@" + msg.path + " ")
			m.refreshMenu()
		}
		return m, nil
	case spinner.TickMsg:
		if !m.busy {
			return m, nil
		}
		var command tea.Cmd
		m.spin, command = m.spin.Update(msg)
		return m, command
	}

	var command tea.Cmd
	m.input, command = m.input.Update(message)
	return m, command
}

func clientCommandNeedsSnapshot(operation string) bool {
	switch operation {
	case "history.clear", "history.rewind", "history.compact", "history.compact.retry",
		"goal.set", "goal.run", "goal.from-context",
		"agent.control", "agent.delete", "budget.cap", "capability.revoke":
		return true
	default:
		return false
	}
}

func (m *model) thinMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Mod.Contains(tea.ModShift) {
		return m, nil
	}
	_, isClick := msg.(tea.MouseClickMsg)
	_, isWheel := msg.(tea.MouseWheelMsg)
	press := isClick || isWheel
	if m.msgActions != nil && press {
		m.msgActions = nil
		return m, nil
	}
	// The REPL panel owns the mouse over its columns: the wheel scrolls it and
	// a press there never seeds a chat selection (selPoint/inputPoint only
	// bound Y). Motion and release still flow to handleMouseSelect so a drag
	// that started in the chat completes wherever the pointer ends.
	inPanel := m.replPanel && inRect(m.frameNow().side, mouse.X, mouse.Y)
	if inPanel && press {
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.replScroll += 3 // the view clamps to the history
		case tea.MouseWheelDown:
			m.replScroll = max(m.replScroll-3, 0)
		case tea.MouseLeft:
			m.sel = nil // like any press: drop the old highlight
		}
		return m, nil
	}
	if handled, command := m.handleMouseSelect(msg); handled {
		return m, command
	}
	if inPanel {
		return m, nil
	}
	if isClick && mouse.Button == tea.MouseLeft {
		m.clickAt(mouse.X, mouse.Y)
		return m, nil
	}
	var command tea.Cmd
	m.vp, command = m.vp.Update(msg)
	m.follow = m.vp.AtBottom()
	return m, command
}

func (m *model) finishTool(msg toolEndMsg) {
	header := -1
	for i := len(m.blocks) - 1; i >= 0; i-- {
		block := &m.blocks[i]
		if block.kind == blockToolRun && block.toolRunning && block.toolID == msg.id {
			block.toolRunning = false
			block.toolFailed = strings.HasPrefix(msg.result, "Error:")
			block.live = ""
			block.text = toolHeaderRow(msg.name, block.toolArgs, block.toolFailed)
			block.stale = true
			header = i
			break
		}
	}
	result := block{kind: blockTool, text: msg.result}
	if header >= 0 && header+1 < len(m.blocks) {
		m.blocks = append(m.blocks[:header+1], append([]block{result}, m.blocks[header+1:]...)...)
		for i := range m.msgBlock {
			if m.msgBlock[i] > header {
				m.msgBlock[i]++
			}
		}
	} else {
		m.blocks = append(m.blocks, result)
	}
	m.follow = true
	m.refreshVP()
}

func (m *model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return m.thinKey(msg) }
