package protocol

import "github.com/context-labs/whip/internal/session"

type CellPageParams struct {
	TurnID ID  `json:"turn_id"`
	Before *ID `json:"before,omitempty"`
	Limit  int `json:"limit" min:"1" max:"100"`
}
type CellPageResult struct {
	Items      []Cell `json:"items"`
	NextCursor *ID    `json:"next_cursor"`
}

func CellPageFromDomain(page session.CellPage) CellPageResult {
	result := CellPageResult{Items: []Cell{}}
	for _, cell := range page.Items {
		result.Items = append(result.Items, CellFromDomain(cell))
	}
	if page.NextCursor != nil {
		result.NextCursor = new(ID(*page.NextCursor))
	}
	return result
}
