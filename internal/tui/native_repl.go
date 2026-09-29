package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

const nativeExecutionBytes = 2 << 20

type nativeExecution struct {
	owner         protocol.ID
	revision      protocol.Counter
	epoch         protocol.ID
	before, older *protocol.ID
	turns         []protocol.Turn
	cells         []protocol.Cell
	operations    []protocol.HostOperation
	partial       bool
	err           error
}

// Execution rows are canonical host evidence. Reading them never starts a
// worker, reconstructs a checkpoint, or turns a tool result into an operation.
func readNativeExecution(ctx context.Context, c *client.Client, owner protocol.ID, revision protocol.Counter, epoch protocol.ID, before, focus *protocol.ID) (*nativeExecution, error) {
	v := &nativeExecution{owner: owner, revision: revision, epoch: epoch, before: before}
	if focus != nil {
		var turn protocol.Turn
		if err := c.Call(ctx, "turns.get", protocol.TurnParams{TurnID: *focus}, &turn); err != nil {
			return nil, err
		}
		if turn.ID != *focus {
			return nil, errors.New("focused turn identity mismatch")
		}
		v.turns = []protocol.Turn{turn}
	} else {
		var page protocol.TurnPageResult
		if err := c.Call(ctx, "sessions.turns", protocol.TurnPageParams{SessionID: owner, Before: before, Limit: 8}, &page); err != nil {
			return nil, err
		}
		if len(page.Items) > 8 {
			return nil, errors.New("execution turn page exceeds bound")
		}
		v.turns, v.older = page.Items, page.NextCursor
	}
	bytes, requests := 2048, 1
	for _, turn := range v.turns {
		raw, _ := json.Marshal(turn)
		bytes += len(raw)
	}
	take := func(value any) bool {
		raw, err := json.Marshal(value)
		if err != nil || len(raw) > nativeExecutionBytes-bytes {
			v.partial = true
			return false
		}
		bytes += len(raw)
		return true
	}
	seen := map[protocol.ID]bool{}
	for _, turn := range v.turns {
		if turn.ID == "" || turn.SessionID != owner || seen[turn.ID] {
			return nil, errors.New("execution turn ownership mismatch")
		}
		seen[turn.ID] = true
		var after *protocol.ID
		for len(v.cells) < 64 && requests < 64 {
			requests++
			var page protocol.CellsResult
			params := protocol.CellsParams{TurnID: turn.ID, After: after, Limit: min(64-len(v.cells), 64)}
			if err := c.Call(ctx, "turns.cells", params, &page); err != nil {
				return nil, err
			}
			if len(page.Items) > params.Limit {
				return nil, errors.New("cell page exceeds bound")
			}
			if len(page.Items) == 0 {
				break
			}
			prior := protocol.ID("")
			if after != nil {
				prior = *after
			}
			for _, cell := range page.Items {
				if cell.ID <= prior || cell.SessionID != owner || cell.TurnID != turn.ID {
					return nil, errors.New("cell owner or cursor mismatch")
				}
				prior = cell.ID
				if !take(cell) {
					break
				}
				v.cells = append(v.cells, cell)
			}
			if len(v.cells) == 0 || v.cells[len(v.cells)-1].ID != prior {
				break
			}
			after = new(prior)
		}
		after = nil
		for len(v.operations) < 128 && requests < 64 {
			requests++
			var page protocol.HostOperationsResult
			params := protocol.HostOperationsParams{TurnID: turn.ID, After: after, Limit: min(128-len(v.operations), 64)}
			if err := c.Call(ctx, "turns.operations", params, &page); err != nil {
				return nil, err
			}
			if len(page.Items) > params.Limit {
				return nil, errors.New("operation page exceeds bound")
			}
			if len(page.Items) == 0 {
				break
			}
			prior := protocol.ID("")
			if after != nil {
				prior = *after
			}
			for _, op := range page.Items {
				if op.ID <= prior || op.SessionID != owner || op.TurnID != turn.ID {
					return nil, errors.New("operation owner or cursor mismatch")
				}
				prior = op.ID
				if !take(op) {
					break
				}
				v.operations = append(v.operations, op)
			}
			if len(v.operations) == 0 || v.operations[len(v.operations)-1].ID != prior {
				break
			}
			after = new(prior)
		}
	}
	v.partial = v.partial || len(v.cells) == 64 || len(v.operations) == 128 || requests >= 64
	var current protocol.Session
	if err := c.Call(ctx, "sessions.get", protocol.SessionParams{SessionID: owner}, &current); err != nil {
		return nil, err
	}
	if current.ID != owner || current.HistoryRevision != revision {
		return nil, errors.New("history changed while reading execution evidence")
	}
	slices.SortFunc(v.cells, func(a, b protocol.Cell) int {
		if n := strings.Compare(a.CreatedAt, b.CreatedAt); n != 0 {
			return n
		}
		return strings.Compare(string(a.ID), string(b.ID))
	})
	return v, nil
}

