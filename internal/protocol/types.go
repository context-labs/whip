// Package protocol defines the v4 wire contract. DTOs contain values only;
// transport handlers and persistence dependencies do not belong in this package.
package protocol

import (
	"encoding/json"
	"errors"
	"strconv"
)

const (
	Major         = 4
	Minor         = 0
	MaxFrameBytes = 8 << 20
)

type Request struct {
	JSONRPC string          `json:"jsonrpc" enum:"2.0"`
	ID      ID              `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type Response struct {
	JSONRPC string          `json:"jsonrpc" enum:"2.0"`
	ID      ID              `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type InitializeParams struct {
	Major             int `json:"major" min:"4" max:"4"`
	ExpectedRuntimeID *ID `json:"expected_runtime_id,omitempty"`
}
type InitializeResult struct {
	Major     int             `json:"major" min:"4" max:"4"`
	Minor     int             `json:"minor"`
	RuntimeID ID              `json:"runtime_id"`
	Builtins  []DefinitionRef `json:"builtins"`
}
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Kind    string `json:"kind" enum:"INVALID,NOT_FOUND,CONFLICT,BUSY,LIMIT,STOPPED,CLOSED,IDENTITY,METHOD,INTERNAL"`
}

type (
	ID      string
	Counter int64
)

// Counter is an exact, nonnegative signed-64-bit decimal string on the wire.
func (c Counter) MarshalJSON() ([]byte, error) {
	if c < 0 {
		return nil, errors.New("negative counter")
	}
	return json.Marshal(strconv.FormatInt(int64(c), 10))
}

func (c *Counter) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return err
	}
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil || number < 0 || strconv.FormatInt(number, 10) != text {
		return errors.New("invalid decimal counter")
	}
	*c = Counter(number)
	return nil
}

