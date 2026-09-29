package model

import (
	"encoding/json"
	"strings"

	"github.com/context-labs/whip/internal/session"
)

// Inspect freezes the public semantic instruction input at Prepare, rather than
// the provider's encoded body (which may carry private continuation or images).
func Inspect(request Request) session.ModelCapture {
	notices := request.Notices
	instructions := request.Instructions
	if notices != "" && strings.HasSuffix(instructions, notices) {
		instructions = strings.TrimSuffix(instructions, notices)
	} else {
		notices = ""
	}
	remaining := session.MaxModelCaptureBytes
	text := func(value string) session.CapturedText {
		body := session.CapturedText{Digest: session.CaptureDigest([]byte(value)), Bytes: int64(len(value)), Status: "oversized", Chunks: []session.ContentReference{}}
		if len(value) <= remaining {
			body.Status, body.Data = "available", []byte(value)
			remaining -= len(value)
		}
		return body
	}
	result := session.ModelCapture{Instructions: text(instructions), Notices: text(notices), Messages: []session.CapturedMessage{}, ToolsCount: len(request.Tools), ContextComplete: len(request.Messages) <= 128}
	for _, message := range request.Messages[:min(len(request.Messages), 128)] {
		raw, _ := json.Marshal(message.Parts)
		result.Messages = append(result.Messages, session.CapturedMessage{ID: message.ID, Role: message.Role, PartsDigest: session.CaptureDigest(raw), PartsCount: len(message.Parts)})
	}
	tools, _ := json.Marshal(request.Tools)
	result.ToolsDigest = session.CaptureDigest(tools)
	result.Seal()
	return result
}
