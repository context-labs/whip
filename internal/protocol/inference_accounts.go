package protocol

type InferenceFlowParams struct {
	FlowID string `json:"flow_id" pattern:"^[A-Z2-7]{26}:[A-Z2-7]{26}$"`
}

type InferenceTeamParams struct {
	FlowID string `json:"flow_id" pattern:"^[A-Z2-7]{26}:[A-Z2-7]{26}$"`
	TeamID string `json:"team_id" pattern:"^[^\\x00-\\x1f\\x7f]{1,256}$"`
}

type InferenceProjectParams struct {
	FlowID    string `json:"flow_id" pattern:"^[A-Z2-7]{26}:[A-Z2-7]{26}$"`
	ProjectID string `json:"project_id" pattern:"^[^\\x00-\\x1f\\x7f]{1,256}$"`
}

type InferenceCreateProjectParams struct {
	FlowID string `json:"flow_id" pattern:"^[A-Z2-7]{26}:[A-Z2-7]{26}$"`
	Name   string `json:"name" pattern:"^[^\\x00-\\x1f\\x7f]{1,512}$"`
}

type InferenceTeam struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type InferenceProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type InferenceFlow struct {
	ID              string             `json:"id" pattern:"^[A-Z2-7]{26}:[A-Z2-7]{26}$"`
	Kind            *string            `json:"kind" pattern:"^(login|rotation)$"`
	State           string             `json:"state" enum:"authorizing,choose_team,loading_projects,choose_project,creating_project,provisioning,persistence_required,setup_required,cleanup_required,succeeded,failed,uncertain,cancelled,expired,interrupted"`
	VerificationURL *string            `json:"verification_url" pattern:"^https://inference[.]net/device/approve[?]user_code=[A-Za-z0-9%+._~-]+$"`
	UserCode        *string            `json:"user_code"`
	ExpiresAt       *AccountTimestamp  `json:"expires_at"`
	Teams           []InferenceTeam    `json:"teams"`
	Projects        []InferenceProject `json:"projects"`
	TeamID          *string            `json:"team_id"`
	ProjectID       *string            `json:"project_id"`
	Failure         *string            `json:"failure"`
}

type InferenceFlowsResult struct {
	Items []InferenceFlow `json:"items"`
}

// Local storage and route declarations do not prove network or model access.
type InferenceAccountStatus struct {
	ManagementState string            `json:"management_state" enum:"absent,stored,expired,unavailable"`
	InferenceState  string            `json:"inference_state" enum:"absent,stored,unavailable"`
	RouteState      string            `json:"route_state" enum:"missing,configured,conflict,unavailable"`
	UserID          *string           `json:"user_id"`
	Email           *string           `json:"email"`
	ExpiresAt       *AccountTimestamp `json:"expires_at"`
	TeamID          *string           `json:"team_id"`
	TeamName        *string           `json:"team_name"`
	ProjectID       *string           `json:"project_id"`
	ProjectName     *string           `json:"project_name"`
	Failure         *string           `json:"failure"`
	CleanupPending  bool              `json:"cleanup_pending"`
}

type InferenceCleanup struct {
	ID           string           `json:"id" pattern:"^[A-Z2-7]{26}:[A-Z2-7]{26}$"`
	ExpiresAt    AccountTimestamp `json:"expires_at"`
	TeamID       *string          `json:"team_id"`
	KeyID        *string          `json:"key_id"`
	KeyState     string           `json:"key_state" enum:"absent,pending,archived"`
	SessionState string           `json:"session_state" enum:"absent,pending,retained,signed_out"`
	Failure      *string          `json:"failure"`
}

type InferenceCleanupResult struct {
	Items   []InferenceCleanup `json:"items"`
	Failure *string            `json:"failure"`
}

type InferenceLogoutResult struct {
	Status         InferenceAccountStatus `json:"status"`
	LocalFailure   *string                `json:"local_failure"`
	CleanupFailure *string                `json:"cleanup_failure"`
	Cleanup        []InferenceCleanup     `json:"cleanup"`
}
