package protocol

type CompletionParams struct {
	RootID  string `json:"root_id"`
	AgentID string `json:"agent_id,omitempty"`
	Kind    string `json:"kind"`
	Prefix  string `json:"prefix"`
	Limit   int    `json:"limit"`
}

type CompletionCandidate struct {
	Text        string `json:"text"`
	Description string `json:"description"`
}

type CompletionResult struct {
	Warnings   []string              `json:"warnings,omitempty"`
	Candidates []CompletionCandidate `json:"candidates"`
	Truncated  bool                  `json:"truncated"`
}
