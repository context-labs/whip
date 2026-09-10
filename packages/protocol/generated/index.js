// Generated from Go wire types. Run npm run generate.
import * as requests from './request-validators.js';
import * as responses from './response-validators.js';
export const manifest = {
  "major": 6,
  "minor": 1,
  "operations": [
    {
      "name": "command.status",
      "surface": "rpc",
      "execution": "query",
      "permission": "client-command-namespace",
      "params_type": "CommandStatusParams",
      "result_type": "CommandResult"
    },
    {
      "name": "command.submit",
      "surface": "rpc",
      "execution": "command",
      "permission": "operation-specific",
      "params_type": "CommandParams",
      "result_type": "CommandResult"
    },
    {
      "name": "config.get",
      "surface": "rpc",
      "execution": "query",
      "permission": "none",
      "params_type": "Empty",
      "result_type": "RuntimeConfiguration"
    },
    {
      "name": "config.update",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "configuration-revision",
      "params_type": "ConfigurationUpdate",
      "result_type": "RuntimeConfiguration"
    },
    {
      "name": "content.read",
      "surface": "rpc",
      "execution": "query",
      "permission": "root-agent-content-grant",
      "params_type": "ContentReadParams",
      "result_type": "ContentReadResult"
    },
    {
      "name": "daemon.ping",
      "surface": "rpc",
      "execution": "query",
      "permission": "none",
      "params_type": "Empty",
      "result_type": "PingResult"
    },
    {
      "name": "daemon.restart",
      "surface": "rpc",
      "execution": "lifecycle",
      "permission": "armed-generation",
      "params_type": "RestartParams",
      "result_type": "Empty"
    },
    {
      "name": "daemon.stop",
      "surface": "rpc",
      "execution": "lifecycle",
      "permission": "armed-generation",
      "params_type": "RestartParams",
      "result_type": "Empty"
    },
    {
      "name": "events.replay",
      "surface": "rpc",
      "execution": "query",
      "permission": "root-association",
      "params_type": "ReplayParams",
      "result_type": "ReplayResult"
    },
    {
      "name": "events.subscribe",
      "surface": "rpc",
      "execution": "subscription",
      "permission": "root-association",
      "params_type": "SubscribeParams",
      "result_type": "SubscribeResult"
    },
    {
      "name": "events.unsubscribe",
      "surface": "rpc",
      "execution": "subscription",
      "permission": "connection-subscription",
      "params_type": "UnsubscribeParams",
      "result_type": "Empty"
    },
    {
      "name": "history.page",
      "surface": "rpc",
      "execution": "query",
      "permission": "root-agent-association",
      "params_type": "HistoryPageParams",
      "result_type": "BoundedTranscriptPage"
    },
    {
      "name": "host.attention",
      "surface": "rpc",
      "execution": "query",
      "permission": "host-runtime",
      "params_type": "HostAttentionParams",
      "result_type": "HostAttentionResult"
    },
    {
      "name": "host.directories.list",
      "surface": "rpc",
      "execution": "query",
      "permission": "host-runtime",
      "params_type": "HostDirectoryParams",
      "result_type": "HostDirectoryResult"
    },
    {
      "name": "host.directory.pick",
      "surface": "rpc",
      "execution": "query",
      "permission": "host-runtime",
      "params_type": "HostDirectoryPickParams",
      "result_type": "HostDirectoryPickResult"
    },
    {
      "name": "host.themes.list",
      "surface": "rpc",
      "execution": "query",
      "permission": "host-runtime",
      "params_type": "EmptyParams",
      "result_type": "CatalogResult"
    },
    {
      "name": "host.themes.resolve",
      "surface": "rpc",
      "execution": "query",
      "permission": "host-runtime",
      "params_type": "HostThemeResolveParams",
      "result_type": "Resolved"
    },
    {
      "name": "initialize",
      "surface": "rpc",
      "execution": "query",
      "permission": "none",
      "params_type": "InitializeParams",
      "result_type": "InitializeResult"
    },
    {
      "name": "mailbox.list",
      "surface": "rpc",
      "execution": "query",
      "permission": "root-agent-association",
      "params_type": "MailboxPageParams",
      "result_type": "MailboxPage"
    },
    {
      "name": "mailbox.read",
      "surface": "rpc",
      "execution": "query",
      "permission": "root-agent-association",
      "params_type": "MailboxReadParams",
      "result_type": "MailboxInspection"
    },
    {
      "name": "operation.invoke",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "operation-specific",
      "sensitive": true,
      "params_type": "QueryParams",
      "result_type": "QueryResult"
    },
    {
      "name": "permission.decide",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "trusted-client-decision",
      "params_type": "PermissionDecisionParams",
      "result_type": "PermissionDecisionResult"
    },
    {
      "name": "provider.create",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "configuration-revision",
      "sensitive": true,
      "params_type": "ProviderCreateParams",
      "result_type": "ProviderConfiguration"
    },
    {
      "name": "provider.disconnect",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "configuration-revision",
      "params_type": "ProviderDisconnectParams",
      "result_type": "ProviderStatus"
    },
    {
      "name": "provider.discover",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "host-configuration",
      "params_type": "ProviderListParams",
      "result_type": "ProviderList"
    },
    {
      "name": "provider.get",
      "surface": "rpc",
      "execution": "query",
      "permission": "host-configuration",
      "params_type": "ProviderNameParams",
      "result_type": "ProviderConfiguration"
    },
    {
      "name": "provider.key.rotate",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "host-configuration",
      "sensitive": true,
      "params_type": "ProviderNameParams",
      "result_type": "ProviderStatus"
    },
    {
      "name": "provider.key.set",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "configuration-revision",
      "sensitive": true,
      "params_type": "ProviderKeySetup",
      "result_type": "RuntimeConfiguration"
    },
    {
      "name": "provider.list",
      "surface": "rpc",
      "execution": "query",
      "permission": "host-configuration",
      "params_type": "ProviderListParams",
      "result_type": "ProviderList"
    },
    {
      "name": "provider.login.begin",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "host-configuration",
      "params_type": "ProviderLoginBeginParams",
      "result_type": "ProviderLoginStatus"
    },
    {
      "name": "provider.login.cancel",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "host-configuration",
      "params_type": "ProviderLoginParams",
      "result_type": "ProviderLoginStatus"
    },
    {
      "name": "provider.login.list",
      "surface": "rpc",
      "execution": "query",
      "permission": "host-configuration",
      "params_type": "Empty",
      "result_type": "ProviderLoginList"
    },
    {
      "name": "provider.login.project.create",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "host-configuration",
      "params_type": "ProviderLoginCreateParams",
      "result_type": "ProviderLoginStatus"
    },
    {
      "name": "provider.login.project.select",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "host-configuration",
      "params_type": "ProviderLoginProjectParams",
      "result_type": "ProviderLoginStatus"
    },
    {
      "name": "provider.login.status",
      "surface": "rpc",
      "execution": "query",
      "permission": "none",
      "params_type": "ProviderLoginParams",
      "result_type": "ProviderLoginStatus"
    },
    {
      "name": "provider.login.team.select",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "host-configuration",
      "params_type": "ProviderLoginTeamParams",
      "result_type": "ProviderLoginStatus"
    },
    {
      "name": "provider.logout",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "host-configuration",
      "params_type": "ProviderNameParams",
      "result_type": "ProviderStatus"
    },
    {
      "name": "provider.remove",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "configuration-revision",
      "params_type": "ProviderRemoveParams",
      "result_type": "ProviderRemoveResult"
    },
    {
      "name": "provider.status",
      "surface": "rpc",
      "execution": "query",
      "permission": "host-configuration",
      "params_type": "ProviderNameParams",
      "result_type": "ProviderStatus"
    },
    {
      "name": "provider.update",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "configuration-revision",
      "sensitive": true,
      "params_type": "ProviderUpdateParams",
      "result_type": "ProviderConfiguration"
    },
    {
      "name": "provider.validate",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "host-configuration",
      "sensitive": true,
      "params_type": "ProviderValidateParams",
      "result_type": "ProviderValidateResult"
    },
    {
      "name": "query",
      "surface": "rpc",
      "execution": "query",
      "permission": "operation-specific",
      "params_type": "QueryParams",
      "result_type": "QueryResult"
    },
    {
      "name": "root.collection",
      "surface": "rpc",
      "execution": "query",
      "permission": "root-association",
      "params_type": "RootCollectionParams",
      "result_type": "RootCollectionPage"
    },
    {
      "name": "root.snapshot",
      "surface": "rpc",
      "execution": "query",
      "permission": "root-association",
      "params_type": "SnapshotParams",
      "result_type": "RootSnapshot"
    },
    {
      "name": "sessions.get",
      "surface": "rpc",
      "execution": "query",
      "permission": "root-association",
      "params_type": "RootParams",
      "result_type": "SessionMetadata"
    },
    {
      "name": "sessions.list",
      "surface": "rpc",
      "execution": "query",
      "permission": "host-runtime",
      "params_type": "SessionCatalogParams",
      "result_type": "SessionCatalogPage"
    },
    {
      "name": "sessions.revision",
      "surface": "rpc",
      "execution": "query",
      "permission": "host-runtime",
      "params_type": "EmptyParams",
      "result_type": "CatalogRevision"
    },
    {
      "name": "sessions.summaries",
      "surface": "rpc",
      "execution": "query",
      "permission": "host-runtime",
      "params_type": "SessionSummariesParams",
      "result_type": "SessionSummariesResult"
    },
    {
      "name": "upload.begin",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "content-grant",
      "params_type": "UploadBeginParams",
      "result_type": "Accepted"
    },
    {
      "name": "upload.chunk",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "connection-upload",
      "params_type": "UploadChunkParams",
      "result_type": "Accepted"
    },
    {
      "name": "upload.finish",
      "surface": "rpc",
      "execution": "ephemeral",
      "permission": "content-grant",
      "params_type": "UploadFinishParams",
      "result_type": "ContentHandle"
    },
    {
      "name": "workspace.complete",
      "surface": "rpc",
      "execution": "query",
      "permission": "root-agent-association",
      "params_type": "CompletionParams",
      "result_type": "CompletionResult"
    },
    {
      "name": "agent.control",
      "surface": "runtime",
      "execution": "command",
      "permission": "agent-authority",
      "params_type": "IDParams",
      "result_type": "Empty"
    },
    {
      "name": "agent.delete",
      "surface": "runtime",
      "execution": "command",
      "permission": "agent-authority",
      "params_type": "IDParams",
      "result_type": "Empty"
    },
    {
      "name": "agent.submit",
      "surface": "runtime",
      "execution": "command",
      "permission": "agent-admission",
      "params_type": "AgentInputParams",
      "result_type": "AgentSubmitResult"
    },
    {
      "name": "agent.transcript",
      "surface": "runtime",
      "execution": "query",
      "permission": "human-transcript-inspection",
      "params_type": "IDParams",
      "result_type": "AgentTranscriptResult"
    },
    {
      "name": "agent.turn.cancel",
      "surface": "runtime",
      "execution": "command",
      "permission": "target-turn",
      "params_type": "AgentCancelParams",
      "result_type": "Empty"
    },
    {
      "name": "agents.list",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "AgentListResult"
    },
    {
      "name": "browser.set_driver",
      "surface": "runtime",
      "execution": "command",
      "permission": "host-configuration",
      "params_type": "BrowserDriverParams",
      "result_type": "BrowserStatusResult"
    },
    {
      "name": "browser.status",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "BrowserStatusResult"
    },
    {
      "name": "budget.cap",
      "surface": "runtime",
      "execution": "command",
      "permission": "budget-authority",
      "params_type": "BudgetCapParams",
      "result_type": "BudgetState"
    },
    {
      "name": "cancel",
      "surface": "runtime",
      "execution": "command",
      "permission": "target-turn",
      "params_type": "CancelParams",
      "result_type": "Empty"
    },
    {
      "name": "capability.revoke",
      "surface": "runtime",
      "execution": "command",
      "permission": "capability-authority",
      "params_type": "IDParams",
      "result_type": "CapabilityRecord"
    },
    {
      "name": "compaction.configure",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-idle",
      "params_type": "CompactionParams",
      "result_type": "CompactionSettingsResult"
    },
    {
      "name": "computer.allow",
      "surface": "runtime",
      "execution": "command",
      "permission": "human-computer-policy",
      "params_type": "ComputerAppParams",
      "result_type": "ComputerStatusResult"
    },
    {
      "name": "computer.deny",
      "surface": "runtime",
      "execution": "command",
      "permission": "human-computer-policy",
      "params_type": "ComputerAppParams",
      "result_type": "ComputerStatusResult"
    },
    {
      "name": "computer.status",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "ComputerStatusResult"
    },
    {
      "name": "context.audit",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "ContextAuditResult"
    },
    {
      "name": "daemon.checkpoint",
      "surface": "runtime",
      "execution": "command",
      "permission": "host-runtime",
      "params_type": "CheckpointParams",
      "result_type": "RestartNotice"
    },
    {
      "name": "goal.from-context",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-admission",
      "params_type": "GoalContextParams",
      "result_type": "GoalResult"
    },
    {
      "name": "goal.run",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-admission",
      "params_type": "TextParams",
      "result_type": "GoalResult"
    },
    {
      "name": "goal.set",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-admission",
      "params_type": "TextParams",
      "result_type": "GoalResult"
    },
    {
      "name": "history.clear",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-idle",
      "params_type": "ClearHistoryParams",
      "result_type": "Empty"
    },
    {
      "name": "history.compact",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-idle",
      "params_type": "EmptyParams",
      "result_type": "CompactionResult"
    },
    {
      "name": "history.compact.log",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "CompactionListResult"
    },
    {
      "name": "history.compact.retry",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-idle",
      "params_type": "EmptyParams",
      "result_type": "CompactionRetryResult"
    },
    {
      "name": "history.rewind",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-idle",
      "params_type": "RewindParams",
      "result_type": "RewindResult"
    },
    {
      "name": "history.user.list",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "UserHistoryResult"
    },
    {
      "name": "lsp.status",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "LSPListResult"
    },
    {
      "name": "mcp.attach",
      "surface": "runtime",
      "execution": "ephemeral",
      "permission": "delegated-mcp-authority",
      "sensitive": true,
      "params_type": "MCPAttachParams",
      "result_type": "Empty"
    },
    {
      "name": "mcp.disable",
      "surface": "runtime",
      "execution": "command",
      "permission": "delegated-mcp-authority",
      "params_type": "MCPServerParams",
      "result_type": "Empty"
    },
    {
      "name": "mcp.enable",
      "surface": "runtime",
      "execution": "command",
      "permission": "delegated-mcp-authority",
      "params_type": "MCPServerParams",
      "result_type": "Empty"
    },
    {
      "name": "mcp.import.configure",
      "surface": "runtime",
      "execution": "command",
      "permission": "host-configuration",
      "params_type": "MCPImportParams",
      "result_type": "MCPImportStatusResult"
    },
    {
      "name": "mcp.import.status",
      "surface": "runtime",
      "execution": "query",
      "permission": "host-configuration",
      "params_type": "EmptyParams",
      "result_type": "MCPImportStatusResult"
    },
    {
      "name": "mcp.reconnect",
      "surface": "runtime",
      "execution": "command",
      "permission": "delegated-mcp-authority",
      "params_type": "MCPServerParams",
      "result_type": "Empty"
    },
    {
      "name": "mcp.status",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "MCPListResult"
    },
    {
      "name": "permission.forget",
      "surface": "runtime",
      "execution": "command",
      "permission": "rule-authority",
      "params_type": "IDParams",
      "result_type": "Empty"
    },
    {
      "name": "permission.mode",
      "surface": "runtime",
      "execution": "command",
      "permission": "trusted-client-mode",
      "params_type": "PermissionConfigureParams",
      "result_type": "Empty"
    },
    {
      "name": "permission.rules",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "PermissionRulesResult"
    },
    {
      "name": "provider.catalogs",
      "surface": "runtime",
      "execution": "query",
      "permission": "host-runtime",
      "params_type": "ProviderCatalogParams",
      "result_type": "ProviderCatalogsResult"
    },
    {
      "name": "question.answer",
      "surface": "runtime",
      "execution": "command",
      "permission": "pending-question",
      "params_type": "QuestionAnswerParams",
      "result_type": "Empty"
    },
    {
      "name": "run.configure",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-idle",
      "params_type": "RunConfigureParams",
      "result_type": "Empty"
    },
    {
      "name": "schedule.create",
      "surface": "runtime",
      "execution": "command",
      "permission": "schedule-budget",
      "params_type": "ScheduleCreateParams",
      "result_type": "ScheduleResult"
    },
    {
      "name": "schedule.delete",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-association",
      "params_type": "ScheduleDeleteParams",
      "result_type": "ScheduleResult"
    },
    {
      "name": "schedule.list",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "ScheduleListResult"
    },
    {
      "name": "session.archive",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-association",
      "params_type": "ArchiveParams",
      "result_type": "ArchiveResult"
    },
    {
      "name": "session.autotitle",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "Empty"
    },
    {
      "name": "session.create",
      "surface": "runtime",
      "execution": "command",
      "permission": "host-runtime",
      "params_type": "CreateSessionParams",
      "result_type": "RootIDResult"
    },
    {
      "name": "session.delete",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-association",
      "params_type": "RootParams",
      "result_type": "RootIDResult"
    },
    {
      "name": "session.effort",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-idle",
      "params_type": "EffortParams",
      "result_type": "EffortResult"
    },
    {
      "name": "session.effort.get",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "EffortResult"
    },
    {
      "name": "session.fork",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-association",
      "params_type": "ForkParams",
      "result_type": "RootIDResult"
    },
    {
      "name": "session.list",
      "surface": "runtime",
      "execution": "query",
      "permission": "host-runtime",
      "params_type": "ListParams",
      "result_type": "SessionListResult"
    },
    {
      "name": "session.model",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-idle",
      "params_type": "ModelParams",
      "result_type": "ModelResult"
    },
    {
      "name": "session.model.get",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "ModelResult"
    },
    {
      "name": "session.open",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "IDParams",
      "result_type": "RootIDResult"
    },
    {
      "name": "session.preview",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "IDParams",
      "result_type": "SessionPreviewResult"
    },
    {
      "name": "session.reload",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-idle",
      "params_type": "EmptyParams",
      "result_type": "ModelResult"
    },
    {
      "name": "session.rename",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-association",
      "params_type": "TitleParams",
      "result_type": "TitleResult"
    },
    {
      "name": "shell.run",
      "surface": "runtime",
      "execution": "command",
      "permission": "tool-permissions",
      "params_type": "ShellParams",
      "result_type": "TextResult"
    },
    {
      "name": "steer",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-admission",
      "params_type": "SubmitPayload",
      "result_type": "TextResult"
    },
    {
      "name": "submit",
      "surface": "runtime",
      "execution": "command",
      "permission": "root-admission",
      "params_type": "SubmitPayload",
      "result_type": "TextResult"
    },
    {
      "name": "terminal.input",
      "surface": "runtime",
      "execution": "ephemeral",
      "permission": "active-terminal",
      "sensitive": true,
      "params_type": "TerminalInputParams",
      "result_type": "Empty"
    },
    {
      "name": "tool.call",
      "surface": "runtime",
      "execution": "command",
      "permission": "tool-permissions",
      "params_type": "ToolCallParams",
      "result_type": "TextResult"
    },
    {
      "name": "tool.configure",
      "surface": "runtime",
      "execution": "command",
      "permission": "tool-permissions",
      "params_type": "ToolConfigureParams",
      "result_type": "Empty"
    },
    {
      "name": "tool.schema",
      "surface": "runtime",
      "execution": "query",
      "permission": "tool-authority",
      "params_type": "EmptyParams",
      "result_type": "ToolSchemaResult"
    },
    {
      "name": "workspace.inspect",
      "surface": "runtime",
      "execution": "query",
      "permission": "root-association",
      "params_type": "EmptyParams",
      "result_type": "PathResult"
    },
    {
      "name": "workspace.set",
      "surface": "runtime",
      "execution": "command",
      "permission": "workspace-authority",
      "params_type": "PathParams",
      "result_type": "PathResult"
    }
  ],
  "events": {
    "event": "EventNotification",
    "subscription.failed": "SubscriptionFailure"
  },
  "event_payloads": {
    "agent.admitted": "LifecycleEvent",
    "agent.prompt.queued": "LifecycleEvent",
    "agent.subtree.deleted": "LifecycleEvent",
    "agent.subtree.stopped": "LifecycleEvent",
    "agent.turn.cancelled": "LifecycleEvent",
    "agent.turn.failed": "LifecycleEvent",
    "agent.turn.interrupted": "LifecycleEvent",
    "agent.turn.started": "LifecycleEvent",
    "agent.turn.succeeded": "LifecycleEvent",
    "blackboard.append": "LifecycleEvent",
    "blackboard.cas": "LifecycleEvent",
    "blackboard.set": "LifecycleEvent",
    "budget.active_child.reserved": "LifecycleEvent",
    "budget.capped": "LifecycleEvent",
    "capability.delegated": "LifecycleEvent",
    "capability.revoked": "LifecycleEvent",
    "command.cancelled": "LifecycleEvent",
    "command.control.queued": "LifecycleEvent",
    "command.failed": "LifecycleEvent",
    "command.interrupted": "LifecycleEvent",
    "command.queued": "LifecycleEvent",
    "command.running": "LifecycleEvent",
    "command.succeeded": "LifecycleEvent",
    "command.waiting": "LifecycleEvent",
    "goal.continued": "LifecycleEvent",
    "inbox.consumed": "LifecycleEvent",
    "inbox.failed": "LifecycleEvent",
    "inbox.queued": "LifecycleEvent",
    "message.deferred": "LifecycleEvent",
    "message.delivered": "LifecycleEvent",
    "message.done": "LifecycleEvent",
    "message.queued": "LifecycleEvent",
    "message.updated": "LifecycleEvent",
    "model.call.corrected": "LifecycleEvent",
    "model.call.interrupted": "LifecycleEvent",
    "model.call.settled": "LifecycleEvent",
    "model.call.started": "LifecycleEvent",
    "permission.auto_approved": "LifecycleEvent",
    "permission.pending": "LifecycleEvent",
    "question.answered": "LifecycleEvent",
    "question.closed": "LifecycleEvent",
    "question.pending": "LifecycleEvent",
    "root.failed": "LifecycleEvent",
    "root.interrupted": "LifecycleEvent",
    "root.stopped": "LifecycleEvent",
    "schedule.fired": "LifecycleEvent",
    "scratch.restored": "LifecycleEvent",
    "session.archived.updated": "SessionUpdateEvent",
    "session.cwd.updated": "SessionUpdateEvent",
    "session.effort.updated": "SessionUpdateEvent",
    "session.model.updated": "SessionUpdateEvent",
    "session.permission_mode.updated": "SessionUpdateEvent",
    "session.reload.failed": "LifecycleEvent",
    "session.title.updated": "SessionUpdateEvent",
    "state.private.append": "LifecycleEvent",
    "state.private.cas": "LifecycleEvent",
    "state.private.set": "LifecycleEvent",
    "stream.accounting": "StreamEvent",
    "stream.cell.host": "StreamEvent",
    "stream.cell.host.started": "StreamEvent",
    "stream.notice": "StreamEvent",
    "stream.reasoning": "StreamEvent",
    "stream.terminal.awaiting": "StreamEvent",
    "stream.terminal.completed": "StreamEvent",
    "stream.terminal.output": "StreamEvent",
    "stream.terminal.started": "StreamEvent",
    "stream.text": "StreamEvent",
    "stream.tool.call": "StreamEvent",
    "stream.tool.completed": "StreamEvent",
    "stream.tool.output": "StreamEvent",
    "stream.tool.started": "StreamEvent",
    "stream.usage": "StreamEvent",
    "subscription.cancelled": "LifecycleEvent",
    "subscription.created": "LifecycleEvent",
    "turn.cancelled": "LifecycleEvent",
    "turn.failed": "LifecycleEvent",
    "turn.interrupted": "LifecycleEvent",
    "turn.started": "LifecycleEvent",
    "turn.succeeded": "LifecycleEvent"
  }
};
function operationLookup(surface) {
  return Object.freeze(Object.fromEntries(manifest.operations.filter(operation => operation.surface === surface).map(operation =>
    [operation.name, Object.freeze({ ...operation, sensitive: operation.sensitive ?? false })],
  )));
}
export const rpcOperations = operationLookup('rpc');
export const runtimeOperations = operationLookup('runtime');
function validatorFor(type, mode) {
  if (mode !== 'request' && mode !== 'response') throw new TypeError('Unknown WHIP validation mode: ' + mode);
  const validators = mode === 'response' ? responses : requests;
  if (!Object.hasOwn(validators, type)) throw new TypeError('Unknown WHIP contract type: ' + type);
  return validators[type];
}
export function validate(type, value, mode = 'request') {
  return validatorFor(type, mode)(value);
}
export function assertValid(type, value, mode = 'request') {
  const validator = validatorFor(type, mode);
  if (!validator(value)) {
    const details = validator.errors.map(error => (error.instancePath || '/') + ' ' + error.message).join('; ');
    throw new TypeError('Invalid WHIP ' + type + ': ' + details);
  }
}