func (m *nativeModel) replCommand(args string) tea.Cmd {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		enabled := nativePreferenceLabel(m.preferences.Repl, false) != "on"
		value, err := updateNativePreferences(m.preferencesDirectory, func(p *nativePreferences) { p.Repl = new(enabled) })
		if err != nil {
			m.status = "REPL preference: " + err.Error()
			return nil
		}
		m.preferences = value
	} else {
		switch {
		case args == "older":
			if m.execution == nil || m.execution.older == nil {
				m.status = "No older execution page is available."
				return nil
			}
			m.replBefore = new(*m.execution.older)
			m.replFocus = nil
		case args == "latest":
			m.replBefore, m.replFocus = nil, nil
		case len(fields) == 2 && fields[0] == "turn":
			m.replBefore = nil
			m.replFocus = new(protocol.ID(fields[1]))
		case args == "focus":
			m.replFocused = !m.replFocused
		default:
			m.status = "usage: /repl [older|latest|turn <id>|focus]"
			return nil
		}
	}
	if strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/repl") {
		m.input.Reset()
	}
	m.replGeneration++
	m.polls = 0
	m.execution = nil
	m.refresh()
	if !m.replVisible() {
		m.status = "The REPL panel needs an enabled preference and at least 120 terminal columns."
	} else {
		m.status = "REPL execution evidence · /repl older|latest|turn <id>|focus"
	}
	return nil
}

func nativeExecutionPart(v *nativeExecution, messages []protocol.Message, cell protocol.Cell, result bool) *protocol.Part {
	if cell.SessionID != v.owner {
		return nil
	}
	id := cell.CallMessageID
	if result {
		if cell.ResultMessageID == nil {
			return nil
		}
		id = *cell.ResultMessageID
	}
	for _, message := range messages {
		if message.ID != id || message.SessionID != v.owner || message.TurnID == nil || *message.TurnID != cell.TurnID || message.RetiredBy != nil || message.RetiredRevision != nil {
			continue
		}
		for i := range message.Parts {
			p := &message.Parts[i]
			if !result && p.Type == "tool_call" && p.Call != nil && p.Call.ID == cell.CallID {
				return p
			}
			if result && p.Type == "tool_result" && p.Result != nil && p.Result.CallID == cell.CallID {
				return p
			}
		}
	}
	return nil
}

