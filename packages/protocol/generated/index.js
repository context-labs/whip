// Generated from Go DTOs. Run npm run generate.
import * as validators from './validators.js';
export const manifest = {
  "major": 4,
  "minor": 0,
  "operations": [
    {
      "name": "workspace.capture",
      "params": "WorkspaceActionParams",
      "result": "WorkspaceResult"
    },
    {
      "name": "workspace.restore",
      "params": "WorkspaceActionParams",
      "result": "WorkspaceResult"
    },
    {
      "name": "workspace.release",
      "params": "WorkspaceActionParams",
      "result": "WorkspaceResult"
    },
    {
      "name": "workspace.action",
      "params": "ReadWorkspaceActionParams",
      "result": "WorkspaceAction"
    },
    {
      "name": "workspace.snapshot",
      "params": "WorkspaceSnapshotParams",
      "result": "WorkspaceSnapshot"
    },
    {
      "name": "workspace.snapshots",
      "params": "WorkspaceSnapshotsParams",
      "result": "WorkspaceSnapshotsResult"
    },
    {
      "name": "providers.presets",
      "params": "EmptyParams",
      "result": "ProviderPresetsResult"
    },
    {
      "name": "providers.bundled",
      "params": "ProviderParams",
      "result": "ProviderModelsResult"
    },
    {
      "name": "providers.list",
      "params": "EmptyParams",
      "result": "ProviderInventory"
    },
    {
      "name": "providers.create",
      "params": "ChangeProviderParams",
      "result": "ProviderInventory"
    },
    {
      "name": "providers.update",
      "params": "ChangeProviderParams",
      "result": "ProviderInventory"
    },
    {
      "name": "providers.remove",
      "params": "RemoveProviderParams",
      "result": "ProviderInventory"
    },
    {
      "name": "providers.defaults",
      "params": "ProviderDefaultsParams",
      "result": "ProviderInventory"
    },
    {
      "name": "providers.compaction",
      "params": "ProviderDefaultsParams",
      "result": "ProviderInventory"
    },
    {
      "name": "providers.catalog",
      "params": "ProviderParams",
      "result": "ProviderCatalog"
    },
    {
      "name": "providers.refresh",
      "params": "ProviderParams",
      "result": "ProviderCatalog"
    },
    {
      "name": "providers.readiness",
      "params": "ProviderReadinessParams",
      "result": "ProviderReadiness"
    },
    {
      "name": "lsp.status",
      "params": "SessionParams",
      "result": "LanguageServersResult"
    },
    {
      "name": "accounts.openai.begin",
      "params": "EmptyParams",
      "result": "OpenAILoginFlow"
    },
    {
      "name": "accounts.openai.get",
      "params": "OpenAIFlowParams",
      "result": "OpenAILoginFlow"
    },
    {
      "name": "accounts.openai.list",
      "params": "EmptyParams",
      "result": "OpenAIFlowsResult"
    },
    {
      "name": "accounts.openai.cancel",
      "params": "OpenAIFlowParams",
      "result": "OpenAILoginFlow"
    },
    {
      "name": "accounts.openai.status",
      "params": "EmptyParams",
      "result": "OpenAIAccountStatus"
    },
    {
      "name": "accounts.openai.setup",
      "params": "EmptyParams",
      "result": "OpenAIAccountStatus"
    },
    {
      "name": "accounts.openai.logout",
      "params": "EmptyParams",
      "result": "OpenAIAccountStatus"
    },
    {
      "name": "accounts.inference.begin",
      "params": "EmptyParams",
      "result": "InferenceFlow"
    },
    {
      "name": "accounts.inference.get",
      "params": "InferenceFlowParams",
      "result": "InferenceFlow"
    },
    {
      "name": "accounts.inference.list",
      "params": "EmptyParams",
      "result": "InferenceFlowsResult"
    },
    {
      "name": "accounts.inference.cancel",
      "params": "InferenceFlowParams",
      "result": "InferenceFlow"
    },
    {
      "name": "accounts.inference.team",
      "params": "InferenceTeamParams",
      "result": "InferenceFlow"
    },
    {
      "name": "accounts.inference.project",
      "params": "InferenceProjectParams",
      "result": "InferenceFlow"
    },
    {
      "name": "accounts.inference.create_project",
      "params": "InferenceCreateProjectParams",
      "result": "InferenceFlow"
    },
    {
      "name": "accounts.inference.retry",
      "params": "InferenceFlowParams",
      "result": "InferenceFlow"
    },
    {
      "name": "accounts.inference.rotate",
      "params": "EmptyParams",
      "result": "InferenceFlow"
    },
    {
      "name": "accounts.inference.status",
      "params": "EmptyParams",
      "result": "InferenceAccountStatus"
    },
    {
      "name": "accounts.inference.setup",
      "params": "EmptyParams",
      "result": "InferenceAccountStatus"
    },
    {
      "name": "accounts.inference.logout",
      "params": "EmptyParams",
      "result": "InferenceLogoutResult"
    },
    {
      "name": "accounts.inference.cleanup",
      "params": "EmptyParams",
      "result": "InferenceCleanupResult"
    },
    {
      "name": "accounts.inference.retry_cleanup",
      "params": "EmptyParams",
      "result": "InferenceCleanupResult"
    },
    {
      "name": "goals.formulate",
      "params": "FormulateGoalParams",
      "result": "Admission"
    },
    {
      "name": "goals.formulation",
      "params": "GoalFormulationParams",
      "result": "GoalFormulation"
    },
    {
      "name": "goals.create",
      "params": "CreateGoalParams",
      "result": "GoalAdmission"
    },
    {
      "name": "goals.current",
      "params": "SessionParams",
      "result": "CurrentGoalResult"
    },
    {
      "name": "goals.get",
      "params": "GoalParams",
      "result": "Goal"
    },
    {
      "name": "goals.resume",
      "params": "ResumeGoalParams",
      "result": "Admission"
    },
    {
      "name": "goals.cancel",
      "params": "GoalParams",
      "result": "GoalChange"
    },
    {
      "name": "schedules.create",
      "params": "CreateScheduleParams",
      "result": "ScheduleAdmission"
    },
    {
      "name": "schedules.get",
      "params": "ScheduleParams",
      "result": "ScheduleResult"
    },
    {
      "name": "schedules.list",
      "params": "ListSchedulesParams",
      "result": "SchedulesResult"
    },
    {
      "name": "schedules.cancel",
      "params": "ScheduleParams",
      "result": "ScheduleAdmission"
    },
    {
      "name": "sessions.compact",
      "params": "CompactParams",
      "result": "Admission"
    },
    {
      "name": "context.head",
      "params": "SessionParams",
      "result": "ContextHead"
    },
    {
      "name": "context.compaction",
      "params": "CompactionParams",
      "result": "CompactionResult"
    },
    {
      "name": "context.compactions",
      "params": "CompactionsParams",
      "result": "CompactionsResult"
    },
    {
      "name": "context.select",
      "params": "SelectCompactionParams",
      "result": "ContextHead"
    },
    {
      "name": "context.snapshot",
      "params": "SessionParams",
      "result": "HistorySnapshot"
    },
    {
      "name": "context.list",
      "params": "ContextHistoryParams",
      "result": "HistoryMetadataResult"
    },
    {
      "name": "context.read",
      "params": "ReadHistoryParams",
      "result": "ReadHistoryResult"
    },
    {
      "name": "context.search",
      "params": "SearchHistoryParams",
      "result": "SearchHistoryResult"
    },
    {
      "name": "turns.output",
      "params": "TurnParams",
      "result": "TurnOutputResult"
    },
    {
      "name": "turns.instructions",
      "params": "TurnParams",
      "result": "InstructionManifestResult"
    },
    {
      "name": "skills.list",
      "params": "ListSkillsParams",
      "result": "ListSkillsResult"
    },
    {
      "name": "completions.list",
      "params": "ListCompletionsParams",
      "result": "ListCompletionsResult"
    },
    {
      "name": "completions.read",
      "params": "ReadCompletionParams",
      "result": "ReadCompletionResult"
    },
    {
      "name": "state.subscribe",
      "params": "SubscribeStateParams",
      "result": "StateSubscription"
    },
    {
      "name": "state.subscriptions",
      "params": "StateSubscriptionsParams",
      "result": "StateSubscriptionsResult"
    },
    {
      "name": "state.unsubscribe",
      "params": "UnsubscribeStateParams",
      "result": "StateSubscription"
    },
    {
      "name": "state.get",
      "params": "GetStateParams",
      "result": "StateVersion"
    },
    {
      "name": "state.write",
      "params": "WriteStateParams",
      "result": "StateVersion"
    },
    {
      "name": "state.append",
      "params": "WriteStateParams",
      "result": "StateVersion"
    },
    {
      "name": "state.read",
      "params": "ReadStateParams",
      "result": "ReadStateResult"
    },
    {
      "name": "state.list",
      "params": "ListStateParams",
      "result": "StateVersionsResult"
    },
    {
      "name": "state.history",
      "params": "StateHistoryParams",
      "result": "StateVersionsResult"
    },
    {
      "name": "mail.send",
      "params": "SendMailParams",
      "result": "MailAdmission"
    },
    {
      "name": "mail.list",
      "params": "ListMailParams",
      "result": "ListMailResult"
    },
    {
      "name": "mail.read",
      "params": "ReadMailParams",
      "result": "ReadMailResult"
    },
    {
      "name": "resources.list",
      "params": "SessionParams",
      "result": "ResourcesResult"
    },
    {
      "name": "resources.set",
      "params": "SetResourceParams",
      "result": "ResourceUsage"
    },
    {
      "name": "budgets.list",
      "params": "SessionParams",
      "result": "BudgetsResult"
    },
    {
      "name": "budgets.set",
      "params": "SetBudgetParams",
      "result": "Budget"
    },
    {
      "name": "sessions.observe",
      "params": "HistoryParams",
      "result": "SessionObservation"
    },
    {
      "name": "cells.get",
      "params": "CellParams",
      "result": "Cell"
    },
    {
      "name": "turns.cells",
      "params": "CellsParams",
      "result": "CellsResult"
    },
    {
      "name": "grants.create",
      "params": "CreateGrantParams",
      "result": "Grant"
    },
    {
      "name": "grants.list",
      "params": "GrantsParams",
      "result": "GrantsResult"
    },
    {
      "name": "grants.revoke",
      "params": "GrantParams",
      "result": "Grant"
    },
    {
      "name": "operations.get",
      "params": "HostOperationParams",
      "result": "HostOperation"
    },
    {
      "name": "turns.operations",
      "params": "HostOperationsParams",
      "result": "HostOperationsResult"
    },
    {
      "name": "permissions.list",
      "params": "PermissionsParams",
      "result": "PermissionsResult"
    },
    {
      "name": "permissions.resolve",
      "params": "ResolvePermissionParams",
      "result": "Permission"
    },
    {
      "name": "permissions.policy",
      "params": "SessionParams",
      "result": "PermissionPolicy"
    },
    {
      "name": "permissions.set_mode",
      "params": "SetPermissionModeParams",
      "result": "PermissionModeEdit"
    },
    {
      "name": "permissions.mode_edit",
      "params": "PermissionModeEditParams",
      "result": "PermissionModeEdit"
    },
    {
      "name": "host.permission_default",
      "params": "EmptyParams",
      "result": "DefaultPermissionMode"
    },
    {
      "name": "host.set_permission_default",
      "params": "SetDefaultPermissionModeParams",
      "result": "DefaultPermissionMode"
    },
    {
      "name": "questions.get",
      "params": "QuestionParams",
      "result": "Question"
    },
    {
      "name": "questions.list",
      "params": "QuestionsParams",
      "result": "QuestionsResult"
    },
    {
      "name": "questions.answer",
      "params": "AnswerQuestionParams",
      "result": "Question"
    },
    {
      "name": "initialize",
      "params": "InitializeParams",
      "result": "InitializeResult"
    },
    {
      "name": "trees.create",
      "params": "CreateTreeParams",
      "result": "CreateTreeResult"
    },
    {
      "name": "trees.get",
      "params": "TreeParams",
      "result": "Tree"
    },
    {
      "name": "trees.update",
      "params": "UpdateTreeParams",
      "result": "Tree"
    },
    {
      "name": "trees.title_decision",
      "params": "TreeParams",
      "result": "AutomaticTitleDecision"
    },
    {
      "name": "trees.title_result",
      "params": "AutomaticTitleResultParams",
      "result": "AutomaticTitleResult"
    },
    {
      "name": "sessions.get",
      "params": "SessionParams",
      "result": "Session"
    },
    {
      "name": "sessions.spawn",
      "params": "SpawnSessionParams",
      "result": "SpawnSessionResult"
    },
    {
      "name": "sessions.list",
      "params": "ListSessionsParams",
      "result": "ListSessionsResult"
    },
    {
      "name": "sessions.configure",
      "params": "UpdateConfigurationParams",
      "result": "Session"
    },
    {
      "name": "sessions.submit",
      "params": "SubmitParams",
      "result": "Admission"
    },
    {
      "name": "sessions.history",
      "params": "HistoryParams",
      "result": "HistoryResult"
    },
    {
      "name": "sessions.rewind",
      "params": "RewindParams",
      "result": "HistoryEdit"
    },
    {
      "name": "sessions.fork",
      "params": "ForkParams",
      "result": "ForkResult"
    },
    {
      "name": "sessions.lifecycle",
      "params": "LifecycleParams",
      "result": "Session"
    },
    {
      "name": "sessions.delete",
      "params": "SessionParams",
      "result": "DeleteResult"
    },
    {
      "name": "turns.get",
      "params": "TurnParams",
      "result": "Turn"
    },
    {
      "name": "turns.attempts",
      "params": "ModelAttemptsParams",
      "result": "ModelAttemptsResult"
    },
    {
      "name": "turns.cancel",
      "params": "TurnParams",
      "result": "Turn"
    },
    {
      "name": "inputs.cancel",
      "params": "InputParams",
      "result": "Input"
    },
    {
      "name": "receipts.get",
      "params": "RequestIdentity",
      "result": "Admission"
    },
    {
      "name": "content.put",
      "params": "PutContentParams",
      "result": "ContentReference"
    },
    {
      "name": "content.read",
      "params": "ReadContentParams",
      "result": "ReadContentResult"
    },
    {
      "name": "definitions.register",
      "params": "DefinitionDocument",
      "result": "Definition"
    },
    {
      "name": "definitions.get",
      "params": "DefinitionRef",
      "result": "Definition"
    }
  ]
};
export function validate(type, value) {
  if (!Object.hasOwn(validators, type)) throw new TypeError('Unknown contract type: ' + type);
  return validators[type](value);
}
export function assertValid(type, value) {
  if (!validate(type, value)) throw new TypeError('Invalid ' + type + ': ' + JSON.stringify(validators[type].errors));
}
