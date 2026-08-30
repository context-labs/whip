package bashrun

// Markers returns per-session child metadata without process-global state.
func Markers(sessionID, model string) map[string]string {
	markers := map[string]string{}
	if sessionID != "" {
		markers["WHIP_SESSION_ID"] = sessionID
	}
	if model != "" {
		markers["WHIP_MODEL"] = model
	}
	return markers
}
