// Generated from Go DTOs. Run npm run generate.
import * as validators from './validators.js';
export const manifest = {
  "major": 4,
  "minor": 0,
  "operations": [
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
