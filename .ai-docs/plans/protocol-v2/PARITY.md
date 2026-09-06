# WHIP v3 parity inventory

Every registered runtime action has a typed request and result and a real-RPC
fixture in `TestRuntimeRegistryEveryOperationOverUnixRPC`. Service-specific
success, authority and recovery scenarios remain in the existing release suite.
Queries use `query`; durable operations use `command.submit`; ephemeral inputs
use `operation.invoke` or their named RPC and never enter the command journal.

## Runtime actions

| Action | Class | Parameters → result | Behavioral coverage |
| --- | --- | --- | --- |
| `agent.control` | command | `IDParams` → `Empty` | Runtime registry fixture + release acceptance |
| `agent.delete` | command | `IDParams` → `Empty` | Runtime registry fixture + release acceptance |
| `agent.submit` | command | `AgentInputParams` → `AgentSubmitResult` | Runtime registry fixture + release acceptance |
| `agent.transcript` | query | `IDParams` → `AgentTranscriptResult` | Runtime registry fixture + release acceptance |
| `agent.turn.cancel` | command | `AgentCancelParams` → `Empty` | Runtime registry fixture + release acceptance |
| `agents.list` | query | `EmptyParams` → `AgentListResult` | Runtime registry fixture + release acceptance |
| `browser.set_driver` | command | `BrowserDriverParams` → `BrowserStatusResult` | Runtime registry fixture + release acceptance |
| `browser.status` | query | `EmptyParams` → `BrowserStatusResult` | Runtime registry fixture + release acceptance |
| `budget.cap` | command | `BudgetCapParams` → `BudgetState` | Runtime registry fixture + release acceptance |
| `cancel` | command | `CancelParams` → `Empty` | Runtime registry fixture + release acceptance |
| `capability.revoke` | command | `IDParams` → `CapabilityRecord` | Runtime registry fixture + release acceptance |
| `compaction.configure` | command | `CompactionParams` → `CompactionSettingsResult` | Runtime registry fixture + release acceptance |
| `computer.allow` | command | `ComputerAppParams` → `ComputerStatusResult` | Runtime registry fixture + release acceptance |
| `computer.deny` | command | `ComputerAppParams` → `ComputerStatusResult` | Runtime registry fixture + release acceptance |
| `computer.status` | query | `EmptyParams` → `ComputerStatusResult` | Runtime registry fixture + release acceptance |
| `context.audit` | query | `EmptyParams` → `ContextAuditResult` | Runtime registry fixture + release acceptance |
| `daemon.checkpoint` | command | `CheckpointParams` → `RestartNotice` | Runtime registry fixture + release acceptance |
| `goal.from-context` | command | `GoalContextParams` → `GoalResult` | Runtime registry fixture + release acceptance |
| `goal.run` | command | `TextParams` → `GoalResult` | Runtime registry fixture + release acceptance |
| `goal.set` | command | `TextParams` → `GoalResult` | Runtime registry fixture + release acceptance |
| `history.clear` | command | `EmptyParams` → `Empty` | Runtime registry fixture + release acceptance |
| `history.compact` | command | `EmptyParams` → `CompactionResult` | Runtime registry fixture + release acceptance |
| `history.compact.log` | query | `EmptyParams` → `CompactionListResult` | Runtime registry fixture + release acceptance |
| `history.compact.retry` | command | `EmptyParams` → `CompactionRetryResult` | Runtime registry fixture + release acceptance |
| `history.rewind` | command | `RewindParams` → `RewindResult` | Runtime registry fixture + release acceptance |
| `history.user.list` | query | `EmptyParams` → `UserHistoryResult` | Runtime registry fixture + release acceptance |
| `lsp.status` | query | `EmptyParams` → `LSPListResult` | Runtime registry fixture + release acceptance |
| `mcp.attach` | ephemeral | `MCPAttachParams` → `Empty` | Runtime registry fixture + release acceptance |
| `mcp.disable` | command | `MCPServerParams` → `Empty` | Runtime registry fixture + release acceptance |
| `mcp.enable` | command | `MCPServerParams` → `Empty` | Runtime registry fixture + release acceptance |
| `mcp.import.configure` | command | `MCPImportParams` → `MCPImportStatusResult` | Runtime registry fixture + release acceptance |
| `mcp.import.status` | query | `EmptyParams` → `MCPImportStatusResult` | Runtime registry fixture + release acceptance |
| `mcp.reconnect` | command | `MCPServerParams` → `Empty` | Runtime registry fixture + release acceptance |
| `mcp.status` | query | `EmptyParams` → `MCPListResult` | Runtime registry fixture + release acceptance |
| `permission.forget` | command | `IDParams` → `Empty` | Runtime registry fixture + release acceptance |
| `permission.mode` | command | `PermissionConfigureParams` → `Empty` | Runtime registry fixture + release acceptance |
| `permission.rules` | query | `EmptyParams` → `PermissionRulesResult` | Runtime registry fixture + release acceptance |
| `provider.catalogs` | query | `EmptyParams` → `ProviderCatalogsResult` | Runtime registry fixture + release acceptance |
| `question.answer` | command | `QuestionAnswerParams` → `Empty` | Runtime registry fixture + release acceptance |
| `run.configure` | command | `RunConfigureParams` → `Empty` | Runtime registry fixture + release acceptance |
| `schedule.create` | command | `ScheduleCreateParams` → `ScheduleResult` | Runtime registry fixture + release acceptance |
| `schedule.delete` | command | `ScheduleDeleteParams` → `ScheduleResult` | Runtime registry fixture + release acceptance |
| `schedule.list` | query | `EmptyParams` → `ScheduleListResult` | Runtime registry fixture + release acceptance |
| `session.autotitle` | command | `EmptyParams` → `Empty` | TUI startup + first-turn title event over Unix/WebSocket; rename protection |
| `session.create` | command | `CreateSessionParams` → `RootIDResult` | Runtime registry fixture + release acceptance |
| `session.delete` | command | `RootParams` → `RootIDResult` | Runtime registry fixture + release acceptance |
| `session.effort` | command | `EffortParams` → `EffortResult` | Runtime registry fixture + release acceptance |
| `session.effort.get` | query | `EmptyParams` → `EffortResult` | Runtime registry fixture + release acceptance |
| `session.fork` | command | `ForkParams` → `RootIDResult` | Runtime registry fixture + release acceptance |
| `session.list` | query | `ListParams` → `SessionListResult` | Runtime registry fixture + release acceptance |
| `session.model` | command | `ModelParams` → `ModelResult` | Runtime registry fixture + release acceptance |
| `session.model.get` | query | `EmptyParams` → `ModelResult` | Runtime registry fixture + release acceptance |
| `session.open` | query | `IDParams` → `RootIDResult` | Runtime registry fixture + release acceptance |
| `session.preview` | query | `IDParams` → `SessionPreviewResult` | Runtime registry fixture + release acceptance |
| `session.reload` | command | `EmptyParams` → `ModelResult` | Runtime registry fixture + release acceptance |
| `session.rename` | command | `TitleParams` → `TitleResult` | Runtime registry fixture + release acceptance |
| `shell.run` | command | `ShellParams` → `TextResult` | Runtime registry fixture + release acceptance |
| `steer` | command | `SubmitPayload` → `TextResult` | Runtime registry fixture + release acceptance |
| `submit` | command | `SubmitPayload` → `TextResult` | Runtime registry fixture + release acceptance |
| `terminal.input` | ephemeral | `TerminalInputParams` → `Empty` | Runtime registry fixture + release acceptance |
| `tool.call` | command | `ToolCallParams` → `TextResult` | Runtime registry fixture + release acceptance |
| `tool.configure` | command | `ToolConfigureParams` → `Empty` | Runtime registry fixture + release acceptance |
| `tool.schema` | query | `EmptyParams` → `ToolSchemaResult` | Runtime registry fixture + release acceptance |
| `workspace.inspect` | query | `EmptyParams` → `PathResult` | Runtime registry fixture + release acceptance |
| `workspace.set` | command | `PathParams` → `PathResult` | Runtime registry fixture + release acceptance |

