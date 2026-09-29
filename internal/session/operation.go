package session

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"
)

type (
	OperationID     string
	GrantID         string
	OperationState  string
	PermissionState string
)

const (
	OperationWaiting    OperationState  = "waiting"
	OperationReady      OperationState  = "ready"
	OperationDispatched OperationState  = "dispatched"
	OperationSucceeded  OperationState  = "succeeded"
	OperationFailed     OperationState  = "failed"
	OperationDenied     OperationState  = "denied"
	OperationCancelled  OperationState  = "cancelled"
	OperationUncertain  OperationState  = "uncertain"
	PermissionPending   PermissionState = "pending"
	PermissionApproved  PermissionState = "approved"
	PermissionDenied    PermissionState = "denied"
	PermissionCancelled PermissionState = "cancelled"
)

const (
	MaxOperationsPerTurn = 1024
	MaxGrantsPerSession  = 1024
)

// Grant authorizes exactly one session, capability and resource. A one-use
// approval is additionally bound to an operation. Revocation never changes scope.
type Grant struct {
	ID          GrantID
	SessionID   SessionID
	Capability  string
	Resource    string
	OperationID *OperationID
	// IssuerID is the exact standing grant held by this session's direct parent.
	IssuerID  *GrantID
	CreatedAt time.Time
	RevokedAt *time.Time
}

type OperationSpec struct {
	ID           OperationID
	CellID       CellID
	DirectTurnID TurnID
	RequestID    string
	Capability   string
	Resource     string
	Arguments    json.RawMessage
}

// Operation owns host-effect evidence; its session and turn are projections of
// its cell or accepted direct host turn. A dispatched but unsettled operation is never automatically replayed.
type Operation struct {
	OperationSpec
	SessionID SessionID
	TurnID    TurnID
	State     OperationState
	GrantID   *GrantID
	// PermissionRevision captures root automatic authority at admission.
	PermissionRevision *Revision
	Result             *OperationResult
	CreatedAt          time.Time
	DispatchedAt       *time.Time
	FinishedAt         *time.Time
}

const (
	MaxOperationAttachments           = 8
	MaxOperationAttachmentBytes int64 = 16 << 20
)

type OperationResult struct {
	ContentReferences []string        `json:"content_references,omitempty"`
	State             OperationState  `json:"state"`
	Value             json.RawMessage `json:"value,omitempty"`
	Failure           *string         `json:"failure,omitempty"`
}

type Permission struct {
	OperationID OperationID
	State       PermissionState
	CreatedAt   time.Time
	ResolvedAt  *time.Time
}

var capabilityName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*(\.[a-zA-Z][a-zA-Z0-9_-]*)*$`)

func validateOperationScope(capability, resource string) error {
	if len(capability) > 128 || !capabilityName.MatchString(capability) {
		return fmt.Errorf("%w: invalid capability name", ErrInvalid)
	}
	if err := ValidateText(resource, 4096); err != nil {
		return err
	}
	if !utf8.ValidString(resource) {
		return fmt.Errorf("%w: resource is not UTF-8", ErrInvalid)
	}
	return nil
}

func (g Grant) Validate() error {
	for _, id := range []string{string(g.ID), string(g.SessionID)} {
		if err := ValidateID(id); err != nil {
			return err
		}
	}
	if g.OperationID != nil {
		if err := ValidateID(string(*g.OperationID)); err != nil {
			return err
		}
	}
	if g.IssuerID != nil {
		if err := ValidateID(string(*g.IssuerID)); err != nil {
			return err
		}
		if g.OperationID != nil || *g.IssuerID == g.ID {
			return fmt.Errorf("%w: delegated grants require a distinct standing issuer", ErrInvalid)
		}
	}
	return validateOperationScope(g.Capability, g.Resource)
}

func (s OperationSpec) Validate() error {
	for _, id := range []string{string(s.ID), s.RequestID} {
		if err := ValidateID(id); err != nil {
			return err
		}
	}
	if (s.CellID == "") == (s.DirectTurnID == "") {
		return fmt.Errorf("%w: exactly one operation execution owner is required", ErrInvalid)
	}
	owner := string(s.CellID)
	if s.DirectTurnID != "" {
		owner = string(s.DirectTurnID)
	}
	if err := ValidateID(owner); err != nil {
		return err
	}
	if err := validateOperationScope(s.Capability, s.Resource); err != nil {
		return err
	}
	var object map[string]json.RawMessage
	if len(s.Arguments) > MaxDocumentBytes || !utf8.Valid(s.Arguments) || json.Unmarshal(s.Arguments, &object) != nil || object == nil {
		return fmt.Errorf("%w: operation arguments require a bounded JSON object", ErrInvalid)
	}
	raw, err := json.Marshal(s.Arguments)
	if err != nil {
		return err
	}
	if len(raw) > MaxDocumentBytes {
		return fmt.Errorf("%w: encoded operation arguments exceed size limit", ErrInvalid)
	}
	return nil
}

func (s OperationState) Terminal() bool {
	return s == OperationSucceeded || s == OperationFailed || s == OperationDenied || s == OperationCancelled || s == OperationUncertain
}

func (r OperationResult) Validate() error {
	if len(r.ContentReferences) > MaxOperationAttachments {
		return fmt.Errorf("%w: operation attachment count exceeds limit", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, id := range r.ContentReferences {
		if ValidateID(id) != nil || seen[id] {
			return fmt.Errorf("%w: invalid or duplicate operation attachment", ErrInvalid)
		}
		seen[id] = true
	}

	if !r.State.Terminal() || (r.State == OperationSucceeded && r.Failure != nil) {
		return fmt.Errorf("%w: invalid operation result", ErrInvalid)
	}
	if len(r.Value) > 0 && (len(r.Value) > MaxDocumentBytes || !utf8.Valid(r.Value) || !json.Valid(r.Value)) {
		return fmt.Errorf("%w: operation value requires bounded JSON", ErrInvalid)
	}
	if r.Failure != nil {
		if !utf8.ValidString(*r.Failure) {
			return fmt.Errorf("%w: operation failure is not UTF-8", ErrInvalid)
		}
		if err := ValidateText(*r.Failure, 16384); err != nil {
			return err
		}
	}
	if (r.State == OperationDenied || r.State == OperationCancelled) && (len(r.Value) != 0 || len(r.ContentReferences) != 0) {
		return fmt.Errorf("%w: an undispatched operation cannot have output", ErrInvalid)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(raw) > MaxDocumentBytes {
		return fmt.Errorf("%w: operation result exceeds size limit", ErrInvalid)
	}
	return nil
}
