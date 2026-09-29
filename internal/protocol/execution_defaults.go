package protocol

// ExecutionDefaults uses actual counts, not legacy retry/round labels. Zero
// compaction percent selects 50; zero goal continuations means none.
type ExecutionDefaults struct {
	Engine               string  `json:"engine" enum:"starlark,quickjs"`
	Effort               string  `json:"effort" pattern:"^[^\\x00]{0,64}$"`
	CompactionPercent    int     `json:"compaction_percent" min:"0" max:"100"`
	GoalMaxContinuations Counter `json:"goal_max_continuations"`
	MaxAttempts          int     `json:"max_attempts" min:"1" max:"9007199254740991"`
}

type HostExecutionDefaults struct {
	ExecutionDefaults
	Revision    string               `json:"revision" pattern:"^[a-f0-9]{64}$"`
	Preferences ExecutionPreferences `json:"preferences"`
}

type SetExecutionDefaultsParams struct {
	ExpectedRevision string            `json:"expected_revision" pattern:"^[a-f0-9]{64}$"`
	Defaults         ExecutionDefaults `json:"defaults"`
}

// Preferences retain unset defaults rather than replacing them with resolved
// counts. Null goal continuations means the host default; explicit zero is none.
type ExecutionPreferences struct {
	Engine               string           `json:"engine" enum:"starlark,quickjs"`
	CompactionPercent    int              `json:"compaction_percent" min:"0" max:"100"`
	CompactionModel      ProviderDefaults `json:"compaction_model"`
	GoalMaxContinuations *Counter         `json:"goal_max_continuations"`
	MaxAttempts          int              `json:"max_attempts" min:"0" max:"9007199254740991"`
	ImportClaude         bool             `json:"import_claude"`
	ImportCodex          bool             `json:"import_codex"`
}

type SetExecutionPreferencesParams struct {
	ExpectedRevision string               `json:"expected_revision" pattern:"^[a-f0-9]{64}$"`
	Preferences      ExecutionPreferences `json:"preferences"`
}
