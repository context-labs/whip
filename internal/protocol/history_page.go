package protocol

type HistoryPageParams struct {
	SessionID        ID       `json:"session_id"`
	Direction        string   `json:"direction" enum:"forward,backward"`
	Cursor           *Counter `json:"cursor,omitempty"`
	ExpectedRevision *Counter `json:"expected_revision,omitempty"`
	Limit            int      `json:"limit" min:"1" max:"100"`
}

type HistoryPageResult struct {
	Snapshot   HistorySnapshot `json:"snapshot"`
	Messages   []Message       `json:"messages"`
	NextCursor *Counter        `json:"next_cursor"`
}
