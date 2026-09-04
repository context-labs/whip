package tui

// dialogOpen reports whether a floating dialog or an input-slot mode owns the
// screen: presses and the wheel must not reach the transcript underneath.
// (The completion menu is not a dialog: keys fall through to the textarea,
// but presses on it are swallowed by the callers that check m.menu.)
func (m *model) dialogOpen() bool {
	return m.palette != nil || m.picker != nil || m.mpicker != nil || m.msgActions != nil || m.rew != nil ||
		(m.permDialog != nil && m.permDialog.daemon != nil)
}
