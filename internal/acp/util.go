package acp

import (
	"log"

	acp "github.com/coder/acp-go-sdk"
)

// logf keeps diagnostics on stderr; stdout carries only editor protocol frames.
func logf(format string, args ...any) {
	log.Printf("whipcode acp: "+format, args...)
}

// updateThoughtText builds an agent_thought_chunk update (no SDK helper).
func updateThoughtText(delta string) acp.SessionUpdate {
	return acp.SessionUpdate{AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
		SessionUpdate: "agent_thought_chunk",
		Content:       acp.TextBlock(delta),
	}}
}