## RPCs

| Method | Class | Parameters → result | Behavioral coverage |
| --- | --- | --- | --- |
| `command.status` | query | `CommandStatusParams` → `CommandResult` | Runtime registry RPC fixtures; admission, cancellation, deduplication and restart tests |
| `command.submit` | command | `CommandParams` → `CommandResult` | Runtime registry RPC fixtures; admission, cancellation, deduplication and restart tests |
| `config.get` | query | `Empty` → `RuntimeConfiguration` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `config.update` | ephemeral | `ConfigurationUpdate` → `RuntimeConfiguration` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `content.read` | query | `ContentReadParams` → `ContentReadResult` | HTTP content/grant tests; cross-transport/browser content and large history tests |
| `daemon.ping` | query | `Empty` → `PingResult` | Cross-transport initialize; autostart, lifecycle and protocol edge tests |
| `daemon.restart` | lifecycle | `RestartParams` → `Empty` | Cross-transport initialize; autostart, lifecycle and protocol edge tests |
| `daemon.stop` | lifecycle | `RestartParams` → `Empty` | Cross-transport initialize; autostart, lifecycle and protocol edge tests |
| `events.replay` | query | `ReplayParams` → `ReplayResult` | Cross-transport views/reconnect; collection, history, root client and snapshot tests |
| `events.subscribe` | subscription | `SubscribeParams` → `SubscribeResult` | Cross-transport views/reconnect; collection, history, root client and snapshot tests |
| `events.unsubscribe` | subscription | `UnsubscribeParams` → `Empty` | Cross-transport views/reconnect; collection, history, root client and snapshot tests |
| `history.page` | query | `HistoryPageParams` → `BoundedTranscriptPage` | Cross-transport views/reconnect; collection, history, root client and snapshot tests |
| `initialize` | query | `InitializeParams` → `InitializeResult` | Cross-transport initialize; autostart, lifecycle and protocol edge tests |
| `operation.invoke` | ephemeral | `QueryParams` → `QueryResult` | Runtime registry RPC fixtures; admission, cancellation, deduplication and restart tests |
| `permission.decide` | ephemeral | `PermissionDecisionParams` → `PermissionDecisionResult` | Cross-transport trusted-client decisions; permission and MCP authority tests |
| `provider.key.rotate` | ephemeral | `ProviderNameParams` → `ProviderStatus` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `provider.key.set` | ephemeral | `ProviderKeySetup` → `RuntimeConfiguration` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `provider.login.begin` | ephemeral | `Empty` → `ProviderLoginStatus` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `provider.login.cancel` | ephemeral | `ProviderLoginParams` → `ProviderLoginStatus` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `provider.login.list` | query | `Empty` → `ProviderLoginList` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `provider.login.project.create` | ephemeral | `ProviderLoginCreateParams` → `ProviderLoginStatus` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `provider.login.project.select` | ephemeral | `ProviderLoginProjectParams` → `ProviderLoginStatus` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `provider.login.status` | query | `ProviderLoginParams` → `ProviderLoginStatus` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `provider.login.team.select` | ephemeral | `ProviderLoginTeamParams` → `ProviderLoginStatus` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `provider.logout` | ephemeral | `ProviderNameParams` → `ProviderStatus` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `provider.status` | query | `ProviderNameParams` → `ProviderStatus` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `provider.validate` | ephemeral | `ProviderValidateParams` → `ProviderValidateResult` | Provider service, revision/configuration and CLI/TUI onboarding tests |
| `query` | query | `QueryParams` → `QueryResult` | Runtime registry RPC fixtures; admission, cancellation, deduplication and restart tests |
| `root.collection` | query | `RootCollectionParams` → `RootCollectionPage` | Cross-transport views/reconnect; collection, history, root client and snapshot tests |
| `root.snapshot` | query | `SnapshotParams` → `RootSnapshot` | Cross-transport views/reconnect; collection, history, root client and snapshot tests |
| `sessions.list` | query | `SessionCatalogParams` → `SessionCatalogPage` | Cross-transport views/reconnect; collection, history, root client and snapshot tests |
| `sessions.revision` | query | `EmptyParams` → `CatalogRevision` | Cross-transport views/reconnect; collection, history, root client and snapshot tests |
| `upload.begin` | ephemeral | `UploadBeginParams` → `Accepted` | HTTP content/grant tests; cross-transport/browser content and large history tests |
| `upload.chunk` | ephemeral | `UploadChunkParams` → `Accepted` | HTTP content/grant tests; cross-transport/browser content and large history tests |
| `upload.finish` | ephemeral | `UploadFinishParams` → `ContentHandle` | HTTP content/grant tests; cross-transport/browser content and large history tests |
| `workspace.complete` | query | `CompletionParams` → `CompletionResult` | Host completion, stale TUI response and architecture tests |

## Shipped clients

| Client/workflow | protocol boundary | Coverage |
| --- | --- | --- |
| TUI, recursive tree and dialogs | RootClient + typed query/command/ephemeral dispatch | TUI, question, permission and recursive acceptance suites |
| Headless run and session CLI | Shared Go client | cmd/whip runtime/session tests |
| ACP/editor and MCP stdio | Shared Go client; existing delegated capability checks | ACP/MCP authority and runtime acceptance |
| Daemon status/start/stop/restart and updater | Initialize/ping and explicit lifecycle request | Daemon-management, autostart and restart tests |
| Provider setup and runtime settings | Daemon ProviderService, versioned patches | Provider/configuration + auth/wizard tests |
| History inspection and export | Bounded raw pages; explicit full revision-pinned content download | TUI history/export tests |

V1 codecs, command RPC, chunked snapshot protocol and build-mismatch restart
paths have been removed. Historical repository design documents are not
compatibility implementations.
