package daemon

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
)

// All capture helpers run under the session mutex. Published metadata is
// immutable; live model messages never acquire a reference to journal state.
func (node *AgentSession) presentationPart(kind, callID, delta string) string {
	node.mu.Lock()
	defer node.mu.Unlock()
	p := llm.BoundPresentation(node.turn.Presentation, 64<<10)
	if p == nil {
		p = &llm.TranscriptPresentation{Version: 1, TurnID: node.turn.TurnID}
	}
	index := -1
	if kind == "tool" {
		index = slices.IndexFunc(p.Parts, func(part llm.PresentationPart) bool { return part.Kind == kind && part.CallID == callID })
	} else if last := len(p.Parts) - 1; last >= 0 && p.Parts[last].Kind == kind {
		index = last
	}
	if index < 0 {
		node.turn.PresentationSerial++
		p.Parts = append(p.Parts, llm.PresentationPart{ID: fmt.Sprintf("%s:p%d", node.turn.TurnID, node.turn.PresentationSerial), Kind: kind, CallID: callID, Start: node.turn.PresentationTextBytes})
		index = len(p.Parts) - 1
	}
	part := &p.Parts[index]
	if kind == "text" {
		node.turn.PresentationProse += llm.PresentationExcerpt(delta, max(0, (48<<10)-len(node.turn.PresentationProse)))
		node.turn.PresentationTextBytes += len(delta)
		part.End = node.turn.PresentationTextBytes
	}
	if kind == "reasoning" {
		remaining := max(0, (48<<10)-len(part.Text))
		part.Text += llm.PresentationExcerpt(delta, remaining)
		if len(delta) > remaining {
			part.Omitted++
		}
	}
	id := part.ID
	node.turn.Presentation = llm.BoundPresentation(p, 64<<10)
	return id
}

func (node *AgentSession) discardPresentation() {
	node.mu.Lock()
	defer node.mu.Unlock()
	node.turn.Presentation, node.turn.PresentationTextBytes = nil, 0
	node.turn.PresentationProse = ""
}

func (node *AgentSession) toolPresentationID(callID string) string {
	node.mu.Lock()
	defer node.mu.Unlock()
	for _, message := range slices.Backward(node.turn.Messages) {
		if message.Role != "assistant" {
			continue
		}
		if message.Presentation != nil {
			for _, part := range message.Presentation.Parts {
				if part.CallID == callID {
					return part.ID
				}
			}
		}
		break
	}
	return ""
}

func (node *AgentSession) recordHostPresentation(call rlm.HostCall) string {
	node.mu.Lock()
	defer node.mu.Unlock()
	for i := len(node.turn.Messages) - 1; i >= 0; i-- {
		message := &node.turn.Messages[i]
		if message.Role != "assistant" {
			continue
		}
		p := llm.BoundPresentation(message.Presentation, 64<<10)
		if p == nil {
			p = &llm.TranscriptPresentation{Version: 1, TurnID: node.turn.TurnID}
		}
		index := slices.IndexFunc(p.Parts, func(part llm.PresentationPart) bool { return part.Kind == "tool" && part.CallID == call.CallID })
		if index < 0 {
			if call.Status != "" {
				return ""
			}
			if !slices.ContainsFunc(message.ToolCalls, func(tool llm.ToolCall) bool { return tool.ID == call.CallID }) {
				return ""
			}
			node.turn.PresentationSerial++
			p.Parts = append(p.Parts, llm.PresentationPart{ID: fmt.Sprintf("%s:p%d", node.turn.TurnID, node.turn.PresentationSerial), Kind: "tool", CallID: call.CallID})
			index = len(p.Parts) - 1
		}
		part := &p.Parts[index]
		status := call.Status
		if status == "" {
			status = "running"
		}
		host := llm.PresentationHost{
			InvocationID: call.InvocationID, Name: call.Module + "." + call.Operation,
			Summary: llm.PresentationExcerpt(call.Summary, 2048), Status: status, Display: call.Display,
			Error: llm.PresentationExcerpt(call.Err, 2048),
		}
		if call.Status != "" {
			host.Duration = call.Duration.String()
		}
		if existing := slices.IndexFunc(part.Hosts, func(h llm.PresentationHost) bool { return h.InvocationID == host.InvocationID }); existing >= 0 {
			part.Hosts[existing] = host
		} else if call.Status == "" {
			part.Hosts = append(part.Hosts, host)
		} else {
			// Its start was outside the retained window. Do not invent a new
			// chronological position when a parallel invocation settles late.
			return part.ID
		}
		id := part.ID
		message.Presentation = llm.BoundPresentation(p, 64<<10)
		return id
	}
	return ""
}

