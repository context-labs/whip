package protocol

import "github.com/context-labs/whip/internal/llm"

// Runtime parameters name domain values; command-line syntax is parsed by
// clients before submitting these objects.
type (
	EmptyParams struct{}
	TextParams  struct {
		Text string `json:"text"`
	}
)

type IDParams struct {
	ID string `json:"id"`
}
type PathParams struct {
	Path string `json:"path"`
}
type TitleParams struct {
	Title string `json:"title"`
}
type ArchiveParams struct {
	Archived bool `json:"archived"`
}
type ForkParams struct {
	ExpectedRevision *int64 `json:"expected_revision,string"`
	Title            string `json:"title,omitempty"`
	Cut              int    `json:"cut,omitempty"`
}
type EffortParams struct {
	Effort         string `json:"effort"`
	PersistDefault bool   `json:"persist_default"`
}
type ModelParams struct {
	Effort         string `json:"effort,omitempty"`
	Model          string `json:"model"`
	Provider       string `json:"provider,omitempty"`
	PersistDefault bool   `json:"persist_default"`
}
type CompactionParams struct {
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`
}
type RewindParams struct {
	ExpectedRevision *int64 `json:"expected_revision,string"`
	Cut              int    `json:"cut"`
}
type ClearHistoryParams struct {
	ExpectedRevision *int64 `json:"expected_revision,omitempty,string"`
}
type GoalContextParams struct {
	Window int `json:"window,omitempty"`
}
type BudgetCapParams struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Limit int64  `json:"limit,string"`
}
type ScheduleCreateParams struct {
	Schedule string `json:"schedule"`
	Prompt   string `json:"prompt"`
}
type ScheduleDeleteParams struct {
	ScheduleID int `json:"schedule_id"`
}
type MCPServerParams struct {
	Name string `json:"name"`
}
type MCPImportParams struct {
	Source  string `json:"source"`
	Enabled bool   `json:"enabled"`
}
type BrowserDriverParams struct {
	Driver string `json:"driver"`
}
type ComputerAppParams struct {
	App string `json:"app"`
}

type CompactionResult struct {
	Cutoff int       `json:"cutoff"`
	Model  string    `json:"model,omitempty"`
	Usage  llm.Usage `json:"usage"`
}
type RewindResult struct {
	Cut           int `json:"cut"`
	RestoredFiles int `json:"restored_files"`
}
type CompactionSettingsResult struct {
	Model          string `json:"model,omitempty"`
	Provider       string `json:"provider,omitempty"`
	BuiltinDefault bool   `json:"builtin_default"`
}
type CompactionRetryResult struct {
	Undone   bool `json:"undone"`
	Sequence int  `json:"sequence,omitempty"`
}
