package protocol

import (
	"github.com/context-labs/whip/internal/session"
)

type ModelInspectionParams struct {
	SessionID ID `json:"session_id"`
	AttemptID ID `json:"attempt_id"`
}

type CapturedText struct {
	Digest string             `json:"digest" pattern:"^[a-f0-9]{64}$"`
	Bytes  Counter            `json:"bytes"`
	Status string             `json:"status" enum:"available,oversized,quota,storage_error"`
	Chunks []ContentReference `json:"chunks"`
}

type CapturedMessage struct {
	ID          *ID    `json:"id"`
	Role        string `json:"role" enum:"system,user,assistant,tool"`
	PartsDigest string `json:"parts_digest" pattern:"^[a-f0-9]{64}$"`
	PartsCount  int    `json:"parts_count" min:"0" max:"1024"`
}

type ModelCapture struct {
	RequestDigest   string            `json:"request_digest" pattern:"^[a-f0-9]{64}$"`
	SourceDigest    string            `json:"source_digest" pattern:"^[a-f0-9]{64}$"`
	Instructions    CapturedText      `json:"instructions"`
	Notices         CapturedText      `json:"notices"`
	Messages        []CapturedMessage `json:"messages"`
	ToolsDigest     string            `json:"tools_digest" pattern:"^[a-f0-9]{64}$"`
	ToolsCount      int               `json:"tools_count" min:"0" max:"1024"`
	ContextComplete bool              `json:"context_complete"`
}

type ModelInspection struct {
	SessionID     ID                  `json:"session_id"`
	AttemptID     ID                  `json:"attempt_id"`
	TurnID        ID                  `json:"turn_id"`
	RequestDigest string              `json:"request_digest" pattern:"^[a-f0-9]{64}$"`
	Capture       *ModelCapture       `json:"capture"`
	Compaction    *CompactionMetadata `json:"compaction"`
}

func ModelInspectionFromDomain(value session.ModelInspection) ModelInspection {
	result := ModelInspection{SessionID: ID(value.SessionID), AttemptID: ID(value.AttemptID), TurnID: ID(value.TurnID), RequestDigest: value.RequestDigest}
	if value.Compaction != nil {
		result.Compaction = new(CompactionFromDomain(*value.Compaction))
	}
	if value.Capture != nil {
		source := value.Capture
		body := func(value session.CapturedText) CapturedText {
			result := CapturedText{Digest: value.Digest, Bytes: Counter(value.Bytes), Status: value.Status, Chunks: []ContentReference{}}
			for _, chunk := range value.Chunks {
				result.Chunks = append(result.Chunks, ContentReferenceFromDomain(chunk))
			}
			return result
		}
		result.Capture = &ModelCapture{RequestDigest: source.RequestDigest, SourceDigest: source.SourceDigest, Instructions: body(source.Instructions), Notices: body(source.Notices), Messages: []CapturedMessage{}, ToolsDigest: source.ToolsDigest, ToolsCount: source.ToolsCount, ContextComplete: source.ContextComplete}
		for _, message := range source.Messages {
			item := CapturedMessage{Role: string(message.Role), PartsDigest: message.PartsDigest, PartsCount: message.PartsCount}
			if message.ID != "" {
				item.ID = new(ID(message.ID))
			}
			result.Capture.Messages = append(result.Capture.Messages, item)
		}
	}
	return result
}
