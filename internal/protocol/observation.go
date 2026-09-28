package protocol

// MessagePreview is explicitly provisional. Arguments are incomplete text, not
// executable ToolCall JSON. MessageID is the eventual committed replacement key.
type MessagePreview struct {
	AttemptID ID            `json:"attempt_id"`
	TurnID    ID            `json:"turn_id"`
	MessageID ID            `json:"message_id"`
	Revision  Counter       `json:"revision"`
	Text      string        `json:"text"`
	Calls     []CallPreview `json:"calls"`
	Truncated bool          `json:"truncated"`
}
type CallPreview struct {
	Index     int    `json:"index" min:"0" max:"15"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type SessionObservation struct {
	Epoch    ID              `json:"epoch"`
	Messages []Message       `json:"messages"`
	Preview  *MessagePreview `json:"preview"`
}
