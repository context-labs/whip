package protocol

import (
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/session"
)

type Checkpoint struct {
	Digest   string          `json:"digest" pattern:"^[a-f0-9]{64}$"`
	Size     Counter         `json:"size"`
	Engine   string          `json:"engine" enum:"starlark,quickjs"`
	Metadata json.RawMessage `json:"metadata"`
}
type Cell struct {
	ID              ID          `json:"id"`
	SessionID       ID          `json:"session_id"`
	TurnID          ID          `json:"turn_id"`
	CallMessageID   ID          `json:"call_message_id"`
	CallID          ID          `json:"call_id"`
	State           string      `json:"state" enum:"running,succeeded,failed,uncertain"`
	ResultMessageID *ID         `json:"result_message_id"`
	Checkpoint      *Checkpoint `json:"checkpoint"`
	CreatedAt       string      `json:"created_at"`
	FinishedAt      *string     `json:"finished_at"`
}
type CellParams struct {
	CellID ID `json:"cell_id"`
}
type CellsParams struct {
	TurnID ID  `json:"turn_id"`
	After  *ID `json:"after,omitempty"`
	Limit  int `json:"limit" min:"1" max:"100"`
}
type CellsResult struct {
	Items []Cell `json:"items"`
}

func CellFromDomain(value session.Cell) Cell {
	result := Cell{ID: ID(value.ID), SessionID: ID(value.SessionID), TurnID: ID(value.TurnID), CallMessageID: ID(value.CallMessageID), CallID: ID(value.CallID), State: string(value.State), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano), FinishedAt: timeString(value.FinishedAt)}
	if value.ResultMessageID != nil {
		result.ResultMessageID = new(ID(*value.ResultMessageID))
	}
	if value.Checkpoint != nil {
		result.Checkpoint = &Checkpoint{Digest: value.Checkpoint.Digest, Size: Counter(value.Checkpoint.Size), Engine: string(value.Checkpoint.Engine), Metadata: append(json.RawMessage(nil), value.Checkpoint.Metadata...)}
	}
	return result
}
