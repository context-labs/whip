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

const nativeAgentLimit = 512

type nativeAgentRow struct {
	id, parent      protocol.ID
	name, lifecycle string
	depth           int
	observedState   *string
}

type nativeAgentTree struct {
	tree, root protocol.ID
	rows       []nativeAgentRow
	partial    bool
}

// This is a bounded display observation, never a cached source of authority.
// Pagination can race admissions/deletions; opening and control reread the exact
// selected owner. Activity is read only for the small visible row window.
type nativeAgentReader interface {
	Call(context.Context, string, any, any) error
}

func readNativeAgents(ctx context.Context, c nativeAgentReader, owner protocol.Session, selected protocol.ID) (*nativeAgentTree, error) {
	tree := &nativeAgentTree{tree: owner.TreeID}
	rows := map[protocol.ID]nativeAgentRow{}
	add := func(v protocol.Session) error {
		if v.ID == "" || v.TreeID != owner.TreeID || v.ParentID != nil && *v.ParentID == v.ID {
			return errors.New("agent tree owner or lineage mismatch")
		}
		row := nativeAgentRow{id: v.ID, name: string(v.Definition.ID), lifecycle: v.Lifecycle}
		if v.ParentID != nil {
			row.parent = *v.ParentID
		}
		rows[v.ID] = row
		return nil
	}
	root := owner
	seen := map[protocol.ID]bool{}
	for depth := 0; ; depth++ {
		if depth >= 32 || seen[root.ID] {
			return nil, errors.New("agent ancestry exceeds its bound or contains a cycle")
		}
		seen[root.ID] = true
		if err := add(root); err != nil {
			return nil, err
		}
		if root.ParentID == nil {
			tree.root = root.ID
			break
		}
		var parent protocol.Session
		if err := c.Call(ctx, "sessions.get", protocol.SessionParams{SessionID: *root.ParentID}, &parent); err != nil {
			return nil, err
		}
		if parent.ID != *root.ParentID {
			return nil, errors.New("agent parent identity mismatch")
		}
		root = parent
	}
	params := protocol.ListSessionsParams{TreeID: owner.TreeID, Limit: 64}
	for pageIndex := range nativeAgentLimit / 64 {
		var page protocol.ListSessionsResult
		if err := c.Call(ctx, "sessions.list", params, &page); err != nil {
			return nil, err
		}
		if len(page.Items) > params.Limit {
			return nil, errors.New("agent page exceeds its bound")
		}
		prior := protocol.ID("")
		if params.After != nil {
			prior = *params.After
		}
		for _, item := range page.Items {
			if item.ID <= prior {
				return nil, errors.New("agent page did not advance")
			}
			prior = item.ID
			if err := add(item); err != nil {
				return nil, err
			}
		}
		if len(page.Items) == 0 {
			break
		}
		params.After = new(prior)
		if pageIndex == nativeAgentLimit/64-1 {
			tree.partial = true
		}
	}
	tree.rows = nativeAgentRows(rows, tree.root)
	lo, hi := nativeAgentWindow(tree.rows, selected, 8)
	for i := lo; i < hi; i++ {
		var activity protocol.SessionActivity
		if err := c.Call(ctx, "sessions.activity", protocol.SessionParams{SessionID: tree.rows[i].id}, &activity); err != nil {
			return nil, err
		}
		if activity.SessionID != tree.rows[i].id || activity.ActiveTurn != nil && activity.ActiveTurn.SessionID != activity.SessionID {
			return nil, errors.New("agent activity owner mismatch")
		}
		state := nativeActivityState(activity)
		tree.rows[i].observedState = &state
	}
	return tree, nil
}

func nativeAgentRows(rows map[protocol.ID]nativeAgentRow, root protocol.ID) []nativeAgentRow {
	ids := make([]protocol.ID, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	children := map[protocol.ID][]protocol.ID{}
	for _, id := range ids {
		r := rows[id]
		children[r.parent] = append(children[r.parent], id)
	}
	out := make([]nativeAgentRow, 0, len(rows))
	seen := map[protocol.ID]bool{}
	var walk func(protocol.ID, int)
	walk = func(id protocol.ID, depth int) {
		if seen[id] {
			return
		}
		seen[id] = true
		r := rows[id]
		r.depth = min(depth, 32)
		out = append(out, r)
		for _, child := range children[id] {
			walk(child, depth+1)
		}
	}
	if _, ok := rows[root]; ok {
		walk(root, 0)
	}
	// A partial observation preserves disconnected rows without inventing lineage.
	for _, id := range ids {
		if !seen[id] {
			walk(id, 0)
		}
	}
	return out
}

func nativeAgentWindow(rows []nativeAgentRow, selected protocol.ID, count int) (int, int) {
	index := 0
	for i, row := range rows {
		if row.id == selected {
			index = i
			break
		}
	}
	start := min(max(index-count/2, 0), max(len(rows)-count, 0))
	return start, min(start+count, len(rows))
}

func nativeAgentState(row nativeAgentRow) string {
	if row.observedState == nil {
		return row.lifecycle + " · activity unread"
	}
	return *row.observedState
}

func nativeActivityState(v protocol.SessionActivity) string {
	if v.PendingPermissionCount > 0 || v.PendingQuestionCount > 0 {
		return "needs decision"
	}
	if v.ActiveTurn != nil {
		return v.ActiveTurn.State
	}
	if v.ActiveWorkspaceActionID != nil {
		return "workspace action"
	}
	if v.QueuedInputCount > 0 {
		return fmt.Sprintf("%s · queued %d", v.Lifecycle, v.QueuedInputCount)
	}
	if v.Lifecycle == "active" {
		return "idle"
	}
	return v.Lifecycle
}

func (m *nativeModel) agentKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	if key.String() == "ctrl+t" {
		m.agentsFocus = !m.agentsFocus
		m.dock = true
		m.polls = 0
		if m.agentSelection == "" || m.agentSelection == m.owner.ID {
			if m.agents != nil && len(m.agents.rows) > 1 {
				m.agentSelection = m.agents.rows[1].id
			}
		}
		m.refresh()
		return nil, true
	}
	if !m.agentsFocus {
		return nil, false
	}
	if key.String() == "ctrl+c" {
		return nil, false
	}
	if key.String() == "esc" {
		m.agentsFocus = false
		if m.agents != nil && m.owner.ID != m.agents.root {
			return m.resumeSession(string(m.agents.root)), true
		}
		return nil, true
	}
	if m.agents == nil {
		m.status = "Agent tree is loading."
		return nil, true
	}
	index := 0
	for i, row := range m.agents.rows {
		if row.id == m.agentSelection {
			index = i
			break
		}
	}
	switch key.String() {
	case "up":
		index = max(index-1, 0)
	case "down":
		index = min(index+1, max(len(m.agents.rows)-1, 0))
	case "enter":
		if len(m.agents.rows) > 0 {
			m.agentsFocus = false
			return m.resumeSession(string(m.agents.rows[index].id)), true
		}
	default:
		m.agentsFocus = false
		return nil, false
	}
	if len(m.agents.rows) > 0 {
		m.agentSelection = m.agents.rows[index].id
	}
	m.polls = 0
	m.refresh()
	return nil, true
}

