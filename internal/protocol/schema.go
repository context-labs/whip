package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/context-labs/whip/internal/hostmodule"
	"github.com/context-labs/whip/internal/session"
	"github.com/google/jsonschema-go/jsonschema"
)

type Operation struct {
	Name           string
	Params, Result reflect.Type
}

func Operations() []Operation {
	return []Operation{
		{"sessions.reload", reflect.TypeFor[ReloadSessionParams](), reflect.TypeFor[ReloadEdit]()},
		{"sessions.reload_edit", reflect.TypeFor[ReloadEditParams](), reflect.TypeFor[ReloadEdit]()},
		{"sessions.cancel_reload", reflect.TypeFor[ReloadEditParams](), reflect.TypeFor[ReloadEdit]()},
		{"host.status", reflect.TypeFor[EmptyParams](), reflect.TypeFor[HostStatus]()},
		{"host.stop", reflect.TypeFor[StopHostParams](), reflect.TypeFor[HostStopAccepted]()},
		{"workspace.complete", reflect.TypeFor[WorkspaceCompletionParams](), reflect.TypeFor[WorkspaceCompletionResult]()},
		{"workspace.inspect", reflect.TypeFor[SessionParams](), reflect.TypeFor[WorkspaceInspection]()},
		{"workspace.set", reflect.TypeFor[WorkspaceSetParams](), reflect.TypeFor[ControlEdit]()},
		{"run.configure", reflect.TypeFor[RunConfigureParams](), reflect.TypeFor[ControlEdit]()},
		{"browser.provider.bind", reflect.TypeFor[BrowserProviderBindParams](), reflect.TypeFor[BrowserProviderBindResult]()},
		{"browser.provider.unbind", reflect.TypeFor[BrowserProviderUnbindParams](), reflect.TypeFor[BrowserAccepted]()},
		{"browser.provider.event", reflect.TypeFor[BrowserProviderEventParams](), reflect.TypeFor[BrowserAccepted]()},
		{"browser.command.result", reflect.TypeFor[BrowserCommandResultParams](), reflect.TypeFor[BrowserAccepted]()},
		{"browser.screenshot.chunk", reflect.TypeFor[BrowserScreenshotChunkParams](), reflect.TypeFor[BrowserAccepted]()},
		{"browser.inventory.result", reflect.TypeFor[BrowserInventoryResultParams](), reflect.TypeFor[BrowserAccepted]()},
		{"browser.attachments", reflect.TypeFor[SessionParams](), reflect.TypeFor[BrowserAttachmentsResult]()},
		{"browser.tabs", reflect.TypeFor[SessionParams](), reflect.TypeFor[BrowserTabsResult]()},
		{"models.inspection", reflect.TypeFor[ModelInspectionParams](), reflect.TypeFor[ModelInspection]()},
		{"trace.page", reflect.TypeFor[TracePageParams](), reflect.TypeFor[TracePageResult]()},
		{"trace.export", reflect.TypeFor[TraceExportParams](), reflect.TypeFor[TraceExportResult]()},
		{"host.attention", reflect.TypeFor[HostAttentionParams](), reflect.TypeFor[HostAttentionResult]()},
		{"host.directories.list", reflect.TypeFor[HostDirectoriesParams](), reflect.TypeFor[HostDirectoriesResult]()},
		{"host.directory.pick", reflect.TypeFor[HostDirectoryPickParams](), reflect.TypeFor[HostDirectoryPickResult]()},
		{"host.directory.create", reflect.TypeFor[HostDirectoryCreateParams](), reflect.TypeFor[HostDirectoryCreateResult]()},
		{"host.skills.roots", reflect.TypeFor[EmptyParams](), reflect.TypeFor[HostSkillRoots]()},
		{"host.skills.publish", reflect.TypeFor[PublishSkillRootParams](), reflect.TypeFor[HostSkillRoots]()},
		{"host.skills.set_defaults", reflect.TypeFor[SetDefaultSkillRootsParams](), reflect.TypeFor[HostSkillRoots]()},
		{"host.skills.complete", reflect.TypeFor[HostSkillsParams](), reflect.TypeFor[HostSkillsResult]()},
		{"host.standing.read", reflect.TypeFor[EmptyParams](), reflect.TypeFor[HostStandingInstructions]()},
		{"host.standing.write", reflect.TypeFor[WriteHostStandingInstructionsParams](), reflect.TypeFor[HostStandingInstructions]()},
		{"host.themes.list", reflect.TypeFor[EmptyParams](), reflect.TypeFor[HostThemesResult]()},
		{"host.themes.resolve", reflect.TypeFor[HostThemeResolveParams](), reflect.TypeFor[HostThemeResolved]()},
		{"tool.schemas", reflect.TypeFor[SessionParams](), reflect.TypeFor[HostToolSchemasResult]()},
		{"tool.call", reflect.TypeFor[CallHostToolParams](), reflect.TypeFor[Admission]()},
		{"shell.run", reflect.TypeFor[RunShellParams](), reflect.TypeFor[Admission]()},
		{"executor.activity", reflect.TypeFor[SessionParams](), reflect.TypeFor[ExecutorActivityResult]()},
		{"executor.bind", reflect.TypeFor[ExecutorBindParams](), reflect.TypeFor[ExecutorLease]()},
		{"executor.pending", reflect.TypeFor[ExecutorPendingParams](), reflect.TypeFor[ExecutorPendingResult]()},
		{"tool.result", reflect.TypeFor[ExecutorToolResultParams](), reflect.TypeFor[ExecutorAccepted]()},
		{"hook.result", reflect.TypeFor[ExecutorHookResultParams](), reflect.TypeFor[ExecutorAccepted]()},
		{"tool.progress", reflect.TypeFor[ExecutorProgressParams](), reflect.TypeFor[ExecutorAccepted]()},
		{"shell.interaction", reflect.TypeFor[ShellInteractionParams](), reflect.TypeFor[ShellInteractionResult]()},
		{"shell.input", reflect.TypeFor[ShellInputParams](), reflect.TypeFor[ShellInputResult]()},
		{"computer.status", reflect.TypeFor[EmptyParams](), reflect.TypeFor[ComputerStatus]()},
		{"computer.configure", reflect.TypeFor[ConfigureComputerParams](), reflect.TypeFor[ComputerStatus]()},
		{"computer.use_bundled", reflect.TypeFor[UseBundledComputerParams](), reflect.TypeFor[ComputerStatus]()},
		{"computer.reconnect", reflect.TypeFor[ComputerConnectionParams](), reflect.TypeFor[ComputerStatus]()},
		{"computer.disconnect", reflect.TypeFor[ComputerConnectionParams](), reflect.TypeFor[ComputerStatus]()},
		{"mcp.configuration", reflect.TypeFor[EmptyParams](), reflect.TypeFor[MCPConfiguration]()},
		{"mcp.configure", reflect.TypeFor[ConfigureMCPParams](), reflect.TypeFor[MCPConfiguration]()},
		{"mcp.import.candidates", reflect.TypeFor[MCPImportCandidatesParams](), reflect.TypeFor[MCPImportCandidatesResult]()},
		{"mcp.import.apply", reflect.TypeFor[MCPImportParams](), reflect.TypeFor[MCPImportResult]()},
		{"mcp.status", reflect.TypeFor[SessionParams](), reflect.TypeFor[MCPStatusResult]()},
		{"mcp.refresh", reflect.TypeFor[SessionParams](), reflect.TypeFor[MCPRefreshResult]()},
		{"mcp.reload", reflect.TypeFor[SessionParams](), reflect.TypeFor[MCPRefreshResult]()},
		{"mcp.reconnect", reflect.TypeFor[MCPServerParams](), reflect.TypeFor[MCPRefreshResult]()},
		{"mcp.enable", reflect.TypeFor[MCPServerParams](), reflect.TypeFor[MCPRefreshResult]()},
		{"mcp.disable", reflect.TypeFor[MCPServerParams](), reflect.TypeFor[MCPRefreshResult]()},
		{"mcp.attach", reflect.TypeFor[MCPAttachParams](), reflect.TypeFor[MCPRefreshResult]()},
		{"mcp.tools", reflect.TypeFor[MCPServerParams](), reflect.TypeFor[MCPToolsResult]()},
		{"mcp.instructions", reflect.TypeFor[MCPServerParams](), reflect.TypeFor[MCPInstructionsResult]()},
		{"mcp.brand.icons", reflect.TypeFor[MCPBrandIconsParams](), reflect.TypeFor[MCPBrandIconsResult]()},

		{"terminal.open", reflect.TypeFor[TerminalOpenParams](), reflect.TypeFor[TerminalInfo]()},
		{"terminal.list", reflect.TypeFor[TerminalListParams](), reflect.TypeFor[TerminalList]()},
		{"terminal.read", reflect.TypeFor[TerminalReadParams](), reflect.TypeFor[TerminalPage]()},
		{"terminal.write", reflect.TypeFor[TerminalWriteParams](), reflect.TypeFor[TerminalAccepted]()},
		{"terminal.resize", reflect.TypeFor[TerminalResizeParams](), reflect.TypeFor[TerminalInfo]()},
		{"terminal.close", reflect.TypeFor[TerminalRef](), reflect.TypeFor[TerminalAccepted]()},
		{"workspace.capture", reflect.TypeFor[WorkspaceActionParams](), reflect.TypeFor[WorkspaceResult]()},
		{"workspace.restore", reflect.TypeFor[WorkspaceActionParams](), reflect.TypeFor[WorkspaceResult]()},
		{"workspace.release", reflect.TypeFor[WorkspaceActionParams](), reflect.TypeFor[WorkspaceResult]()},
		{"workspace.action", reflect.TypeFor[ReadWorkspaceActionParams](), reflect.TypeFor[WorkspaceAction]()},
		{"workspace.snapshot", reflect.TypeFor[WorkspaceSnapshotParams](), reflect.TypeFor[WorkspaceSnapshot]()},
		{"workspace.snapshots", reflect.TypeFor[WorkspaceSnapshotsParams](), reflect.TypeFor[WorkspaceSnapshotsResult]()},
		{"providers.disconnect", reflect.TypeFor[DisconnectProviderParams](), reflect.TypeFor[ProviderDisconnectResult]()},
		{"providers.set_preferences", reflect.TypeFor[ProviderPreferencesParams](), reflect.TypeFor[ProviderInventory]()},
		{"host.set_execution_preferences", reflect.TypeFor[SetExecutionPreferencesParams](), reflect.TypeFor[HostExecutionDefaults]()},
		{"providers.candidates", reflect.TypeFor[EmptyParams](), reflect.TypeFor[ProviderCandidates]()},
		{"providers.use_candidate", reflect.TypeFor[UseProviderCandidateParams](), reflect.TypeFor[ProviderInventory]()},
		{"providers.set_enabled", reflect.TypeFor[SetProviderEnabledParams](), reflect.TypeFor[ProviderInventory]()},
		{"providers.presets", reflect.TypeFor[EmptyParams](), reflect.TypeFor[ProviderPresetsResult]()},
		{"providers.bundled", reflect.TypeFor[ProviderParams](), reflect.TypeFor[ProviderModelsResult]()},
		{"providers.list", reflect.TypeFor[EmptyParams](), reflect.TypeFor[ProviderInventory]()},
		{"providers.setup_key", reflect.TypeFor[ProviderKeySetup](), reflect.TypeFor[ProviderInventory]()},
		{"providers.create", reflect.TypeFor[ChangeProviderParams](), reflect.TypeFor[ProviderInventory]()},
		{"providers.update", reflect.TypeFor[ChangeProviderParams](), reflect.TypeFor[ProviderInventory]()},
		{"providers.remove", reflect.TypeFor[RemoveProviderParams](), reflect.TypeFor[ProviderInventory]()},
		{"providers.defaults", reflect.TypeFor[ProviderDefaultsParams](), reflect.TypeFor[ProviderInventory]()},
		{"providers.compaction", reflect.TypeFor[ProviderDefaultsParams](), reflect.TypeFor[ProviderInventory]()},
		{"providers.catalog", reflect.TypeFor[ProviderParams](), reflect.TypeFor[ProviderCatalog]()},
		{"providers.refresh", reflect.TypeFor[ProviderParams](), reflect.TypeFor[ProviderCatalog]()},
		{"providers.readiness", reflect.TypeFor[ProviderReadinessParams](), reflect.TypeFor[ProviderReadiness]()},
		{"lsp.status", reflect.TypeFor[SessionParams](), reflect.TypeFor[LanguageServersResult]()},
		{"accounts.openai.begin", reflect.TypeFor[EmptyParams](), reflect.TypeFor[OpenAILoginFlow]()},
		{"accounts.openai.get", reflect.TypeFor[OpenAIFlowParams](), reflect.TypeFor[OpenAILoginFlow]()},
		{"accounts.openai.list", reflect.TypeFor[EmptyParams](), reflect.TypeFor[OpenAIFlowsResult]()},
		{"accounts.openai.cancel", reflect.TypeFor[OpenAIFlowParams](), reflect.TypeFor[OpenAILoginFlow]()},
		{"accounts.openai.status", reflect.TypeFor[EmptyParams](), reflect.TypeFor[OpenAIAccountStatus]()},
		{"accounts.openai.setup", reflect.TypeFor[EmptyParams](), reflect.TypeFor[OpenAIAccountStatus]()},
		{"accounts.openai.logout", reflect.TypeFor[EmptyParams](), reflect.TypeFor[OpenAIAccountStatus]()},

		{"accounts.inference.begin", reflect.TypeFor[EmptyParams](), reflect.TypeFor[InferenceFlow]()},
		{"accounts.inference.get", reflect.TypeFor[InferenceFlowParams](), reflect.TypeFor[InferenceFlow]()},
		{"accounts.inference.list", reflect.TypeFor[EmptyParams](), reflect.TypeFor[InferenceFlowsResult]()},
		{"accounts.inference.cancel", reflect.TypeFor[InferenceFlowParams](), reflect.TypeFor[InferenceFlow]()},
		{"accounts.inference.team", reflect.TypeFor[InferenceTeamParams](), reflect.TypeFor[InferenceFlow]()},
		{"accounts.inference.project", reflect.TypeFor[InferenceProjectParams](), reflect.TypeFor[InferenceFlow]()},
		{"accounts.inference.create_project", reflect.TypeFor[InferenceCreateProjectParams](), reflect.TypeFor[InferenceFlow]()},
		{"accounts.inference.retry", reflect.TypeFor[InferenceFlowParams](), reflect.TypeFor[InferenceFlow]()},
		{"accounts.inference.rotate", reflect.TypeFor[EmptyParams](), reflect.TypeFor[InferenceFlow]()},
		{"accounts.inference.status", reflect.TypeFor[EmptyParams](), reflect.TypeFor[InferenceAccountStatus]()},
		{"accounts.inference.setup", reflect.TypeFor[EmptyParams](), reflect.TypeFor[InferenceAccountStatus]()},
		{"accounts.inference.logout", reflect.TypeFor[EmptyParams](), reflect.TypeFor[InferenceLogoutResult]()},
		{"accounts.inference.cleanup", reflect.TypeFor[EmptyParams](), reflect.TypeFor[InferenceCleanupResult]()},
		{"accounts.inference.retry_cleanup", reflect.TypeFor[EmptyParams](), reflect.TypeFor[InferenceCleanupResult]()},

		{"goals.formulate", reflect.TypeFor[FormulateGoalParams](), reflect.TypeFor[Admission]()},
		{"goals.formulation", reflect.TypeFor[GoalFormulationParams](), reflect.TypeFor[GoalFormulation]()},
		{"goals.create", reflect.TypeFor[CreateGoalParams](), reflect.TypeFor[GoalAdmission]()},
		{"goals.current", reflect.TypeFor[SessionParams](), reflect.TypeFor[CurrentGoalResult]()},
		{"goals.get", reflect.TypeFor[GoalParams](), reflect.TypeFor[Goal]()},
		{"goals.resume", reflect.TypeFor[ResumeGoalParams](), reflect.TypeFor[Admission]()},
		{"goals.cancel", reflect.TypeFor[GoalParams](), reflect.TypeFor[GoalChange]()},
		{"schedules.create", reflect.TypeFor[CreateScheduleParams](), reflect.TypeFor[ScheduleAdmission]()},
		{"schedules.get", reflect.TypeFor[ScheduleParams](), reflect.TypeFor[ScheduleResult]()},
		{"schedules.list", reflect.TypeFor[ListSchedulesParams](), reflect.TypeFor[SchedulesResult]()},
		{"schedules.cancel", reflect.TypeFor[ScheduleParams](), reflect.TypeFor[ScheduleAdmission]()},
		{"sessions.compact", reflect.TypeFor[CompactParams](), reflect.TypeFor[Admission]()},
		{"context.head", reflect.TypeFor[SessionParams](), reflect.TypeFor[ContextHead]()},
		{"context.compaction", reflect.TypeFor[CompactionParams](), reflect.TypeFor[CompactionResult]()},
		{"context.compactions", reflect.TypeFor[CompactionsParams](), reflect.TypeFor[CompactionsResult]()},
		{"context.select", reflect.TypeFor[SelectCompactionParams](), reflect.TypeFor[ContextHead]()},
		{"context.usage", reflect.TypeFor[SessionParams](), reflect.TypeFor[ContextUsage]()},
		{"context.snapshot", reflect.TypeFor[SessionParams](), reflect.TypeFor[HistorySnapshot]()},
		{"context.list", reflect.TypeFor[ContextHistoryParams](), reflect.TypeFor[HistoryMetadataResult]()},
		{"context.read", reflect.TypeFor[ReadHistoryParams](), reflect.TypeFor[ReadHistoryResult]()},
		{"context.search", reflect.TypeFor[SearchHistoryParams](), reflect.TypeFor[SearchHistoryResult]()},
		{"turns.output", reflect.TypeFor[TurnParams](), reflect.TypeFor[TurnOutputResult]()},
		{"turns.instructions", reflect.TypeFor[TurnParams](), reflect.TypeFor[InstructionManifestResult]()},
		{"skills.list", reflect.TypeFor[ListSkillsParams](), reflect.TypeFor[ListSkillsResult]()},
		{"completions.list", reflect.TypeFor[ListCompletionsParams](), reflect.TypeFor[ListCompletionsResult]()},
		{"completions.read", reflect.TypeFor[ReadCompletionParams](), reflect.TypeFor[ReadCompletionResult]()},
		{"state.subscribe", reflect.TypeFor[SubscribeStateParams](), reflect.TypeFor[StateSubscription]()},
		{"state.subscriptions", reflect.TypeFor[StateSubscriptionsParams](), reflect.TypeFor[StateSubscriptionsResult]()},
		{"state.unsubscribe", reflect.TypeFor[UnsubscribeStateParams](), reflect.TypeFor[StateSubscription]()},
		{"state.get", reflect.TypeFor[GetStateParams](), reflect.TypeFor[StateVersion]()},
		{"state.write", reflect.TypeFor[WriteStateParams](), reflect.TypeFor[StateVersion]()},
		{"state.append", reflect.TypeFor[WriteStateParams](), reflect.TypeFor[StateVersion]()},
		{"state.read", reflect.TypeFor[ReadStateParams](), reflect.TypeFor[ReadStateResult]()},
		{"state.list", reflect.TypeFor[ListStateParams](), reflect.TypeFor[StateVersionsResult]()},
		{"state.history", reflect.TypeFor[StateHistoryParams](), reflect.TypeFor[StateVersionsResult]()},
		{"mail.send", reflect.TypeFor[SendMailParams](), reflect.TypeFor[MailAdmission]()},
		{"mail.list", reflect.TypeFor[ListMailParams](), reflect.TypeFor[ListMailResult]()},
		{"mail.read", reflect.TypeFor[ReadMailParams](), reflect.TypeFor[ReadMailResult]()},
		{"resources.list", reflect.TypeFor[SessionParams](), reflect.TypeFor[ResourcesResult]()},
		{"resources.set", reflect.TypeFor[SetResourceParams](), reflect.TypeFor[ResourceUsage]()},
		{"usage.turn", reflect.TypeFor[TurnUsageParams](), reflect.TypeFor[TurnUsage]()},
		{"usage.get", reflect.TypeFor[SessionParams](), reflect.TypeFor[Usage]()},
		{"budgets.list", reflect.TypeFor[SessionParams](), reflect.TypeFor[BudgetsResult]()},
		{"budgets.set", reflect.TypeFor[SetBudgetParams](), reflect.TypeFor[Budget]()},
		{"sessions.observe", reflect.TypeFor[HistoryParams](), reflect.TypeFor[SessionObservation]()},
		{"cells.output", reflect.TypeFor[SessionParams](), reflect.TypeFor[CellOutput]()},
		{"cells.get", reflect.TypeFor[CellParams](), reflect.TypeFor[Cell]()},
		{"turns.cells", reflect.TypeFor[CellsParams](), reflect.TypeFor[CellsResult]()},
		{"turns.cells_page", reflect.TypeFor[CellPageParams](), reflect.TypeFor[CellPageResult]()},
		{"grants.create", reflect.TypeFor[CreateGrantParams](), reflect.TypeFor[Grant]()},
		{"grants.list", reflect.TypeFor[GrantsParams](), reflect.TypeFor[GrantsResult]()},
		{"grants.revoke", reflect.TypeFor[GrantParams](), reflect.TypeFor[Grant]()},
		{"operations.get", reflect.TypeFor[HostOperationParams](), reflect.TypeFor[HostOperation]()},
		{"turns.operations", reflect.TypeFor[HostOperationsParams](), reflect.TypeFor[HostOperationsResult]()},
		{"permissions.list", reflect.TypeFor[PermissionsParams](), reflect.TypeFor[PermissionsResult]()},
		{"permissions.resolve", reflect.TypeFor[ResolvePermissionParams](), reflect.TypeFor[Permission]()},
		{"permissions.policy", reflect.TypeFor[SessionParams](), reflect.TypeFor[PermissionPolicy]()},
		{"permissions.set_mode", reflect.TypeFor[SetPermissionModeParams](), reflect.TypeFor[PermissionModeEdit]()},
		{"permissions.set_denial", reflect.TypeFor[SetPermissionDenialParams](), reflect.TypeFor[PermissionDenialEdit]()},
		{"permissions.denial_edit", reflect.TypeFor[PermissionModeEditParams](), reflect.TypeFor[PermissionDenialEdit]()},
		{"permissions.mode_edit", reflect.TypeFor[PermissionModeEditParams](), reflect.TypeFor[PermissionModeEdit]()},
		{"host.profiles", reflect.TypeFor[EmptyParams](), reflect.TypeFor[HostProfiles]()},
		{"host.external_browser", reflect.TypeFor[EmptyParams](), reflect.TypeFor[ExternalBrowserStatus]()},
		{"host.set_external_browser", reflect.TypeFor[ConfigureExternalBrowserParams](), reflect.TypeFor[ExternalBrowserStatus]()},
		{"browser.external_sessions", reflect.TypeFor[SessionParams](), reflect.TypeFor[ExternalBrowserSessions]()},
		{"browser.reconnect_external", reflect.TypeFor[ExternalBrowserConnectionParams](), reflect.TypeFor[ExternalBrowserSession]()},
		{"browser.disconnect_external", reflect.TypeFor[ExternalBrowserConnectionParams](), reflect.TypeFor[ExternalBrowserSession]()},
		{"host.browser_driver", reflect.TypeFor[EmptyParams](), reflect.TypeFor[HostBrowserDriver]()},
		{"host.set_browser_driver", reflect.TypeFor[SetBrowserDriverParams](), reflect.TypeFor[HostBrowserDriver]()},
		{"host.execution_defaults", reflect.TypeFor[EmptyParams](), reflect.TypeFor[HostExecutionDefaults]()},
		{"host.set_execution_defaults", reflect.TypeFor[SetExecutionDefaultsParams](), reflect.TypeFor[HostExecutionDefaults]()},
		{"host.set_profiles", reflect.TypeFor[SetHostProfilesParams](), reflect.TypeFor[HostProfiles]()},
		{"host.permission_default", reflect.TypeFor[EmptyParams](), reflect.TypeFor[DefaultPermissionMode]()},
		{"host.set_permission_default", reflect.TypeFor[SetDefaultPermissionModeParams](), reflect.TypeFor[DefaultPermissionMode]()},
		{"questions.get", reflect.TypeFor[QuestionParams](), reflect.TypeFor[Question]()},
		{"questions.list", reflect.TypeFor[QuestionsParams](), reflect.TypeFor[QuestionsResult]()},
		{"questions.answer", reflect.TypeFor[AnswerQuestionParams](), reflect.TypeFor[Question]()},
		{"initialize", reflect.TypeFor[InitializeParams](), reflect.TypeFor[InitializeResult]()},
		{"trees.create", reflect.TypeFor[CreateTreeParams](), reflect.TypeFor[CreateTreeResult]()},
		{"trees.creation", reflect.TypeFor[TreeCreationParams](), reflect.TypeFor[CreateTreeResult]()},
		{"trees.catalog", reflect.TypeFor[EmptyParams](), reflect.TypeFor[TreeCatalog]()},
		{"trees.recent", reflect.TypeFor[RecentTreesParams](), reflect.TypeFor[RecentTreesResult]()},
		{"trees.list", reflect.TypeFor[ListTreesParams](), reflect.TypeFor[ListTreesResult]()},
		{"trees.summaries", reflect.TypeFor[TreeSummariesParams](), reflect.TypeFor[TreeSummariesResult]()},
		{"definitions.list", reflect.TypeFor[ListDefinitionsParams](), reflect.TypeFor[ListDefinitionsResult]()},
		{"trees.get", reflect.TypeFor[TreeParams](), reflect.TypeFor[Tree]()},
		{"trees.update", reflect.TypeFor[UpdateTreeParams](), reflect.TypeFor[Tree]()},
		{"trees.title_decision", reflect.TypeFor[TreeParams](), reflect.TypeFor[AutomaticTitleDecision]()},
		{"trees.title_result", reflect.TypeFor[AutomaticTitleResultParams](), reflect.TypeFor[AutomaticTitleResult]()},
		{"sessions.get", reflect.TypeFor[SessionParams](), reflect.TypeFor[Session]()},
		{"sessions.spawn", reflect.TypeFor[SpawnSessionParams](), reflect.TypeFor[SpawnSessionResult]()},
		{"sessions.list", reflect.TypeFor[ListSessionsParams](), reflect.TypeFor[ListSessionsResult]()},
		{"sessions.configure", reflect.TypeFor[UpdateConfigurationParams](), reflect.TypeFor[Session]()},
		{"sessions.submit", reflect.TypeFor[SubmitParams](), reflect.TypeFor[Admission]()},
		{"inputs.steer", reflect.TypeFor[SteerInputParams](), reflect.TypeFor[InputSteeringResult]()},
		{"inputs.steering", reflect.TypeFor[InputSteeringParams](), reflect.TypeFor[InputSteeringResult]()},
		{"sessions.history_page", reflect.TypeFor[HistoryPageParams](), reflect.TypeFor[HistoryPageResult]()},
		{"sessions.history", reflect.TypeFor[HistoryParams](), reflect.TypeFor[HistoryResult]()},
		{"sessions.rewind", reflect.TypeFor[RewindParams](), reflect.TypeFor[HistoryEdit]()},
		{"sessions.fork", reflect.TypeFor[ForkParams](), reflect.TypeFor[ForkResult]()},
		{"sessions.lifecycle", reflect.TypeFor[LifecycleParams](), reflect.TypeFor[Session]()},
		{"sessions.delete", reflect.TypeFor[SessionParams](), reflect.TypeFor[DeleteResult]()},
		{"turns.get", reflect.TypeFor[TurnParams](), reflect.TypeFor[Turn]()},
		{"sessions.turns", reflect.TypeFor[TurnPageParams](), reflect.TypeFor[TurnPageResult]()},
		{"turns.attempts", reflect.TypeFor[ModelAttemptsParams](), reflect.TypeFor[ModelAttemptsResult]()},
		{"turns.cancel", reflect.TypeFor[TurnParams](), reflect.TypeFor[Turn]()},
		{"sessions.activity", reflect.TypeFor[SessionParams](), reflect.TypeFor[SessionActivity]()},
		{"inputs.recent_text", reflect.TypeFor[RecentInputTextParams](), reflect.TypeFor[InputTextPage]()},
		{"inputs.page", reflect.TypeFor[InputPageParams](), reflect.TypeFor[InputPageResult]()},
		{"inputs.get", reflect.TypeFor[SessionInputParams](), reflect.TypeFor[Input]()},
		{"inputs.cancel", reflect.TypeFor[InputParams](), reflect.TypeFor[Input]()},
		{"receipts.match", reflect.TypeFor[MatchReceiptParams](), reflect.TypeFor[Admission]()},
		{"receipts.get", reflect.TypeFor[RequestIdentity](), reflect.TypeFor[Admission]()},
		{"content.put", reflect.TypeFor[PutContentParams](), reflect.TypeFor[ContentReference]()},
		{"content.get", reflect.TypeFor[ReadContentParams](), reflect.TypeFor[ContentReference]()},
		{"content.read", reflect.TypeFor[ReadContentParams](), reflect.TypeFor[ReadContentResult]()},
		{"definitions.register", reflect.TypeFor[DefinitionDocument](), reflect.TypeFor[Definition]()},
		{"definitions.get", reflect.TypeFor[DefinitionRef](), reflect.TypeFor[Definition]()},
	}
}

