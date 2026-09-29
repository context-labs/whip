// Package rpc validates wire requests and maps them onto runtime operations.
// It owns connections, never accepted execution.
package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/account"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/hostview"
	"github.com/context-labs/whip/internal/inferenceaccount"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/providerhost"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/shell"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/terminal"
)

var (
	ErrIdentity = errors.New("runtime identity mismatch")
	ErrMethod   = errors.New("unknown method")
)

func decode[P any](raw json.RawMessage, fn func(P) (any, error)) (any, error) {
	var params P
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("%w: %w", session.ErrInvalid, err)
	}
	return fn(params)
}

func Dispatch(ctx context.Context, r *runtime.Runtime, host HostServices, method string, raw json.RawMessage) (any, error) {
	found := false
	for _, op := range protocol.Operations() {
		if op.Name == method {
			found = true
			if err := protocol.Validate(op.Params.Name(), raw); err != nil {
				return nil, fmt.Errorf("%w: %w", session.ErrInvalid, err)
			}
			break
		}
	}
	if !found {
		return nil, ErrMethod
	}
	switch method {
	case "host.standing.read", "host.standing.write":
		return dispatchStanding(ctx, r, method, raw)
	case "sessions.reload", "sessions.reload_edit", "sessions.cancel_reload":
		return dispatchReload(ctx, r, method, raw)
	case "browser.tabs", "browser.attachments":
		return dispatchBrowserRead(ctx, r, method, raw)
	case "receipts.match":
		return dispatchReceiptMatch(ctx, r, raw)
	case "workspace.inspect", "workspace.set", "run.configure":
		return dispatchControls(ctx, r, method, raw)
	case "models.inspection", "trace.page", "trace.export":
		return dispatchTrace(ctx, r, method, raw)
	case "workspace.complete", "host.attention", "host.directories.list", "host.directory.pick", "host.skills.complete", "host.themes.list", "host.themes.resolve":
		return dispatchHostViews(ctx, r, method, raw)
	case "tool.schemas", "tool.call", "shell.run":
		return dispatchHostOperation(ctx, r, method, raw)
	case "sessions.activity", "inputs.recent_text", "inputs.page", "inputs.get", "inputs.steer", "inputs.steering":
		return dispatchActivity(ctx, r, method, raw)
	case "executor.activity":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			value, err := r.ExecutorActivity(ctx, session.SessionID(p.SessionID))
			result := protocol.ExecutorActivityResult{}
			if value != nil {
				activity := protocol.ExecutorActivity{Epoch: protocol.ID(value.Epoch), TurnID: protocol.ID(value.TurnID), Revision: protocol.Counter(value.Revision), Decisions: []protocol.HookDecision{}, Truncated: value.Truncated}
				for _, decision := range value.Decisions {
					item := protocol.HookDecision{Hook: decision.Hook, Operation: decision.Operation, Decision: decision.Decision, Reason: decision.Reason}
					if decision.InvocationID != "" {
						item.InvocationID = new(protocol.ID(decision.InvocationID))
					}
					activity.Decisions = append(activity.Decisions, item)
				}
				if value.Progress != nil {
					activity.Progress = &protocol.ExecutorProgress{InvocationID: protocol.ID(value.Progress.InvocationID), OperationID: protocol.ID(value.Progress.OperationID), Text: value.Progress.Text}
				}
				result.Activity = &activity
			}
			return result, err
		})

	case "shell.interaction", "shell.input":
		return dispatchShell(ctx, r, method, raw)
	case "computer.status", "computer.configure", "computer.use_bundled", "computer.reconnect", "computer.disconnect":
		return dispatchComputer(ctx, r, method, raw)
	case "mcp.configuration", "mcp.configure", "mcp.import.candidates", "mcp.import.apply", "mcp.status", "mcp.refresh", "mcp.reload", "mcp.reconnect", "mcp.enable", "mcp.disable", "mcp.attach", "mcp.tools", "mcp.instructions", "mcp.brand.icons":
		return dispatchMCP(ctx, r, method, raw)
	case "terminal.open", "terminal.list", "terminal.read", "terminal.write", "terminal.resize", "terminal.close":
		return terminalDispatch(ctx, r, host, method, raw)
	case "workspace.capture", "workspace.restore", "workspace.release", "workspace.action", "workspace.snapshot", "workspace.snapshots":
		return dispatchWorkspace(ctx, r, method, raw)
	case "providers.presets", "providers.bundled", "providers.list", "providers.setup_key", "providers.create", "providers.update", "providers.remove", "providers.defaults", "providers.compaction", "providers.catalog", "providers.refresh", "providers.readiness":
		return dispatchProvider(ctx, host.ProviderHost, method, raw)
	case "lsp.status":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			values, err := r.LSPStatus(ctx, session.SessionID(p.SessionID))
			return languageServersFromDomain(values), err
		})
	case "accounts.openai.begin", "accounts.openai.get", "accounts.openai.list", "accounts.openai.cancel", "accounts.openai.status", "accounts.openai.setup", "accounts.openai.logout":
		return dispatchAccount(ctx, host.OpenAI, method, raw)
	case "accounts.inference.begin", "accounts.inference.get", "accounts.inference.list", "accounts.inference.cancel", "accounts.inference.team", "accounts.inference.project", "accounts.inference.create_project", "accounts.inference.retry", "accounts.inference.rotate", "accounts.inference.status", "accounts.inference.setup", "accounts.inference.logout", "accounts.inference.cleanup", "accounts.inference.retry_cleanup":
		return dispatchInferenceAccount(ctx, host, method, raw)
	case "sessions.compact", "context.head", "context.compaction", "context.compactions", "context.select", "context.usage", "context.snapshot", "context.list", "context.read", "context.search":
		return dispatchContext(ctx, r, method, raw)
	case "turns.output":
		return decode(raw, func(p protocol.TurnParams) (any, error) {
			value, err := r.TurnOutput(ctx, session.TurnID(p.TurnID))
			return protocol.TurnOutputFromDomain(value), err
		})
	case "skills.list":
		return decode(raw, func(p protocol.ListSkillsParams) (any, error) {
			values, next, err := r.Skills(ctx, session.SessionID(p.SessionID), p.Prefix, p.After, p.Limit)
			result := protocol.ListSkillsResult{Items: []protocol.SkillMetadata{}, NextAfter: next}
			for _, value := range values {
				result.Items = append(result.Items, protocol.SkillMetadata{Name: value.Name, Description: value.Description, Disabled: value.Disabled, Source: protocol.InstructionSourceFromDomain(value.Source)})
			}
			return result, err
		})
	case "turns.instructions":
		return decode(raw, func(p protocol.TurnParams) (any, error) {
			value, err := r.InstructionManifest(ctx, session.TurnID(p.TurnID))
			return protocol.InstructionManifestFromDomain(value), err
		})
	case "completions.list", "completions.read":
		return dispatchCompletion(ctx, r, method, raw)
	case "state.subscribe", "state.subscriptions", "state.unsubscribe":
		return dispatchStateSubscription(ctx, r, method, raw)
	case "state.get", "state.write", "state.append", "state.read", "state.list", "state.history":
		return dispatchState(ctx, r, method, raw)
	case "mail.send", "mail.list", "mail.read":
		return dispatchMail(ctx, r, method, raw)
	case "goals.create", "goals.current", "goals.get", "goals.resume", "goals.cancel", "goals.formulate", "goals.formulation":
		return dispatchGoal(ctx, r, method, raw)
	case "schedules.create", "schedules.get", "schedules.list", "schedules.cancel":
		return dispatchSchedule(ctx, r, method, raw)
	case "resources.list", "resources.set":
		return dispatchResource(ctx, r, method, raw)
	case "usage.turn", "usage.get", "budgets.list", "budgets.set":
		return dispatchBudget(ctx, r, method, raw)
	case "sessions.observe":
		return dispatchObservation(ctx, r, raw)
	case "host.profiles", "host.set_profiles":
		return dispatchHostProfiles(ctx, r, method, raw)
	case "host.browser_driver", "host.set_browser_driver":
		return dispatchBrowserDriver(ctx, r, method, raw)
	case "host.execution_defaults", "host.set_execution_defaults":
		return dispatchExecutionDefaults(ctx, r, method, raw)
	case "permissions.set_denial", "permissions.denial_edit", "permissions.policy", "permissions.set_mode", "permissions.mode_edit", "host.permission_default", "host.set_permission_default":
		return dispatchPermissionMode(ctx, r, method, raw)
	case "questions.get", "questions.list", "questions.answer":
		return dispatchQuestion(ctx, r, method, raw)
	case "grants.create", "grants.list", "grants.revoke", "operations.get", "turns.operations", "permissions.list", "permissions.resolve", "cells.get", "cells.output", "turns.cells":
		return dispatchOperation(ctx, r, method, raw)
	case "content.put":
		return decode(raw, func(p protocol.PutContentParams) (any, error) {
			if len(p.DataBase64) > base64.StdEncoding.EncodedLen(session.MaxContentBytes) {
				return nil, fmt.Errorf("%w: content exceeds 4 MiB", session.ErrInvalid)
			}
			data, err := base64.StdEncoding.Strict().DecodeString(p.DataBase64)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid content encoding", session.ErrInvalid)
			}
			value, err := r.PutContent(ctx, session.SessionID(p.SessionID), string(p.ReferenceID), p.MediaType, data)
			return protocol.ContentReferenceFromDomain(value), err
		})
	case "content.get":
		return decode(raw, func(p protocol.ReadContentParams) (any, error) {
			value, err := r.ContentReference(ctx, session.SessionID(p.SessionID), string(p.ReferenceID))
			return protocol.ContentReferenceFromDomain(value), err
		})
	case "content.read":
		return decode(raw, func(p protocol.ReadContentParams) (any, error) {
			value, data, err := r.ReadContent(ctx, session.SessionID(p.SessionID), string(p.ReferenceID), session.MaxContentBytes)
			return protocol.ReadContentResult{Reference: protocol.ContentReferenceFromDomain(value), DataBase64: base64.StdEncoding.EncodeToString(data)}, err
		})
	case "host.status":
		return decode(raw, func(protocol.EmptyParams) (any, error) {
			if host.Lifecycle == nil {
				return nil, fmt.Errorf("%w: process lifecycle is unavailable", store.ErrNotFound)
			}
			return host.Lifecycle.snapshot(), nil
		})
	case "host.stop":
		return decode(raw, func(p protocol.StopHostParams) (any, error) {
			if host.Lifecycle == nil {
				return nil, fmt.Errorf("%w: process lifecycle is unavailable", store.ErrNotFound)
			}
			current := host.Lifecycle.snapshot()
			if p.RuntimeID != current.RuntimeID || p.ProcessEpoch != current.ProcessEpoch {
				return nil, ErrIdentity
			}
			return protocol.HostStopAccepted(p), nil
		})
	case "initialize":
		return decode(raw, func(p protocol.InitializeParams) (any, error) {
			if p.ExpectedProcessEpoch != nil && string(*p.ExpectedProcessEpoch) != r.ProcessEpoch() {
				return nil, ErrIdentity
			}
			if p.ExpectedRuntimeID != nil && string(*p.ExpectedRuntimeID) != string(r.Identity()) {
				return nil, ErrIdentity
			}
			refs, err := r.Builtins()
			if err != nil {
				return nil, err
			}
			result := protocol.InitializeResult{NetworkClient: p.NetworkClient, ProcessEpoch: protocol.ID(r.ProcessEpoch()), Major: protocol.Major, Minor: protocol.Minor, RuntimeID: protocol.ID(r.Identity()), Builtins: []protocol.DefinitionRef{}}
			for _, ref := range refs {
				result.Builtins = append(result.Builtins, protocol.DefinitionRef{ID: protocol.ID(ref.ID), Revision: ref.Revision})
			}
			return result, nil
		})
	case "trees.create":
		return decode(raw, func(p protocol.CreateTreeParams) (any, error) {
			patch, err := p.Overrides.Domain()
			if err != nil {
				return nil, err
			}
			result, err := r.CreateRoot(ctx, session.TreeCreationRequest{
				ID: session.CreationID(p.CreationID), PermissionMode: p.DomainPermissionMode(),
				Metadata: session.TreeMetadata(p.Metadata), Engine: session.Engine(p.Engine),
				Resources: protocol.ResourceLimitsDomain(p.Resources), Definition: definitionRef(p.Definition),
				Overrides: patch, WorkingDirectory: p.WorkingDirectory,
			})
			if err != nil {
				return nil, err
			}
			return protocol.CreationFromDomain(result)
		})
	case "trees.creation":
		return decode(raw, func(p protocol.TreeCreationParams) (any, error) {
			result, err := r.TreeCreation(ctx, session.CreationID(p.CreationID))
			if err != nil {
				return nil, err
			}
			return protocol.CreationFromDomain(result)
		})
	case "trees.catalog":
		return decode(raw, func(_ protocol.EmptyParams) (any, error) {
			revision, err := r.TreeCatalog(ctx)
			return protocol.TreeCatalog{Revision: protocol.Counter(revision)}, err
		})
	case "trees.summaries":
		return decode(raw, func(p protocol.TreeSummariesParams) (any, error) {
			roots := make([]session.SessionID, len(p.RootIDs))
			for i, id := range p.RootIDs {
				roots[i] = session.SessionID(id)
			}
			page, err := r.TreeSummaries(ctx, roots)
			if err != nil {
				return nil, err
			}
			return protocol.TreeSummariesFromDomain(page), nil
		})
	case "trees.recent":
		return decode(raw, func(p protocol.RecentTreesParams) (any, error) {
			page, err := r.RecentTrees(ctx, p.Limit)
			return protocol.RecentTreesFromDomain(page), err
		})
	case "trees.list":
		return listTrees(ctx, r, raw)
	case "definitions.list":
		return listDefinitions(ctx, r, raw)
	case "trees.get":
		return decode(raw, func(p protocol.TreeParams) (any, error) {
			value, err := r.Tree(ctx, session.TreeID(p.TreeID))
			return protocol.TreeFromDomain(value), err
		})
	case "trees.title_decision":
		return decode(raw, func(p protocol.TreeParams) (any, error) {
			value, err := r.AutomaticTitleDecision(ctx, session.TreeID(p.TreeID))
			return protocol.AutomaticTitleDecisionFromDomain(value), err
		})
	case "trees.title_result":
		return decode(raw, func(p protocol.AutomaticTitleResultParams) (any, error) {
			value, err := r.AutomaticTitleResult(ctx, session.TreeID(p.TreeID), session.ModelAttemptID(p.AttemptID))
			return protocol.AutomaticTitleResultFromDomain(value), err
		})
	case "trees.update":
		return decode(raw, func(p protocol.UpdateTreeParams) (any, error) {
			value, err := r.UpdateTree(ctx, session.TreeID(p.TreeID), session.Revision(p.ExpectedRevision), session.TreeMetadata(p.Metadata))
			return protocol.TreeFromDomain(value), err
		})
	case "sessions.get":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			value, err := r.Session(ctx, session.SessionID(p.SessionID))
			if err != nil {
				return nil, err
			}
			return protocol.SessionFromDomain(value)
		})
	case "sessions.spawn":
		return decode(raw, func(p protocol.SpawnSessionParams) (any, error) {
			request, err := childRequest(p)
			if err != nil {
				return nil, err
			}
			value, err := r.SpawnChild(ctx, identity(p.Identity), request)
			if err != nil {
				return nil, err
			}
			result := protocol.SpawnSessionResult{Admission: admission(value.Admission)}
			if value.Session != nil {
				wire, err := protocol.SessionFromDomain(*value.Session)
				if err != nil {
					return nil, err
				}
				result.Session = &wire
			}
			return result, nil
		})
	case "sessions.list":
		return decode(raw, func(p protocol.ListSessionsParams) (any, error) {
			var after session.SessionID
			if p.After != nil {
				after = session.SessionID(*p.After)
			}
			values, err := r.Sessions(ctx, session.TreeID(p.TreeID), after, p.Limit)
			if err != nil {
				return nil, err
			}
			result := protocol.ListSessionsResult{Items: []protocol.Session{}}
			for _, value := range values {
				wire, err := protocol.SessionFromDomain(value)
				if err != nil {
					return nil, err
				}
				result.Items = append(result.Items, wire)
			}
			return result, nil
		})
	case "sessions.configure":
		return decode(raw, func(p protocol.UpdateConfigurationParams) (any, error) {
			patch, err := p.Patch.Domain()
			if err != nil {
				return nil, err
			}
			value, err := r.UpdateConfiguration(ctx, session.SessionID(p.SessionID), session.Revision(p.ExpectedRevision), patch)
			if err != nil {
				return nil, err
			}
			return protocol.SessionFromDomain(value)
		})
	case "sessions.submit":
		return decode(raw, func(p protocol.SubmitParams) (any, error) {
			value, err := r.Admit(ctx, identity(p.Identity), submissionRequest(p))
			return admission(value), err
		})
	case "sessions.history_page":
		return decode(raw, func(p protocol.HistoryPageParams) (any, error) {
			request := session.HistoryPageRequest{SessionID: session.SessionID(p.SessionID), Direction: p.Direction, Limit: p.Limit, ExpectedRevision: expectedHistoryRevision(p.ExpectedRevision)}
			if p.Cursor != nil {
				request.Cursor = new(int64(*p.Cursor))
			}
			page, err := r.TranscriptPage(ctx, request)
			if err != nil {
				return nil, err
			}
			result := protocol.HistoryPageResult{Snapshot: protocol.HistorySnapshotFromDomain(page.Snapshot), Messages: []protocol.Message{}}
			for _, message := range page.Messages {
				result.Messages = append(result.Messages, protocol.MessageFromDomain(message))
			}
			if page.NextCursor != nil {
				result.NextCursor = new(protocol.Counter(*page.NextCursor))
			}
			return result, nil
		})
	case "sessions.history":
		return decode(raw, func(p protocol.HistoryParams) (any, error) {
			snapshot, values, err := r.HistoryPage(ctx, session.SessionID(p.SessionID), int64(p.After), p.Limit, expectedHistoryRevision(p.ExpectedRevision))
			if err != nil {
				return nil, err
			}
			result := protocol.HistoryResult{Snapshot: protocol.HistorySnapshotFromDomain(snapshot), Items: []protocol.Message{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.MessageFromDomain(value))
			}
			return result, nil
		})
	case "sessions.rewind":
		return decode(raw, func(p protocol.RewindParams) (any, error) {
			value, err := r.Rewind(ctx, session.RewindRequest{
				ID: session.HistoryEditID(p.EditID), SessionID: session.SessionID(p.SessionID),
				ExpectedRevision: session.Revision(p.ExpectedRevision), ObservedThrough: int64(p.ObservedThrough),
				KeepThrough: int64(p.KeepThrough),
			})
			return protocol.HistoryEditFromDomain(value), err
		})
	case "sessions.fork":
		return decode(raw, func(p protocol.ForkParams) (any, error) {
			value, err := r.Fork(ctx, session.ForkRequest{
				ID: session.ForkID(p.ForkID), SessionID: session.SessionID(p.SessionID),
				ExpectedHistoryRevision: session.Revision(p.ExpectedHistoryRevision), ExpectedConfigRevision: session.Revision(p.ExpectedConfigRevision),
				ObservedThrough: int64(p.ObservedThrough), KeepThrough: int64(p.KeepThrough), Title: p.Title,
			})
			if err != nil {
				return nil, err
			}
			return protocol.ForkResultFromDomain(value)
		})
	case "sessions.lifecycle":
		return decode(raw, func(p protocol.LifecycleParams) (any, error) {
			value, err := r.SetLifecycle(ctx, session.SessionID(p.SessionID), session.Lifecycle(p.Lifecycle))
			if err != nil {
				return nil, err
			}
			return protocol.SessionFromDomain(value)
		})
	case "sessions.delete":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			err := r.DeleteSubtree(ctx, session.SessionID(p.SessionID))
			return protocol.DeleteResult{Deleted: err == nil}, err
		})
	case "turns.get":
		return decode(raw, func(p protocol.TurnParams) (any, error) {
			value, err := r.Turn(ctx, session.TurnID(p.TurnID))
			return protocol.TurnFromDomain(value), err
		})
	case "sessions.turns":
		return decode(raw, func(p protocol.TurnPageParams) (any, error) {
			var before session.TurnID
			if p.Before != nil {
				before = session.TurnID(*p.Before)
			}
			value, err := r.TurnPage(ctx, session.SessionID(p.SessionID), before, p.Limit)
			return protocol.TurnPageFromDomain(value), err
		})
	case "turns.attempts":
		return decode(raw, func(p protocol.ModelAttemptsParams) (any, error) {
			var after session.ModelAttemptID
			if p.After != nil {
				after = session.ModelAttemptID(*p.After)
			}
			values, err := r.ModelAttempts(ctx, session.TurnID(p.TurnID), after, p.Limit)
			if err != nil {
				return nil, err
			}
			result := protocol.ModelAttemptsResult{Items: []protocol.ModelAttempt{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.ModelAttemptFromDomain(value))
			}
			return result, nil
		})
	case "turns.cancel":
		return decode(raw, func(p protocol.TurnParams) (any, error) {
			value, err := r.CancelTurn(ctx, session.TurnID(p.TurnID))
			return protocol.TurnFromDomain(value), err
		})
	case "inputs.cancel":
		return decode(raw, func(p protocol.InputParams) (any, error) {
			value, err := r.CancelInput(ctx, session.InputID(p.InputID))
			return protocol.InputFromDomain(value), err
		})
	case "receipts.get":
		return decode(raw, func(p protocol.RequestIdentity) (any, error) {
			value, err := r.Admission(ctx, identity(p))
			return admission(value), err
		})
	case "definitions.register":
		return decode(raw, func(p protocol.DefinitionDocument) (any, error) {
			document, err := p.Domain()
			if err != nil {
				return nil, err
			}
			value, err := r.RegisterDefinition(ctx, document)
			if err != nil {
				return nil, err
			}
			return protocol.DefinitionFromDomain(value)
		})
	case "definitions.get":
		return decode(raw, func(p protocol.DefinitionRef) (any, error) {
			value, err := r.Definition(ctx, definitionRef(p))
			if err != nil {
				return nil, err
			}
			return protocol.DefinitionFromDomain(value)
		})
	default:
		return nil, ErrMethod
	}
}

