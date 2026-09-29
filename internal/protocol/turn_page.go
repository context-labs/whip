package protocol

import "github.com/context-labs/whip/internal/session"

type TurnPageParams struct {
	SessionID ID  `json:"session_id"`
	Before    *ID `json:"before,omitempty"`
	Limit     int `json:"limit" min:"1" max:"100"`
}

type TurnPageResult struct {
	Items      []Turn `json:"items"`
	NextCursor *ID    `json:"next_cursor"`
}

func TurnPageFromDomain(page session.TurnPage) TurnPageResult {
	result := TurnPageResult{Items: []Turn{}}
	for _, turn := range page.Items {
		result.Items = append(result.Items, TurnFromDomain(turn))
	}
	if page.NextCursor != nil {
		result.NextCursor = new(ID(*page.NextCursor))
	}
	return result
}