func Types() map[string]reflect.Type {
	result := map[string]reflect.Type{}
	result["CapturedText"] = reflect.TypeFor[CapturedText]()
	result["BrowserEvent"] = reflect.TypeFor[BrowserEvent]()
	result["BrowserCommand"] = reflect.TypeFor[BrowserCommand]()
	result["BrowserCommandCancel"] = reflect.TypeFor[BrowserCommandCancel]()
	result["BrowserInventoryRequest"] = reflect.TypeFor[BrowserInventoryRequest]()
	result["BrowserScopesRetired"] = reflect.TypeFor[BrowserScopesRetired]()
	result["ExecutorEvent"] = reflect.TypeFor[ExecutorEvent]()
	result["RPCError"] = reflect.TypeFor[RPCError]()
	result["Request"] = reflect.TypeFor[Request]()
	result["GatewayDiscovery"] = reflect.TypeFor[GatewayDiscovery]()
	result["Response"] = reflect.TypeFor[Response]()
	result["Part"] = reflect.TypeFor[Part]()
	result["Message"] = reflect.TypeFor[Message]()
	result["MessagePresentation"] = reflect.TypeFor[MessagePresentation]()
	result["AttemptPresentation"] = reflect.TypeFor[AttemptPresentation]()
	result["ToolCall"] = reflect.TypeFor[ToolCall]()
	result["ToolResult"] = reflect.TypeFor[ToolResult]()
	for _, op := range Operations() {
		result[op.Params.Name()] = op.Params
		result[op.Result.Name()] = op.Result
	}
	return result
}

