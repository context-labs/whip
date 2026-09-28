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
		Revision: Counter(value.Revision), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano),
	}
}

func timeString(value *time.Time) *string {
	if value == nil {
		return nil
	}
	text := value.Format(time.RFC3339Nano)
	return &text
}

func counter(value *int64) *Counter {
	if value == nil {
		return nil
	}
	number := Counter(*value)
	return &number
}

func ModelAttemptFromDomain(value session.ModelAttempt) ModelAttempt {
	r := value.Request
	result := ModelAttempt{
		ID: ID(value.ID), TurnID: ID(value.TurnID), LogicalID: ID(value.LogicalID), Number: value.Number, State: string(value.State),
		CostNanoUSD: counter(value.CostNanoUSD), CostSource: value.CostSource, CostNote: value.CostNote, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano), DispatchedAt: timeString(value.DispatchedAt), FinishedAt: timeString(value.FinishedAt),
		Request: ModelRequestSnapshot{
			InputTokenBound: counter(r.InputTokenBound),
			Purpose:         ID(r.Purpose), Model: ModelSelection{Provider: ID(r.Model.Provider), Name: r.Model.Name, Effort: r.Model.Effort}, Route: r.Route, Adapter: ID(r.Adapter), RequestDigest: r.RequestDigest, MaxOutputTokens: Counter(r.MaxOutputTokens), TimeoutMillis: Counter(r.TimeoutMillis),
			Prices: ModelPrices{Input: counter(r.Prices.Input), Output: counter(r.Prices.Output), Reasoning: counter(r.Prices.Reasoning), CachedInput: counter(r.Prices.CachedInput), CachedOutput: counter(r.Prices.CachedOutput)},
		},
	}
	if value.MessageID != nil {
		id := ID(*value.MessageID)
		result.MessageID = &id
	}
	if value.Result != nil {
		u := value.Result.Usage
		result.Result = &ModelAttemptResult{
			ElapsedMillis: counter(value.Result.ElapsedMillis),
			State:         string(value.Result.State), Failure: value.Result.Failure, UsageNote: value.Result.UsageNote, ReportedCostNanoUSD: counter(value.Result.ReportedCostNanoUSD),
			Usage: ModelUsage{Input: counter(u.Input), Output: counter(u.Output), Reasoning: counter(u.Reasoning), CachedInput: counter(u.CachedInput), CachedOutput: counter(u.CachedOutput)},
		}
	}
	return result
}

func TurnFromDomain(value session.Turn) Turn {
	var goal *GoalRef
	if value.Goal != nil {
		goal = &GoalRef{ID: ID(value.Goal.ID), Revision: Counter(value.Goal.Revision)}
	}
	return Turn{
		Goal: goal,
		ID:   ID(value.ID), SessionID: ID(value.SessionID), Kind: string(value.Kind), ConfigRevision: Counter(value.ConfigRevision), State: string(value.State),
		Failure: value.Failure, StartedAt: value.StartedAt.Format(time.RFC3339Nano), FinishedAt: timeString(value.FinishedAt),
	}
}

func InputFromDomain(value session.Input) Input {
	result := Input{ID: ID(value.ID), SessionID: ID(value.SessionID), Source: string(value.Source), Kind: string(value.Kind), State: string(value.State), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano)}
	if value.Goal != nil {
		result.Goal = &GoalRef{ID: ID(value.Goal.ID), Revision: Counter(value.Goal.Revision)}
	}
	if value.Schedule != nil {
		result.Schedule = &ScheduleOccurrence{ScheduleID: ID(value.Schedule.ScheduleID), ScheduledFor: scheduleTime(value.Schedule.ScheduledFor)}
	}
	if value.TurnID != nil {
		id := ID(*value.TurnID)
		result.TurnID = &id
	}
	result.Parts = make([]Part, len(value.Parts))
	for i, part := range value.Parts {
		result.Parts[i] = PartFromDomain(part)
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
	if p.GoalsEnabled != nil {
		fields["goals_enabled"] = p.GoalsEnabled
	}
	if p.Compaction != nil {
		fields["compaction"] = p.Compaction
	}
	if p.ReportMode != nil {
		fields["report_mode"] = p.ReportMode
	}
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
		parts[i] = PartFromDomain(part)
	}
	result := Message{ID: ID(value.ID), SessionID: ID(value.SessionID), TurnID: ID(value.TurnID), Sequence: Counter(value.Sequence), Role: string(value.Role), Parts: parts, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano)}
	if value.InputID != nil {
		id := ID(*value.InputID)
		result.InputID = &id
	}
	if value.Mail != nil {
		result.Mail = &MailRef{ID: ID(value.Mail.ID), Revision: Counter(value.Mail.Revision), Presentation: string(value.Mail.Presentation)}
	}
	return result
}

func PartFromDomain(value session.Part) Part {
	result := Part{Type: value.Type, Text: value.Text, ReferenceID: ID(value.ReferenceID)}
	if value.Call != nil {
		result.Call = &ToolCall{ID: ID(value.Call.ID), Name: value.Call.Name, Arguments: append(json.RawMessage(nil), value.Call.Arguments...)}
	}
	if value.Result != nil {
		result.Result = &ToolResult{CallID: ID(value.Result.CallID), Output: value.Result.Output, IsError: value.Result.IsError}
	}
	return result
}

func (part Part) Domain() session.Part {
	result := session.Part{Type: part.Type, Text: part.Text, ReferenceID: string(part.ReferenceID)}
	if part.Call != nil {
		result.Call = &session.ToolCall{ID: string(part.Call.ID), Name: part.Call.Name, Arguments: append(json.RawMessage(nil), part.Call.Arguments...)}
	}
	if part.Result != nil {
		result.Result = &session.ToolResult{CallID: string(part.Result.CallID), Output: part.Result.Output, IsError: part.Result.IsError}
	}
	return result
}