func (m *nativeModel) replRows(width int) []string {
	v := m.execution
	if v == nil {
		return []string{"Loading canonical execution evidence…"}
	}
	if v.owner != m.owner.ID || v.revision != m.history.snapshot.Revision || v.epoch != m.history.epoch {
		return []string{"Execution evidence is awaiting the current owner/history."}
	}
	rows := []string{string(v.owner) + " · " + m.owner.Configuration.Model.Name, "Up to 8 turns / 64 cells / 128 operations / 2 MiB"}
	if v.err != nil {
		rows = append(rows, "Evidence unavailable: "+nativeDisplayText(v.err.Error()))
	}
	if v.before != nil {
		rows = append(rows, "Older turn page; live work continues. /repl latest")
	}
	if v.partial {
		rows = append(rows, "Partial execution page; use /repl turn <id> for a specific turn.")
	}
	appendText := func(text string) {
		text, cut := nativeTextPrefix(text, 8192)
		part := nativePlainRows(text, width, false)
		if len(part) > 48 {
			part = part[:48]
			cut = true
		}
		rows = append(rows, part...)
		if cut {
			rows = append(rows, "Display clipped; full canonical body remains on the host.")
		}
	}
	messages := m.history.messages
	if m.browse != nil {
		messages = m.browse.transcript.messages
	}
	if p := m.history.preview; v.before == nil && m.replFocus == nil && p != nil {
		for _, call := range p.Calls {
			if call.Name == "execute" {
				rows = append(rows, "Provisional call arguments · not yet a cell")
				appendText(call.Arguments)
			}
		}
	}
	for _, cell := range v.cells {
		if cell.SessionID != v.owner {
			continue
		}
		rows = append(rows, "", fmt.Sprintf("Cell %s · %s", cell.ID, cell.State))
		for _, turn := range v.turns {
			if turn.ID == cell.TurnID && turn.HistoryRevision != v.revision {
				rows = append(rows, fmt.Sprintf("Retained evidence from history revision %d; not current REPL state.", turn.HistoryRevision))
			}
		}
		if p := nativeExecutionPart(v, messages, cell, false); p != nil {
			text := string(p.Call.Arguments)
			var args struct {
				Code *string `json:"code"`
			}
			if p.Call.Name == "execute" && len(p.Call.Arguments) <= nativeRenderInput && json.Unmarshal(p.Call.Arguments, &args) == nil && args.Code != nil {
				text = *args.Code
			}
			appendText(text)
		} else {
			rows = append(rows, "Call body is outside this transcript page.")
		}
		for _, op := range v.operations {
			if op.Origin == "cell" && op.CellID != nil && *op.CellID == cell.ID && op.TurnID == cell.TurnID && op.SessionID == cell.SessionID {
				rows = append(rows, "  "+op.Capability+" · "+op.State)
				if op.Result != nil && op.Result.Failure != nil {
					appendText(*op.Result.Failure)
				}
			}
		}
		if p := nativeExecutionPart(v, messages, cell, true); p != nil {
			appendText(nativeResultText(p.Result.Output))
		} else if cell.ResultMessageID != nil {
			rows = append(rows, "Result body is outside this transcript page.")
		}
		if p := m.history.cellOutput; v.err == nil && v.before == nil && m.replFocus == nil && cell.State == "running" && p != nil && p.SessionID == cell.SessionID && p.CellID == cell.ID && p.TurnID == cell.TurnID && p.CallMessageID == cell.CallMessageID && p.CallID == cell.CallID && p.HistoryRevision == v.revision {
			rows = append(rows, "Live stdout · provisional")
			appendText(p.Text)
			if p.Truncated {
				rows = append(rows, "Host stdout preview truncated.")
			}
		}
		if cell.Checkpoint != nil {
			rows = append(rows, fmt.Sprintf("Checkpoint retained · %s · %d bytes", cell.Checkpoint.Engine, cell.Checkpoint.Size))
		}
	}
	for _, op := range v.operations {
		if op.SessionID == v.owner && op.Origin == "host_operation" && op.CellID == nil {
			rows = append(rows, "", fmt.Sprintf("Direct operation %s · %s · %s", op.ID, op.Capability, op.State))
			if op.Result != nil {
				if op.Result.Failure != nil {
					appendText(*op.Result.Failure)
				}
				if len(op.Result.Value) > 0 {
					appendText(string(op.Result.Value))
				}
			}
		}
	}
	if len(v.cells) == 0 && len(v.operations) == 0 {
		rows = append(rows, "No local cells or operations on this turn page. Imported exchanges remain conversation history.")
	}
	if v.older != nil {
		rows = append(rows, "", "Older execution: /repl older")
	}
	return boundNativeRows(rows, 128<<10, 4096)
}
