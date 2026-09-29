package protocol

import (
	"github.com/context-labs/whip/internal/session"
	"github.com/google/jsonschema-go/jsonschema"
	"reflect"
)

// Text ranges use UTF-8 byte offsets in concatenated canonical text parts, or
// MessagePreview.text while provisional. IDs are local to the source attempt;
// imported presentation is display evidence only, never local execution authority.
type MessagePresentation struct {
	Version   int                `json:"version" min:"1" max:"1"`
	AttemptID ID                 `json:"attempt_id"`
	Parts     []PresentationPart `json:"parts" maxItems:"128"`
	Truncated bool               `json:"truncated"`
}
type PresentationPart struct {
	ID        ID           `json:"id"`
	Type      string       `json:"type" enum:"text,reasoning,tool_call"`
	Text      string       `json:"text,omitempty"`
	Start     *int         `json:"start,omitempty" min:"0" max:"1048576"`
	End       *int         `json:"end,omitempty" min:"0" max:"1048576"`
	CallIndex *int         `json:"call_index,omitempty" min:"0" max:"15"`
	CallID    *ID          `json:"call_id,omitempty"`
	Call      *CallPreview `json:"call,omitempty"`
}
type AttemptPresentation struct {
	GroupID         ID                  `json:"group_id"`
	SourceSessionID *ID                 `json:"source_session_id,omitempty"`
	AttemptID       ID                  `json:"attempt_id"`
	TurnID          ID                  `json:"turn_id"`
	MessageID       *ID                 `json:"message_id,omitempty"`
	State           string              `json:"state" enum:"failed,cancelled,uncertain"`
	Presentation    MessagePresentation `json:"presentation"`
}

func PresentationFromDomain(v *session.MessagePresentation) *MessagePresentation {
	if v == nil {
		return nil
	}
	r := &MessagePresentation{Version: v.Version, AttemptID: ID(v.AttemptID), Parts: []PresentationPart{}, Truncated: v.Truncated}
	for _, p := range v.Parts {
		part := PresentationPart{ID: ID(p.ID), Type: p.Type, Text: p.Text, Start: p.Start, End: p.End, CallIndex: p.CallIndex}
		if p.CallID != "" {
			part.CallID = new(ID(p.CallID))
		}
		if p.Call != nil {
			part.Call = &CallPreview{Index: p.Call.Index, ID: p.Call.ID, Name: p.Call.Name, Arguments: p.Call.Arguments}
		}
		r.Parts = append(r.Parts, part)
	}
	return r
}
func AttemptPresentationFromDomain(v session.AttemptPresentation) AttemptPresentation {
	var source *ID
	if v.SourceSessionID != nil {
		source = new(ID(*v.SourceSessionID))
	}
	return AttemptPresentation{GroupID: ID(v.GroupID), SourceSessionID: source, AttemptID: ID(v.AttemptID), TurnID: ID(v.TurnID), MessageID: localID(string(v.MessageID)), State: string(v.State), Presentation: *PresentationFromDomain(v.Presentation)}
}

func presentationSchema(schema *jsonschema.Schema, t reflect.Type) {
	switch t {
	case reflect.TypeFor[MessagePresentation]():
		parts := schema.Properties["parts"]
		parts.Type = "array"
		parts.Types = nil
		parts.MaxItems = new(session.MaxPresentationParts)
	case reflect.TypeFor[SessionObservation](), reflect.TypeFor[HistoryPageResult]():
		schema.Properties["attempt_presentations"].MaxItems = new(64)
	case reflect.TypeFor[PresentationPart]():
		// Native output references and failed provisional bodies are distinct shapes.
		variant := func(kind string, fields, required []string) *jsonschema.Schema {
			p := map[string]*jsonschema.Schema{"id": schema.Properties["id"].CloneSchemas(), "type": {Type: "string", Enum: []any{kind}}}
			for _, field := range fields {
				p[field] = schema.Properties[field].CloneSchemas()
			}
			return &jsonschema.Schema{Type: "object", Properties: p, Required: append([]string{"id", "type"}, required...), AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}}
		}
		*schema = jsonschema.Schema{OneOf: []*jsonschema.Schema{
			variant("reasoning", []string{"text"}, nil),
			variant("text", []string{"start", "end"}, []string{"start", "end"}),
			variant("text", []string{"text"}, nil),
			variant("tool_call", []string{"call_index", "call_id", "call"}, []string{"call_index"}),
		}}
	}
}
