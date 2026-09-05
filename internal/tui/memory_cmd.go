package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/context-labs/whip/internal/memory"
)

// /memory lists saved markdown notes; /memory <n> [session] marks an entry
// done. The files stay plain markdown the user can also edit by hand.
func (m *model) memoryCommand(args []string) {
	scopes := []memory.Scope{memory.Installation(), memory.Session(m.sessionID)}

	// /memory <n> [session|installation] — mark entry n done.
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			m.append(errStyle.Render("/memory: entry number expected, got " + args[0]))
			return
		}
		which := "installation"
		if len(args) > 1 {
			which = args[1]
		}
		var s memory.Scope
		switch which {
		case "installation", "install", "global":
			s = scopes[0]
		case "session":
			s = scopes[1]
		default:
			m.append(errStyle.Render("/memory: scope must be installation or session, got " + which))
			return
		}
		if s.Path == "" {
			m.append(dimStyle.Render("(no " + which + " memory scope — start a session first)"))
			return
		}
		if err := s.Forget(n); err != nil {
			m.append(errStyle.Render("/memory: " + err.Error()))
			return
		}
		m.append(dimStyle.Render(fmt.Sprintf("✓ %s memory %d marked done", which, n)))
		return
	}

	// bare /memory — list both scopes, open entries first.
	var b strings.Builder
	b.WriteString(dimStyle.Render("memory notes · /memory <n> [session] marks done · edit the files directly:"))
	found := false
	for _, s := range scopes {
		entries := s.Entries()
		if s.Path == "" || len(entries) == 0 {
			continue
		}
		found = true
		fmt.Fprintf(&b, "\n%s (%s)", s.Name, s.Path)
		for _, e := range entries {
			if e.Done {
				fmt.Fprintf(&b, "\n  %s", dimStyle.Render(fmt.Sprintf("%d. ~~%s~~", e.N, e.Text)))
			} else {
				fmt.Fprintf(&b, "\n  %d. %s", e.N, e.Text)
			}
		}
	}
	if !found {
		b.WriteString("\n  (empty — write a line like \"- [ ] prefers pnpm\" in a memory file)")
	}
	m.append(b.String())
}
