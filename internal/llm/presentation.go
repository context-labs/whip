package llm

import (
	"encoding/json"
	"slices"
	"unicode/utf8"
)

// TranscriptPresentation is bounded, observational UI history. It never enters
// a provider request. Text ranges refer to UTF-8 bytes in Message.Content.
type TranscriptPresentation struct {
	Version int                `json:"version"`
	TurnID  string             `json:"turn_id,omitempty"`
	Parts   []PresentationPart `json:"parts,omitempty"`
	Omitted int                `json:"omitted,omitempty"`
}

type PresentationPart struct {
	ID       string             `json:"id"`
	Kind     string             `json:"kind"`
	Start    int                `json:"start,omitempty"`
	End      int                `json:"end,omitempty"`
	Text     string             `json:"text,omitempty"`
	ToolName string             `json:"tool_name,omitempty"`
	Status   string             `json:"status,omitempty"`
	CallID   string             `json:"call_id,omitempty"`
	Hosts    []PresentationHost `json:"hosts,omitempty"`
	Omitted  int                `json:"omitted,omitempty"`
}

type OperationDisplay struct {
	Target  string `json:"target,omitempty"`
	Command string `json:"command,omitempty"`
	Query   string `json:"query,omitempty"`
	ChildID string `json:"child_id,omitempty"`
	Label   string `json:"label,omitempty"`
}

type PresentationHost struct {
	InvocationID string            `json:"invocation_id"`
	Name         string            `json:"name"`
	Summary      string            `json:"summary,omitempty"`
	Status       string            `json:"status"`
	Duration     string            `json:"duration,omitempty"`
	Error        string            `json:"error,omitempty"`
	Display      *OperationDisplay `json:"display,omitempty"`
}

// PresentationExcerpt bounds bytes without splitting UTF-8. The capture path
// also bounds each field before retaining it, not just before serialization.
func PresentationExcerpt(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

// BoundPresentation returns an independent value: journal readers may hold an
// earlier version while another operation completes. Omission is always explicit.
func BoundPresentation(value *TranscriptPresentation, maxBytes int) *TranscriptPresentation {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Parts = slices.Clone(value.Parts)
	for i := range copy.Parts {
		copy.Parts[i].Hosts = slices.Clone(copy.Parts[i].Hosts)
		if maxBytes < 64<<10 {
			part := &copy.Parts[i]
			if len(part.Text) > 128 {
				part.Text = PresentationExcerpt(part.Text, 128)
				part.Omitted++
			}
			for j := range part.Hosts {
				host := &part.Hosts[j]
				if host.Summary != "" || host.Error != "" || host.Display != nil {
					part.Omitted++
				}
				host.Summary, host.Error = "", ""
				if host.Display != nil {
					host.Display = &OperationDisplay{ChildID: host.Display.ChildID}
				}
			}
		}
	}
	if len(copy.Parts) > 128 {
		copy.Omitted += len(copy.Parts) - 128
		copy.Parts = copy.Parts[len(copy.Parts)-128:]
	}
	for i := range copy.Parts {
		part := &copy.Parts[i]
		if len(part.Hosts) > 128 {
			part.Omitted += len(part.Hosts) - 128
			part.Hosts = part.Hosts[len(part.Hosts)-128:]
		}
	}
	for {
		encoded, _ := json.Marshal(copy)
		if len(encoded) <= maxBytes || len(copy.Parts) == 0 {
			break
		}
		// Keep identities and operation outcomes before long reasoning previews.
		trimmed := false
		for i := range copy.Parts {
			part := &copy.Parts[i]
			if len(part.Text) > 512 {
				part.Text = PresentationExcerpt(part.Text, max(512, len(part.Text)/2))
				part.Omitted++
				trimmed = true
				break
			}
			if len(part.Hosts) > 0 {
				part.Hosts = part.Hosts[1:]
				part.Omitted++
				trimmed = true
				break
			}
		}
		if !trimmed {
			copy.Parts = copy.Parts[1:]
			copy.Omitted++
		}
	}
	return &copy
}
