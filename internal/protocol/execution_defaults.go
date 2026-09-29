package protocol

// ExecutionDefaults uses actual counts, not legacy retry/round labels. Zero
// compaction percent selects 50; zero goal continuations means none.
type ExecutionDefaults struct {
	Engine               string  `json:"engine" enum:"starlark,quickjs"`
	Effort               string  `json:"effort" pattern:"^[^\\x00]{0,64}$"`
	CompactionPercent    int     `json:"compaction_percent" min:"0" max:"100"`
	GoalMaxContinuations Counter `json:"goal_max_continuations"`
	MaxAttempts          int     `json:"max_attempts" min:"1" max:"5"`
}

type HostExecutionDefaults struct {
	ExecutionDefaults
	Revision string `json:"revision" pattern:"^[a-f0-9]{64}$"`
}

type SetExecutionDefaultsParams struct {
	ExpectedRevision string            `json:"expected_revision" pattern:"^[a-f0-9]{64}$"`
	Defaults         ExecutionDefaults `json:"defaults"`
}
