package protocol

type ContentReadParams struct {
	RootID      string `json:"root_id"`
	AgentID     string `json:"agent_id,omitempty"`
	ReferenceID string `json:"reference_id"`
	Offset      int64  `json:"offset,string"`
	Limit       int    `json:"limit"`
}

type ContentReadResult struct {
	Data    []byte        `json:"data"`
	Content ContentHandle `json:"content"`
}
