package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

const MaxPreviewBytes = 128 << 10

type presentationSlot struct {
	part session.PresentationPart
	text strings.Builder
}

// PresentationAccumulator belongs to one synchronous provider attempt. The
// runtime serializes Append and Snapshot while observing it; settlement reads it
// only after Execute has returned and no callback can still append.
type PresentationAccumulator struct {
	attempt         session.ModelAttemptID
	text, reasoning strings.Builder
	calls           map[int]*presentationCallBuffer
	slots           []*presentationSlot
	bytes           int
	truncated       bool
}
type presentationCallBuffer struct{ id, name, arguments strings.Builder }
type PresentationPreview struct {
	Text, Reasoning string
	Calls           []session.PresentationCall
	Presentation    *session.MessagePresentation
	Truncated       bool
}

func NewPresentationAccumulator(id session.ModelAttemptID) *PresentationAccumulator {
	return &PresentationAccumulator{attempt: id, calls: map[int]*presentationCallBuffer{}}
}
func (a *PresentationAccumulator) append(b *strings.Builder, value string, limit int) {
	if a.truncated {
		return
	}
	if !utf8.ValidString(value) {
		a.truncated = true
		return
	}
	n := min(len(value), MaxPreviewBytes-a.bytes, limit-b.Len())
	if n < len(value) {
		a.truncated = true
		n = prefixLength(value, n)
	}
	b.WriteString(value[:n])
	a.bytes += n
}
func prefixLength(s string, n int) int {
	for n > 0 && !utf8.ValidString(s[:n]) {
		n--
	}
	return n
}
func (a *PresentationAccumulator) slot(kind string) *presentationSlot {
	if len(a.slots) == session.MaxPresentationParts {
		a.truncated = true
		return nil
	}
	p := &presentationSlot{part: session.PresentationPart{ID: fmt.Sprintf("p%d", len(a.slots)), Type: kind}}
	a.slots = append(a.slots, p)
	return p
}
func (a *PresentationAccumulator) Append(chunk Chunk) {
	if a.truncated {
		return
	}
	if chunk.Text != "" {
		var p *presentationSlot
		if len(a.slots) > 0 && a.slots[len(a.slots)-1].part.Type == "text" {
			p = a.slots[len(a.slots)-1]
		} else {
			p = a.slot("text")
			if p == nil {
				return
			}
			p.part.Start = new(a.text.Len())
		}
		a.append(&a.text, chunk.Text, MaxPreviewBytes)
		p.part.End = new(a.text.Len())
	}
	if chunk.Reasoning != "" {
		var p *presentationSlot
		if len(a.slots) > 0 && a.slots[len(a.slots)-1].part.Type == "reasoning" {
			p = a.slots[len(a.slots)-1]
		} else {
			p = a.slot("reasoning")
			if p == nil {
				return
			}
		}
		// Explicit streamed reasoning alone enters the presentation accumulator.
		before := a.reasoning.Len()
		a.append(&a.reasoning, chunk.Reasoning, MaxPreviewBytes)
		p.text.WriteString(a.reasoning.String()[before:])
	}
	if chunk.Call != nil {
		c := chunk.Call
		if c.Index < 0 || c.Index >= session.MaxToolCalls {
			a.truncated = true
			return
		}
		b := a.calls[c.Index]
		if b == nil {
			p := a.slot("tool_call")
			if p == nil {
				return
			}
			p.part.CallIndex = new(c.Index)
			b = &presentationCallBuffer{}
			a.calls[c.Index] = b
		}
		a.append(&b.id, c.ID, 128)
		a.append(&b.name, c.Name, 64)
		a.append(&b.arguments, c.Arguments, MaxPreviewBytes)
	}
}
func (a *PresentationAccumulator) Snapshot() PresentationPreview {
	p := &session.MessagePresentation{Version: 1, AttemptID: a.attempt, Parts: []session.PresentationPart{}, Truncated: a.truncated}
	for _, slot := range a.slots {
		part := slot.part
		if part.Start != nil {
			part.Start = new(*part.Start)
		}
		if part.End != nil {
			part.End = new(*part.End)
		}
		if part.CallIndex != nil {
			part.CallIndex = new(*part.CallIndex)
		}
		if part.Type == "reasoning" {
			part.Text = strings.Clone(slot.text.String())
		}
		if part.Type == "tool_call" {
			part.CallID = strings.Clone(a.calls[*part.CallIndex].id.String())
			if session.ValidateID(part.CallID) != nil {
				part.CallID = ""
			}
		}
		p.Parts = append(p.Parts, part)
	}
	boundPresentation(p)
	result := PresentationPreview{Text: strings.Clone(a.text.String()), Reasoning: strings.Clone(a.reasoning.String()), Presentation: p, Truncated: a.truncated || p.Truncated, Calls: []session.PresentationCall{}}
	for index := range session.MaxToolCalls {
		if b := a.calls[index]; b != nil {
			result.Calls = append(result.Calls, session.PresentationCall{Index: index, ID: strings.Clone(b.id.String()), Name: strings.Clone(b.name.String()), Arguments: strings.Clone(b.arguments.String())})
		}
	}
	return result
}

