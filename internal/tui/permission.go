package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

// Permission prompts: when the agent is about to run a gated tool (bash,
// write, edit) and no saved rule covers it, the turn pauses and a modal asks
// Allow once / Allow always / Reject. "Always" previews the exact rule it
// installs (arity-collapsed: "git checkout main" → rule "git checkout");
// Reject takes a free-text redirect back to the model.
//
// The gate runs on a tool goroutine; the dialog runs on the UI thread. They
// meet on a channel: the gate sends a request, the UI answers it. This is
// the turn-pausing modal computer.go's consent hook was waiting for.

// permRequest is the gate→UI half; the answer comes back on reply.
type permRequest struct {
	req   tools.GateRequest
	reply chan permAnswer
}

type permAnswer struct {
	decision tools.GateDecision
	redirect string // free-text redirect on reject
}

// permDialog is the UI-thread modal state while a request is open.
type permDialog struct {
	req       tools.GateRequest
	reply     chan permAnswer
	sel       int  // 0=allow once, 1=allow always, 2=reject
	rejecting bool // typing the redirect message
	rejectIn  string
	daemon    *session.PermissionSnapshot
	deciding  bool
}

// permRules is the saved "allow always" set, persisted to
// ~/.loopy/permissions.json as a flat list of "tool:rule" strings.
type permRules map[string]bool

func loadPermRules() permRules {
	out := permRules{}
	var list []string
	if err := config.ReadJSON("permissions.json", &list); err == nil {
		for _, r := range list {
			out[r] = true
		}
	}
	return out
}

func (r permRules) save() {
	list := make([]string, 0, len(r))
	for k := range r {
		list = append(list, k)
	}
	_ = config.WriteJSON("permissions.json", list) // best-effort; next save retries
}

// ruleKey is the stored rule: tool + the arity-collapsed command/path.
func ruleKey(tool, rule string) string { return tool + ":" + rule }

// coveredBy reports whether a saved rule covers this call. bash matches on
// the collapsed command rule; write/edit match on the exact path (a file
// rule is already specific — collapsing paths is overreach).
func (r permRules) coveredBy(req tools.GateRequest) bool {
	rule := req.Rule
	if req.Tool != "bash" {
		rule = req.Command // path rules are exact
	}
	return r[ruleKey(req.Tool, rule)]
}

// permKey handles keys while the dialog is open. Returns (handled).
func (m *model) permKey(msg tea.KeyMsg) bool {
	d := m.permDialog
	if d == nil {
		return false
	}
	answer := func(a permAnswer) {
		d.reply <- a
		m.permDialog = nil
	}
	if d.rejecting {
		switch msg.Type {
		case tea.KeyEnter:
			answer(permAnswer{tools.GateReject, strings.TrimSpace(d.rejectIn)})
		case tea.KeyEsc:
			d.rejecting, d.rejectIn = false, "" // back to the buttons
		case tea.KeyBackspace:
			if len(d.rejectIn) > 0 {
				d.rejectIn = d.rejectIn[:len(d.rejectIn)-1]
			}
		case tea.KeyRunes, tea.KeySpace:
			d.rejectIn += string(msg.Runes)
			if msg.Type == tea.KeySpace {
				d.rejectIn += " "
			}
		}
		return true
	}
	switch msg.Type {
	case tea.KeyLeft, tea.KeyUp:
		d.sel = (d.sel + 2) % 3
	case tea.KeyRight, tea.KeyDown:
		d.sel = (d.sel + 1) % 3
	case tea.KeyEnter:
		switch d.sel {
		case 0:
			answer(permAnswer{decision: tools.GateAllowOnce})
		case 1:
			rule := d.req.Rule
			if d.req.Tool != "bash" {
				rule = d.req.Command
			}
			m.permsMu.Lock()
			m.perms[ruleKey(d.req.Tool, rule)] = true
			m.perms.save()
			m.permsMu.Unlock()
			answer(permAnswer{decision: tools.GateAllowAlways})
		case 2:
			d.rejecting = true // take the redirect text
		}
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "a":
			answer(permAnswer{decision: tools.GateAllowOnce})
		case "A":
			rule := d.req.Rule
			if d.req.Tool != "bash" {
				rule = d.req.Command
			}
			m.permsMu.Lock()
			m.perms[ruleKey(d.req.Tool, rule)] = true
			m.perms.save()
			m.permsMu.Unlock()
			answer(permAnswer{decision: tools.GateAllowAlways})
		case "r":
			d.rejecting = true
		}
	case tea.KeyEsc:
		answer(permAnswer{tools.GateReject, "rejected without a reason"})
	}
	return true
}