func SchemaFor(t reflect.Type) (*jsonschema.Schema, error) {
	schema, err := jsonschema.ForType(t, &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[BrowserToken]():     {Type: "string", MinLength: new(1), MaxLength: new(128), Pattern: "^[^\x00-\x20\x7f]+$"},
		reflect.TypeFor[AccountTimestamp](): {Type: "string", Format: "account-time"},
		reflect.TypeFor[ID]():               {Type: "string", Pattern: `^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`},
		reflect.TypeFor[Counter]():          {Type: "string", Pattern: `^(0|[1-9][0-9]{0,18})$`, Format: "counter"},
		reflect.TypeFor[json.RawMessage]():  {},
		reflect.TypeFor[Part]():             partSchema("text", "content", "tool_call", "tool_result"),
		reflect.TypeFor[ToolCall]():         toolCallSchema(),
		reflect.TypeFor[ToolResult]():       toolResultSchema(),
	}})
	if err != nil {
		return nil, err
	}
	applyTags(schema, t)
	if t == reflect.TypeFor[Response]() {
		schema.OneOf = []*jsonschema.Schema{{Required: []string{"result"}, Not: &jsonschema.Schema{Required: []string{"error"}}}, {Required: []string{"error"}, Not: &jsonschema.Schema{Required: []string{"result"}}}}
	}
	schema.Schema = "http://json-schema.org/draft-07/schema#"
	schema.ID = "https://whip.dev/protocol/v4/" + t.Name()
	schema.Title = t.Name()
	return schema, nil
}

