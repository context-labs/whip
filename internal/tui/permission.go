package tui

import (
	"fmt"
	"strings"

	"github.com/context-labs/whip/internal/session"
)

// permDialog is presentation state for a daemon-owned permission request.
type permDialog struct {
	sel        int
	rejecting  bool
	rejectIn   string
	daemon     *session.PermissionSnapshot
	deciding   bool
	detailPage int
}

// permOptions lists the dialog's choices; "always" exists only when the
// daemon named a rule for the request.
func permOptions(permission *session.PermissionSnapshot) []string {
	if permission.Rule == "" {
		return []string{"allow once (a)", "reject (r)"}
	}
	return []string{"allow once (a)", "allow always for this tree (t)", "reject (r)"}
}

func (m *model) applyClientPermissions(permissions []session.PermissionSnapshot) {
	if m.permDialog != nil {
		for i := range permissions {
			if m.permDialog.daemon != nil && permissions[i].ID == m.permDialog.daemon.ID {
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

func (m *model) permView() string {
	if m.permDialog == nil || m.permDialog.daemon == nil {
		return ""
	}
	permission := m.permDialog.daemon
	var out strings.Builder
	out.WriteString(youStyle.Render("⚠ Allow " + permission.Operation + "?"))
	detail := permission.Command
	if detail == "" {
		detail = permission.CanonicalPath
	}
	if detail == "" {
		detail = "request " + permission.RequestDigest
	}
	if permission.Operation == "mcp.call" {
		lines, pageSize := m.permissionDetailLines(detail)
		page := min(m.permDialog.detailPage, (len(lines)-1)/pageSize)
		start := page * pageSize
		end := min(start+pageSize, len(lines))
		out.WriteString("\n  " + strings.Join(lines[start:end], "\n  "))
		if len(lines) > pageSize {
			out.WriteString(dimStyle.Render(fmt.Sprintf("\n  details %d–%d of %d · pgup/pgdown", start+1, end, len(lines))))
		}
	} else {
		out.WriteString("\n  " + ansiTruncate(detail, m.width-4))
	}
	if permission.Rule != "" {
		label := permission.Operation + " " + permission.Rule
		if permission.Operation == "mcp.call" {
			label = "this MCP tool and server definition in this tree"
		}
		out.WriteString(dimStyle.Render("\n  always: " + label))
	}
	out.WriteString(dimStyle.Render("\n  agent " + permission.AgentID + " · permission " + permission.ID))
	if m.permDialog.deciding {
		out.WriteString(dimStyle.Render("\n  sending signed decision…"))
		return out.String()
	}
	if m.permDialog.rejecting {
		out.WriteString("\n" + youStyle.Render("  reject with message: ") + m.permDialog.rejectIn + "█")
		out.WriteString(dimStyle.Render("\n  enter sends · esc back"))
		return out.String()
	}
	out.WriteString("\n  ")
	for i, option := range permOptions(permission) {
		if i == m.permDialog.sel {
			out.WriteString(youStyle.Render(glyphUser + option + "  "))
		} else {
			out.WriteString(dimStyle.Render("  " + option + "  "))
		}
	}
	return out.String()
}

func (m *model) permissionDetailLines(detail string) ([]string, int) {
	pageSize := max(1, m.height/3)
	if m.height == 0 {
		pageSize = 8
	}
	return strings.Split(wrap(detail, max(m.width-4, 20)), "\n"), pageSize
}

func ansiTruncate(value string, width int) string {
	if width <= 0 {
		width = 80
	}
	if len(value) <= width {
		return value
	}
	return value[:width-1] + "…"
}
