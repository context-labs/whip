package session

import "fmt"

// PermissionInspection is a fixed, owner-scoped read. It cannot grant authority.
type PermissionInspection struct {
	SessionID      SessionID   `json:"session_id"`
	ConfigRevision Revision    `json:"config_revision"`
	Action         string      `json:"action"`
	OperationID    OperationID `json:"operation_id"`
}

func (r PermissionInspection) Validate() error {
	if err := ValidateID(string(r.SessionID)); err != nil {
		return err
	}
	if r.ConfigRevision < 1 {
		return fmt.Errorf("%w: missing captured configuration", ErrInvalid)
	}
	switch r.Action {
	case "request":
		if r.OperationID == "" {
			return nil
		}
	case "status":
		return ValidateID(string(r.OperationID))
	}
	return fmt.Errorf("%w: invalid permission inspection", ErrInvalid)
}
