package protocol

import (
	"reflect"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/google/jsonschema-go/jsonschema"
)

type AutomaticTitleResultParams struct {
	TreeID    ID `json:"tree_id"`
	AttemptID ID `json:"attempt_id"`
}

// AutomaticTitleDecision is immutable naming intent, never current work status.
// ReceiptIdentity permits ordinary inspection once maintenance has been admitted.
type AutomaticTitleDecision struct {
	TreeID           ID               `json:"tree_id"`
	SessionID        ID               `json:"session_id"`
	InputID          *ID              `json:"input_id"`
	ReceiptIdentity  *RequestIdentity `json:"receipt_identity"`
	ConfigRevision   Counter          `json:"config_revision"`
	ExpectedRevision Counter          `json:"expected_revision"`
	Enabled          bool             `json:"enabled"`
	Model            ModelSelection   `json:"model"`
	Source           string           `json:"source"`
	Reason           string           `json:"reason" enum:"eligible,disabled,short,manual,ineligible,fork"`
	CreatedAt        string           `json:"created_at" format:"date-time"`
}

// AutomaticTitleResult records historical CAS application. Tree metadata alone
// owns the selected title, including any later manual replacement or clear.
type AutomaticTitleResult struct {
	TreeID    ID     `json:"tree_id"`
	AttemptID ID     `json:"attempt_id"`
	Text      string `json:"text"`
	Applied   bool   `json:"applied"`
	CreatedAt string `json:"created_at" format:"date-time"`
}

func AutomaticTitleDecisionFromDomain(value session.AutomaticTitleDecision) AutomaticTitleDecision {
	selection := value.Model.Clone()
	result := AutomaticTitleDecision{
		TreeID: ID(value.TreeID), SessionID: ID(value.SessionID), ConfigRevision: Counter(value.ConfigRevision),
		ExpectedRevision: Counter(value.ExpectedRevision), Enabled: value.Enabled, Source: value.Source,
		Model:  ModelSelection{Provider: ID(selection.Provider), Name: selection.Name, Effort: selection.Effort, Temperature: selection.Temperature, TopP: selection.TopP},
		Reason: value.Reason, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano),
	}
	if value.InputID != nil {
		result.InputID = new(ID(*value.InputID))
	}
	if value.Reason == "eligible" {
		identity := session.AutomaticTitleIdentity(value.TreeID)
		result.ReceiptIdentity = &RequestIdentity{ClientID: ID(identity.ClientID), RequestID: ID(identity.RequestID)}
	}
	return result
}

func AutomaticTitleResultFromDomain(value session.AutomaticTitleResult) AutomaticTitleResult {
	return AutomaticTitleResult{
		TreeID: ID(value.TreeID), AttemptID: ID(value.AttemptID), Text: value.Text,
		Applied: value.Applied, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano),
	}
}

func automaticTitleSchema(schema *jsonschema.Schema, t reflect.Type) {
	switch t {
	case reflect.TypeFor[AutomaticTitleDecision]():
		schema.Properties["source"].Pattern = `^[\s\S]{0,300}$`
	case reflect.TypeFor[AutomaticTitleResult]():
		schema.Properties["text"].Pattern = `^[^\x00-\x1f\x7f-\x9f` + "\u2028\u2029" + `]{1,80}$`
		schema.Properties["text"].Not = &jsonschema.Schema{Pattern: `^[\s\p{Z}]|[\s\p{Z}]$`}
	}
}
