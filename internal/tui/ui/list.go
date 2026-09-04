package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/tui/theme"
)

// ListItem is one selectable row: a left label and a right-aligned hint.
type ListItem struct{ Left, Right string }

// ListGroup is a run of items under an accent header ("" = no header).
type ListGroup struct {
	Title string
	Items []ListItem
}

// List is the floating chooser every dialog shares (commands, message
// actions, models, sessions): a panel of Width cells on the panel surface
// with a bold title and a right-aligned hint, an optional search row, grouped
// items with the selection as a full-width primary fill, and a footer of
// key/description pairs. Window limits the item rows shown, kept around Sel,
// so the selection can never scroll off a short terminal.
type List struct {
	Title, Hint string // header row: Title bold left, Hint muted right (e.g. "esc")
	Search      bool   // show the search row: Query, or the muted placeholder when empty
	Query       string
	Groups      []ListGroup
	Sel         int      // index over all items in group order
	Empty       string   // shown instead of items when there are none
	Footer      []string // key, description pairs: "enter", "select", "type", "to filter"
	Width       int
	Window      int // max item rows (0 = all)
}

// Render returns the rows, each exactly Width cells.
func (l List) Render(th *theme.Theme) []string {
	bg := th.Surface.Panel
	text, muted := th.On(th.Text, bg), th.On(th.Muted, bg)
	head, accent := text.Bold(true), th.On(th.Accent, bg).Bold(true)
	blank := PadRow("", l.Width, bg)
	// lr assembles left+right onto one padded row: left at col 2, right at the edge
	lr := func(left, right string) string {
		gap := max(l.Width-2-lipgloss.Width(left)-lipgloss.Width(right)-2, 1)
		return PadRow(th.On(nil, bg).Render("  ")+left+th.On(nil, bg).Render(strings.Repeat(" ", gap))+right, l.Width, bg)
	}

	rows := []string{blank, lr(head.Render(l.Title), muted.Render(l.Hint)), blank}
	if l.Search {
		if l.Query == "" {
			rows = append(rows, lr(muted.Render("Search"), ""))
		} else {
			rows = append(rows, lr(text.Render(l.Query), ""))
		}
		rows = append(rows, blank)
	}

	total := 0
	for _, g := range l.Groups {
		total += len(g.Items)
	}
	lo, hi := 0, total
	if l.Window > 0 && l.Window < total {
		lo, hi = listWindow(total, l.Sel, l.Window)
	}
	idx, lastTitle, started := 0, "", false
	for _, g := range l.Groups {
		for _, it := range g.Items {
			i := idx
			idx++
			if i < lo || i >= hi {
				continue
			}
			if g.Title != lastTitle || !started {
				if started {
					rows = append(rows, blank)
				}
				if g.Title != "" {
					rows = append(rows, lr(accent.Render(g.Title), ""))
				}
				lastTitle, started = g.Title, true
			}
			right := ansi.Truncate(it.Right, max(l.Width-4-lipgloss.Width(it.Left)-2, 0), "…")
			if i == l.Sel { // full-width primary fill, the selected-row treatment
				sel := th.Selected
				row := sel.Render("  "+it.Left) + sel.Render(strings.Repeat(" ", max(l.Width-2-lipgloss.Width(it.Left)-lipgloss.Width(right)-2, 1))) + sel.Render(right+"  ")
				rows = append(rows, PadRow(row, l.Width, th.Primary))
			} else {
				rows = append(rows, lr(text.Render(it.Left), muted.Render(right)))
			}
		}
	}
	if total == 0 && l.Empty != "" {
		rows = append(rows, lr(muted.Render(l.Empty), ""))
	}
	if len(l.Footer) > 0 {
		var f strings.Builder
		for i := 0; i+1 < len(l.Footer); i += 2 {
			if i > 0 {
				f.WriteString(th.On(nil, bg).Render("  "))
			}
			f.WriteString(text.Render(l.Footer[i]) + muted.Render(" "+l.Footer[i+1]))
		}
		rows = append(rows, blank, lr(f.String(), ""))
	}
	return append(rows, blank)
}

// listWindow returns the [lo,hi) item range showing up to budget rows
// centered on idx.
func listWindow(n, idx, budget int) (int, int) {
	if budget >= n {
		return 0, n
	}
	lo := max(idx-budget/2, 0)
	hi := min(lo+budget, n)
	return max(hi-budget, 0), hi
}
