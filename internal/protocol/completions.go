package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

// CompletionMetadata describes an outcome awaiting automatic mail publication.
// Reading it neither delivers nor acknowledges the report.
type CompletionMetadata struct {
	ParentID     ID      `json:"parent_id"`
	ChildID      ID      `json:"child_id"`
	TurnID       ID      `json:"turn_id"`
	InputID      *ID     `json:"input_id"`
	MessageID    *ID     `json:"message_id"`
	State        string  `json:"state" enum:"succeeded,failed,cancelled,interrupted"`
	Failure      *string `json:"failure"`
	Mode         string  `json:"mode" enum:"notice,inline,message"`
	FinishedAt   string  `json:"finished_at"`
	TextBytes    Counter `json:"text_bytes"`
	OmittedParts Counter `json:"omitted_parts"`
}

type ListCompletionsParams struct {
	ParentID ID  `json:"parent_id"`
	After    *ID `json:"after,omitempty"`
	Limit    int `json:"limit" min:"1" max:"100"`
}

type ListCompletionsResult struct {
	Items []CompletionMetadata `json:"items"`
}

type ReadCompletionParams struct {
	ParentID ID      `json:"parent_id"`
	ChildID  ID      `json:"child_id"`
	TurnID   ID      `json:"turn_id"`
	Offset   Counter `json:"offset"`
	Length   int     `json:"length" min:"1" max:"65536"`
}

type ReadCompletionResult struct {
	Completion CompletionMetadata `json:"completion"`
	Offset     Counter            `json:"offset"`
	TotalBytes Counter            `json:"total_bytes"`
	DataBase64 string             `json:"data_base64"`
	NextOffset *Counter           `json:"next_offset"`
}

func CompletionFromDomain(v session.CompletionMetadata) CompletionMetadata {
	result := CompletionMetadata{
		ParentID: ID(v.ParentID), ChildID: ID(v.ChildID), TurnID: ID(v.TurnID),
		State: string(v.State), Failure: v.Failure, Mode: string(v.Mode),
		FinishedAt: v.FinishedAt.Format(time.RFC3339Nano), TextBytes: Counter(v.TextBytes), OmittedParts: Counter(v.OmittedParts),
	}
	if v.InputID != nil {
		result.InputID = new(ID(*v.InputID))
	}
	if v.MessageID != nil {
		result.MessageID = new(ID(*v.MessageID))
	}
	return result
}