// Failed retains provisional bodies only where no canonical assistant exists.
func (a *PresentationAccumulator) Failed() *session.MessagePresentation {
	snapshot := a.Snapshot()
	p := snapshot.Presentation
	if len(p.Parts) == 0 {
		return nil
	}
	for i := range p.Parts {
		part := &p.Parts[i]
		if part.Type == "text" {
			part.Text = snapshot.Text[*part.Start:*part.End]
			part.Start = nil
			part.End = nil
		}
		if part.Type == "tool_call" {
			for _, call := range snapshot.Calls {
				if call.Index == *part.CallIndex {
					part.Call = &call
					break
				}
			}
		}
	}
	boundPresentation(p)
	return p
}

// Successful uses references to canonical output. If a provider's final text
// differs from its preview, retain reasoning but use the actual canonical text;
// mark the lost ordering rather than presenting invented text as durable output.
func (a *PresentationAccumulator) Successful(parts []session.Part) *session.MessagePresentation {
	p := a.Snapshot().Presentation
	var text strings.Builder
	calls := []session.ToolCall{}
	for _, part := range parts {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
		if part.Call != nil {
			calls = append(calls, *part.Call)
		}
	}
	matches := strings.HasPrefix(text.String(), a.text.String())
	retained := p.Parts[:0]
	seen := map[string]bool{}
	for _, part := range p.Parts {
		if part.Type == "text" && !matches {
			p.Truncated = true
			continue
		}
		if part.Type == "tool_call" {
			matched := ""
			for index, call := range calls {
				if (part.CallID != "" && part.CallID == call.ID) || (part.CallID == "" && index == *part.CallIndex) {
					matched = call.ID
					break
				}
			}
			if matched == "" {
				p.Truncated = true
				continue
			}
			part.CallID = matched
			seen[matched] = true
		}
		retained = append(retained, part)
	}
	p.Parts = retained
	// IDs allocated after all observed slots cannot collide with omitted slots.
	next := len(a.slots)
	add := func(part session.PresentationPart) {
		if len(p.Parts) == session.MaxPresentationParts {
			p.Truncated = true
			return
		}
		part.ID = fmt.Sprintf("p%d", next)
		next++
		p.Parts = append(p.Parts, part)
	}
	start := 0
	if matches {
		start = a.text.Len()
	}
	if start < text.Len() {
		add(session.PresentationPart{Type: "text", Start: new(start), End: new(text.Len())})
	}
	for index, call := range calls {
		if !seen[call.ID] {
			add(session.PresentationPart{Type: "tool_call", CallIndex: new(index), CallID: call.ID})
		}
	}
	if len(p.Parts) == 0 {
		return nil
	}
	boundPresentation(p)
	return p
}

// JSON escaping counts toward the durable byte cap. Trim only display text,
// never canonical references; preserve slot identity when a body is shortened.
func boundPresentation(p *session.MessagePresentation) {
	for {
		raw, _ := json.Marshal(p)
		if len(raw) <= session.MaxPresentationBytes {
			return
		}
		p.Truncated = true
		var longest *string
		for i := range p.Parts {
			part := &p.Parts[i]
			if longest == nil || len(part.Text) > len(*longest) {
				longest = &part.Text
			}
			if part.Call != nil && len(part.Call.Arguments) > len(*longest) {
				longest = &part.Call.Arguments
			}
		}
		if longest == nil || len(*longest) == 0 {
			p.Parts = p.Parts[:len(p.Parts)-1]
			continue
		}
		*longest = (*longest)[:prefixLength(*longest, len(*longest)/2)]
	}
}