// permView renders the modal: the action, the rule "always" would install,
// and the three options (or the redirect prompt).
func (m *model) permView() string {
	d := m.permDialog
	if d == nil {
		return ""
	}
	if d.daemon != nil {
		return m.daemonPermView(d)
	}
	var b strings.Builder
	title := "Allow " + d.req.Tool + "?"
	if d.req.Tool == "bash" {
		title = "Run this command?"
	}
	b.WriteString(youStyle.Render("⚠ " + title))
	b.WriteString("\n  " + ansi_Truncate(d.req.Command, m.width-4))
	rule := d.req.Rule
	if d.req.Tool != "bash" {
		rule = d.req.Command
	}
	b.WriteString(dimStyle.Render("\n  always allows: " + ruleKey(d.req.Tool, rule)))
	if d.rejecting {
		b.WriteString("\n" + youStyle.Render("  reject with message: ") + d.rejectIn + "█")
		b.WriteString(dimStyle.Render("\n  enter sends · esc back"))
		return b.String()
	}
	opts := []string{"allow once (a)", "allow always (A)", "reject (r)"}
	b.WriteString("\n  ")
	for i, o := range opts {
		if i == d.sel {
			b.WriteString(youStyle.Render(glyphUser + o + "  "))
		} else {
			b.WriteString(dimStyle.Render("  " + o + "  "))
		}
	}
	return b.String()
}

func (m *model) applyClientPermissions(permissions []session.PermissionSnapshot) {
	if m.permDialog != nil && m.permDialog.daemon == nil {
		return
	}
	if m.permDialog != nil {
		for i := range permissions {
			if permissions[i].ID == m.permDialog.daemon.ID {
				permission := permissions[i]
				m.permDialog.daemon = &permission
				return
			}
		}
		m.permDialog = nil
	}
	if len(permissions) > 0 {
		permission := permissions[0]
		m.permDialog = &permDialog{daemon: &permission}
	}
}

func (m *model) daemonPermView(dialog *permDialog) string {
	permission := dialog.daemon
	var b strings.Builder
	b.WriteString(youStyle.Render("⚠ Allow " + permission.Operation + "?"))
	detail := permission.CanonicalPath
	if detail == "" {
		detail = "request " + permission.RequestDigest
	}
	b.WriteString("\n  " + ansi_Truncate(detail, m.width-4))
	b.WriteString(dimStyle.Render("\n  agent " + permission.AgentID + " · permission " + permission.ID))
	if dialog.deciding {
		b.WriteString(dimStyle.Render("\n  sending signed decision…"))
		return b.String()
	}
	if dialog.rejecting {
		b.WriteString("\n" + youStyle.Render("  reject with message: ") + dialog.rejectIn + "█")
		b.WriteString(dimStyle.Render("\n  enter sends · esc back"))
		return b.String()
	}
	options := []string{"allow once (a)", "reject (r)"}
	b.WriteString("\n  ")
	for i, option := range options {
		if i == dialog.sel {
			b.WriteString(youStyle.Render(glyphUser + option + "  "))
		} else {
			b.WriteString(dimStyle.Render("  " + option + "  "))
		}
	}
	return b.String()
}

// small local helper so this file doesn't import ansi for one call
func ansi_Truncate(s string, width int) string {
	if width <= 0 {
		width = 80
	}
	if len(s) <= width {
		return s
	}
	return s[:width-1] + "…"
}

var _ = fmt.Sprint
