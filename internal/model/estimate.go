package model

import "math"

// EstimateInputTokens is a planning heuristic, not a tokenizer, usage report,
// reservation bound or proof that a provider will accept or reject a request.
// Text uses roughly four bytes per token plus message/tool framing. Binary
// content uses its approximate base64 size at four bytes per token, with a
// 1200-token floor also used for missing content. Compressed image bytes do not
// establish their provider token cost. Each referenced occurrence is counted;
// unreferenced hydrated content is omitted. The function performs no I/O or
// mutation and saturates at MaxInt64. Provider preparation still validates and
// requires all content to have been authorized and hydrated.
func EstimateInputTokens(request Request) int64 {
	var total int64
	add := func(value int64) { total = addTokenEstimate(total, value) }
	text := func(value string) { add(estimateByteTokens(len(value), 4)) }
	if request.Instructions != "" {
		add(4)
		text("system")
		text(request.Instructions)
	}
	for _, message := range request.Messages {
		add(4)
		text(string(message.Role))
		for _, part := range message.Parts {
			switch part.Type {
			case "text":
				text(part.Text)
			case "tool_call":
				add(8)
				if part.Call != nil {
					text(part.Call.ID)
					text(part.Call.Name)
					add(estimateByteTokens(len(part.Call.Arguments), 4))
				}
			case "tool_result":
				add(4)
				if part.Result != nil {
					text(part.Result.CallID)
					text(part.Result.Output)
				}
			case "content":
				add(4)
				content := request.Contents[part.ReferenceID]
				text(content.MediaType)
				if content.MediaType == "text/plain" {
					add(estimateByteTokens(len(content.Data), 4))
				} else {
					add(max(1200, estimateByteTokens(len(content.Data), 3)))
				}
			}
		}
	}
	for _, tool := range request.Tools {
		add(8)
		text(tool.Name)
		text(tool.Description)
		add(estimateByteTokens(len(tool.InputSchema), 4))
	}
	return total
}

func estimateByteTokens(size, bytesPerToken int) int64 {
	value := int64(size / bytesPerToken)
	if size%bytesPerToken != 0 {
		value++
	}
	return value
}

func addTokenEstimate(total, value int64) int64 {
	if value > math.MaxInt64-total {
		return math.MaxInt64
	}
	return total + value
}
