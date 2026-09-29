package session

import "time"

// ReloadOverrides records explicit whole-field choices independently of their
// values. Equal, empty and false choices remain explicit across later reloads.
type ReloadOverrides uint8

const (
	OverrideModules ReloadOverrides = 1 << iota
	OverrideMCP
	OverrideTitle
	OverrideGoals
	OverrideReport
	OverrideCompaction
	OverrideInstructions
	AllReloadOverrides = OverrideModules | OverrideMCP | OverrideTitle | OverrideGoals | OverrideReport | OverrideCompaction | OverrideInstructions
)

func ExplicitReloadOverrides(p ConfigPatch) (fields ReloadOverrides) {
	if p.Modules != nil {
		fields |= OverrideModules
	}
	if p.MCPServers != nil {
		fields |= OverrideMCP
	}
	if p.AutomaticTitle != nil {
		fields |= OverrideTitle
	}
	if p.GoalsEnabled != nil {
		fields |= OverrideGoals
	}
	if p.ReportMode != nil {
		fields |= OverrideReport
	}
	if p.Compaction != nil {
		fields |= OverrideCompaction
	}
	if p.Instructions != nil {
		fields |= OverrideInstructions
	}
	return fields
}

// RefreshConfiguration refreshes only inherited session preferences. Immutable
// definition defaults still win; model selection, bindings, run configuration
// and the output contract remain exactly as captured by the current session.
func RefreshConfiguration(current, host Configuration, definition DefinitionDocument, explicit ReloadOverrides) (Configuration, error) {
	inherited, err := Resolve(host, definition, ConfigPatch{})
	if err != nil {
		return Configuration{}, err
	}
	result := current.Clone()
	if explicit&OverrideModules == 0 {
		result.Modules = inherited.Modules
	}
	if explicit&OverrideMCP == 0 {
		result.MCPServers = inherited.MCPServers
	}
	if explicit&OverrideTitle == 0 {
		result.AutomaticTitle = inherited.AutomaticTitle
	}
	if explicit&OverrideGoals == 0 {
		result.GoalsEnabled = inherited.GoalsEnabled
	}
	if explicit&OverrideReport == 0 {
		result.ReportMode = inherited.ReportMode
	}
	if explicit&OverrideCompaction == 0 {
		result.Compaction = inherited.Compaction
	}
	if explicit&OverrideInstructions == 0 {
		result.Instructions = inherited.Instructions
	}
	return result, result.Validate()
}

type ReloadRequest struct {
	ID               string    `json:"id"`
	SessionID        SessionID `json:"session_id"`
	ExpectedRevision Revision  `json:"expected_revision,string"`
}

func (r ReloadRequest) Validate() error {
	return validateControl(r.ID, r.SessionID, r.ExpectedRevision)
}

type ReloadState string

const (
	ReloadPending     ReloadState = "pending"
	ReloadApplied     ReloadState = "applied"
	ReloadConflicted  ReloadState = "conflicted"
	ReloadInterrupted ReloadState = "interrupted"
	ReloadUnavailable ReloadState = "unavailable"
)

// ReloadEdit keeps original admission evidence and its single terminal outcome.
// Configuration contains safe session data, never provider or MCP credentials.
type ReloadEdit struct {
	ReloadRequest
	TreeID        TreeID
	HostRevision  string
	Configuration Configuration
	State         ReloadState
	Revision      *Revision
	CreatedAt     time.Time
	SettledAt     *time.Time
}