func definitionRef(p protocol.DefinitionRef) session.DefinitionRef {
	return session.DefinitionRef{ID: string(p.ID), Revision: p.Revision}
}

func identity(p protocol.RequestIdentity) session.RequestIdentity {
	return session.RequestIdentity{ClientID: string(p.ClientID), RequestID: string(p.RequestID)}
}

func admission(value store.Admission) protocol.Admission {
	result := protocol.Admission{Receipt: protocol.ReceiptFromDomain(value.Receipt)}
	if value.Input != nil {
		wire := protocol.InputFromDomain(*value.Input)
		result.Input = &wire
	}
	if value.Turn != nil {
		wire := protocol.TurnFromDomain(*value.Turn)
		result.Turn = &wire
	}
	return result
}

func wireError(err error) *protocol.RPCError {
	kinds := []struct {
		err  error
		code int
		kind string
	}{
		{hostview.ErrUnavailable, -32036, "HOST_UNAVAILABLE"},
		{computer.ErrBundledUnavailable, -32036, "HOST_UNAVAILABLE"},
		{hostview.ErrPickerLimit, -32011, "LIMIT"},
		{hostview.ErrPickerClosed, -32013, "CLOSED"},
		{shell.ErrNotFound, -32004, "NOT_FOUND"},
		{shell.ErrInputConflict, -32009, "CONFLICT"},
		{shell.ErrLimit, -32011, "LIMIT"},
		{shell.ErrClosed, -32013, "CLOSED"},
		{providerhost.ErrInvalid, -32602, "INVALID"},
		{providerhost.ErrMissing, -32004, "NOT_FOUND"},
		{providerhost.ErrExists, -32009, "CONFLICT"},
		{providerhost.ErrBusy, -32010, "BUSY"},
		{providerhost.ErrClosed, -32013, "CLOSED"},
		{providerhost.ErrStale, -32009, "CONFLICT"},
		{providerhost.ErrCredentials, -32030, "PROVIDER_CREDENTIALS"},
		{providerhost.ErrDiscovery, -32031, "PROVIDER_DISCOVERY"},
		{providerhost.ErrStorage, -32032, "PROVIDER_CONFIGURATION"},
		{config.ErrRevisionConflict, -32009, "CONFLICT"},
		{config.ErrKeyConflict, -32009, "CONFLICT"},
		{config.ErrKeyStoragePending, -32033, "PROVIDER_KEY_PENDING"},
		{config.ErrKeyStorage, -32034, "PROVIDER_KEY_STORAGE"},
		{session.ErrInvalid, -32602, "INVALID"},
		{account.ErrInvalid, -32602, "INVALID"},
		{account.ErrNotFound, -32004, "NOT_FOUND"},
		{account.ErrLimit, -32011, "LIMIT"},
		{account.ErrClosed, -32013, "CLOSED"},
		{account.ErrCredentials, -32020, "ACCOUNT_CREDENTIALS"},
		{account.ErrSetupRequired, -32021, "ACCOUNT_SETUP"},
		{account.ErrConfiguration, -32022, "ACCOUNT_CONFIGURATION"},
		{account.ErrLogout, -32023, "ACCOUNT_LOGOUT"},
		{inferenceaccount.ErrInvalid, -32602, "INVALID"},
		{inferenceaccount.ErrNotFound, -32004, "NOT_FOUND"},
		{inferenceaccount.ErrBusy, -32010, "BUSY"},
		{inferenceaccount.ErrLimit, -32011, "LIMIT"},
		{inferenceaccount.ErrClosed, -32013, "CLOSED"},
		{inferenceaccount.ErrCredentials, -32020, "ACCOUNT_CREDENTIALS"},
		{inferenceaccount.ErrSetup, -32021, "ACCOUNT_SETUP"},
		{inferenceaccount.ErrManagement, -32024, "ACCOUNT_MANAGEMENT"},
		{store.ErrTransferFailed, -32037, "TRANSFER_FAILED"},
		{store.ErrTransferUncertain, -32038, "TRANSFER_UNCERTAIN"},
		{store.ErrTransferInterrupted, -32039, "TRANSFER_INTERRUPTED"},
		{store.ErrTransferCancelled, -32040, "TRANSFER_CANCELLED"},
		{store.ErrTransferDeleted, -32041, "TRANSFER_DELETED"},
		{store.ErrNotFound, -32004, "NOT_FOUND"},
		{store.ErrConflict, -32009, "CONFLICT"},
		{runtime.ErrWorkspaceChanged, -32009, "CONFLICT"},
		{config.ErrRevisionConflict, -32009, "CONFLICT"},
		{store.ErrBusy, -32010, "BUSY"},
		{store.ErrLimit, -32011, "LIMIT"},
		{runtime.ErrWorkspaceLimit, -32011, "LIMIT"},
		{store.ErrStopped, -32012, "STOPPED"},
		{runtime.ErrClosed, -32013, "CLOSED"},
		{ErrIdentity, -32014, "IDENTITY"},
		{ErrNetworkRestricted, -32015, "NETWORK_RESTRICTED"},
		{terminal.ErrNotFound, -32004, "NOT_FOUND"},
		{terminal.ErrLimit, -32005, "LIMIT"},
		{terminal.ErrExited, -32012, "STOPPED"},
		{terminal.ErrClosed, -32013, "CLOSED"},
		{terminal.ErrCursor, -32009, "CONFLICT"},
		{errTerminalWriteUncertain, -32016, "TERMINAL_WRITE_UNCERTAIN"},
		{ErrMethod, -32601, "METHOD"},
		{errMCPUnavailable, -32035, "MCP_UNAVAILABLE"},
	}
	for _, kind := range kinds {
		if errors.Is(err, kind.err) {
			return &protocol.RPCError{Code: kind.code, Kind: kind.kind, Message: kind.err.Error()}
		}
	}
	return &protocol.RPCError{Code: -32603, Kind: "INTERNAL", Message: "runtime operation failed"}
}
