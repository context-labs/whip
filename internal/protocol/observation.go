package protocol

// MessagePreview is explicitly provisional. Arguments are incomplete text, not
// executable ToolCall JSON. Explicit reasoning is retained in bounded settlement presentation.
// MessageID is the eventual committed replacement key.
type MessagePreview struct {
	Presentation *MessagePresentation `json:"presentation,omitempty"`
	AttemptID    ID                   `json:"attempt_id"`
	TurnID       ID                   `json:"turn_id"`
	MessageID    ID                   `json:"message_id"`
	Revision     Counter              `json:"revision"`
	Text         string               `json:"text"`
	Reasoning    string               `json:"reasoning"`
	Calls        []CallPreview        `json:"calls"`
	Truncated    bool                 `json:"truncated"`
}
type CallPreview struct {
	Index     int    `json:"index" min:"0" max:"15"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type SessionObservation struct {
	AttemptPresentations          []AttemptPresentation `json:"attempt_presentations,omitempty" maxItems:"64"`
	AttemptPresentationsTruncated bool                  `json:"attempt_presentations_truncated,omitempty"`
	Snapshot                      HistorySnapshot       `json:"snapshot"`
	Epoch                         ID                    `json:"epoch"`
	Messages                      []Message             `json:"messages"`
	Preview                       *MessagePreview       `json:"preview"`
}