type DefinitionRef struct {
	ID       ID     `json:"id"`
	Revision string `json:"revision" pattern:"^[a-f0-9]{64}$"`
}
type ModelSelection struct {
	Provider ID     `json:"provider"`
	Name     string `json:"name"`
	Effort   string `json:"effort"`
}
type Instructions struct {
	Text           string   `json:"text"`
	ProjectFiles   []string `json:"project_files"`
	DiscoverSkills bool     `json:"discover_skills"`
}
type ToolDeclaration struct {
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
}
type HookDeclaration struct {
	Operations    []ID `json:"operations"`
	Optional      bool `json:"optional"`
	TimeoutMillis int  `json:"timeout_millis" min:"1" max:"60000"`
}
type OutputPolicy struct {
	Schema json.RawMessage `json:"schema"`
}
type Configuration struct {
	ReportMode   string                     `json:"report_mode" enum:"notice,inline,message"`
	Model        ModelSelection             `json:"model"`
	Instructions Instructions               `json:"instructions"`
	Tools        map[string]ToolDeclaration `json:"tools"`
	Children     map[string]DefinitionRef   `json:"children"`
	Hooks        map[string]HookDeclaration `json:"hooks"`
	OutputSchema json.RawMessage            `json:"output_schema"`
}
type ConfigPatch struct {
	ReportMode   *string                    `json:"report_mode,omitempty" enum:"notice,inline,message"`
	Model        *ModelSelection            `json:"model,omitempty"`
	Instructions *Instructions              `json:"instructions,omitempty"`
	Tools        map[string]ToolDeclaration `json:"tools,omitempty"`
	Children     map[string]DefinitionRef   `json:"children,omitempty"`
	Hooks        map[string]HookDeclaration `json:"hooks,omitempty"`
	Output       *OutputPolicy              `json:"output,omitempty"`
}
type DefinitionDocument struct {
	ID       ID          `json:"id"`
	Name     string      `json:"name"`
	Defaults ConfigPatch `json:"defaults"`
}
type Definition struct {
	Ref       DefinitionRef      `json:"ref"`
	Document  DefinitionDocument `json:"document"`
	CreatedAt string             `json:"created_at"`
}
type TreeMetadata struct {
	Title    *string `json:"title"`
	Archived bool    `json:"archived"`
	Pinned   bool    `json:"pinned"`
}
type Tree struct {
	ID        ID           `json:"id"`
	Metadata  TreeMetadata `json:"metadata"`
	Engine    string       `json:"engine" enum:"starlark,quickjs"`
	Revision  Counter      `json:"revision"`
	CreatedAt string       `json:"created_at"`
}
type Session struct {
	ID               ID            `json:"id"`
	TreeID           ID            `json:"tree_id"`
	ParentID         *ID           `json:"parent_id"`
	Definition       DefinitionRef `json:"definition"`
	ConfigRevision   Counter       `json:"config_revision"`
	Configuration    Configuration `json:"configuration"`
	WorkingDirectory string        `json:"working_directory"`
	Lifecycle        string        `json:"lifecycle" enum:"active,stopped"`
	CreatedAt        string        `json:"created_at"`
}
type Part struct {
	Type        string      `json:"type"`
	Text        string      `json:"text,omitempty"`
	ReferenceID ID          `json:"reference_id,omitempty"`
	Call        *ToolCall   `json:"call,omitempty"`
	Result      *ToolResult `json:"result,omitempty"`
}
type ToolCall struct {
	ID        ID              `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
type ToolResult struct {
	CallID  ID     `json:"call_id"`
	Output  string `json:"output"`
	IsError bool   `json:"is_error"`
}
type RequestIdentity struct {
	ClientID  ID `json:"client_id"`
	RequestID ID `json:"request_id"`
}
type Receipt struct {
	Identity  RequestIdentity `json:"identity"`
	Digest    string          `json:"digest" pattern:"^[a-f0-9]{64}$"`
	InputID   *ID             `json:"input_id"`
	DeletedAt *string         `json:"deleted_at"`
	CreatedAt string          `json:"created_at"`
}
type Input struct {
	ID        ID     `json:"id"`
	SessionID ID     `json:"session_id"`
	Source    string `json:"source" enum:"user,agent,schedule"`
	Kind      string `json:"kind" enum:"prompt,compact"`
	Parts     []Part `json:"parts"`
	State     string `json:"state" enum:"queued,claimed,cancelled"`
	TurnID    *ID    `json:"turn_id"`
	CreatedAt string `json:"created_at"`
}
type Turn struct {
	ID             ID      `json:"id"`
	SessionID      ID      `json:"session_id"`
	Kind           string  `json:"kind" enum:"prompt,compact"`
	ConfigRevision Counter `json:"config_revision"`
	State          string  `json:"state" enum:"running,cancelling,succeeded,failed,cancelled,interrupted"`
	Failure        *string `json:"failure"`
	StartedAt      string  `json:"started_at"`
	FinishedAt     *string `json:"finished_at"`
}
type Message struct {
	ID        ID       `json:"id"`
	SessionID ID       `json:"session_id"`
	TurnID    ID       `json:"turn_id"`
	InputID   *ID      `json:"input_id"`
	Mail      *MailRef `json:"mail"`
	Sequence  Counter  `json:"sequence"`
	Role      string   `json:"role" enum:"system,user,assistant,tool"`
	Parts     []Part   `json:"parts"`
	CreatedAt string   `json:"created_at"`
}
type Admission struct {
	Receipt Receipt `json:"receipt"`
	Input   *Input  `json:"input"`
	Turn    *Turn   `json:"turn"`
}

type CreateTreeParams struct {
	Metadata         TreeMetadata    `json:"metadata"`
	Engine           string          `json:"engine" enum:"starlark,quickjs"`
	Resources        []ResourceLimit `json:"resources,omitempty"`
	Definition       DefinitionRef   `json:"definition"`
	Overrides        ConfigPatch     `json:"overrides"`
	WorkingDirectory string          `json:"working_directory"`
}
type CreateTreeResult struct {
	Tree Tree    `json:"tree"`
	Root Session `json:"root"`
}
type TreeParams struct {
	TreeID ID `json:"tree_id"`
}
type UpdateTreeParams struct {
	TreeID           ID           `json:"tree_id"`
	ExpectedRevision Counter      `json:"expected_revision"`
	Metadata         TreeMetadata `json:"metadata"`
}
type SessionParams struct {
	SessionID ID `json:"session_id"`
}
type SpawnSessionParams struct {
	Identity         RequestIdentity `json:"identity"`
	ParentID         ID              `json:"parent_id"`
	Definition       *DefinitionRef  `json:"definition,omitempty"`
	Overrides        ConfigPatch     `json:"overrides"`
	WorkingDirectory *string         `json:"working_directory,omitempty"`
	Parts            []Part          `json:"parts"`
	GrantIDs         []ID            `json:"grant_ids"`
	Budgets          []BudgetLimit   `json:"budgets,omitempty"`
	Resources        []ResourceLimit `json:"resources,omitempty"`
}
type SpawnSessionResult struct {
	Session   *Session  `json:"session"`
	Admission Admission `json:"admission"`
}
type ListSessionsParams struct {
	TreeID ID  `json:"tree_id"`
	After  *ID `json:"after,omitempty"`
	Limit  int `json:"limit" min:"1" max:"100"`
}
type ListSessionsResult struct {
	Items []Session `json:"items"`
}
type UpdateConfigurationParams struct {
	SessionID        ID          `json:"session_id"`
	ExpectedRevision Counter     `json:"expected_revision"`
	Patch            ConfigPatch `json:"patch"`
}
type SubmitParams struct {
	Identity  RequestIdentity `json:"identity"`
	SessionID ID              `json:"session_id"`
	Source    string          `json:"source" enum:"user,agent,schedule"`
	Parts     []Part          `json:"parts"`
}
type HistoryParams struct {
	SessionID ID      `json:"session_id"`
	After     Counter `json:"after"`
	Limit     int     `json:"limit" min:"1" max:"100"`
}
type HistoryResult struct {
	Items []Message `json:"items"`
}
type TurnParams struct {
	TurnID ID `json:"turn_id"`
}
type ModelUsage struct {
	Input        *Counter `json:"input"`
	Output       *Counter `json:"output"`
	Reasoning    *Counter `json:"reasoning"`
	CachedInput  *Counter `json:"cached_input"`
	CachedOutput *Counter `json:"cached_output"`
}
type ModelPrices struct {
	Input        *Counter `json:"input"`
	Output       *Counter `json:"output"`
	Reasoning    *Counter `json:"reasoning"`
	CachedInput  *Counter `json:"cached_input"`
	CachedOutput *Counter `json:"cached_output"`
}
type ModelRequestSnapshot struct {
	Purpose         ID             `json:"purpose"`
	Model           ModelSelection `json:"model"`
	Route           string         `json:"route"`
	Adapter         ID             `json:"adapter"`
	RequestDigest   string         `json:"request_digest" pattern:"^[a-f0-9]{64}$"`
	Prices          ModelPrices    `json:"prices"`
	MaxOutputTokens Counter        `json:"max_output_tokens"`
	InputTokenBound *Counter       `json:"input_token_bound"`
	TimeoutMillis   Counter        `json:"timeout_millis"`
}
type ModelAttemptResult struct {
	State               string     `json:"state" enum:"succeeded,failed,cancelled,uncertain"`
	Usage               ModelUsage `json:"usage"`
	ReportedCostNanoUSD *Counter   `json:"reported_cost_nano_usd"`
	Failure             *string    `json:"failure"`
	UsageNote           *string    `json:"usage_note"`
	ElapsedMillis       *Counter   `json:"elapsed_millis"`
}
type ModelAttempt struct {
	ID           ID                   `json:"id"`
	TurnID       ID                   `json:"turn_id"`
	LogicalID    ID                   `json:"logical_id"`
	Number       int                  `json:"number" min:"1" max:"100"`
	Request      ModelRequestSnapshot `json:"request"`
	State        string               `json:"state" enum:"reserved,dispatched,succeeded,failed,cancelled,uncertain"`
	Result       *ModelAttemptResult  `json:"result"`
	CostNanoUSD  *Counter             `json:"cost_nano_usd"`
	CostSource   string               `json:"cost_source" enum:"unknown,provider,prices,not_dispatched"`
	CostNote     *string              `json:"cost_note"`
	MessageID    *ID                  `json:"message_id"`
	CreatedAt    string               `json:"created_at"`
	DispatchedAt *string              `json:"dispatched_at"`
	FinishedAt   *string              `json:"finished_at"`
}
type ModelAttemptsParams struct {
	TurnID ID  `json:"turn_id"`
	After  *ID `json:"after,omitempty"`
	Limit  int `json:"limit" min:"1" max:"100"`
}
type ModelAttemptsResult struct {
	Items []ModelAttempt `json:"items"`
}
type InputParams struct {
	InputID ID `json:"input_id"`
}
type LifecycleParams struct {
	SessionID ID     `json:"session_id"`
	Lifecycle string `json:"lifecycle" enum:"active,stopped"`
}
type DeleteResult struct {
	Deleted bool `json:"deleted"`
}