func (m *nativeModel) agentsCommand(args string) tea.Cmd {
	fields := strings.Fields(args)
	owner := m.owner
	if len(fields) == 0 || args == "list" {
		return m.control("Agents", false, func(ctx context.Context) nativeControlResult {
			tree, err := readNativeAgents(ctx, m.connection, owner, owner.ID)
			if err != nil {
				return nativeControlResult{err: err}
			}
			lines := []string{"Current tree " + string(tree.tree) + " · bounded observation"}
			for _, row := range tree.rows {
				lines = append(lines, fmt.Sprintf("%s%s · %s · %s", strings.Repeat("  ", row.depth), row.id, row.name, nativeAgentState(row)))
			}
			if tree.partial {
				lines = append(lines, "Limited tree observation: up to 8 pages / 512 owners plus current ancestry. /resume <exact ID> opens any known owner.")
			}
			return nativeControlResult{notice: strings.Join(lines, "\n")}
		})
	}
	if len(fields) == 2 && fields[0] == "revoke" {
		return m.permissionsCommand("forget " + fields[1])
	}
	if len(fields) != 2 || (fields[0] != "open" && fields[0] != "stop" && fields[0] != "delete") {
		m.status = "usage: /agents [list|open <id>|stop <id>|delete <child-id>|revoke <grant-id on displayed owner>]"
		return nil
	}
	if !m.navigationAllowed() {
		return nil
	}
	id := protocol.ID(fields[1])
	action := fields[0]
	return m.control("Agent "+action, false, func(ctx context.Context) nativeControlResult {
		var target protocol.Session
		if err := m.connection.Call(ctx, "sessions.get", protocol.SessionParams{SessionID: id}, &target); err != nil {
			return nativeControlResult{err: err}
		}
		if target.ID != id || target.TreeID != owner.TreeID {
			return nativeControlResult{err: errors.New("agent is outside the displayed tree")}
		}
		if action == "open" {
			return nativeControlResult{attach: &target}
		}
		if action == "delete" {
			if target.ParentID == nil {
				return nativeControlResult{err: errors.New("agent deletion only accepts a child; root deletion is a separate session control")}
			}
			var result protocol.DeleteResult
			err := m.connection.Call(ctx, "sessions.delete", protocol.SessionParams{SessionID: id}, &result)
			var attach *protocol.Session
			if err == nil && id == owner.ID {
				var parent protocol.Session
				err = m.connection.Call(ctx, "sessions.get", protocol.SessionParams{SessionID: *target.ParentID}, &parent)
				if err == nil && (parent.ID != *target.ParentID || parent.TreeID != owner.TreeID) {
					err = errors.New("deleted child's parent identity mismatch")
				}
				if err == nil {
					attach = &parent
				}
			}
			if err != nil {
				err = fmt.Errorf("outcome may be partial or unknown; inspect /agents list before another explicit action: %w", err)
			}
			return nativeControlResult{attach: attach, err: err, mutation: true, notice: "Child deletion requested for " + string(id) + ". Refresh the tree to inspect the result."}
		}
		var result protocol.Session
		err := m.connection.Call(ctx, "sessions.lifecycle", protocol.LifecycleParams{SessionID: id, Lifecycle: "stopped"}, &result)
		if err == nil && (result.ID != id || result.TreeID != owner.TreeID || result.Lifecycle != "stopped") {
			err = errors.New("agent lifecycle owner mismatch")
		}
		if err != nil {
			err = fmt.Errorf("outcome may be partial or unknown; inspect /agents list before another explicit action: %w", err)
		}
		return nativeControlResult{owner: &result, err: err, mutation: true, notice: "Stopped " + string(id) + "; queued inputs remain owned by that session."}
	})
}
