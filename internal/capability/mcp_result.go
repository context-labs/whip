package capability

// MCPAttachment is a binary part of a tool result (image, audio, blob
// resource). The daemon stores it as a content handle owned by the calling
// agent; Placeholder is the exact text the flattener wrote for it, so the
// handle can be spliced into that line.
type MCPAttachment struct {
	MIME        string
	Data        []byte
	Placeholder string
}

// MCPResult is a checked call's output: the text the model reads, with any
// structured content appended as JSON, plus binary parts kept as attachments
// instead of being dropped.
type MCPResult struct {
	Text        string
	Attachments []MCPAttachment
}