func applyTags(schema *jsonschema.Schema, t reflect.Type) {
	if schema == nil {
		return
	}
	nullable := t.Kind() == reflect.Pointer
	if nullable {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		if t == reflect.TypeFor[HostExecutionDefaults]() {
			applyTags(schema, reflect.TypeFor[ExecutionDefaults]())
		}
		for field := range t.Fields() {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			child := schema.Properties[name]
			if child == nil {
				continue
			}
			if value := field.Tag.Get("enum"); value != "" {
				for item := range strings.SplitSeq(value, ",") {
					child.Enum = append(child.Enum, item)
				}
			}
			if value := field.Tag.Get("pattern"); value != "" {
				child.Pattern = value
			}
			if value := field.Tag.Get("min"); value != "" {
				number, _ := strconv.ParseFloat(value, 64)
				child.Minimum = &number
			}
			if value := field.Tag.Get("max"); value != "" {
				number, _ := strconv.ParseFloat(value, 64)
				child.Maximum = &number
			}
			applyTags(child, field.Type)
			if field.Type == reflect.TypeFor[[]Part]() {
				child.Type = "array"
				child.Types = nil
				child.MinItems = new(1)
				child.MaxItems = new(128)
			}
		}
		if t == reflect.TypeFor[InputText]() {
			schema.Properties["text"].MaxLength = new(session.MaxInputRecallBytes)
		}
		if t == reflect.TypeFor[InputTextPage]() {
			schema.Properties["items"].MaxItems = new(500)
		}
		if t == reflect.TypeFor[HostOperationResult]() {
			refs := schema.Properties["content_references"]
			refs.Type = "array"
			refs.Types = nil
			refs.MaxItems = new(session.MaxOperationAttachments)
			refs.UniqueItems = true
		}
		if t == reflect.TypeFor[HostProfiles]() || t == reflect.TypeFor[SetHostProfilesParams]() {
			schema.Properties["profiles"].Type = "array"
			schema.Properties["profiles"].Types = nil
			schema.Properties["profiles"].MaxItems = new(16)
		}
		if t == reflect.TypeFor[HostProfile]() {
			schema.Properties["url"].MaxLength = new(2048)
		}
		skillRootSchema(schema, t)
		accountSchema(schema, t)
		if t == reflect.TypeFor[WorkspaceSnapshotsResult]() {
			schema.Properties["items"].Type = "array"
			schema.Properties["items"].Types = nil
			schema.Properties["items"].MaxItems = new(100)
		}
		automaticTitleSchema(schema, t)
		providerSchema(schema, t)
		executorSchema(schema, t)
		browserSchema(schema, t)
		questionSchema(schema, t)
		languageServerSchema(schema, t)
		mcpSchema(schema, t)
		computerSchema(schema, t)
		externalBrowserSchema(schema, t)
		terminalSchema(schema, t)
		presentationSchema(schema, t)
		hostOperationSchema(schema, t)
		if t == reflect.TypeFor[MatchReceiptParams]() {
			schema.Properties["params_base64"].MinLength = new(1)
			schema.Properties["params_base64"].MaxLength = new(5592408)
		}
		attentionSchema(schema, t)
		traceSchema(schema, t)
		if t == reflect.TypeFor[CapturedText]() {
			schema.Properties["chunks"].Type = "array"
			schema.Properties["chunks"].Types = nil
			schema.Properties["chunks"].MaxItems = new(4)
		}
		if t == reflect.TypeFor[ModelCapture]() {
			schema.Properties["messages"].Type = "array"
			schema.Properties["messages"].Types = nil
			schema.Properties["messages"].MaxItems = new(128)
		}
		controlsSchema(schema, t)
		lifecycleSchema(schema, t)
		steeringSchema(schema, t)
		if t == reflect.TypeFor[InputSummary]() {
			schema.Properties["text_preview"].MaxLength = new(512)
		}
		hostViewsSchema(schema, t)
		standingSchema(schema, t)
		discoverySchema(schema, t)
		recentSchema(schema, t)
		navigationSchema(schema, t)
		if t == reflect.TypeFor[GoalFormulationRequest]() {
			schema.Properties["tail_messages"] = &jsonschema.Schema{OneOf: []*jsonschema.Schema{
				{Type: "integer", Enum: []any{0}},
				{Type: "integer", Minimum: new(2.0), Maximum: new(100.0)},
			}}
		}
		if t == reflect.TypeFor[Configuration]() || t == reflect.TypeFor[ConfigPatch]() {
			modules := schema.Properties["modules"]
			modules.MaxItems = new(len(hostmodule.Names()))
			modules.UniqueItems = true
			for _, name := range hostmodule.Names() {
				modules.Items.Enum = append(modules.Items.Enum, name)
			}
			if t == reflect.TypeFor[Configuration]() {
				modules.Type, modules.Types = "array", nil
			}
		}
		if t == reflect.TypeFor[Configuration]() {
			schema.Properties["compaction"].Properties["threshold_percent"].Minimum = new(1.0)
		}
		if t == reflect.TypeFor[ListSkillsResult]() {
			schema.Properties["items"].Type = "array"
			schema.Properties["items"].Types = nil
			schema.Properties["items"].MaxItems = new(100)
		}
		if t == reflect.TypeFor[InstructionManifest]() {
			schema.Properties["sources"].Type = "array"
			schema.Properties["sources"].Types = nil
			schema.Properties["sources"].MaxItems = new(1152)
		}
		if t == reflect.TypeFor[SpawnSessionParams]() {
			attachments := schema.Properties["browser_attachments"]
			attachments.Type, attachments.Types = "array", nil
			attachments.MaxItems = new(4)
			attachments.UniqueItems = true
			attachments.Items.MaxLength = new(128)
		}
		if t == reflect.TypeFor[Input]() || t == reflect.TypeFor[SubmitParams]() || t == reflect.TypeFor[SpawnSessionParams]() {
			schema.Properties["parts"].Items = partSchema("text", "content")
		}
		if t == reflect.TypeFor[Input]() {
			prompt := schema.CloneSchemas()
			prompt.Type, prompt.Types = "object", nil
			prompt.Properties["kind"] = &jsonschema.Schema{Type: "string", Enum: []any{"prompt"}}
			prompt.Properties["host_operation"] = &jsonschema.Schema{Type: "null"}
			compact := schema.CloneSchemas()
			compact.Type, compact.Types = "object", nil
			compact.Properties["kind"] = &jsonschema.Schema{Type: "string", Enum: []any{"compact", "goal_formulation", "automatic_title"}}
			compact.Properties["parts"] = &jsonschema.Schema{Type: "array", MaxItems: new(0), Items: partSchema("text", "content")}
			compact.Properties["host_operation"] = &jsonschema.Schema{Type: "null"}
			direct := schema.CloneSchemas()
			direct.Type, direct.Types = "object", nil
			direct.Properties["kind"] = &jsonschema.Schema{Type: "string", Enum: []any{"host_operation"}}
			direct.Properties["source"] = &jsonschema.Schema{Type: "string", Enum: []any{"user"}}
			direct.Properties["parts"] = compact.Properties["parts"].CloneSchemas()
			direct.Properties["host_operation"].Type, direct.Properties["host_operation"].Types = "object", nil
			*schema = jsonschema.Schema{OneOf: []*jsonschema.Schema{prompt, compact, direct}}
			if nullable {
				schema.OneOf = append(schema.OneOf, &jsonschema.Schema{Type: "null"})
			}
		}
		if t == reflect.TypeFor[Message]() {
			var variants []*jsonschema.Schema
			for _, role := range []string{"user", "system", "assistant", "tool"} {
				items := partSchema("text", "content")
				maxItems := 128
				switch role {
				case "assistant":
					items = partSchema("text", "content", "tool_call")
				case "tool":
					items, maxItems = partSchema("tool_result"), 1+session.MaxOperationAttachments
				}
				// Keep common required fields in each variant. Conditional-only
				// branches validate in JSON Schema but lose those fields in TS unions.
				variant := schema.CloneSchemas()
				variant.Properties["role"] = &jsonschema.Schema{Type: "string", Enum: []any{role}}
				variant.Properties["parts"] = &jsonschema.Schema{Type: "array", MinItems: new(1), MaxItems: &maxItems, Items: items}
				if role == "tool" {
					variant.Properties["parts"] = &jsonschema.Schema{Type: "array", MinItems: new(1), MaxItems: &maxItems, ItemsArray: []*jsonschema.Schema{partSchema("tool_result")}, AdditionalItems: partSchema("content"), UniqueItems: true}
				}
				variants = append(variants, variant)
			}
			*schema = jsonschema.Schema{OneOf: variants}
		}
	case reflect.Slice:
		if t != reflect.TypeFor[json.RawMessage]() && schema.Type == "array" {
			schema.Type = ""
			schema.Types = []string{"array", "null"}
		}
		applyTags(schema.Items, t.Elem())
	case reflect.Map:
		if schema.Type == "object" {
			schema.Type = ""
			schema.Types = []string{"object", "null"}
		}
		applyTags(schema.AdditionalProperties, t.Elem())
	}
}

// Validate checks schema shape and Go decoding together. Shape validation owns
// required/unknown fields; the typed decoder enforces exact decimal bounds.
func Validate(name string, raw []byte) error {
	t, ok := Types()[name]
	if !ok {
		return fmt.Errorf("unknown contract type %q", name)
	}
	if len(raw) > MaxFrameBytes {
		return errors.New("contract document exceeds 8 MiB")
	}
	schema, err := SchemaFor(t)
	if err != nil {
		return err
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if err := resolved.Validate(value); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(reflect.New(t).Interface()); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