func (node *AgentSession) finishPresentation() {
	node.mu.Lock()
	defer node.mu.Unlock()
	if pending := node.turn.Presentation; pending != nil && len(node.turn.Messages) > 0 {
		message := &node.turn.Messages[len(node.turn.Messages)-1]
		p := llm.BoundPresentation(message.Presentation, 64<<10)
		if p == nil {
			p = &llm.TranscriptPresentation{Version: 1, TurnID: node.turn.TurnID}
		}
		// Without an assistant record, the bounded pending source is the only
		// copy. Append it after the last record; offsets no longer refer to that
		// record's body (which may be authored input or a tool result).
		for _, part := range pending.Parts {
			if part.Kind == "text" {
				prose := node.turn.PresentationProse
				part.Text = prose[min(part.Start, len(prose)):min(part.End, len(prose))]
				if part.End > len(prose) {
					part.Omitted++
				}
				part.Start, part.End = 0, 0
			}
			if part.Kind == "reasoning" || part.Kind == "text" {
				p.Parts = append(p.Parts, part)
			} else {
				p.Omitted++
			}
		}
		p.Omitted += pending.Omitted
		message.Presentation = llm.BoundPresentation(p, 64<<10)
	}
	node.turn.Presentation = nil
	node.turn.PresentationProse = ""
	for i := range node.turn.Messages {
		message := &node.turn.Messages[i]
		p := llm.BoundPresentation(message.Presentation, 64<<10)
		if p == nil {
			continue
		}
		for j := range p.Parts {
			for k := range p.Parts[j].Hosts {
				host := &p.Parts[j].Hosts[k]
				if host.Status == "running" {
					host.Status = "unknown"
				}
			}
		}
		message.Presentation = p
	}
}

// attachPresentationLocked transfers the observed ordering to canonical prose.
func (node *AgentSession) attachPresentationLocked(message *llm.Message) {
	if message.Role == "tool" && message.ToolCallID != "" {
		// Identity and outcome stay inline even when the result itself needs a
		// scoped content handle. The result body remains the sole source of output.
		node.turn.PresentationSerial++
		status := "unknown"
		if strings.HasPrefix(message.Content, "Error:") {
			status = "failed"
		} else {
			var result struct {
				FormatVersion int  `json:"format_version"`
				Steps         *int `json:"steps"`
			}
			if json.Unmarshal([]byte(message.Content), &result) == nil && (result.FormatVersion == 2 || result.Steps != nil) {
				status = "completed"
			}
		}
		message.Presentation = &llm.TranscriptPresentation{Version: 1, TurnID: node.turn.TurnID, Parts: []llm.PresentationPart{{
			ID: fmt.Sprintf("%s:p%d", node.turn.TurnID, node.turn.PresentationSerial), Kind: "result", CallID: message.ToolCallID, ToolName: message.Name, Status: status,
		}}}
		return
	}
	if message.Role != "assistant" {
		return
	}
	p := llm.BoundPresentation(node.turn.Presentation, 64<<10)
	if p == nil {
		p = &llm.TranscriptPresentation{Version: 1, TurnID: node.turn.TurnID}
	}
	for i := range p.Parts {
		part := &p.Parts[i]
		if part.Kind == "text" && (part.Start > len(message.Content) || part.End > len(message.Content)) {
			part.Start, part.End = 0, 0
			p.Omitted++
		}
	}
	// Providers may return content without text callbacks (e.g. a final fallback).
	if strings.TrimSpace(message.Content) != "" && node.turn.PresentationTextBytes != len(message.Content) {
		p.Parts = slices.DeleteFunc(p.Parts, func(part llm.PresentationPart) bool { return part.Kind == "text" })
		node.turn.PresentationSerial++
		p.Parts = append(p.Parts, llm.PresentationPart{ID: fmt.Sprintf("%s:p%d", node.turn.TurnID, node.turn.PresentationSerial), Kind: "text", End: len(message.Content)})
	}
	for _, call := range message.ToolCalls {
		if index := slices.IndexFunc(p.Parts, func(part llm.PresentationPart) bool { return part.Kind == "tool" && part.CallID == call.ID }); index >= 0 {
			p.Parts[index].ToolName = call.Function.Name
			continue
		}
		node.turn.PresentationSerial++
		p.Parts = append(p.Parts, llm.PresentationPart{ID: fmt.Sprintf("%s:p%d", node.turn.TurnID, node.turn.PresentationSerial), Kind: "tool", CallID: call.ID, ToolName: call.Function.Name})
	}
	message.Presentation = llm.BoundPresentation(p, 64<<10)
	node.turn.Presentation, node.turn.PresentationTextBytes = nil, 0
	node.turn.PresentationProse = ""
}
