package tui

// The left column's panels, in order.
const (
	paneAgents = iota
	paneContext
	paneLSP
)

// paneIndex maps the config's panel name to a pane; unknown names mean Agents.
func paneIndex(name string) int {
	switch name {
	case "context":
		return paneContext
	case "lsp":
		return paneLSP
	}
	return paneAgents
}

// paneHeights splits the column's rows between the panels: the open one takes
// what the two collapsed headers and the gap rows between them leave. A column
// too short for three headers shows the open panel alone.
func paneHeights(height, open int) [3]int {
	collapsed := 2*currentTheme().Space.PadY + 1
	var out [3]int
	if height < 3*collapsed+2 {
		out[open] = height
		return out
	}
	for i := range out {
		out[i] = collapsed
	}
	out[open] = height - 2*(collapsed+1)
	return out
}
