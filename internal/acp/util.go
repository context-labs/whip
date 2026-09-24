package acp

import (
	"log"

	acp "github.com/coder/acp-go-sdk"
)

// config_logf logs to stderr (stdout is the protocol channel — nothing else
// may write there) and to whip's event log when it's initialized.
func config_logf(format string, args ...any) {
	log.Printf("whip acp: "+format, args...)
	if eventLogf != nil {
		eventLogf(format, args...)
	}
}

// eventLogf is wired by cmd/whip to config.LogEvent so ACP-mode diagnostics
// land in the same log as the rest of whip.
var eventLogf func(format string, args ...any)

// SetEventLog installs the event-log hook (cmd/whip startup).
func SetEventLog(f func(format string, args ...any)) { eventLogf = f }

// updateThoughtText builds an agent_thought_chunk update (no SDK helper).
func updateThoughtText(delta string) acp.SessionUpdate {
	return acp.SessionUpdate{AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
		SessionUpdate: "agent_thought_chunk",
		Content:       acp.TextBlock(delta),
	}}
}
