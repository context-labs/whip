package protocol

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/session"
)

type Fixture struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
	Valid bool            `json:"valid"`
}

// Fixtures encode actual Go DTOs, including projections from domain values.
// They are deterministic contract evidence, not a substitute for runtime tests.
func Fixtures() ([]Fixture, error) {
	created := time.Date(2026, 9, 27, 12, 0, 0, 123456000, time.UTC)
	_, _, ref, err := session.CanonicalDefinition(session.Builtins()[0])
	if err != nil {
		return nil, err
	}
	config, err := session.Resolve(session.Configuration{Model: session.ModelSelection{Provider: "fixture", Name: "scripted"}}, session.Builtins()[0], session.ConfigPatch{})
	if err != nil {
		return nil, err
	}
	root, err := SessionFromDomain(session.Session{ID: "session_root", TreeID: "tree_fixture", Definition: ref, HistoryRevision: 9007199254740993, ConfigRevision: 9007199254740993, Config: config, WorkingDirectory: "/workspace", Lifecycle: session.Active, CreatedAt: created})
	if err != nil {
		return nil, err
	}
	child := root
	child.ID = "session_child"
	parent := root.ID
	child.ParentID = &parent
	message := MessageFromDomain(session.Message{ID: "message_fixture", SessionID: "session_child", GroupID: "turn_fixture", TurnID: "turn_fixture", Sequence: 9007199254740993, Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "Completed."}, {Type: "content", ReferenceID: "content_fixture"}}, CreatedAt: created})
	callMessage := MessageFromDomain(session.Message{
		ID: "message_call", SessionID: "session_child", GroupID: "turn_fixture", TurnID: "turn_fixture", Sequence: 9007199254740994,
		Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call_fixture", Name: "execute", Arguments: json.RawMessage(`{"code":"print(1)"}`)}}}, CreatedAt: created,
	})
	toolMessage := MessageFromDomain(session.Message{
		ID: "message_result", SessionID: "session_child", GroupID: "turn_fixture", TurnID: "turn_fixture", Sequence: 9007199254740995,
		Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "call_fixture", Output: "1", IsError: false}}}, CreatedAt: created,
	})
	attempt := ModelAttemptFromDomain(session.ModelAttempt{
		ID: "attempt_fixture", TurnID: "turn_fixture", LogicalID: "call_fixture", Number: 1, State: session.AttemptSucceeded,
		Request: session.ModelRequestSnapshot{Purpose: "turn", Model: session.ModelSelection{Provider: "fixture", Name: "model", Temperature: new(0.0), TopP: new(0.9)}, Route: "https://provider.example/v1/chat/completions", Adapter: "openai-chat", RequestDigest: ref.Revision, MaxOutputTokens: 4096, TimeoutMillis: 30000},
		Result:  &session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(9007199254740993))}, CostNanoUSD: new(int64(9007199254740993)), CostSource: "provider", MessageID: new(session.MessageID(message.ID)), CreatedAt: created, DispatchedAt: &created, FinishedAt: &created,
	})
	contentDigest := sha256.Sum256([]byte("hello"))
	helper := attempt
	helper.Request.Purpose = "model_helper"
	helper.OperationID, helper.BatchIndex = new(ID("operation_helper")), new(31)
	helper.MessageID = nil
	titleAttempt := attempt
	titleAttempt.Request.Purpose = session.AutomaticTitlePurpose
	titleAttempt.MessageID = nil

	executorLease := ExecutorLease{Epoch: "executor_fixture", Definition: DefinitionRef{ID: "custom", Revision: ref.Revision}, Generation: 9007199254740993, Tools: []ID{"lookup"}, Hooks: []ID{}}
	executorInvocation := ExecutorInvocation{Origin: "cell", InvocationID: "invocation_fixture", Lease: executorLease, Kind: "tool", Name: "lookup", SessionID: "session_root", TurnID: "turn_fixture", CellID: new(ID("cell_fixture")), OperationID: new(ID("operation_fixture")), Operation: "tools.lookup", ArgumentsBase64: new(base64.StdEncoding.EncodeToString([]byte(`{"count":9007199254740993}`))), DeadlineMillis: Counter(created.UnixMilli())}
	browserScope := BrowserScope{ProviderID: "provider", ProviderEpoch: "provider_epoch", TabID: "tab", TabGeneration: "tab_generation", ProfileID: "profile", ControlLineage: "lineage", AttachmentID: "attachment", AttachmentGeneration: "attachment_generation", Preview: &BrowserPreviewScope{HostID: "host", HostIdentity: "runtime", ConnectionGeneration: "connection", EnvironmentID: "environment", Loopback: "127.0.0.1", Ports: []int{3000}}}
	browserCommand := BrowserCommand{CommandID: "command", OperationID: "operation", RootID: "session_root", AgentID: "session_child", ProviderEpoch: browserScope.ProviderEpoch, Scope: browserScope, ExpectedDocument: "document", DeadlineMillis: 9007199254740993, Kind: "cdp", Arguments: json.RawMessage(`{"method":"Page.captureScreenshot","params":{"format":"jpeg"}}`)}
	directInput := InputFromDomain(session.Input{ID: "direct_input", SessionID: "session_root", Source: session.UserInput, Kind: session.HostOperationInputKind, State: session.Queued, Parts: []session.Part{}, HostOperation: &session.HostOperation{Module: "shell", Name: "run", Arguments: json.RawMessage(`{"command":"printf direct"}`)}, CreatedAt: created})
	modelFree := root
	modelFree.Configuration.Model = ModelSelection{}
	values := []struct {
		name  string
		value any
	}{
		{"HostStatus", HostStatus{RuntimeID: "runtime_fixture", ProcessEpoch: "boot_fixture", PID: 123, Build: "fixture", StartedAt: created.Format(time.RFC3339Nano)}},
		{"StopHostParams", StopHostParams{RuntimeID: "runtime_fixture", ProcessEpoch: "boot_fixture"}},
		{"HostStopAccepted", HostStopAccepted{RuntimeID: "runtime_fixture", ProcessEpoch: "boot_fixture"}},
		{"HostExecutionDefaults", HostExecutionDefaults{Revision: ref.Revision, Engine: "quickjs", Effort: "high", CompactionPercent: 0, GoalMaxContinuations: 9007199254740993, MaxAttempts: 3}},
		{"SetExecutionDefaultsParams", SetExecutionDefaultsParams{ExpectedRevision: ref.Revision, Defaults: ExecutionDefaults{Engine: "starlark", GoalMaxContinuations: 0, MaxAttempts: 1}}},
		{"BrowserProviderBindParams", BrowserProviderBindParams{RootID: "session_root", Version: 2, DesktopID: "desktop", WindowID: "window", OfferRevision: "offer", CreateProfileID: "profile", OfferedTabs: []BrowserOfferedTab{{TabID: "tab", TabGeneration: "tab_generation", ProfileID: "profile", DocumentRevision: "document", URL: "https://example.test/", Title: "Example", Preview: browserScope.Preview}}, OfferedPreviewHosts: []BrowserPreviewScope{*browserScope.Preview}}},
		{"BrowserProviderBindResult", BrowserProviderBindResult{Version: 2, ProviderID: browserScope.ProviderID, ProviderEpoch: browserScope.ProviderEpoch}},
		{"BrowserProviderUnbindParams", BrowserProviderUnbindParams{RootID: "session_root", ProviderEpoch: browserScope.ProviderEpoch}},
		{"BrowserAccepted", BrowserAccepted{Accepted: true}},
		{"BrowserAttachmentsResult", BrowserAttachmentsResult{Attachments: []BrowserAttachment{{Scope: browserScope, RootID: "session_root", AgentID: "session_child", DocumentRevision: "document", URL: "https://example.test/", Title: "Example"}}}},
		{"BrowserTabsResult", BrowserTabsResult{Tabs: []BrowserTab{{TabID: "tab", TabGeneration: "tab_generation", DocumentRevision: "document", URL: "https://example.test/", Title: "Example", State: "attached", AttachmentID: new(BrowserToken("attachment"))}}}},
		{"BrowserCommandResultParams", BrowserCommandResultParams{CommandID: "command", RootID: "session_root", ProviderEpoch: browserScope.ProviderEpoch, AttachmentGeneration: browserScope.AttachmentGeneration, DocumentRevision: "document", URL: "https://example.test/", Title: "Example", Screenshot: &BrowserScreenshot{Size: 4194304, Digest: strings.Repeat("a", 64), MediaType: "image/jpeg"}}},
		{"BrowserScreenshotChunkParams", BrowserScreenshotChunkParams{CommandID: "command", RootID: "session_root", ProviderEpoch: browserScope.ProviderEpoch, AttachmentGeneration: browserScope.AttachmentGeneration, Offset: 9007199254740993, DataBase64: "eA=="}},
		{"BrowserInventoryResultParams", BrowserInventoryResultParams{RequestID: "inventory", RootID: "session_root", ProviderEpoch: browserScope.ProviderEpoch, Tabs: []BrowserTab{}}},
		{"BrowserProviderEventParams", BrowserProviderEventParams{RootID: "session_root", ProviderEpoch: browserScope.ProviderEpoch, TabID: browserScope.TabID, TabGeneration: browserScope.TabGeneration, AttachmentID: browserScope.AttachmentID, AttachmentGeneration: browserScope.AttachmentGeneration, Sequence: 9007199254740993, OperationID: new(ID("operation")), DocumentRevision: "document", Kind: "cdp", Method: "Page.loadEventFired", Params: json.RawMessage(`{}`)}},
		{"BrowserEvent", BrowserEvent{JSONRPC: "2.0", Method: "browser.command", Command: &browserCommand}},
		{"BrowserEvent", BrowserEvent{JSONRPC: "2.0", Method: "browser.inventory", Inventory: &BrowserInventoryRequest{RequestID: "inventory", RootID: "session_root", AgentID: "session_child", ProviderID: browserScope.ProviderID, ProviderEpoch: browserScope.ProviderEpoch, Tabs: []BrowserInventoryTarget{{TabID: browserScope.TabID, TabGeneration: browserScope.TabGeneration}}}}},
		{"BrowserEvent", BrowserEvent{JSONRPC: "2.0", Method: "browser.command.cancel", Cancel: &BrowserCommandCancel{CommandID: "command", RootID: "session_root", ProviderEpoch: browserScope.ProviderEpoch, AttachmentGeneration: browserScope.AttachmentGeneration}}},
		{"BrowserEvent", BrowserEvent{JSONRPC: "2.0", Method: "browser.provider.revoked", Revoked: &BrowserProviderRevoked{RootID: "session_root", ProviderID: browserScope.ProviderID, ProviderEpoch: browserScope.ProviderEpoch, Reason: "closed"}}},
		{"BrowserEvent", BrowserEvent{JSONRPC: "2.0", Method: "browser.scopes.retired", Retired: &BrowserScopesRetired{RootID: "session_root", ProviderID: browserScope.ProviderID, ProviderEpoch: browserScope.ProviderEpoch, Scopes: []BrowserScope{browserScope}}}},
		{"HostProfiles", HostProfiles{Revision: ref.Revision, Profiles: []HostProfile{{ID: "remote", Name: "Remote", URL: "https://example.test:8443/", RuntimeID: "runtime_remote", ConnectOnLaunch: true}}}},
		{"SetHostProfilesParams", SetHostProfilesParams{ExpectedRevision: ref.Revision, Profiles: []HostProfile{}}},
		{"Input", directInput},
		{"HostAttentionParams", HostAttentionParams{Limit: 100, MaxBytes: 524288}},
		{"Usage", UsageFromDomain(session.Usage{SessionID: "session_root", Attempts: session.UsageAttempts{Settled: 2, InFlight: 1}, ReportedCost: session.UsageCost{Value: 9007199254740993, Attempts: 1}, UnknownCost: 1, InputTokens: session.UsageQuantity{Value: 9007199254740993, KnownAttempts: 1, MissingAttempts: 1}, OutputTokens: session.UsageQuantity{MissingAttempts: 2}, ReasoningTokens: session.UsageQuantity{MissingAttempts: 2}, CachedInput: session.UsageQuantity{MissingAttempts: 2}, CachedOutput: session.UsageQuantity{MissingAttempts: 2}, ElapsedMillis: session.UsageQuantity{MissingAttempts: 2}})},
		{"CapturedText", CapturedText{Digest: strings.Repeat("a", 64), Bytes: 16777217, Status: "oversized", Chunks: []ContentReference{}}},
		{"ModelInspectionParams", ModelInspectionParams{SessionID: "session_root", AttemptID: "attempt"}},
		{"ModelInspection", ModelInspectionFromDomain(session.ModelInspection{SessionID: "session_root", TurnID: "turn", AttemptID: "attempt", RequestDigest: strings.Repeat("a", 64)})},
		{"TracePageParams", TracePageParams{RootID: "session_root", After: new(Counter(9007199254740993)), ExpectedRevision: new(Counter(9007199254740999)), Limit: 2048, MaxBytes: 524288}},
		{"TracePageParams", TracePageParams{RootID: "session_root", Before: new((*Counter)(nil)), Limit: 2048, MaxBytes: 524288}},
		{"TracePageParams", TracePageParams{RootID: "session_root", Before: new(new(Counter(9007199254740993))), ExpectedRevision: new(Counter(9007199254740999)), Limit: 2048, MaxBytes: 524288}},
		{"TracePageResult", TracePageFromDomain(session.TracePage{ObservedAtNS: 1790600000000001000, Revision: 9007199254740999, Next: 9007199254740999, Items: []session.TraceRow{{Sequence: 9007199254740999, RootID: "session_root", SessionID: "session_child", TurnID: "turn", SourceKind: "attempt", SourceID: "attempt", SpanID: session.TraceSpanID("attempt", "attempt"), Span: &session.TraceSpan{TraceID: session.TraceID("turn"), Kind: "llm", Name: "turn model", State: "succeeded", StartNS: 1790600000000000000, EndNS: new(int64(1790600000000001000)), Attributes: []session.TraceAttribute{{Key: "whip.cost.nano_usd", Count: new(int64(9007199254740993))}, {Key: "whip.input.body_available", Flag: new(false)}, {Key: "gen_ai.request.model", Text: new("model")}}}}, {Sequence: 9007199254740998, RootID: "session_root", SessionID: "deleted", TurnID: "gone", SourceKind: "turn", SourceID: "gone", SpanID: session.TraceSpanID("turn", "gone")}}})},
		{"TraceExportParams", TraceExportParams{RootID: "session_root"}},
		{"HostAttentionResult", HostAttentionResult{Items: []HostAttentionItem{}}},
		{"WorkspaceCompletionParams", WorkspaceCompletionParams{SessionID: "session_root", Kind: "mention", Prefix: "roadmap", Limit: 64}},
		{"WorkspaceCompletionResult", WorkspaceCompletionResult{WorkingDirectory: "/workspace", Candidates: []WorkspaceCompletionCandidate{{Text: "@docs/roadmap.md", Description: ""}, {Text: "@docs/", Description: "dir"}}, Truncated: true}},
		{"HostDirectoriesParams", HostDirectoriesParams{Path: "/workspace", Limit: 64}},
		{"HostDirectoriesResult", HostDirectoriesResult{Path: "/workspace", Parent: "/", Entries: []HostDirectoryEntry{{Name: "project", Path: "/workspace/project"}}}},
		{"HostDirectoryPickParams", HostDirectoryPickParams{}},
		{"HostDirectoryPickResult", HostDirectoryPickResult{Cancelled: true}},
		{"HostSkillsParams", HostSkillsParams{Scope: "global", Limit: 32}},
		{"WorkspaceInspection", WorkspaceInspection{SessionID: root.ID, WorkingDirectory: "/workspace ", ConfigurationRevision: 9007199254740993}},
		{"WorkspaceSetParams", WorkspaceSetParams{ID: "workspace_edit", SessionID: root.ID, ExpectedRevision: 9007199254740993, Path: "../workspace "}},
		{"RunConfigureParams", RunConfigureParams{ID: "run_edit", SessionID: root.ID, ExpectedRevision: 9007199254740993, Configuration: RunConfiguration{MaxTurns: 0, Headless: true, System: "", CacheKey: ""}}},
		{"ReloadSessionParams", ReloadSessionParams{EditID: "Reload.Mixed", SessionID: root.ID, ExpectedRevision: 9007199254740993}},
		{"ReloadEditParams", ReloadEditParams{SessionID: root.ID, EditID: "Reload.Mixed"}},
		{"ReloadEdit", ReloadEdit{ID: "Reload.Mixed", SessionID: root.ID, TreeID: root.TreeID, ExpectedRevision: 9007199254740993, HostRevision: strings.Repeat("a", 64), Configuration: root.Configuration, State: "pending", CreatedAt: created.Format(time.RFC3339Nano)}},
		{"ControlEdit", ControlEdit{ID: "workspace_edit", SessionID: root.ID, Revision: 9007199254740993, Session: &root}},
		{"ControlEdit", ControlEdit{ID: "workspace_deleted", SessionID: root.ID, Revision: 9007199254740993, Deleted: true}},
		{"HostSkillsResult", HostSkillsResult{Candidates: []HostSkillCandidate{{Text: "$fixture", Description: "Fixture"}}}},
		{"HostThemesResult", HostThemesResult{Themes: []HostThemeMetadata{{ID: "dark", Name: "Dark", Dark: true, Source: "builtin"}}, Errors: []HostThemeError{}}},
		{"HostThemeResolveParams", HostThemeResolveParams{Name: "dark"}},
		{"CallHostToolParams", CallHostToolParams{Identity: RequestIdentity{ClientID: "human", RequestID: "direct"}, SessionID: "session_root", Operation: *directInput.HostOperation}},
		{"RunShellParams", RunShellParams{Identity: RequestIdentity{ClientID: "human", RequestID: "direct"}, SessionID: "session_root", Command: "printf direct"}},
		{"HostToolSchemasResult", HostToolSchemasResult{Items: []HostToolSchema{{Module: "files", Name: "read", Description: "Read", InputSchema: json.RawMessage(`{"type":"object"}`)}}}},
		{"ExecutorActivityResult", ExecutorActivityResult{Activity: &ExecutorActivity{Epoch: "executor_fixture", TurnID: "turn_fixture", Revision: 9007199254740993, Decisions: []HookDecision{{Hook: "before_tool", Operation: "files.read", Decision: "skipped", Reason: "unavailable"}}}}},
		{"ExecutorBindParams", ExecutorBindParams{Definition: executorLease.Definition, Tools: []ID{"lookup"}, Hooks: []ID{}}},
		{"ExecutorLease", executorLease},
		{"ExecutorPendingParams", ExecutorPendingParams{Epoch: executorLease.Epoch, Definition: executorLease.Definition, Generation: executorLease.Generation}},
		{"ExecutorPendingResult", ExecutorPendingResult{Items: []ExecutorInvocation{executorInvocation}}},
		{"ExecutorToolResultParams", ExecutorToolResultParams{Epoch: executorLease.Epoch, Generation: executorLease.Generation, InvocationID: executorInvocation.InvocationID, OutputBase64: new("bnVsbA==")}},
		{"ExecutorHookResultParams", ExecutorHookResultParams{Epoch: executorLease.Epoch, Generation: executorLease.Generation, InvocationID: "hook_fixture"}},
		{"ExecutorProgressParams", ExecutorProgressParams{Epoch: executorLease.Epoch, Generation: executorLease.Generation, InvocationID: executorInvocation.InvocationID, Text: "bounded progress"}},
		{"ExecutorAccepted", ExecutorAccepted{Accepted: true}},
		{"ExecutorEvent", ExecutorEvent{JSONRPC: "2.0", Method: "executor.invoke", Epoch: executorLease.Epoch, Generation: executorLease.Generation, InvocationID: executorInvocation.InvocationID, Invocation: &executorInvocation}},
		{"ExecutorEvent", ExecutorEvent{JSONRPC: "2.0", Method: "executor.cancel", Epoch: executorLease.Epoch, Generation: executorLease.Generation, InvocationID: executorInvocation.InvocationID}},
		{"ShellInteractionParams", ShellInteractionParams{SessionID: "shell-owner", Cursor: 9007199254740993}},
		{"ShellInteractionResult", ShellInteractionResult{}},
		{"ShellInteractionResult", ShellInteractionResult{Interaction: &ShellInteraction{OperationID: "shell-operation", StartedAt: created.Format(time.RFC3339Nano), DataBase64: "cHJvbXB0", From: 9007199254740993, Through: 9007199254740999, NextInput: 9007199254740993, SecondsLeft: 12}}},
		{"ShellInputParams", ShellInputParams{SessionID: "shell-owner", OperationID: "shell-operation", Sequence: 9007199254740993, DataBase64: "a2V5"}},
		{"ShellInputResult", ShellInputResult{Sequence: 9007199254740993}},
		{"RPCError", RPCError{Code: -32035, Kind: "MCP_UNAVAILABLE", Message: "MCP operation unavailable; inspect configuration and connection status"}},
		{"ComputerStatus", ComputerStatus{Revision: ref.Revision, Configuration: ComputerConfiguration{Allow: []string{}, Deny: []string{}, DefaultDeny: true}, Generation: "control_fixture", State: "disabled", PlatformSupported: true}},
		{"UseBundledComputerParams", UseBundledComputerParams{Revision: ref.Revision}},
		{"MCPConfiguration", MCPConfiguration{Revision: ref.Revision, Servers: []MCPDeclaration{}, Imports: MCPImportPolicy{}, BrandIcons: true}},
		{"MCPImportCandidatesResult", MCPImportCandidatesResult{Revision: ref.Revision, Candidates: []MCPImportCandidate{{Fingerprint: ref.Revision, Name: "candidate", Source: "codex", State: "importable", Gated: false, BrandHint: "example.com", BrandKey: "example.com"}}, SourceErrors: map[string]string{}}},
		{"MCPImportParams", MCPImportParams{Revision: ref.Revision, Fingerprints: map[string]string{"candidate": ref.Revision}}},
		{"ConfigureMCPParams", ConfigureMCPParams{Revision: ref.Revision, Name: "fixture", Server: &MCPServerInput{URL: "https://example.com/mcp", Command: []string{}, Env: map[string]string{}, Headers: map[string]string{}}}},
		{"MCPStatusResult", MCPStatusResult{Items: []MCPServerStatus{{Name: "fixture", State: "not_started"}}}},
		{"MCPRefreshResult", MCPRefreshResult{Added: []string{}, Existing: []string{}, Changed: []string{}, Servers: []MCPServerStatus{}, Blocked: []MCPServerStatus{}, SourceErrors: []MCPServerStatus{}}},
		{"MCPToolsResult", MCPToolsResult{Items: []MCPTool{{Name: "visible", Server: "fixture", Generation: "generation", Capability: "mcp.call.trusted", Resource: "resource", InputSchema: json.RawMessage(`{"type":"object"}`)}}}},
		{"MCPInstructionsResult", MCPInstructionsResult{Server: "fixture", Generation: "generation", Resource: "resource", Text: "Use visible", ContentParts: []ContentReference{}, Bytes: 11}},
		{"MCPBrandIconsResult", MCPBrandIconsResult{Icons: map[string]string{}}},
		{"ProviderParams", ProviderParams{Provider: "explicit"}},
		{"ProviderKeySetup", ProviderKeySetup{Revision: strings.Repeat("a", 64), Provider: "openrouter", Key: &ProviderKeyPublication{ID: "fixture-key", Key: "fixture-private"}}},
		{"ProviderPresetsResult", ProviderPresetsResult{Items: []ProviderPreset{{ID: "openai", Name: "OpenAI", Kind: "openai-responses", BaseURL: "https://api.openai.com/v1", Methods: []string{"api_key"}, Environments: []string{"OPENAI_API_KEY"}, SuggestedModels: []string{"gpt-6-astra"}}}}},
		{"ProviderModelsResult", ProviderModelsResult{Items: []ProviderModel{{ID: "model", Prices: ModelPrices{Input: new(Counter(9007199254740993)), Output: new(Counter(0))}, ContextWindowTokens: new(Counter(1000000)), ReasoningEfforts: []string{}, MetadataSource: "advertised"}}}},
		{"ProviderInventory", ProviderInventory{Revision: ref.Revision, Routes: []ProviderRoute{}}},
		{"ChangeProviderParams", ChangeProviderParams{Revision: ref.Revision, Provider: "custom", Declaration: ProviderDeclaration{Kind: "openai-chat", BaseURL: "https://example.test/v1", Credential: &ProviderCredentialInput{Source: "file"}}, Key: &ProviderKeyPublication{ID: "stable-key", Key: "fixture-only-key"}}},
		{"ProviderDefaultsParams", ProviderDefaultsParams{Revision: ref.Revision, Defaults: ProviderDefaults{Selection: &ModelSelection{Provider: "custom", Name: "explicit-model", Temperature: new(0.0)}, Settings: &ProviderModelSettings{Prices: ModelPrices{Input: new(Counter(9007199254740993)), Output: new(Counter(0))}, MaxOutputTokens: 4096}}}},
		{"RemoveProviderParams", RemoveProviderParams{Revision: ref.Revision, Provider: "custom", Replacement: &ProviderDefaults{}}},
		{"ProviderCatalog", ProviderCatalog{Provider: "custom", State: "missing", ScopeState: "unverified", Discovery: "not_checked", Models: []ProviderModel{}}},
		{"ProviderReadinessParams", ProviderReadinessParams{Selection: ModelSelection{Provider: "custom", Name: "model"}}},
		{"ProviderReadiness", ProviderReadiness{Configured: true, CredentialState: "unchecked", CatalogState: "missing", ModelState: "unknown", InferenceState: "not_tested"}},
		{"RPCError", RPCError{Code: -32033, Kind: "PROVIDER_KEY_PENDING", Message: "Published key durability is unconfirmed; retry the same key identity"}},
		{"EmptyParams", EmptyParams{}},
		{"RPCError", RPCError{Code: -32024, Kind: "ACCOUNT_MANAGEMENT", Message: "Management authorization is required"}},
		{"LanguageServersResult", LanguageServersResult{Items: []LanguageServerStatus{{Name: "gopls", State: "not_started"}, {Name: "custom", State: "connected", WorkspaceRoot: new("/workspace")}}}},
		{"InferenceFlowParams", InferenceFlowParams{FlowID: "AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB"}},
		{"InferenceTeamParams", InferenceTeamParams{FlowID: "AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB", TeamID: "team"}},
		{"InferenceProjectParams", InferenceProjectParams{FlowID: "AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB", ProjectID: "project"}},
		{"InferenceCreateProjectParams", InferenceCreateProjectParams{FlowID: "AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB", Name: "Explicit project"}},
		{"InferenceFlow", InferenceFlow{ID: "AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB", Kind: new("login"), State: "authorizing", VerificationURL: new("https://inference.net/device/approve?user_code=PUBLIC-CODE"), UserCode: new("PUBLIC-CODE"), ExpiresAt: new(AccountTimestamp(created.Format(time.RFC3339Nano))), Teams: []InferenceTeam{}, Projects: []InferenceProject{}}},
		{"InferenceFlow", InferenceFlow{ID: "AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB", State: "interrupted", Teams: []InferenceTeam{}, Projects: []InferenceProject{}}},
		{"InferenceFlowsResult", InferenceFlowsResult{Items: []InferenceFlow{}}},
		{"InferenceAccountStatus", InferenceAccountStatus{ManagementState: "absent", InferenceState: "absent", RouteState: "missing"}},
		{"InferenceAccountStatus", InferenceAccountStatus{ManagementState: "expired", InferenceState: "stored", RouteState: "configured", UserID: new("user"), Email: new("person@example.test"), ExpiresAt: new(AccountTimestamp("2500-01-02T03:04:05.123456789Z")), TeamID: new("team"), ProjectID: new("project"), CleanupPending: true}},
		{"InferenceCleanupResult", InferenceCleanupResult{Items: []InferenceCleanup{{ID: "AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB", ExpiresAt: AccountTimestamp(created.Format(time.RFC3339Nano)), TeamID: new("team"), KeyID: new("key"), KeyState: "pending", SessionState: "retained", Failure: new("Remote cleanup remains unconfirmed")}}}},
		{"InferenceCleanupResult", InferenceCleanupResult{Items: []InferenceCleanup{}, Failure: new("Remote cleanup remains unconfirmed")}},
		{"InferenceLogoutResult", InferenceLogoutResult{Status: InferenceAccountStatus{ManagementState: "absent", InferenceState: "absent", RouteState: "configured"}, Cleanup: []InferenceCleanup{}}},
		{"OpenAIFlowParams", OpenAIFlowParams{FlowID: "AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB"}},
		{"OpenAILoginFlow", OpenAILoginFlow{ID: "AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB", State: "authorizing", VerificationURL: new("https://auth.openai.com/codex/device"), UserCode: new("SAFE-CODE"), ExpiresAt: new(AccountTimestamp(created.Format(time.RFC3339Nano)))}},
		{"OpenAILoginFlow", OpenAILoginFlow{ID: "AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB", State: "interrupted"}},
		{"OpenAIFlowsResult", OpenAIFlowsResult{Items: []OpenAILoginFlow{}}},
		{"OpenAIAccountStatus", OpenAIAccountStatus{AuthState: "signed_out", RouteState: "missing"}},
		{"OpenAIAccountStatus", OpenAIAccountStatus{AuthState: "stored", RouteState: "configured", AccountID: new("account"), Email: new("person@example.test"), Plan: new("pro"), ExpiresAt: new(AccountTimestamp("2500-01-02T03:04:05.123456789Z"))}},
		{"Admission", Admission{Receipt: Receipt{Identity: RequestIdentity{ClientID: "client", RequestID: "deleted"}, Digest: ref.Revision, CreatedAt: created.Format(time.RFC3339Nano), DeletedAt: new(created.Format(time.RFC3339Nano))}}},
		{"CompactParams", CompactParams{Identity: RequestIdentity{ClientID: "client", RequestID: "compact"}, SessionID: child.ID}},
		{"TreeCreationParams", TreeCreationParams{CreationID: "MiXeD:Creation"}},
		{"CreateTreeParams", CreateTreeParams{CreationID: "MiXeD:Creation", Engine: "quickjs", Definition: DefinitionRef{ID: ID(ref.ID), Revision: ref.Revision}, WorkingDirectory: "/workspace", Overrides: ConfigPatch{}}},
		{"CreateTreeParams", CreateTreeParams{CreationID: "default_engine", Definition: DefinitionRef{ID: ID(ref.ID), Revision: ref.Revision}, WorkingDirectory: "/workspace", Overrides: ConfigPatch{}}},
		{"CreateTreeResult", CreateTreeResult{Creation: TreeCreation{ID: "MiXeD:Creation", TreeID: "tree_fixture", RootID: "session_root", CreatedAt: created.Format(time.RFC3339Nano)}, Tree: &Tree{ID: "tree_fixture", Engine: "quickjs", Revision: 1, CreatedAt: created.Format(time.RFC3339Nano)}, Root: &root}},
		{"CreateTreeResult", CreateTreeResult{Creation: TreeCreation{ID: "Deleted:Creation", TreeID: "tree_fixture", RootID: "session_root", CreatedAt: created.Format(time.RFC3339Nano)}, Deleted: true}},
		{"TerminalList", TerminalList{ProcessEpoch: "boot_fixture", Items: []TerminalInfo{}}},
		{"TerminalPage", TerminalPage{Terminal: TerminalInfo{ProcessEpoch: "boot_fixture", ID: "term_fixture", Cwd: "/workspace", Shell: "/bin/sh", Cols: 80, Rows: 24, Start: 9007199254740993, End: 9007199254740996, CreatedAt: created.Format(time.RFC3339Nano)}, From: 9007199254740993, Next: 9007199254740996, End: 9007199254740996, Truncated: true, DataBase64: "AAH/"}},
		{"TreeCatalog", TreeCatalog{Revision: 9007199254740993}},
		{"ListTreesParams", ListTreesParams{Limit: 100, Archived: new(false), Pinned: new(true)}},
		{"ListTreesResult", ListTreesResult{Revision: 9007199254740993, Items: []TreeSummary{{Tree: Tree{ID: "tree_fixture", Metadata: TreeMetadata{Title: new("Catalog title")}, Engine: "starlark", Revision: 9007199254740993, CreatedAt: created.Format(time.RFC3339Nano)}, RootID: "session_root", WorkingDirectory: "/workspace"}}, NextCursor: new(ID("tree_fixture"))}},
		{"ListTreesResult", ListTreesResult{Revision: 9007199254740993, Items: []TreeSummary{}}},
		{"RecentTreesParams", RecentTreesParams{Limit: 50}},
		{"RecentTreesResult", RecentTreesResult{CatalogRevision: 1, Items: []RecentTree{}, HasMore: false}},
		{"RecentTreesResult", RecentTreesResult{CatalogRevision: 1, Items: []RecentTree{{Tree: Tree{ID: "tree_recent", Engine: "starlark", Revision: 1, CreatedAt: created.Format(time.RFC3339Nano)}, RootID: root.ID, WorkingDirectory: "/workspace", Model: root.Configuration.Model, LastActivityAt: created.Format(time.RFC3339Nano)}}, HasMore: true}},
		{"ListDefinitionsParams", ListDefinitionsParams{Limit: 1, After: &DefinitionRef{ID: ID(ref.ID), Revision: ref.Revision}}},
		{"ListDefinitionsResult", ListDefinitionsResult{Items: []DefinitionSummary{{Ref: DefinitionRef{ID: ID(ref.ID), Revision: ref.Revision}, Name: "Assistant", CreatedAt: created.Format(time.RFC3339Nano)}}}},
		{"ListDefinitionsResult", ListDefinitionsResult{Items: []DefinitionSummary{}}},
		{"AutomaticTitleDecision", AutomaticTitleDecisionFromDomain(session.AutomaticTitleDecision{TreeID: "tree_fixture", SessionID: "session_root", InputID: new(session.InputID("input_title")), ConfigRevision: 9007199254740993, ExpectedRevision: 9007199254740994, Enabled: true, Model: config.Model, Source: "Authored source for one generated title.", Reason: "eligible", CreatedAt: created})},
		{"AutomaticTitleDecision", AutomaticTitleDecisionFromDomain(session.AutomaticTitleDecision{TreeID: "manual_tree", SessionID: "manual_root", ConfigRevision: 1, ExpectedRevision: 1, Model: config.Model, Reason: "manual", CreatedAt: created})},
		{"AutomaticTitleResultParams", AutomaticTitleResultParams{TreeID: "tree_fixture", AttemptID: "attempt_title"}},
		{"AutomaticTitleResult", AutomaticTitleResultFromDomain(session.AutomaticTitleResult{TreeID: "tree_fixture", AttemptID: "attempt_title", Text: "Generated title", Applied: true, CreatedAt: created})},
		{"AutomaticTitleResult", AutomaticTitleResultFromDomain(session.AutomaticTitleResult{TreeID: "tree_fixture", AttemptID: "attempt_superseded", Text: "Superseded candidate", Applied: false, CreatedAt: created})},
		{"Input", InputFromDomain(session.Input{ID: "input_title", SessionID: "session_root", Source: session.AgentInput, Kind: session.AutomaticTitleInputKind, State: session.Claimed, Parts: []session.Part{}, CreatedAt: created})},
		{"Turn", TurnFromDomain(session.Turn{ID: "turn_title", SessionID: "session_root", Kind: session.AutomaticTitleInputKind, ConfigRevision: 9007199254740993, HistoryRevision: 1, State: session.Succeeded, StartedAt: created, FinishedAt: &created})},
		{"ScheduleAdmission", ScheduleAdmissionFromDomain(session.ScheduleAdmission{ID: "schedule_fixture", Schedule: &session.ScheduleMetadata{ID: "schedule_fixture", SessionID: "session_child", Expression: "@every 0.000000001s", FirstDue: created, NextDue: &created, CreatedAt: created, PartsBytes: 123, Latest: &session.ScheduleInput{ScheduleID: "schedule_fixture", ScheduledFor: created, InputID: "scheduled_input", ClientID: "schedule", RequestID: "slot_fixture"}}})},
		{"FormulateGoalParams", FormulateGoalParams{Identity: RequestIdentity{ClientID: "client", RequestID: "formulate"}, SessionID: "session_child", Request: GoalFormulationRequest{GoalID: "goal_formulated"}}},
		{"FormulateGoalParams", FormulateGoalParams{Identity: RequestIdentity{ClientID: "client", RequestID: "formulate_zero"}, SessionID: "session_child", Request: GoalFormulationRequest{GoalID: "goal_formulated", MaxContinuations: new(Counter(0)), TailMessages: 100, Start: true}}},
		{"GoalFormulation", GoalFormulationFromDomain(session.GoalFormulation{HistoryRevision: 9007199254740993, InputID: "input_formulate", SessionID: "session_child", Request: session.GoalFormulationRequest{GoalID: "goal_formulated", MaxContinuations: new(int64(9007199254740993)), TailMessages: 8}, AfterSequence: 9007199254740993, ThroughSequence: 9007199254740994, TurnID: "turn_formulate", AttemptID: "attempt_formulate", Text: "Build the requested exporter.", CreatedAt: created})},
		{"GoalFormulation", GoalFormulationFromDomain(session.GoalFormulation{HistoryRevision: 9007199254740993, InputID: "input_rejected", SessionID: "session_child", Request: session.GoalFormulationRequest{GoalID: "goal_rejected", Expected: &session.GoalRef{ID: "previous", Revision: 9007199254740993}, TailMessages: 2}, TurnID: "turn_rejected", AttemptID: "attempt_rejected", Text: "Retained candidate.", Rejection: new("conflicting revision"), CreatedAt: created})},
		{"Input", Input{ID: "input_formulate", SessionID: child.ID, Source: "user", Kind: "goal_formulation", State: "queued", Parts: []Part{}, CreatedAt: created.Format(time.RFC3339Nano)}},
		{"Turn", Turn{ID: "turn_formulate", SessionID: child.ID, Kind: "goal_formulation", HistoryRevision: 9007199254740993, ConfigRevision: 9007199254740993, State: "interrupted", StartedAt: created.Format(time.RFC3339Nano), FinishedAt: new(created.Format(time.RFC3339Nano))}},
		{"CreateGoalParams", CreateGoalParams{SessionID: "session_child", GoalID: "goal_default", Spec: GoalRequest{Text: "Objective"}}},
		{"CreateGoalParams", CreateGoalParams{SessionID: "session_child", GoalID: "goal_zero", ExpectedCurrent: &GoalRef{ID: "goal_default", Revision: 9007199254740993}, Spec: GoalRequest{Text: "Initial only", MaxContinuations: new(Counter(0))}, Start: true}},
		{"Goal", GoalFromDomain(session.Goal{ID: "goal_fixture", Revision: 9007199254740993, SessionID: "session_child", Spec: session.GoalSpec{Text: "Objective", MaxContinuations: 9007199254740994}, OriginFormulationAttemptID: new(session.ModelAttemptID("attempt_formulate")), State: session.GoalCompleted, ContinuationsUsed: 9007199254740993, CompletionTurnID: new(session.TurnID("turn_fixture")), CompletionOperationID: new(session.OperationID("operation_fixture")), CreatedAt: created})},
		{"CurrentGoalResult", CurrentGoalResult{}},
		{"GoalAdmission", GoalAdmission{ID: "goal_deleted", DeletedAt: new(created.Format(time.RFC3339Nano))}},
		{"ResumeGoalParams", ResumeGoalParams{SessionID: "session_child", Identity: RequestIdentity{ClientID: "client", RequestID: "resume"}, Goal: GoalRef{ID: "goal_fixture", Revision: 9007199254740993}}},
		{"CreateScheduleParams", CreateScheduleParams{SessionID: "session_child", ScheduleID: "schedule_fixture", Expression: "@at 2500-01-02T03:04:05.123456789Z", Parts: []Part{{Type: "text", Text: "wake up"}}}},
		{"SchedulesResult", SchedulesResult{Items: []ScheduleMetadata{}, NextCursor: &ScheduleCursor{ID: "schedule_fixture", Due: "2500-01-02T03:04:05.123456789Z"}}},
		{"Input", InputFromDomain(session.Input{ID: "scheduled_input", SessionID: "session_child", Kind: session.PromptInput, Source: session.ScheduledInput, State: session.Queued, Parts: []session.Part{{Type: "text", Text: "wake"}}, CreatedAt: created, Schedule: &session.ScheduleOccurrence{ScheduleID: "schedule_fixture", ScheduledFor: created}})},
		{"Input", InputFromDomain(session.Input{ID: "goal_input", SessionID: "session_child", Source: session.GoalInput, Kind: session.PromptInput, State: session.Queued, Goal: &session.GoalRef{ID: "goal_fixture", Revision: 9007199254740993}, Parts: []session.Part{{Type: "text", Text: "Work on the goal."}}, CreatedAt: created})},
		{"Input", Input{ID: "input_compact", SessionID: child.ID, Source: "user", Kind: "compact", State: "queued", Parts: []Part{}, CreatedAt: created.Format(time.RFC3339Nano)}},
		{"ContextHead", ContextHeadFromDomain(session.ContextHead{SessionID: session.SessionID(child.ID), Revision: 9007199254740993, CompactionID: new(session.CompactionID("summary"))})},
		{"ContextHead", ContextHeadFromDomain(session.ContextHead{SessionID: session.SessionID(child.ID)})},
		{"CompactionResult", CompactionResult{Metadata: CompactionFromDomain(session.CompactionMetadata{HistoryRevision: 9007199254740993, ID: "summary", SessionID: session.SessionID(child.ID), TurnID: "turn", AttemptID: "attempt", ExpectedRevision: 9007199254740993, ThroughSequence: 9007199254740994, PinnedMessageIDs: []session.MessageID{"message"}, TextBytes: 14, CreatedAt: created}), Text: "Exact summary."}},
		{"SelectCompactionParams", SelectCompactionParams{SessionID: child.ID, ExpectedRevision: 9007199254740993}},
		{"RewindParams", RewindParams{EditID: "edit", SessionID: child.ID, ExpectedRevision: 9007199254740993, ObservedThrough: 9007199254740995, KeepThrough: 0}},
		{"ForkParams", ForkParams{ForkID: "fork", SessionID: child.ID, ExpectedHistoryRevision: 9007199254740993, ExpectedConfigRevision: 9007199254740994, ObservedThrough: 9007199254740995, KeepThrough: 0}},
		{"ForkResult", ForkResult{Fork: Fork{ID: "fork", SessionID: child.ID, ExpectedHistoryRevision: 9007199254740993, ExpectedConfigRevision: 9007199254740994, ObservedThrough: 9007199254740995, TreeID: "deleted_tree", RootID: "deleted_root", CreatedAt: created.Format(time.RFC3339Nano)}, Deleted: true}},
		{"HistoryEdit", HistoryEditFromDomain(session.HistoryEdit{ID: "edit", SessionID: session.SessionID(child.ID), Digest: ref.Revision, ExpectedRevision: 9007199254740993, Revision: 9007199254740994, ObservedThrough: 9007199254740995, KeepThrough: 0, CreatedAt: created})},
		{"Message", MessageFromDomain(session.Message{ID: "imported", SessionID: "session_child", GroupID: "imported_group", OpeningInput: true, Source: &session.MessageSource{SessionID: "source", MessageID: "original", Sequence: 9007199254740993}, Sequence: 1, Role: session.User, Parts: []session.Part{{Type: "text", Text: "Imported authored input"}}, CreatedAt: created})},
		{"CompactionResult", CompactionResult{Metadata: CompactionFromDomain(session.CompactionMetadata{HistoryRevision: 1, ID: "imported_summary", SessionID: "session_child", Source: &session.CompactionSource{SessionID: "source", CompactionID: "source_summary"}, ThroughSequence: 1, PinnedMessageIDs: []session.MessageID{}, TextBytes: 8, CreatedAt: created}), Text: "Summary."}},
		{"HistorySnapshot", HistorySnapshot{Revision: 9007199254740993, SessionID: child.ID, ThroughSequence: 9007199254740993, MessageCount: 9007199254740993}},
		{"ReadHistoryResult", ReadHistoryResult{Message: HistoryMetadata{GroupID: "turn", ID: "message", SessionID: child.ID, TurnID: new(ID("turn")), Sequence: 9007199254740993, Role: "assistant", PartsBytes: 88}, Offset: 1, NextOffset: new(Counter(6)), DataBase64: base64.StdEncoding.EncodeToString([]byte{0x9f, 0x8c, 0x8d, '\n', '9'})}},
		{"HistoryMetadataResult", HistoryMetadataResult{Revision: 9007199254740993, Items: []HistoryMetadata{}, ThroughSequence: 9007199254740993}},
		{"SearchHistoryResult", SearchHistoryResult{Revision: 9007199254740993, Matches: []HistoryMatch{}, ThroughSequence: 9007199254740993, NextAfter: new(Counter(100)), ScannedMessages: 100, ScannedBytes: 65536}},
		{"TurnOutputResult", TurnOutputFromDomain(nil)},
		{"ListSkillsParams", ListSkillsParams{SessionID: "session_root", Prefix: "my", After: "my-a", Limit: 1}},
		{"ListSkillsResult", ListSkillsResult{Items: []SkillMetadata{}}},
		{"ListSkillsResult", ListSkillsResult{Items: []SkillMetadata{{Name: "my-skill", Description: "Selected explicitly.", Disabled: true, Source: InstructionSource{Kind: "skill_metadata", Scope: "workspace", Path: ".agents/skills/my-skill/SKILL.md", Bytes: 40, SHA256: ref.Revision}}}, NextAfter: new("my-skill")}},
		{"InstructionManifestResult", InstructionManifestFromDomain(&session.InstructionManifest{Bytes: 80000, SHA256: ref.Revision, Sources: []session.InstructionSource{{Kind: "invoked_skill", Scope: "workspace", Path: ".agents/skills/my-skill/SKILL.md", Bytes: 70000, SHA256: ref.Revision}}})},
		{"InstructionManifestResult", InstructionManifestFromDomain(&session.InstructionManifest{Bytes: 100, SHA256: ref.Revision, Sources: []session.InstructionSource{{Kind: "skill_metadata", Scope: "host", RootID: new("team"), Path: "review/SKILL.md", Bytes: 40, SHA256: ref.Revision}}})},
		{"InstructionManifestResult", InstructionManifestFromDomain(&session.InstructionManifest{Bytes: 100, SHA256: ref.Revision, Sources: []session.InstructionSource{{Kind: "standing_instructions", Scope: "host", RootID: new("standing"), Path: "me.md", Bytes: 40, SHA256: ref.Revision}}})},
		{"InstructionManifestResult", InstructionManifestFromDomain(&session.InstructionManifest{Bytes: 100, SHA256: ref.Revision, Sources: []session.InstructionSource{{Kind: "project_file", Scope: "project", RootID: new("project"), Path: "app/AGENTS.md", Bytes: 40, SHA256: ref.Revision}}})},
		{"InstructionManifestResult", InstructionManifestFromDomain(nil)},
		{"InstructionManifestResult", InstructionManifestFromDomain(&session.InstructionManifest{Bytes: 40, SHA256: ref.Revision, Sources: []session.InstructionSource{{Kind: "project_file", Scope: "workspace", Path: "AGENTS.md", Bytes: 5, SHA256: hex.EncodeToString(contentDigest[:])}}})},
		{"InstructionManifestResult", InstructionManifestFromDomain(&session.InstructionManifest{Bytes: 20, SHA256: ref.Revision})},
		{"TurnOutputResult", TurnOutputFromDomain(&session.StructuredOutput{TurnID: "turn_fixture", MessageID: "message_output", Value: json.RawMessage(`{"count":9007199254740993}`)})},
		{"TurnOutputResult", TurnOutputFromDomain(&session.StructuredOutput{TurnID: "turn_null", MessageID: "message_null", Value: json.RawMessage(`null`)})},
		{"ListCompletionsResult", ListCompletionsResult{Items: []CompletionMetadata{{ParentID: "session_root", ChildID: "session_deleted", TurnID: "turn_fixture", InputID: new(ID("input_fixture")), State: "interrupted", Failure: new("runtime restarted"), Mode: "message", FinishedAt: created.Format(time.RFC3339Nano), TextBytes: 0, OmittedParts: 0}}}},
		{"ReadCompletionParams", ReadCompletionParams{ParentID: "session_root", ChildID: "session_deleted", TurnID: "turn_fixture", Offset: 0, Length: 65536}},
		{"ReadCompletionResult", ReadCompletionResult{Completion: CompletionMetadata{ParentID: "session_root", ChildID: "session_deleted", TurnID: "turn_fixture", State: "succeeded", Mode: "inline", FinishedAt: created.Format(time.RFC3339Nano), TextBytes: 70000}, Offset: 65536, TotalBytes: 71000, DataBase64: "e30="}},
		{"MailAdmission", MailAdmission{MailID: "mail_deleted", DeletedAt: new(created.Format(time.RFC3339Nano))}},
		{"SendMailParams", SendMailParams{MailID: "mail_evidence", SenderID: "session_root", RecipientID: "session_child", Delivery: "next_turn", EvidenceRef: new(ID("sender_evidence"))}},
		{"ReadMailResult", ReadMailResult{Mail: MailMetadataFromDomain(session.MailMetadata{ID: "mail_evidence", Revision: 1, Source: session.MailSource{Kind: "session", ID: "session_root"}, RecipientID: "session_child", Delivery: session.MailNextTurn, EvidenceRef: new("recipient_evidence"), State: session.MailPending, AvailableAt: created, CreatedAt: created, RevisedAt: created})}},
		{"ReadMailResult", ReadMailResult{Mail: MailMetadataFromDomain(session.MailMetadata{ID: "mail_fixture", Revision: 128, Source: session.MailSource{Kind: "session", ID: "session_root"}, RecipientID: "session_child", Delivery: session.MailNextTurn, Subject: "Subject", BodyBytes: 5, State: session.MailPending, AvailableAt: created, CreatedAt: created, RevisedAt: created}), Body: "hello"}},
		{"Message", MessageFromDomain(session.Message{ID: "message_mail", SessionID: "session_child", GroupID: "turn_fixture", TurnID: "turn_fixture", Sequence: 9007199254740995, Role: session.User, Mail: &session.MailRef{ID: "mail_fixture", Revision: 128, Presentation: session.MailDigest}, Parts: []session.Part{{Type: "text", Text: "Mail from session_root: Subject"}}, CreatedAt: created})},
		{"ResourceUsage", ResourceUsageFromDomain(session.ResourceUsage{SessionID: "session_root", Kind: session.ResourceQueuedInputs, Revision: 9007199254740993, Limit: new(int64(9007199254740994)), Used: 9007199254740993})},
		{"ResourceUsage", ResourceUsageFromDomain(session.ResourceUsage{SessionID: "session_child", Kind: session.ResourceDescendants, Revision: 0, Limit: nil, Used: 0})},
		{"SetResourceParams", SetResourceParams{SessionID: "session_child", ExpectedRevision: 9007199254740993, Resource: ResourceLimit{Kind: "descendants", Limit: nil}}},
		{"Budget", BudgetFromDomain(session.Budget{SessionID: "session_root", Kind: session.BudgetModelTokens, Revision: 9007199254740993, Limit: new(int64(9007199254740994)), Used: 9007199254740993, Reserved: 1})},
		{"MatchReceiptParams", MatchReceiptParams{Method: "sessions.compact", ParamsBase64: base64.StdEncoding.EncodeToString([]byte(`{"session_id":"session_root","identity":{"client_id":"client","request_id":"compact"}}`))}},
		{"SessionActivity", SessionActivity{SessionID: child.ID, Lifecycle: "active", QueuedInputCount: 9007199254740993}},
		{"InputPageParams", InputPageParams{SessionID: child.ID, State: "queued", After: new(Counter(9007199254740993)), Limit: 100}},
		{"TurnPageParams", TurnPageParams{SessionID: child.ID, Before: new(ID("turn_cursor")), Limit: 100}},
		{"TurnPageResult", TurnPageResult{Items: []Turn{{ID: "turn_direct", SessionID: child.ID, Kind: "host_operation", HistoryRevision: 9007199254740993, ConfigRevision: 9007199254740993, State: "succeeded", StartedAt: created.Format(time.RFC3339Nano), FinishedAt: new(created.Format(time.RFC3339Nano))}}, NextCursor: new(ID("turn_direct"))}},
		{"Message", MessageFromDomain(session.Message{ID: "authored", SessionID: "session_child", GroupID: "turn_fixture", InputID: new(session.InputID("input_fixture")), InputIdentity: &session.RequestIdentity{ClientID: "client_fixture", RequestID: "request_fixture"}, Sequence: 9007199254740993, Role: session.User, Parts: []session.Part{{Type: "text", Text: "Authored input"}}, CreatedAt: created})},
		{"InputPageResult", InputPageResult{Items: []InputSummary{{ID: "internal_fixture", SessionID: child.ID, Ordinal: 1, Source: "agent", Kind: "prompt", State: "queued", CreatedAt: created.Format(time.RFC3339Nano)}}}},
		{"InputPageResult", InputPageResult{Items: []InputSummary{{Identity: &RequestIdentity{ClientID: "client_fixture", RequestID: "request_fixture"}, ID: "queued_fixture", SessionID: child.ID, Ordinal: 9007199254740993, Source: "user", Kind: "prompt", State: "queued", CreatedAt: created.Format(time.RFC3339Nano), TextPreview: "Preview", PreviewTruncated: true, AttachmentCount: 1}}, NextCursor: new(Counter(9007199254740993))}},
		{"SessionObservation", SessionObservation{Snapshot: HistorySnapshot{Revision: 9007199254740993, SessionID: child.ID, ThroughSequence: 9007199254740995, MessageCount: 3}, Epoch: "boot_fixture", Messages: []Message{message}, Preview: &MessagePreview{AttemptID: "attempt_live", TurnID: "turn_fixture", MessageID: "message_live", Revision: 9007199254740993, Text: "In progress", Reasoning: "Considering the request", Calls: []CallPreview{{Index: 0, ID: "call_partial", Name: "execute", Arguments: `{"code":"print(`}}}}},
		{"SessionObservation", SessionObservation{Snapshot: HistorySnapshot{Revision: 9007199254740993, SessionID: child.ID, ThroughSequence: 9007199254740995, MessageCount: 3}, Epoch: "boot_restarted", Messages: []Message{}, Preview: nil}},
		{"Grant", GrantFromDomain(session.Grant{ID: "grant_fixture", SessionID: "session_child", Capability: "files.read", Resource: "/workspace", IssuerID: new(session.GrantID("grant_parent")), CreatedAt: created})},
		{"HostOperation", OperationFromDomain(session.Operation{ID: "operation_fixture", CellID: "cell_fixture", RequestID: "1:1", Capability: "files.read", Resource: "/workspace", Arguments: json.RawMessage(`{"path":"example.txt","offset":1,"limit":2000}`), SessionID: "session_child", TurnID: "turn_fixture", State: session.OperationSucceeded, GrantID: new(session.GrantID("grant_fixture")), Result: &session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(`{"output":"1: hello"}`), ContentReferences: []string{"image_ref"}}, CreatedAt: created, DispatchedAt: &created, FinishedAt: &created})},
		{"Permission", PermissionFromDomain(session.Permission{OperationID: "operation_fixture", State: session.PermissionApproved, CreatedAt: created, ResolvedAt: &created})},
		{"Cell", CellFromDomain(session.Cell{ID: "cell_fixture", SessionID: "session_child", TurnID: "turn_fixture", CallMessageID: "message_call", CallID: "call_fixture", State: session.CellSucceeded, ResultMessageID: new(session.MessageID("message_result")), Checkpoint: &session.Checkpoint{Digest: ref.Revision, Size: 123, Engine: session.Starlark, Metadata: json.RawMessage(`{"format_version":1}`)}, CreatedAt: created, FinishedAt: &created})},
		{"PutContentParams", PutContentParams{SessionID: child.ID, ReferenceID: "content_fixture", MediaType: "text/plain", DataBase64: "aGVsbG8="}},
		{"ReadContentResult", ReadContentResult{
			Reference:  ContentReferenceFromDomain(session.ContentReference{ID: "content_fixture", SessionID: session.SessionID(child.ID), Digest: hex.EncodeToString(contentDigest[:]), Size: 5, MediaType: "text/plain", CreatedAt: created}),
			DataBase64: "aGVsbG8=",
		}},
		{"ModelAttemptsResult", ModelAttemptsResult{Items: []ModelAttempt{attempt}}},
		{"ModelAttemptsResult", ModelAttemptsResult{Items: []ModelAttempt{helper}}},
		{"ModelAttemptsResult", ModelAttemptsResult{Items: []ModelAttempt{titleAttempt}}},
		{"InitializeParams", InitializeParams{Major: Major, ExpectedRuntimeID: new(ID("runtime_fixture"))}},
		{"InitializeResult", InitializeResult{ProcessEpoch: "boot_fixture", Major: Major, Minor: Minor, RuntimeID: "runtime_fixture", Builtins: []DefinitionRef{{ID: ID(ref.ID), Revision: ref.Revision}}}},
		{"Request", Request{JSONRPC: "2.0", ID: "call", Method: "initialize", Params: json.RawMessage(`{"major":4}`)}},
		{"Response", Response{JSONRPC: "2.0", ID: "call", Result: json.RawMessage(`{"items":[]}`)}},
		{"Response", Response{JSONRPC: "2.0", ID: "call", Error: &RPCError{Code: -32009, Kind: "CONFLICT", Message: "request conflict"}}},
		{"Session", root},
		{"Session", child},
		{"HistoryPageParams", HistoryPageParams{SessionID: child.ID, Direction: "backward", Cursor: new(Counter(9007199254740995)), ExpectedRevision: new(Counter(9007199254740993)), Limit: 10}},
		{"HistoryPageResult", HistoryPageResult{Snapshot: HistorySnapshot{Revision: 9007199254740993, SessionID: child.ID, ThroughSequence: 9007199254740995, MessageCount: 3}, Messages: []Message{message}, NextCursor: new(Counter(9007199254740993))}},
		{"Session", modelFree},
		{"HistoryResult", HistoryResult{Snapshot: HistorySnapshot{Revision: 9007199254740993, SessionID: child.ID, ThroughSequence: 9007199254740995, MessageCount: 3}, Items: []Message{message, callMessage, toolMessage}}},
		{"Part", callMessage.Parts[0]},
		{"Part", toolMessage.Parts[0]},
		{"Message", MessageFromDomain(session.Message{ID: "message_image", SessionID: "session_child", GroupID: "turn_fixture", TurnID: "turn_fixture", Sequence: 9007199254740996, Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "image_call", Output: "unchanged output"}}, {Type: "content", ReferenceID: "image_ref"}}, CreatedAt: created})},
		{"SpawnSessionParams", SpawnSessionParams{Identity: RequestIdentity{ClientID: "client", RequestID: "spawn"}, ParentID: root.ID, Parts: []Part{{Type: "text", Text: "Child work"}}, GrantIDs: []ID{}}},
		{"SpawnSessionParams", SpawnSessionParams{Identity: RequestIdentity{ClientID: "client", RequestID: "transfer"}, ParentID: root.ID, Parts: []Part{{Type: "text", Text: "Transferred child"}}, BrowserAttachments: []ID{"attachment-1", "attachment-2", "attachment-3", "attachment-4"}}},
		{"Response", Response{JSONRPC: "2.0", ID: "unavailable", Error: &RPCError{Code: -32036, Kind: "HOST_UNAVAILABLE", Message: "host service unavailable"}}},
		{"Response", Response{JSONRPC: "2.0", ID: "transfer", Error: &RPCError{Code: -32038, Kind: "TRANSFER_UNCERTAIN", Message: "accepted child browser transfer has an uncertain effect; it will not be repeated"}}},
		{"Input", InputFromDomain(session.Input{ID: "private-transfer", SessionID: session.SessionID(root.ID), Source: session.UserInput, Kind: session.HostOperationInputKind, State: session.Claimed, Parts: []session.Part{}, TurnID: new(session.TurnID("host-turn")), HostOperation: &session.HostOperation{Module: "agents", Name: "spawn", Arguments: json.RawMessage(`{"identity":{"ClientID":"client","RequestID":"transfer"},"request":{"parent_id":"session_root","browser_attachments":["attachment"]}}`)}, CreatedAt: created})},
		{"SubmitParams", SubmitParams{Identity: RequestIdentity{ClientID: "client", RequestID: "request"}, SessionID: child.ID, Source: "user", Parts: []Part{{Type: "text", Text: "Run this."}}}},
		{"SteerInputParams", SteerInputParams{EditID: "edit", SessionID: "root", InputID: "queued", TurnID: "active"}},
		{"InputSteeringParams", InputSteeringParams{EditID: "edit", SessionID: "root"}},
		{"InputSteeringResult", InputSteeringResult{ID: "edit", SessionID: "root", InputID: "queued", TurnID: "active", CreatedAt: created.Format(time.RFC3339Nano), Deleted: true}},
		{"SubmitParams", SubmitParams{Identity: RequestIdentity{ClientID: "human", RequestID: "steer"}, SessionID: child.ID, Source: "user", Parts: []Part{{Type: "text", Text: "More"}}, Delivery: "steer", TargetTurnID: new(ID("active"))}},
		{"Input", InputFromDomain(session.Input{ID: "queued", SessionID: "root", Source: session.UserInput, Kind: session.PromptInput, State: session.Claimed, Parts: []session.Part{{Type: "text", Text: "More"}}, TurnID: new(session.TurnID("active")), Steering: &session.InputSteeringRef{ID: "edit", TurnID: "active", Consumed: true}, CreatedAt: created})},
		{"SubmitParams", SubmitParams{Identity: RequestIdentity{ClientID: "human", RequestID: "design"}, SessionID: child.ID, Source: "user", Parts: []Part{{Type: "text", Text: "Selected evidence"}, {Type: "content", ReferenceID: "context"}}, DesignContext: &DesignContext{ContextAttachmentID: "context", Elements: []DesignContextElement{{Label: "Save", Selector: "button.save"}}, ElementCount: 1, PageTitle: "Settings"}}},
		{"UpdateConfigurationParams", UpdateConfigurationParams{SessionID: child.ID, ExpectedRevision: 9007199254740993, Patch: ConfigPatch{Modules: []ID{}, AutomaticTitle: new(false), GoalsEnabled: new(false), Compaction: &CompactionPolicy{Model: nil, ThresholdPercent: 0}, ReportMode: new("inline"), Tools: map[string]ToolDeclaration{}, Output: &OutputPolicy{}}}},
		{"Turn", Turn{Goal: &GoalRef{ID: "goal_fixture", Revision: 9007199254740993}, ID: "turn_fixture", SessionID: child.ID, Kind: "prompt", HistoryRevision: 9007199254740993, ConfigRevision: 9007199254740993, State: "running", StartedAt: created.Format(time.RFC3339Nano)}},
	}
	result := make([]Fixture, 0, len(values)+8)
	for _, value := range values {
		raw, err := json.Marshal(value.value)
		if err != nil {
			return nil, err
		}
		result = append(result, Fixture{Type: value.name, Value: raw, Valid: true})
	}
	profile := HostProfile{ID: "remote", Name: "Remote", URL: "https://example.test", RuntimeID: "runtime"}
	tooMany := make([]HostProfile, 17)
	for index := range tooMany {
		tooMany[index] = profile
	}
	badURL := profile
	badURL.URL = "https://" + strings.Repeat("a", 2048)
	for _, profiles := range [][]HostProfile{nil, tooMany, {badURL}} {
		raw, err := json.Marshal(SetHostProfilesParams{ExpectedRevision: ref.Revision, Profiles: profiles})
		if err != nil {
			return nil, err
		}
		result = append(result, Fixture{Type: "SetHostProfilesParams", Value: raw, Valid: false})
	}
	for _, test := range []struct {
		raw   string
		valid bool
	}{
		{`{"session_id":"session_child","expected_revision":"1","patch":{"model":{"provider":"p","name":"model","effort":"","temperature":null,"top_p":null}}}`, true},
		{`{"session_id":"session_child","expected_revision":"1","patch":{"model":{"provider":"p","name":"model","effort":"","temperature":0,"top_p":1}}}`, true},
		{`{"session_id":"session_child","expected_revision":"1","patch":{"model":{"provider":"p","name":"model","effort":"","temperature":2.01}}}`, false},
		{`{"session_id":"session_child","expected_revision":"1","patch":{"model":{"provider":"p","name":"model","effort":"","top_p":-0.01}}}`, false},
		{`{"session_id":"session_child","expected_revision":"1","patch":{"model":{"provider":"p","name":"model","effort":"","top_p":1.01}}}`, false},
		{`{"session_id":"session_child","expected_revision":"1","patch":{"model":{"provider":"p","name":"model","effort":"","temperature":"0"}}}`, false},
	} {
		result = append(result, Fixture{Type: "UpdateConfigurationParams", Value: json.RawMessage(test.raw), Valid: test.valid})
	}
	for _, raw := range []string{
		`{"identity":{"client_id":"client","request_id":"formulate"},"session_id":"session_child","request":{"goal_id":"goal","expected_current":null,"start":false,"tail_messages":-1}}`,
		`{"identity":{"client_id":"client","request_id":"formulate"},"session_id":"session_child","request":{"goal_id":"goal","expected_current":null,"start":false,"tail_messages":1}}`,
		`{"identity":{"client_id":"client","request_id":"formulate"},"session_id":"session_child","request":{"goal_id":"goal","expected_current":null,"start":false,"tail_messages":101}}`,
	} {
		result = append(result, Fixture{Type: "FormulateGoalParams", Value: json.RawMessage(raw), Valid: false})
	}
	for _, raw := range []string{
		`{"session_id":"session_child","after":9007199254740993,"limit":10}`,
		`{"session_id":"session_child","after":"9223372036854775808","limit":10}`,
		`{"session_id":"session_child","after":"01","limit":10}`,
		`{"session_id":"session_child","after":"-1","limit":10}`,
		`{"session_id":"session_child","after":"0","limit":101}`,
		`{"session_id":"session_child","after":"0","limit":10,"unexpected":true}`,
	} {
		result = append(result, Fixture{Type: "HistoryParams", Value: json.RawMessage(raw), Valid: false})
	}
	for _, raw := range []string{
		`{"session_id":"session_child","expected_revision":"1","resource":{"kind":"queued_inputs","limit":1}}`,
		`{"session_id":"session_child","expected_revision":1,"resource":{"kind":"queued_inputs","limit":"1"}}`,
		`{"session_id":"session_child","expected_revision":"1","resource":{"kind":"unknown","limit":"1"}}`,
		`{"session_id":"session_child","expected_revision":"1","resource":{"kind":"depth","limit":"9223372036854775808"}}`,
	} {
		result = append(result, Fixture{Type: "SetResourceParams", Value: json.RawMessage(raw), Valid: false})
	}
	result = append(result, Fixture{Type: "SubmitParams", Valid: false, Value: json.RawMessage(`{"identity":{"client_id":"client","request_id":"request"},"session_id":"session_child","source":"user","parts":[{"type":"text","text":"x","reference_id":"content"}]}`)})
	result = append(result, Fixture{Type: "SubmitParams", Valid: false, Value: json.RawMessage(`{"identity":{"client_id":"client","request_id":"request"},"session_id":"session_child","source":"user","parts":[{"type":"tool_call","call":{"id":"call_fixture","name":"execute","arguments":{"code":"print(1)"}}}]}`)})
	for _, raw := range []string{
		`{"type":"tool_call","call":{"id":"call","name":"execute","arguments":null}}`,
		`{"type":"tool_call","call":{"id":"call","name":"execute","arguments":[]}}`,
		`{"type":"tool_call","call":{"id":"call","name":"execute","arguments":{}},"text":"extra"}`,
		`{"type":"tool_result","result":{"call_id":"call","output":"missing flag"}}`,
	} {
		result = append(result, Fixture{Type: "Part", Valid: false, Value: json.RawMessage(raw)})
	}
	callMessage.Role = "user"
	toolMessage.Role = "assistant"
	for _, message := range []Message{callMessage, toolMessage} {
		raw, err := json.Marshal(HistoryResult{Snapshot: HistorySnapshot{Revision: 9007199254740993, SessionID: child.ID, ThroughSequence: 9007199254740995, MessageCount: 3}, Items: []Message{message}})
		if err != nil {
			return nil, err
		}
		result = append(result, Fixture{Type: "HistoryResult", Value: raw, Valid: false})
	}
	for _, raw := range []string{`{"jsonrpc":"2.0","id":"call"}`, `{"jsonrpc":"2.0","id":"call","result":{},"error":{"code":-32009,"kind":"CONFLICT","message":"conflict"}}`} {
		result = append(result, Fixture{Type: "Response", Value: json.RawMessage(raw), Valid: false})
	}
	for _, raw := range []string{`{"mcp_servers":{"all":true,"servers":["fixture"]}}`, `{"mcp_servers":{"all":false,"servers":null}}`, `{"mcp_servers":{"all":false,"servers":["duplicate","duplicate"]}}`} {
		result = append(result, Fixture{Type: "UpdateConfigurationParams", Value: json.RawMessage(`{"session_id":"root","expected_revision":"1","patch":` + raw + `}`), Valid: false})
	}
	result = append(result, Fixture{Type: "InitializeParams", Value: json.RawMessage(`{"major":3}`), Valid: false})
	for _, expiry := range []string{"2026-02-29T12:00:00Z", "2026-09-27T24:00:00Z", "2026-09-27T12:00:00.1234567891Z", "2026-09-27T12:00:00.10Z", "2026-09-27T12:00:00+00:00", "0000-01-01T00:00:00Z", "not-a-time"} {
		raw, err := json.Marshal(OpenAIAccountStatus{AuthState: "stored", RouteState: "missing", ExpiresAt: new(AccountTimestamp(expiry))})
		if err != nil {
			return nil, err
		}
		result = append(result, Fixture{Type: "OpenAIAccountStatus", Value: raw, Valid: false})
	}

	for _, delivery := range []string{"queued", "invalid"} {
		raw, err := json.Marshal(SubmitParams{Identity: RequestIdentity{ClientID: "human", RequestID: "steer"}, SessionID: child.ID, Source: "user", Parts: []Part{{Type: "text", Text: "More"}}, Delivery: delivery, TargetTurnID: new(ID("active"))})
		if err != nil {
			return nil, err
		}
		result = append(result, Fixture{Type: "SubmitParams", Value: raw, Valid: false})
	}
	questions, err := questionFixtures(created)
	if err != nil {
		return nil, err
	}
	result = append(result, questions...)
	return workspaceFixtures(append(result, permissionModeFixtures()...), created)
}
