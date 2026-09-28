package protocol

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TreeFromDomain(value session.Tree) Tree {
	return Tree{
		ID: ID(value.ID), Metadata: TreeMetadata(value.Metadata), Engine: string(value.Engine),
		Policy: TreePolicy(value.Policy), Revision: Counter(value.Revision), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano),
	}
}

func timeString(value *time.Time) *string {
	if value == nil {
		return nil
	}
	text := value.Format(time.RFC3339Nano)
	return &text
}

func TurnFromDomain(value session.Turn) Turn {
	return Turn{
		ID: ID(value.ID), SessionID: ID(value.SessionID), ConfigRevision: Counter(value.ConfigRevision), State: string(value.State),
		Failure: value.Failure, StartedAt: value.StartedAt.Format(time.RFC3339Nano), FinishedAt: timeString(value.FinishedAt),
	}
}

func InputFromDomain(value session.Input) Input {
	result := Input{ID: ID(value.ID), SessionID: ID(value.SessionID), Source: string(value.Source), State: string(value.State), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano)}
	if value.TurnID != nil {
		id := ID(*value.TurnID)
		result.TurnID = &id
	}
	result.Parts = make([]Part, len(value.Parts))
	for i, part := range value.Parts {
		result.Parts[i] = Part{Type: part.Type, Text: part.Text, ReferenceID: ID(part.ReferenceID)}
	}
	return result
}

func ReceiptFromDomain(value session.Receipt) Receipt {
	result := Receipt{Identity: RequestIdentity{ClientID: ID(value.ClientID), RequestID: ID(value.RequestID)}, Digest: value.Digest, DeletedAt: timeString(value.DeletedAt), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano)}
	if value.InputID != nil {
		id := ID(*value.InputID)
		result.InputID = &id
	}
	return result
}

func DefinitionFromDomain(value session.DefinitionRevision) (Definition, error) {
	raw, err := json.Marshal(value.Document)
	if err != nil {
		return Definition{}, err
	}
	result := Definition{Ref: DefinitionRef{ID: ID(value.Ref.ID), Revision: value.Ref.Revision}, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano)}
	err = json.Unmarshal(raw, &result.Document)
	return result, err
}

func (p DefinitionDocument) Domain() (session.DefinitionDocument, error) {
	defaults, err := p.Defaults.Domain()
	return session.DefinitionDocument{ID: string(p.ID), Name: p.Name, Defaults: defaults}, err
}

// ConfigurationFromDomain copies a durable value across the wire boundary.
// Encoding is deliberate: configuration DTOs use the same documented JSON field
// names but are separate types, so no map/slice storage can alias a live caller.
func ConfigurationFromDomain(value session.Configuration) (Configuration, error) {
	var result Configuration
	raw, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}

func (p ConfigPatch) Domain() (session.ConfigPatch, error) {
	var result session.ConfigPatch
	// Maps must preserve nil versus explicitly empty. The public JSON encoder
	// below does that too; ordinary omitempty would erase a clear operation.
	raw, err := json.Marshal(p)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	return result, result.Validate()
}

func (p ConfigPatch) MarshalJSON() ([]byte, error) {
	fields := map[string]any{}
	if p.Model != nil {
		fields["model"] = p.Model
	}
	if p.Instructions != nil {
		fields["instructions"] = p.Instructions
	}
	if p.Tools != nil {
		fields["tools"] = p.Tools
	}
	if p.Children != nil {
		fields["children"] = p.Children
	}
	if p.Hooks != nil {
		fields["hooks"] = p.Hooks
	}
	if p.Output != nil {
		fields["output"] = p.Output
	}
	return json.Marshal(fields)
}

func SessionFromDomain(value session.Session) (Session, error) {
	configuration, err := ConfigurationFromDomain(value.Config)
	if err != nil {
		return Session{}, fmt.Errorf("encode session configuration: %w", err)
	}
	result := Session{
		ID: ID(value.ID), TreeID: ID(value.TreeID), Definition: DefinitionRef{ID: ID(value.Definition.ID), Revision: value.Definition.Revision},
		ConfigRevision: Counter(value.ConfigRevision), Configuration: configuration, WorkingDirectory: value.WorkingDirectory, Lifecycle: string(value.Lifecycle), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano),
	}
	if value.ParentID != nil {
		id := ID(*value.ParentID)
		result.ParentID = &id
	}
	return result, nil
}

func MessageFromDomain(value session.Message) Message {
	parts := make([]Part, len(value.Parts))
	for i, part := range value.Parts {
		parts[i] = Part{Type: part.Type, Text: part.Text, ReferenceID: ID(part.ReferenceID)}
	}
	result := Message{ID: ID(value.ID), SessionID: ID(value.SessionID), TurnID: ID(value.TurnID), Sequence: Counter(value.Sequence), Role: string(value.Role), Parts: parts, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano)}
	if value.InputID != nil {
		id := ID(*value.InputID)
		result.InputID = &id
	}
	return result
}
