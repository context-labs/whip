package protocol

import (
	"encoding/json"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/session"
)

type CreateSessionParams struct {
	Kind     session.SessionKind `json:"kind"`
	CWD      string              `json:"cwd"`
	Model    string              `json:"model"`
	Provider string              `json:"provider"`
}
type RootParams struct {
	RootID string `json:"root_id"`
}
type ListParams struct {
	Limit int `json:"limit,omitempty"`
}
type CheckpointParams struct {
	Reason string `json:"reason,omitempty"`
}
type CancelParams struct {
	TurnID          string `json:"turn_id,omitempty"`
	TargetCommandID string `json:"target_command_id,omitempty"`
}
type AgentCancelParams struct {
	ID     string `json:"id"`
	TurnID string `json:"turn_id"`
}
type AgentInputParams struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Delivery string `json:"delivery,omitempty"`
}
type QuestionAnswerParams struct {
	ID        string   `json:"id"`
	Answer    []string `json:"answer"`
	Dismissed bool     `json:"dismissed"`
}
type TerminalInputParams struct {
	ID    string `json:"id"`
	Bytes []byte `json:"bytes"`
}
type ShellParams struct {
	Command string `json:"command"`
}
type ToolCallParams struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}
type ToolConfigureParams struct {
	DenyPermissions bool `json:"deny_permissions"`
}
type RunConfigureParams struct {
	System   string `json:"system,omitempty"`
	MaxTurns int    `json:"max_turns,omitempty"`
	Headless bool   `json:"headless,omitempty"`
	CacheKey string `json:"cache_key,omitempty"`
}
type PermissionConfigureParams struct {
	ExternalPermissions bool `json:"external_permissions"`
}
type MCPAttachParams struct {
	Servers map[string]mcp.ServerConfig `json:"servers"`
}

type RootIDResult struct {
	RootID string `json:"root_id"`
}
type PathResult struct {
	Path string `json:"path"`
}
type TitleResult struct {
	Title string `json:"title"`
}
type GoalResult struct {
	Goal string `json:"goal"`
}
type TextResult struct {
	Text string `json:"text"`
}
type EffortResult struct {
	Effort string `json:"effort"`
}
type ModelResult struct {
	ReloadPending bool   `json:"reload_pending,omitempty"`
	Model         string `json:"model"`
	Provider      string `json:"provider"`
}
type ScheduleResult struct {
	ScheduleID int `json:"schedule_id"`
}
type BrowserStatusResult struct {
	Enabled bool   `json:"enabled"`
	Driver  string `json:"driver,omitempty"`
}
type MCPImportStatusResult struct {
	Claude bool `json:"claude"`
	Codex  bool `json:"codex"`
}
type ComputerStatusResult struct {
	Enabled        bool     `json:"enabled"`
	DefaultDeny    bool     `json:"default_deny"`
	Allowed        []string `json:"allowed"`
	Denied         []string `json:"denied"`
	SessionAllowed []string `json:"session_allowed"`
	SessionDenied  []string `json:"session_denied"`
}
type PermissionRulesResult struct {
	Rules  []session.PermissionRule `json:"rules"`
	Global []string                 `json:"global"`
}
type SessionListResult []session.Meta
type UserHistoryResult []string
type CompactionListResult []session.Compaction
type AgentListResult []session.RuntimeAgent
type ScheduleListResult []session.Schedule
type ToolSchemaResult []llm.Tool
type MCPListResult []MCPStatusResult
type LSPListResult []LSPStatusResult
