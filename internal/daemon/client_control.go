package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/schedule"
	sessionstore "github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

type clientActionPayload struct {
	ExpectedRevision    *int64          `json:"expected_revision,omitempty,string"`
	TurnID              string          `json:"turn_id,omitempty"`
	TargetCommandID     string          `json:"target_command_id,omitempty"`
	Title               string          `json:"title,omitempty"`
	Path                string          `json:"path,omitempty"`
	Effort              string          `json:"effort,omitempty"`
	Model               string          `json:"model,omitempty"`
	Provider            string          `json:"provider,omitempty"`
	Window              int             `json:"window,omitempty"`
	Kind                string          `json:"kind,omitempty"`
	Limit               int64           `json:"limit,string,omitempty"`
	Schedule            string          `json:"schedule,omitempty"`
	Prompt              string          `json:"prompt,omitempty"`
	ScheduleID          int             `json:"schedule_id,omitempty"`
	Name                string          `json:"name,omitempty"`
	Source              string          `json:"source,omitempty"`
	Enabled             bool            `json:"enabled,omitempty"`
	Driver              string          `json:"driver,omitempty"`
	App                 string          `json:"app,omitempty"`
	Text                string          `json:"text,omitempty"`
	Command             string          `json:"command,omitempty"`
	Cut                 int             `json:"cut,omitempty"`
	ID                  string          `json:"id,omitempty"`
	Delivery            string          `json:"delivery,omitempty"`
	Answer              []string        `json:"answer,omitempty"`
	Dismissed           bool            `json:"dismissed,omitempty"`
	Bytes               []byte          `json:"bytes,omitempty"`
	System              string          `json:"system,omitempty"`
	MaxTurns            int             `json:"max_turns,omitempty"`
	Headless            bool            `json:"headless,omitempty"`
	CacheKey            string          `json:"cache_key,omitempty"`
	Tool                string          `json:"tool,omitempty"`
	Arguments           json.RawMessage `json:"arguments,omitempty"`
	DenyPermissions     bool            `json:"deny_permissions,omitempty"`
	ExternalPermissions bool            `json:"external_permissions,omitempty"`
	PersistDefault      bool            `json:"persist_default,omitempty"`
}

type commandStart struct {
	result CommandResult
	reply  chan clientCommandReply
}

type clientCommandReply struct {
	result CommandResult
	err    error
}

type clientCommandCompletion struct {
	automatic   bool
	independent bool
	clientID    string
	commandID   string
	operation   string
	ingress     int64
	output      string
	err         error
	replacement *clientReplacement
	compact     *clientCompaction
	rewind      *clientRewind
	goal        *clientGoal
	reply       chan clientCommandReply
}

type clientReplacement struct {
	disposed       sync.Once
	installed      bool
	meta           sessionstore.Meta
	previousEffort string
	components     Components
	persistDefault bool
}

type clientCompaction struct {
	summary      string
	cutoff       int
	rawTailStart int
	rawCutoff    *int
	model        string
	usage        llm.Usage
	before       []llm.Message
}

type clientRewind struct {
	cut      int
	restored int
}

type clientGoal struct {
	text  string
	usage llm.Usage
}

func isClientOperation(operation string) bool {
	switch operation {
	case "cancel", "goal.set", "goal.run", "goal.from-context", "schedule.list", "schedule.create", "schedule.delete", "session.fork", "workspace.inspect", "workspace.set",
		"session.effort", "session.model", "session.effort.get", "session.model.get", "session.list", "session.open", "session.rename", "session.reload", "session.autotitle", "run.configure",
		"history.clear", "history.rewind", "history.compact",
		"history.compact.log", "history.compact.retry", "compaction.configure",
		"history.user.list", "session.preview", "agents.list", "agent.transcript", "agent.submit", "agent.turn.cancel", "question.answer",
		"provider.catalogs",
		"agent.control", "agent.delete", "budget.cap", "capability.revoke", "shell.run",
		"context.audit", "mcp.status", "mcp.reconnect", "mcp.enable", "mcp.disable", "mcp.import.status", "mcp.import.configure", "lsp.status", "browser.status", "browser.set_driver", "computer.status", "computer.allow", "computer.deny", "terminal.input",
		"tool.configure", "tool.schema", "tool.call", "permission.mode", "permission.rules", "permission.forget", "mcp.attach":
		return true
	default:
		return false
	}
}

type clientWorkspaceRunner interface {
	ResolveWorkingDirectory(string) (string, error)
	SetWorkingDirectory(string)
}

type clientShellRunner interface {
	RunShell(context.Context, string) (string, error)
}

type clientHistoryRunner interface {
	ReplaceHistory([]llm.Message)
}

type clientGoalRunner interface {
	FormGoal(context.Context, int) (string, llm.Usage, error)
}

type clientTerminalRunner interface {
	SendTerminalInput(string, []byte) error
}

type clientCompactRunner interface {
	CompactNow(context.Context) (clientCompaction, error)
	ReplaceHistory([]llm.Message)
}

type clientReplaceRunner interface {
	CanReplace() error
}

type clientRunRunner interface {
	ConfigureRun(system string, maxTurns int, headless bool, cacheKey string)
}

type clientToolRunner interface {
	ToolDefinitions(context.Context) ([]llm.Tool, error)
	CallTool(context.Context, string, json.RawMessage) (string, error)
	DenyToolPermissions()
}

type clientPermissionRunner interface {
	SetExternalPermissions(bool)
	ExternalPermissionsEnabled() bool
	ResolvePermission(string, capability.Decision) error
}

type clientMCPManager interface {
	Statuses() []mcp.Server
	Blocked() []mcp.Server
	Reconnect(string) bool
	Enable(string) bool
	Disable(string) bool
}

func (r *AgentSession) ResolveWorkingDirectory(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		path = r.agent.WorkingDir
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(r.agent.WorkingDir, path)
	}
	if path == "" {
		path = "."
	}
	return r.agent.Services.ResolveWorkingDirectory(path)
}

func (r *AgentSession) SetWorkingDirectory(path string) { r.agent.WorkingDir = path }

func (r *AgentSession) RunShell(ctx context.Context, command string) (string, error) {
	ctx = tools.WithWorkingDirectory(ctx, r.agent.WorkingDir)
	result, err := r.agent.Services.RunBash(ctx, command, 120*time.Second)
	return result.Output, err
}

func (r *AgentSession) ReplaceHistory(history []llm.Message) {
	r.mu.Lock()
	r.turn = turnJournal{}
	r.mu.Unlock()
	r.agent.ReplaceHistory(history)
}

func (r *AgentSession) ConfigureRun(system string, maxTurns int, headless bool, cacheKey string) {
	r.mu.Lock()
	r.promptOverride = system
	r.mu.Unlock()
	r.agent.MaxTurns = maxTurns
	r.agent.Services.SetHeadlessPermissions(headless)
	if headless {
		r.agent.Services.SetExternalPermissions(false)
	}
	// bind keyed the prompt cache by session id; a stable caller-chosen key
	// (`whip run -cache-key repo/reviewer`) shares the cached prefix across
	// runs. Children keep caching under the root session id.
	if cacheKey != "" {
		r.agent.SetSessionID(cacheKey)
	}
}

func (r *AgentSession) ToolDefinitions(ctx context.Context) ([]llm.Tool, error) {
	return r.agent.Services.ToolDefinitions(ctx)
}

func (r *AgentSession) CallTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	return r.agent.Services.CallTool(ctx, name, arguments)
}

func (r *AgentSession) DenyToolPermissions() {
	r.agent.Services.SetExternalPermissions(false)
	r.agent.Services.SetGate(func(context.Context, tools.GateRequest) (tools.GateDecision, string) {
		return tools.GateReject, "this automation client cannot approve side effects"
	})
}

func (r *AgentSession) SetExternalPermissions(enabled bool) {
	r.agent.Services.SetExternalPermissions(enabled)
	r.agent.Services.SetMCPAutomatic(!enabled)
	r.agent.Services.SetHeadlessPermissions(false)
}

func (r *AgentSession) ExternalPermissionsEnabled() bool {
	return r.agent.Services.ExternalPermissionsEnabled()
}

func (r *AgentSession) ResolvePermission(permissionID string, decision capability.Decision) error {
	return r.agent.Services.ResolvePermission(permissionID, decision)
}

func (r *AgentSession) FormGoal(ctx context.Context, window int) (string, llm.Usage, error) {
	tail, err := agent.GoalFromContextMessages(r.agent.MessagesSnapshot(), window)
	if err != nil {
		return "", r.agent.Usage(), err
	}
	goal, _, err := r.complete(ctx, agent.BuildGoalFromContextPrompt(tail), 8192)
	goal = strings.TrimSpace(goal)
	if err == nil && goal == "" {
		err = errors.New("model returned an empty goal")
	}
	return goal, r.agent.Usage(), err
}

func (r *AgentSession) CompactNow(ctx context.Context) (clientCompaction, error) {
	before := r.agent.MessagesSnapshot()
	summary, cutoff, info, err := r.agent.CompactNow(ctx)
	rawCutoff := agent.RawCompactionCutoff(before, cutoff)
	return clientCompaction{
		summary: summary, cutoff: cutoff, rawTailStart: agent.CompactionRawTailStart(before, cutoff), rawCutoff: &rawCutoff,
		model: info.Model, usage: r.agent.Usage(), before: before,
	}, err
}

func (r *AgentSession) CanReplace() error {
	if r.agent.TurnRunning() {
		return errors.New("a turn is running")
	}
	return nil
}

func (r *AgentSession) browserManager() *browser.Manager {
	if r.agent.Services == nil {
		return nil
	}
	return r.agent.Services.Browser()
}

func (r *AgentSession) lspManager() *lsp.Manager {
	if r.agent.Services == nil {
		return nil
	}
	manager, _ := r.agent.Services.Diagnostics().(*lsp.Manager)
	return manager
}

func (r *AgentSession) computerPolicy() *computer.Policy {
	if r.agent.Services == nil {
		return nil
	}
	return r.agent.Services.ComputerPolicy()
}

// ClientCommand admits a non-turn user action on the root actor. The durable
// command row is the idempotency boundary; matching retries never re-execute
// the action and instead return its authoritative terminal outcome.
// Cancellation stops waiting; an admitted command may still finish. Retry
// the same command ID to observe its durable outcome.
func (s *Session) ClientCommand(ctx context.Context, admission sessionstore.CommandAdmission, operation string, payload json.RawMessage) (CommandResult, error) {
	return s.clientCommand(ctx, admission, operation, payload, true)
}

// AcceptClientCommand returns once the actor has admitted the command and
// launched any supervised work. It never waits for a provider or shell result.
func (s *Session) AcceptClientCommand(ctx context.Context, admission sessionstore.CommandAdmission, operation string, payload json.RawMessage) (CommandResult, error) {
	return s.clientCommand(ctx, admission, operation, payload, false)
}

func (s *Session) clientCommand(ctx context.Context, admission sessionstore.CommandAdmission, operation string, payload json.RawMessage, wait bool) (CommandResult, error) {
	if s.meta.Kind == sessionstore.SessionKindToolHost && !isToolHostOperation(operation) {
		return CommandResult{}, fmt.Errorf("tool-host sessions do not support %q", operation)
	}
	admission.Scope = sessionstore.CommandScopeRoot
	admission.RootID = s.meta.ID
	admission.AgentID = s.authority.AgentID
	admission.Kind = operation
	admission.Payload.Data = slices.Clone(admission.Payload.Data)
	payload = slices.Clone(payload)

	started, err := routeControlValue(s, ctx, func(actorCtx context.Context) (commandStart, error) {
		var result CommandResult
		var asyncReply chan clientCommandReply
		finish := func(err error) (commandStart, error) {
			return commandStart{result: result, reply: asyncReply}, err
		}
		admitted, admitErr := s.store.AdmitControlCommand(actorCtx, admission)
		if admitErr != nil {
			return finish(admitErr)
		}
		result = CommandResult{
			CommandID: admitted.Command.CommandID, IngressSeq: admitted.Command.IngressSeq,
			Status: admitted.Command.Status, Operation: operation,
		}
		if !admitted.New {
			if admitted.Command.Status == "queued" || admitted.Command.Status == "running" || admitted.Command.Status == "waiting" {
				return finish(nil)
			}
			body, resolveErr := s.store.ResolveRuntimeValue(actorCtx, s.meta.ID, admitted.Command.Outcome)
			if resolveErr != nil {
				return finish(resolveErr)
			}
			result.Result = body
			result.Output, result.Error = decodeCommandPresentation(operation, body, admitted.Command.Status)
			return finish(nil)
		}
		completionReply := make(chan clientCommandReply, 1)
		s.supervisor.post(workerEnvelope{kind: workerControl, controlCtx: s.supervisor.ctx, reply: make(chan error, 1), control: func(executionCtx context.Context) error {
			execution, err := s.executeClientCommand(executionCtx, admission, operation, payload, admitted, completionReply)
			if execution.reply == nil {
				completionReply <- clientCommandReply{result: execution.result, err: err}
			}
			if err != nil {
				s.supervisor.report("accepted client command", err)
			}
			return nil
		}})
		return commandStart{result: result, reply: completionReply}, nil
	})
	if err != nil || started.reply == nil || !wait {
		return started.result, err
	}
	select {
	case reply := <-started.reply:
		return reply.result, reply.err
	case <-ctx.Done():
		return started.result, ctx.Err()
	case <-s.supervisor.ctx.Done():
		return started.result, ErrStopped
	}
}

func (s *Session) executeClientCommand(actorCtx context.Context, admission sessionstore.CommandAdmission, operation string, payload json.RawMessage, admitted sessionstore.CommandAdmissionResult, completionReply chan clientCommandReply) (commandStart, error) {
	result := CommandResult{CommandID: admission.CommandID, IngressSeq: admitted.Command.IngressSeq, Status: "running", Operation: operation}
	var asyncReply chan clientCommandReply
	finish := func(err error) (commandStart, error) { return commandStart{result: result, reply: asyncReply}, err }
	if err := s.store.SetCommandState(actorCtx, admission.ClientID, admission.CommandID, "running"); err != nil {
		return finish(err)
	}
	if operation == "cancel" {
		var action clientActionPayload
		if err := decodeClientAction(payload, &action); err != nil {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", err, &result))
		}
		if action.TargetCommandID != "" {
			var cancelErr error
			if action.TurnID != "" {
				cancelErr = rpcFailure(-32602, "provide one cancellation target")
			} else {
				cancelErr = s.cancelInputCommand(actorCtx, admission.ClientID, action.TargetCommandID)
			}
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", cancelErr, &result))
		}
	}
	if operation == "permission.mode" && (s.hasRunningAgent() || s.clientBusy) {
		return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("permission mode cannot change while an agent is running"), &result))
	}
	if operation == "session.model" || operation == "session.reload" || operation == "compaction.configure" {
		var action clientActionPayload
		if err := decodeClientAction(payload, &action); err != nil {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", err, &result))
		}
		if operation == "session.reload" && (s.hasRunningAgent() || s.clientBusy) {
			s.reloadPending = true
			output, _ := marshalClientOutput(protocol.ModelResult{Model: s.meta.Model, Provider: s.meta.Provider, ReloadPending: true}, nil)
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, output, nil, &result))
		}
		if s.hasRunningAgent() || s.clientBusy || s.clientIntegrations > 0 {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("model cannot change while an agent or client operation is running"), &result))
		}
		model, provider := action.Model, action.Provider
		if operation == "session.reload" || operation == "compaction.configure" {
			model, provider = s.meta.Model, s.meta.Provider
		}
		if provider == "" {
			provider = s.meta.Provider
		}
		if model == "" {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("model is required"), &result))
		}
		if operation == "session.model" && model == s.meta.Model && provider == s.meta.Provider {
			var err error
			if action.PersistDefault {
				_, _, err = config.UpdateVersioned("", func(cfg *config.Config) error { cfg.DefaultModel, cfg.DefaultProvider = model, provider; return nil })
			}
			output, _ := marshalClientOutput(protocol.ModelResult{Model: model, Provider: provider}, nil)
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, output, err, &result))
		}
		if s.factory == nil {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner cannot be rebuilt"), &result))
		}
		if replaceable, ok := s.runner.(clientReplaceRunner); ok {
			if err := replaceable.CanReplace(); err != nil {
				return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", fmt.Errorf("model cannot change while %w", err), &result))
			}
		}
		factory, rootID := s.factory, s.meta.ID
		s.clientBusy = true
		s.clientPreparing = true
		asyncReply = completionReply
		if !s.supervisor.launchWorker("prepare replacement runtime", func() {
			var configOutput string
			var err error
			if operation == "compaction.configure" {
				cfg, _, patchErr := config.UpdateVersioned("", func(cfg *config.Config) error {
					cfg.CompactModel, cfg.CompactProvider = action.Model, action.Provider
					if action.Model == "" {
						cfg.CompactProvider = ""
						return nil
					}
					_, _, _, err := cfg.Resolve(cfg.CompactModel, cfg.CompactProvider)
					return err
				})
				err = patchErr
				if err == nil {
					configOutput, _ = marshalClientOutput(protocol.CompactionSettingsResult{Model: cfg.CompactModel, Provider: cfg.CompactProvider, BuiltinDefault: cfg.CompactModel == ""}, nil)
				}
			}
			var replacement *clientReplacement
			if err == nil {
				replacement, err = s.prepareReplacement(s.supervisor.ctx, factory, rootID, model, provider)
			}
			if replacement != nil {
				replacement.persistDefault = action.PersistDefault
			}
			s.supervisor.post(workerEnvelope{kind: workerClientCommand, client: &clientCommandCompletion{clientID: admission.ClientID, commandID: admission.CommandID, operation: operation, ingress: admitted.Command.IngressSeq, replacement: replacement, output: configOutput, err: err, reply: completionReply}})
		}) {
			s.clientBusy = false
			s.clientPreparing = false
			asyncReply = nil
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", ErrStopped, &result))
		}
		return finish(nil)
	}
	if operation == "mcp.reconnect" || operation == "mcp.enable" || operation == "mcp.disable" || operation == "browser.set_driver" {
		if s.clientPreparing {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("runtime replacement is preparing"), &result))
		}
		var action clientActionPayload
		if err := decodeClientAction(payload, &action); err != nil {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", err, &result))
		}
		var work func() (string, error)
		if operation == "browser.set_driver" {
			runner, ok := s.runner.(*AgentSession)
			if !ok || runner.browserManager() == nil {
				return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("browser automation is unavailable"), &result))
			}
			manager := runner.browserManager()
			work = func() (string, error) { return setBrowserDriver(manager, action.Driver) }
		} else {
			manager, ok := s.currentMCP().(clientMCPManager)
			if !ok {
				return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("MCP integration is unavailable"), &result))
			}
			work = func() (string, error) { return applyMCPAction(manager, operation, action.Name) }
		}
		s.clientIntegrations++
		asyncReply = completionReply
		if !s.supervisor.launchWorker("client integration", func() {
			output, err := work()
			s.supervisor.post(workerEnvelope{kind: workerClientCommand, client: &clientCommandCompletion{clientID: admission.ClientID, commandID: admission.CommandID, operation: operation, ingress: admitted.Command.IngressSeq, output: output, err: err, reply: completionReply, independent: true}})
		}) {
			s.clientIntegrations--
			asyncReply = nil
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", ErrStopped, &result))
		}
		return finish(nil)
	}
	if operation == "tool.call" {
		if s.clientBusy || s.running != nil {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("another root operation is already running"), &result))
		}
		var action clientActionPayload
		if err := decodeClientAction(payload, &action); err != nil || action.Tool == "" {
			if err == nil {
				err = errors.New("tool name is required")
			}
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", err, &result))
		}
		runner, ok := s.runner.(clientToolRunner)
		if !ok {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner does not support tool calls"), &result))
		}
		asyncReply = completionReply
		s.clientBusy = true
		result.Status = "running"
		launched := s.supervisor.launchWorker("client tool call", func() {
			output, callErr := runner.CallTool(s.supervisor.ctx, action.Tool, action.Arguments)
			s.supervisor.post(workerEnvelope{kind: workerClientCommand, client: &clientCommandCompletion{
				clientID: admission.ClientID, commandID: admission.CommandID, operation: operation,
				ingress: admitted.Command.IngressSeq, output: output, err: callErr, reply: asyncReply,
			}})
		})
		if !launched {
			s.clientBusy = false
			asyncReply = nil
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", ErrStopped, &result))
		}
		return finish(nil)
	}
	if operation == "shell.run" {
		if s.clientBusy || s.running != nil {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("another root operation is already running"), &result))
		}
		var action clientActionPayload
		if err := decodeClientAction(payload, &action); err != nil || strings.TrimSpace(action.Command) == "" {
			if err == nil {
				err = errors.New("shell command is required")
			}
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", err, &result))
		}
		runner, ok := s.runner.(clientShellRunner)
		if !ok {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner does not support shell commands"), &result))
		}
		asyncReply = completionReply
		s.clientBusy = true
		result.Status = "running"
		launched := s.supervisor.launchWorker("client shell", func() {
			output, runErr := runner.RunShell(s.supervisor.ctx, action.Command)
			s.supervisor.post(workerEnvelope{kind: workerClientCommand, client: &clientCommandCompletion{
				clientID: admission.ClientID, commandID: admission.CommandID, operation: operation,
				ingress: admitted.Command.IngressSeq, output: output, err: runErr, reply: asyncReply,
			}})
		})
		if !launched {
			s.clientBusy = false
			asyncReply = nil
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", ErrStopped, &result))
		}
		return finish(nil)
	}
	if operation == "history.compact" {
		if s.clientBusy || s.running != nil {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("history cannot compact while a root operation is running"), &result))
		}
		runner, ok := s.runner.(clientCompactRunner)
		if !ok {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner does not support compaction"), &result))
		}
		asyncReply = completionReply
		s.clientBusy = true
		result.Status = "running"
		launched := s.supervisor.launchWorker("client compaction", func() {
			compaction, compactErr := runner.CompactNow(s.supervisor.ctx)
			s.supervisor.post(workerEnvelope{kind: workerClientCommand, client: &clientCommandCompletion{
				clientID: admission.ClientID, commandID: admission.CommandID, operation: operation,
				ingress: admitted.Command.IngressSeq, compact: &compaction, err: compactErr, reply: asyncReply,
			}})
		})
		if !launched {
			s.clientBusy = false
			asyncReply = nil
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", ErrStopped, &result))
		}
		return finish(nil)
	}
	if operation == "goal.from-context" {
		if s.clientBusy || s.running != nil {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("a goal cannot be formulated while a root operation is running"), &result))
		}
		var action clientActionPayload
		if err := decodeClientAction(payload, &action); err != nil {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("invalid goal context payload"), &result))
		}
		window := action.Window
		if window == 0 {
			window = agent.GoalFromContextDefaultWindow
		}
		if window < 2 {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("goal context window must be at least 2"), &result))
		}

		runner, ok := s.runner.(clientGoalRunner)
		if !ok {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner does not support goal formulation"), &result))
		}
		asyncReply = completionReply
		s.clientBusy = true
		result.Status = "running"
		launched := s.supervisor.launchWorker("client goal formulation", func() {
			goal, usage, goalErr := runner.FormGoal(s.supervisor.ctx, window)
			s.supervisor.post(workerEnvelope{kind: workerClientCommand, client: &clientCommandCompletion{
				clientID: admission.ClientID, commandID: admission.CommandID, operation: operation,
				ingress: admitted.Command.IngressSeq, goal: &clientGoal{text: goal, usage: usage},
				err: goalErr, reply: asyncReply,
			}})
		})
		if !launched {
			s.clientBusy = false
			asyncReply = nil
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", ErrStopped, &result))
		}
		return finish(nil)
	}
	if operation == "history.rewind" {
		if s.clientBusy || s.running != nil {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("history cannot rewind while a root operation is running"), &result))
		}
		var action clientActionPayload
		if err := decodeClientAction(payload, &action); err != nil {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("invalid rewind payload"), &result))
		}
		cut := action.Cut
		if action.ExpectedRevision != nil {
			if err := s.store.CheckHistoryRevision(actorCtx, s.meta.ID, *action.ExpectedRevision); err != nil {
				return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", err, &result))
			}
		}
		if cut < 1 {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("rewind requires a positive conversation index"), &result))
		}
		if _, ok := s.runner.(clientHistoryRunner); !ok {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner does not support history replacement"), &result))
		}
		snapshots, err := s.store.WorkspaceSnapshotsFrom(actorCtx, s.meta.ID, cut)
		if err != nil {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", err, &result))
		}
		workspace, canRestore := s.runner.(workspaceSnapshotRunner)
		if len(snapshots) > 0 && !canRestore {
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner cannot restore workspace snapshots"), &result))
		}
		asyncReply = completionReply
		s.clientBusy = true
		result.Status = "running"
		launched := s.supervisor.launchWorker("client rewind", func() {
			restored := 0
			var rewindErr error
			if len(snapshots) > 0 {
				restored, rewindErr = workspace.RestoreWorkspace(s.supervisor.ctx, snapshots[0].Ref)
				if rewindErr == nil {
					for _, snapshot := range snapshots[1:] {
						workspace.DropWorkspaceSnapshot(s.supervisor.ctx, snapshot.Ref)
					}
				}
			}
			s.supervisor.post(workerEnvelope{kind: workerClientCommand, client: &clientCommandCompletion{
				clientID: admission.ClientID, commandID: admission.CommandID, operation: operation,
				ingress: admitted.Command.IngressSeq, rewind: &clientRewind{cut: cut, restored: restored},
				err: rewindErr, reply: asyncReply,
			}})
		})
		if !launched {
			s.clientBusy = false
			asyncReply = nil
			return finish(s.finishClientCommandInline(actorCtx, admission, operation, "", ErrStopped, &result))
		}
		return finish(nil)
	}

	output, actionErr := s.applyClientCommand(actorCtx, operation, payload)
	return finish(s.finishClientCommandInline(actorCtx, admission, operation, output, actionErr, &result))
}

func isToolHostOperation(operation string) bool {
	switch operation {
	case "tool.configure", "tool.schema", "tool.call", "permission.mode":
		return true
	default:
		return false
	}
}

func (s *Session) finishClientCommandInline(ctx context.Context, admission sessionstore.CommandAdmission, operation, output string, actionErr error, result *CommandResult) error {
	status := "succeeded"
	if actionErr != nil {
		status = "failed"
		output = actionErr.Error()
	}
	body := encodeCommandOutcome(operation, output, actionErr)
	_, finishErr := s.store.FinishCommand(context.WithoutCancel(ctx), admission.ClientID, admission.CommandID, status, sessionstore.RuntimePayload{
		Data: body, MediaType: "application/json", Source: operation + " outcome",
	})
	if finishErr != nil {
		return errors.Join(actionErr, finishErr)
	}
	result.Status, result.Operation, result.Result = status, operation, body
	result.Output, result.Error = decodeCommandPresentation(operation, body, status)

	return nil
}

func (s *Session) completeClientCommand(completion *clientCommandCompletion) (CommandResult, error) {
	if completion == nil || !s.clientBusy && !completion.independent {
		return CommandResult{}, errors.New("client command completion has no running operation")
	}
	defer s.startPendingReload()
	if completion.independent {
		s.clientIntegrations--
		result := CommandResult{CommandID: completion.commandID, IngressSeq: completion.ingress}
		err := s.finishClientCommandInline(s.supervisor.ctx, sessionstore.CommandAdmission{ClientID: completion.clientID, CommandID: completion.commandID}, completion.operation, completion.output, completion.err, &result)
		return result, err
	}
	s.clientBusy = false
	s.clientPreparing = false
	if completion.replacement != nil && completion.err == nil {
		replacement := completion.replacement
		_, completion.err = s.installReplacement(s.supervisor.ctx, replacement)
		if completion.err == nil && replacement.persistDefault {
			_, _, completion.err = config.UpdateVersioned("", func(cfg *config.Config) error {
				cfg.DefaultModel, cfg.DefaultProvider = replacement.meta.Model, replacement.meta.Provider
				return nil
			})
		}
		if completion.output == "" {
			completion.output, _ = marshalClientOutput(protocol.ModelResult{Model: s.meta.Model, Provider: s.meta.Provider, ReloadPending: s.reloadPending}, nil)
		}
	}

	if completion.automatic {
		if completion.err != nil {
			payload, _ := json.Marshal(sessionstore.LifecycleEvent{Error: completion.err.Error()})
			_, err := s.store.AppendRootEvent(s.supervisor.ctx, s.meta.ID, "session.reload.failed", sessionstore.RuntimePayload{Data: payload, MediaType: "application/json", Source: "session reload failure"})
			return CommandResult{}, err
		}
		return CommandResult{}, nil
	}
	if completion.compact != nil && completion.err == nil {
		compaction := completion.compact
		var rawCutoff int
		var err error
		if compaction.rawCutoff != nil {
			rawCutoff = *compaction.rawCutoff
			err = s.store.RecordRawCompaction(s.supervisor.ctx, s.meta.ID, s.meta.ID, rawCutoff, compaction.summary)
		} else {
			rawCutoff = s.rawCompactionCutoff(compaction.cutoff, compaction.rawTailStart)
			err = s.store.RecordCompaction(s.meta.ID, rawCutoff, compaction.summary)
		}
		if err != nil {
			if runner, ok := s.runner.(clientCompactRunner); ok {
				history := compaction.before
				if len(history) > 0 && history[0].Role == "system" {
					history = history[1:]
				}
				runner.ReplaceHistory(history)
			}
			completion.err = err
		} else {
			_ = s.store.SetUsage(s.meta.ID, compaction.usage.PromptTokens, compaction.usage.Cached(), compaction.usage.CompletionTokens)
			completion.output, completion.err = marshalClientOutput(protocol.CompactionResult{Cutoff: rawCutoff, Model: compaction.model, Usage: compaction.usage}, nil)
		}
	}
	if completion.rewind != nil && completion.err == nil {
		history, err := s.store.RewindHistory(s.supervisor.ctx, s.meta.ID, completion.rewind.cut)
		if err != nil {
			completion.err = err
		} else {
			s.runner.(clientHistoryRunner).ReplaceHistory(history)
			completion.output, completion.err = marshalClientOutput(protocol.RewindResult{Cut: completion.rewind.cut, RestoredFiles: completion.rewind.restored}, nil)
		}
	}
	if completion.goal != nil && completion.err == nil {
		goal := completion.goal
		if err := s.store.SetGoal(s.meta.ID, goal.text); err != nil {
			completion.err = err
		} else if _, err := s.enqueue(s.supervisor.ctx, "goal", goal.text, false); err != nil {
			completion.err = err
		} else {
			s.meta.Goal = goal.text
			s.goalRounds = 0
			completion.output = goal.text
			_ = s.store.SetUsage(s.meta.ID, goal.usage.PromptTokens, goal.usage.Cached(), goal.usage.CompletionTokens)
		}
	}
	result := CommandResult{CommandID: completion.commandID, IngressSeq: completion.ingress}
	err := s.finishClientCommandInline(s.supervisor.ctx, sessionstore.CommandAdmission{
		ClientID: completion.clientID, CommandID: completion.commandID,
	}, completion.operation, completion.output, completion.err, &result)
	return result, err
}

func (s *Session) rawCompactionCutoff(cutoff, rawTailStart int) int {
	events := s.store.Compactions(s.meta.ID)
	if len(events) == 0 {
		raw := s.store.RawMessages(s.meta.ID)
		if len(raw) > 0 && raw[0].Role != "system" {
			return cutoff - 1
		}
		return cutoff
	}
	if rawTailStart < 1 {
		rawTailStart = 2
	}
	return events[len(events)-1].Cutoff + cutoff - rawTailStart
}

func (s *Session) applyClientCommand(ctx context.Context, operation string, raw json.RawMessage) (string, error) {
	if operation == "mcp.attach" {
		var attach protocol.MCPAttachParams
		if err := json.Unmarshal(raw, &attach); err != nil {
			return "", errors.New("invalid MCP attachment payload")
		}
		if err := s.attachMCP(attach.Servers); err != nil {
			return "", err
		}
		return "configured", nil
	}
	var payload clientActionPayload
	if len(raw) > 0 {
		if err := decodeClientAction(raw, &payload); err != nil {
			return "", errors.New("invalid client action payload")
		}
	}
	switch operation {
	case "permission.mode":
		runner, ok := s.runner.(clientPermissionRunner)
		if !ok {
			return "", errors.New("session runner does not support external permissions")
		}
		runner.SetExternalPermissions(payload.ExternalPermissions)
		if runtime, ok := s.runtime.(interface{ SetExternalPermissions(bool) }); ok {
			runtime.SetExternalPermissions(payload.ExternalPermissions)
		}
		return "configured", nil
	case "tool.configure":
		runner, ok := s.runner.(clientToolRunner)
		if !ok {
			return "", errors.New("session runner does not support tool configuration")
		}
		if payload.DenyPermissions {
			runner.DenyToolPermissions()
			if runtime, ok := s.runtime.(interface{ DenyToolPermissions() }); ok {
				runtime.DenyToolPermissions()
			}
		}
		return "configured", nil
	case "tool.schema":
		runner, ok := s.runner.(clientToolRunner)
		if !ok {
			return "", errors.New("session runner does not support tool schemas")
		}
		definitions, err := runner.ToolDefinitions(ctx)
		return marshalClientOutput(definitions, err)
	case "run.configure":
		if s.hasRunningAgent() || s.clientBusy || s.clientIntegrations > 0 {
			return "", errors.New("run configuration cannot change while a root operation is running")
		}
		if payload.MaxTurns < 0 {
			return "", errors.New("max turns cannot be negative")
		}
		runner, ok := s.runner.(clientRunRunner)
		if !ok {
			return "", errors.New("session runner does not support run configuration")
		}
		runner.ConfigureRun(payload.System, payload.MaxTurns, payload.Headless, payload.CacheKey)
		if runtime, ok := s.runtime.(interface{ SetHeadlessPermissions(bool) }); ok {
			runtime.SetHeadlessPermissions(payload.Headless)
		}
		return "configured", nil
	case "cancel":
		if err := s.checkTurnTarget(ctx, s.authority.AgentID, payload.TurnID); err != nil {
			return "", err
		}
		if s.turnCancel == nil {
			return "already idle", nil
		}
		s.turnCancel()
		return "cancellation requested", nil
	case "goal.set", "goal.run":
		goal := strings.TrimSpace(payload.Text)
		if err := s.store.SetGoal(s.meta.ID, goal); err != nil {
			return "", err
		}
		s.meta.Goal = goal
		s.goalRounds = 0
		if operation == "goal.run" && goal != "" {
			if _, err := s.enqueue(ctx, "goal", goal, false); err != nil {
				return "", err
			}
		}
		return goal, nil
	case "schedule.list", "schedule.create", "schedule.delete":
		return s.clientSchedule(ctx, operation, payload)
	case "session.fork":
		if payload.ExpectedRevision != nil {
			if err := s.store.CheckHistoryRevision(ctx, s.meta.ID, *payload.ExpectedRevision); err != nil {
				return "", err
			}
		}
		cut := payload.Cut
		if cut <= 0 {
			cut = int(^uint(0) >> 1)
		}
		id, err := s.store.Fork(s.meta.ID, cut, strings.TrimSpace(payload.Title))
		return id, err
	case "workspace.inspect":
		if runner, ok := s.runner.(clientWorkspaceRunner); ok {
			return runner.ResolveWorkingDirectory(".")
		}
		return s.meta.CWD, nil
	case "workspace.set":
		if s.running != nil || s.clientBusy {
			return "", errors.New("working directory cannot change while the root agent is running")
		}
		runner, ok := s.runner.(clientWorkspaceRunner)
		if !ok {
			return "", errors.New("session runner does not support working-directory changes")
		}
		path, err := runner.ResolveWorkingDirectory(strings.TrimSpace(payload.Path))
		if err != nil {
			return "", err
		}
		if err := s.store.SetWorkingDirectory(s.meta.ID, path); err != nil {
			return "", err
		}
		runner.SetWorkingDirectory(path)
		s.meta.CWD = path
		s.emitSessionUpdate(ctx, "session.cwd.updated", SessionUpdateEvent{WorkingDir: path})
		return path, nil
	case "session.effort.get":
		return daemonEffortLabel(s.meta.Effort), nil
	case "session.effort":
		requested := strings.TrimSpace(payload.Effort)
		if requested == "" {
			return "", errors.New("effort is required; use off to disable reasoning")
		}
		if s.running != nil || s.clientBusy {
			return "", errors.New("effort cannot change while the root agent is running")
		}
		if err := validateEffort(s.meta.Model, s.meta.Provider, requested); err != nil {
			return "", err
		}
		level := requested
		if level == "off" {
			level = ""
		}
		if setter, ok := s.runner.(interface{ SetEffort(string) }); ok {
			setter.SetEffort(level)
		}
		if err := s.store.SetEffort(s.meta.ID, requested); err != nil {
			return "", err
		}
		s.meta.Effort = requested
		if payload.PersistDefault {
			if _, _, err := config.UpdateVersioned("", func(cfg *config.Config) error { cfg.DefaultEffort = requested; return nil }); err != nil {
				return "", err
			}
		}

		s.emitSessionUpdate(ctx, "session.effort.updated", SessionUpdateEvent{Effort: requested, EffortChanged: true})
		return daemonEffortLabel(requested), nil
	case "session.model.get":
		return marshalClientOutput(protocol.ModelResult{Model: s.meta.Model, Provider: s.meta.Provider}, nil)

	case "session.autotitle":
		s.autoTitle = true
		return "configured", nil
	case "session.list":
		metas, err := s.store.RecentContext(ctx, 50)
		return marshalClientOutput(metas, err)
	case "session.preview":
		if payload.ID == "" {
			return "", errors.New("session preview requires an ID")
		}
		user, assistant := s.store.LastExchange(payload.ID)
		return marshalClientOutput(SessionPreviewResult{RootID: payload.ID, User: user, Assistant: assistant}, nil)
	case "session.open":
		meta, _, err := s.store.Load(strings.TrimSpace(payload.ID))
		if err != nil {
			return "", err
		}
		return meta.ID, nil
	case "session.rename":
		title := strings.TrimSpace(payload.Title)
		if title == "" {
			return "", errors.New("session title is required")
		}
		if err := s.store.SetTitle(s.meta.ID, title); err != nil {
			return "", err
		}
		s.meta.Title = title
		s.emitSessionUpdate(ctx, "session.title.updated", SessionUpdateEvent{Title: title})
		return title, nil
	case "history.clear":
		if s.running != nil || s.clientBusy {
			return "", errors.New("history cannot change while a turn is running")
		}
		runner, ok := s.runner.(clientHistoryRunner)
		if !ok {
			return "", errors.New("session runner does not support history replacement")
		}
		snapshots, err := s.store.WorkspaceSnapshotsFrom(ctx, s.meta.ID, 1)
		if err != nil {
			return "", err
		}
		workspace, canDrop := s.runner.(workspaceSnapshotRunner)
		if len(snapshots) > 0 && !canDrop {
			return "", errors.New("session runner cannot release workspace snapshots")
		}
		history, err := s.store.RewindHistory(ctx, s.meta.ID, 1)
		if err != nil {
			return "", err
		}
		runner.ReplaceHistory(history)
		if len(snapshots) > 0 {
			s.supervisor.launchWorker("release cleared workspace snapshots", func() {
				for _, snapshot := range snapshots {
					workspace.DropWorkspaceSnapshot(s.supervisor.ctx, snapshot.Ref)
				}
			})
		}
		return "history cleared", nil
	case "history.user.list":
		history, err := s.store.UserHistory(500)
		return marshalClientOutput(history, err)
	case "provider.catalogs":
		return s.clientProviderCatalogs(ctx)
	case "history.compact.log":
		return marshalClientOutput(s.store.Compactions(s.meta.ID), nil)
	case "history.compact.retry":
		if s.running != nil || s.clientBusy {
			return "", errors.New("history cannot change while a turn is running")
		}
		events := s.store.Compactions(s.meta.ID)
		if len(events) == 0 {
			return marshalClientOutput(protocol.CompactionRetryResult{}, nil)
		}
		last := events[len(events)-1]
		if err := s.store.DeleteCompaction(s.meta.ID, last.Seq); err != nil {
			return "", err
		}
		_, history, err := s.store.Load(s.meta.ID)
		if err != nil {
			return "", err
		}
		if runner, ok := s.runner.(clientHistoryRunner); ok {
			runner.ReplaceHistory(history)
		}
		return marshalClientOutput(protocol.CompactionRetryResult{Undone: true, Sequence: last.Seq}, nil)

	case "agents.list":
		agents, err := s.store.RootAgentViews(ctx, s.meta.ID)
		return marshalClientOutput(agents, err)
	case "agent.transcript":
		return s.clientAgentTranscript(ctx, payload.ID)
	case "agent.submit":
		return s.clientAgentSubmit(ctx, payload.ID, payload.Text, payload.Delivery)
	case "agent.turn.cancel":
		if err := s.checkTurnTarget(ctx, payload.ID, payload.TurnID); err != nil {
			return "", err
		}
		return s.clientAgentTurnCancel(payload.ID)
	case "question.answer":
		return s.answerQuestion(ctx, payload.ID, payload.Answer, payload.Dismissed)
	case "agent.control":
		return s.clientAgentControl(ctx, payload.ID, "stopped")
	case "agent.delete":
		return s.clientAgentControl(ctx, payload.ID, "deleted")
	case "budget.cap":
		return s.clientBudget(ctx, payload.ID, payload.Kind, payload.Limit)
	case "capability.revoke":
		record, err := s.revokeCapability(ctx, s.authority.AgentID, strings.TrimSpace(payload.ID))
		return marshalClientOutput(record, err)
	case "permission.rules":
		rules, err := s.store.ListPermissionRules(ctx, s.meta.ID)
		if err != nil {
			return "", err
		}
		return marshalClientOutput(protocol.PermissionRulesResult{Rules: rules, Global: s.store.GlobalPermissionRules()}, nil)
	case "permission.forget":
		id := strings.TrimSpace(payload.ID)
		if err := s.store.DeletePermissionRule(ctx, s.meta.ID, id); err != nil {
			return "", err
		}
		return "forgot rule " + id, nil
	case "context.audit":
		if runner, ok := s.runner.(interface{ ContextAudit() ContextAuditResult }); ok {
			return marshalClientOutput(runner.ContextAudit(), nil)
		}
		return "", errors.New("session runner does not support context inspection")
	case "terminal.input":
		runner, ok := s.runner.(clientTerminalRunner)
		if !ok {
			return "", errors.New("session runner does not support interactive terminal input")
		}
		if payload.ID == "" || len(payload.Bytes) == 0 || len(payload.Bytes) > 4<<10 {
			return "", errors.New("terminal input requires an active terminal and at most 4 KiB")
		}
		return "input delivered", runner.SendTerminalInput(payload.ID, payload.Bytes)
	case "mcp.status", "mcp.reconnect", "mcp.enable", "mcp.disable", "mcp.import.status", "mcp.import.configure":
		return s.clientMCP(ctx, operation, payload)
	case "lsp.status":
		runner, ok := s.runner.(*AgentSession)
		if !ok || runner.lspManager() == nil {
			return "[]", nil
		}

		statuses := runner.lspManager().Statuses()
		result := make([]LSPStatusResult, 0, len(statuses))
		for _, status := range statuses {
			result = append(result, LSPStatusResult{Name: status.Name, Root: status.Root, State: status.State, Error: status.Err})
		}
		return marshalClientOutput(result, nil)
	case "browser.status", "browser.set_driver":
		return s.clientBrowser(operation, payload.Driver)
	case "computer.status", "computer.allow", "computer.deny":
		return s.clientComputer(operation, payload.App)
	default:
		return "", fmt.Errorf("unsupported root command %q", operation)
	}
}

func (s *Session) clientMCP(ctx context.Context, operation string, payload clientActionPayload) (string, error) {
	manager, ok := s.currentMCP().(clientMCPManager)
	if operation == "mcp.import.status" || operation == "mcp.import.configure" {
		return s.clientMCPImport(ctx, operation, payload.Source, payload.Enabled)
	}
	if !ok {
		if operation != "mcp.status" {
			return "", errors.New("MCP integration is unavailable")
		}
		return "[]", nil
	}
	if operation == "mcp.status" {
		statuses := append(manager.Statuses(), manager.Blocked()...)
		slices.SortFunc(statuses, func(a, b mcp.Server) int { return strings.Compare(a.Name, b.Name) })
		result := make([]MCPStatusResult, 0, len(statuses))
		for _, status := range statuses {
			result = append(result, MCPStatusResult{
				Name: status.Name, Status: status.Status.String(), Note: status.Note,
				Error: status.Err, Tools: status.Tools, Source: status.Source,
			})
		}
		return marshalClientOutput(result, nil)
	}
	return applyMCPAction(manager, operation, payload.Name)
}

func applyMCPAction(manager clientMCPManager, operation, name string) (string, error) {
	action := strings.TrimPrefix(operation, "mcp.")
	if name == "" {
		return "", errors.New("MCP server name is required")
	}
	var changed bool
	switch action {
	case "reconnect":
		changed = manager.Reconnect(name)
	case "enable":
		changed = manager.Enable(name)
	case "disable":
		changed = manager.Disable(name)
	default:
		return "", errors.New("mcp action must be reconnect, enable, or disable")
	}
	if !changed {
		return "", fmt.Errorf("no MCP server named %s", name)
	}
	return name + ": " + action, nil
}

func (s *Session) clientMCPImport(ctx context.Context, operation, sourceName string, enabled bool) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	if operation == "mcp.import.status" {
		return marshalClientOutput(protocol.MCPImportStatusResult{Claude: importState(cfg.MCPImport, "claude") == "on", Codex: importState(cfg.MCPImport, "codex") == "on"}, nil)
	}
	if sourceName != "claude" && sourceName != "codex" {
		return "", errors.New("mcp import requires claude|codex and on|off")
	}
	cfg, _, err = config.UpdateVersioned("", func(cfg *config.Config) error {
		if cfg.MCPImport == nil {
			cfg.MCPImport = &config.MCPImport{}
		}
		source := cfg.MCPImport.Claude
		if sourceName == "codex" {
			source = cfg.MCPImport.Codex
		}
		if source == nil {
			source = &config.MCPImportSource{}
			if sourceName == "claude" {
				cfg.MCPImport.Claude = source
			} else {
				cfg.MCPImport.Codex = source
			}
		}
		source.Enabled = &enabled
		return nil
	})
	if err != nil {
		return "", err
	}

	_, err = s.reloadSession(ctx)
	if err != nil {
		return "", err
	}
	return marshalClientOutput(protocol.MCPImportStatusResult{Claude: importState(cfg.MCPImport, "claude") == "on", Codex: importState(cfg.MCPImport, "codex") == "on"}, nil)
}

func importState(value *config.MCPImport, source string) string {
	if value == nil {
		return "on"
	}
	setting := value.Claude
	if source == "codex" {
		setting = value.Codex
	}
	if setting == nil || setting.Enabled == nil || *setting.Enabled {
		return "on"
	}
	return "off"
}

func (s *Session) reloadSession(context.Context) (string, error) {
	s.reloadPending = true
	s.startPendingReload()
	return "reload pending", nil
}

func (s *Session) clientBrowser(operation, driver string) (string, error) {
	runner, ok := s.runner.(*AgentSession)
	if !ok || runner.browserManager() == nil {
		if operation != "browser.status" {
			return "", errors.New("browser automation is unavailable")
		}
		return marshalClientOutput(protocol.BrowserStatusResult{}, nil)
	}
	manager := runner.browserManager()
	if operation == "browser.status" {
		return marshalClientOutput(protocol.BrowserStatusResult{Enabled: true, Driver: manager.Driver()}, nil)
	}
	return setBrowserDriver(manager, driver)
}

func setBrowserDriver(manager *browser.Manager, driver string) (string, error) {

	if driver != browser.DriverRod && driver != browser.DriverChromedp {
		return "", errors.New("browser driver must be rod or chromedp")
	}
	manager.SwitchDriver(driver)
	if manager.Driver() != driver {
		return "", fmt.Errorf("browser driver %q is pinned by WHIP_BROWSER_DRIVER", manager.Driver())
	}
	if _, _, err := config.UpdateVersioned("", func(cfg *config.Config) error { cfg.Browser.Driver = driver; return nil }); err != nil {
		return "", err
	}

	return marshalClientOutput(protocol.BrowserStatusResult{Enabled: true, Driver: manager.Driver()}, nil)
}

func (s *Session) clientComputer(operation, app string) (string, error) {
	runner, ok := s.runner.(*AgentSession)
	if !ok || runner.computerPolicy() == nil {
		if operation != "computer.status" {
			return "", errors.New("computer automation is unavailable")
		}
		return marshalClientOutput(protocol.ComputerStatusResult{}, nil)
	}
	policy := runner.computerPolicy()
	if operation == "computer.status" {
		return computerStatus(policy)
	}
	if strings.TrimSpace(app) == "" {
		return "", errors.New("computer application is required")
	}
	action := strings.TrimPrefix(operation, "computer.")
	if action == "allow" {
		policy.Approve(app)
	} else {
		policy.Deny(app)
	}
	return computerStatus(policy)
}

func computerStatus(policy *computer.Policy) (string, error) {
	state := policy.State()
	return marshalClientOutput(protocol.ComputerStatusResult{Enabled: true, DefaultDeny: state.DefaultDeny, Allowed: state.Allowed, Denied: state.Denied, SessionAllowed: state.SessionAllowed, SessionDenied: state.SessionDenied}, nil)
}

func (s *Session) prepareReplacement(ctx context.Context, factory Factory, rootID, model, provider string) (*clientReplacement, error) {
	meta, history, err := s.store.Load(rootID)
	if err != nil {
		return nil, err
	}
	meta.Model, meta.Provider = model, provider
	previousEffort := meta.Effort
	meta.Effort = compatibleEffort(model, provider, meta.Effort)
	components, err := factory(ctx, meta, history)
	if err != nil {
		(&clientReplacement{components: components}).close()
		return nil, err
	}
	return &clientReplacement{meta: meta, previousEffort: previousEffort, components: components}, nil
}

func (s *Session) installReplacement(ctx context.Context, replacement *clientReplacement) (string, error) {
	meta, previousEffort, components := replacement.meta, replacement.previousEffort, replacement.components
	model, provider := meta.Model, meta.Provider

	cleanup := replacement.close
	if components.Runner == nil {
		cleanup()
		return "", errors.New("root factory returned no replacement runner")
	}
	if current, ok := s.runner.(interface{ permissionServices() *tools.Services }); ok {
		if replacement, ok := components.Runner.(interface{ permissionServices() *tools.Services }); ok {
			replacement.permissionServices().CopyPermissionPolicyFrom(current.permissionServices())
		}
	}
	if binder, ok := components.Runner.(interface{ bind(*Session) error }); ok {
		if err := binder.bind(s); err != nil {
			cleanup()
			return "", err
		}
	}
	if components.Bind != nil {
		if err := components.Bind(ctx, s); err != nil {
			cleanup()
			return "", err
		}
	}
	configureMCP(s, components)
	if err := s.store.SetModelProvider(s.meta.ID, model, provider); err != nil {
		cleanup()
		return "", err
	}
	if meta.Effort != previousEffort {
		if err := s.store.SetEffort(s.meta.ID, meta.Effort); err != nil {
			cleanup()
			return "", err
		}
	}
	oldRunner, oldRuntime := s.runner, s.runtime
	oldMCP := s.swapMCP(components.MCP)
	s.runner, s.runtime = components.Runner, components.Runtime
	replacement.installed = true
	s.meta.Model, s.meta.Provider, s.meta.Effort = model, provider, meta.Effort
	s.emitSessionUpdate(ctx, "session.model.updated", SessionUpdateEvent{
		Model: model, Provider: provider, Effort: meta.Effort, EffortChanged: true,
	})
	s.supervisor.launchWorker("retire replaced runtime", func() {
		_ = safeClose("previous runner", oldRunner.Close)
		if oldMCP != nil {
			_ = safeClose("previous MCP", oldMCP.Close)
		}
		if oldRuntime != nil {
			_ = safeClose("previous runtime", oldRuntime.Close)
		}
	})
	return model + " @ " + provider, nil
}

func daemonEffortLabel(level string) string {
	if level == "" {
		return "off"
	}
	return level
}

func validateEffort(model, provider, requested string) error {
	if requested == "off" {
		return nil
	}
	if !slices.Contains([]string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}, requested) {
		return fmt.Errorf("unknown effort level %q", requested)
	}
	cfg, err := config.Load()
	if err != nil {
		return nil //nolint:nilerr // best-effort: skip the catalog check when config cannot load
	}
	_, _, apiID, err := cfg.Resolve(model, provider)
	if err != nil {
		return nil //nolint:nilerr // best-effort: skip the catalog check when the model cannot be resolved
	}
	catalog, ok := config.LoadCatalogs()[provider]
	if !ok {
		return nil
	}
	info := catalog.Find(apiID)
	if info != nil && len(info.ReasoningEfforts) > 0 && !slices.Contains(info.ReasoningEfforts, requested) {
		return fmt.Errorf("%s does not support effort %q", model, requested)
	}
	return nil
}

func compatibleEffort(model, provider, current string) string {
	runtimeLevel := current
	if runtimeLevel == "off" {
		runtimeLevel = ""
	}
	cfg, err := config.Load()
	if err != nil {
		return current
	}
	_, _, apiID, err := cfg.Resolve(model, provider)
	if err != nil {
		return current
	}
	catalog, ok := config.LoadCatalogs()[provider]
	if !ok {
		return current
	}
	info := catalog.Find(apiID)
	if info == nil || slices.Contains(append([]string{""}, info.ReasoningEfforts...), runtimeLevel) {
		return current
	}
	return "off"
}

func (s *Session) emitSessionUpdate(ctx context.Context, kind string, event SessionUpdateEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = s.store.AppendRootEvent(ctx, s.meta.ID, kind, sessionstore.RuntimePayload{
		Data: payload, MediaType: "application/json", Source: kind,
	})
}

func (r *AgentSession) SetEffort(level string) { r.agent.Effort = level }

func (s *Session) clientSchedule(ctx context.Context, operation string, payload clientActionPayload) (string, error) {
	if operation == "schedule.list" {
		schedules, err := s.store.SchedulesContext(ctx, s.meta.ID)
		return marshalClientOutput(schedules, err)
	}
	if operation == "schedule.delete" {
		id := payload.ScheduleID
		if id < 1 {
			return "", errors.New("schedule ID must be positive")
		}
		if err := s.store.DeleteSchedule(s.meta.ID, id); err != nil {
			return "", err
		}
		return marshalClientOutput(protocol.ScheduleResult{ScheduleID: id}, nil)
	}
	expression := payload.Schedule
	if strings.TrimSpace(payload.Prompt) == "" {
		return "", errors.New("schedule prompt is required")
	}

	parsed, err := schedule.Parse(expression)
	if err != nil {
		return "", err
	}
	prompt := payload.Prompt
	reservations := append(durableReservations(len(expression)+len(prompt)), capability.Reservation{
		Kind: string(sessionstore.BudgetSchedulesSubscriptions), Amount: 1, Consume: true,
	})
	var id int
	err = s.consumeBudgets(ctx, s.authority.AgentID, reservations, func() error {
		id, err = s.store.AddSchedule(s.meta.ID, parsed.String(), prompt, time.Now().UTC())
		return err
	})
	if err != nil {
		return "", err
	}
	return marshalClientOutput(protocol.ScheduleResult{ScheduleID: id}, nil)
}

func (s *Session) clientAgentControl(ctx context.Context, id, status string) (string, error) {
	if id == "" {
		return "", errors.New("agent control requires one child ID")
	}

	runtime, ok := s.runtime.(interface {
		ControlAgent(context.Context, string, string) error
	})
	if !ok {
		return "", errors.New("session runtime does not support agent control")
	}
	err := runtime.ControlAgent(ctx, id, status)
	if errors.Is(err, sessionstore.ErrAgentTerminal) {
		err = nil
	}
	return status, err
}

func (s *Session) clientAgentTranscript(ctx context.Context, id string) (string, error) {
	agentValue, err := s.store.LoadAgent(ctx, s.meta.ID, id)
	if err != nil {
		return "", err
	}
	snapshot, err := s.store.SnapshotRootView(ctx, s.meta.ID, sessionstore.SnapshotViewOptions{RecentMessages: 1, CollectionLimit: 128, MaxBytes: 128 << 10})
	if err != nil {
		return "", err
	}
	inbox := make([]sessionstore.InboxItem, 0)
	for _, item := range snapshot.Inbox {
		if item.AgentID == id {
			inbox = append(inbox, item)
		}
	}
	page, err := s.store.ReadTranscriptPage(ctx, s.meta.ID, id, sessionstore.TranscriptReadOptions{
		Recent: true, ThroughSeq: -1, Revision: &snapshot.HistoryRevision, Limit: 64, MaxBytes: 256 << 10,
	})
	presentation := snapshot.AgentPresentations[id]
	if agentValue.ParentID == "" {
		presentation = snapshot.Presentation
	}
	return marshalClientOutput(AgentTranscriptResult{
		Cursor: snapshot.Cursor, Agent: agentValue, Page: page, Presentation: presentation, Inbox: inbox,
	}, err)
}

// clientAgentSubmit enqueues human input for a descendant. delivery "steer"
// joins a running turn at its next loop boundary; anything else waits for its
// own turn.
func (s *Session) clientAgentSubmit(ctx context.Context, id, text, delivery string) (string, error) {
	text = strings.TrimSpace(text)
	if id == "" || text == "" {
		return "", errors.New("agent submission requires an agent and text")
	}
	kind := "submit"
	if delivery == "steer" {
		kind = "steer"
	}
	agentValue, err := s.store.LoadAgent(ctx, s.meta.ID, id)
	if err != nil {
		return "", err
	}
	if agentValue.ParentID == "" {
		return "", errors.New("use submit for the root agent")
	}
	if agentValue.Status == "stopped" || agentValue.Status == "deleted" || agentValue.Status == "failed" {
		return "", sessionstore.ErrAgentTerminal
	}
	sequence, err := s.store.EnqueueInbox(ctx, sessionstore.InboxEnqueue{
		RootID: s.meta.ID, AgentID: id, Kind: kind,
		Payload: sessionstore.RuntimePayload{Data: []byte(text), MediaType: "text/plain", Source: "human child submission"},
	})
	if err != nil {
		return "", err
	}
	s.wakeAgent(id)
	return marshalClientOutput(AgentSubmitResult{AgentID: id, InboxSeq: sequence.InboxSeq, Kind: kind, Status: "queued"}, nil)
}

func (s *Session) clientAgentTurnCancel(id string) (string, error) {
	runtime, ok := s.runtime.(interface {
		CancelAgentTurn(string) bool
	})
	if !ok {
		return "", errors.New("session runtime does not support agent cancellation")
	}
	if !runtime.CancelAgentTurn(id) {
		return "already idle", nil
	}
	return "cancellation requested", nil
}

func (s *Session) clientBudget(ctx context.Context, id, kind string, limit int64) (string, error) {
	if id == "" || kind == "" || limit < 0 {
		return "", errors.New("budget requires an agent, kind and nonnegative limit")
	}

	state, err := s.store.CapBudget(ctx, s.meta.ID, s.authority.AgentID, id, sessionstore.BudgetKind(kind), limit)
	if err == nil {
		// A raised cap can unblock queued descendants; re-derive readiness.
		s.reconcileAgentWork()
	}
	return marshalClientOutput(state, err)
}

func marshalClientOutput(value any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(value)
	return string(raw), err
}

func (s *Session) clientProviderCatalogs(ctx context.Context) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	catalogs := config.LoadCatalogs()
	result := ProviderCatalogsResult{Catalogs: catalogs, Models: map[string]protocol.ModelDescriptor{}, Providers: map[string]protocol.ProviderDescriptor{}, Errors: map[string]string{}}
	for name, model := range cfg.Models {
		result.Models[name] = protocol.ModelDescriptor{Name: model.Name, ID: model.ID, Providers: model.Providers, Context: model.ContextWindow(), Vision: model.Vision}
	}
	for name, provider := range cfg.Providers {
		result.Providers[name] = protocol.ProviderDescriptor{BaseURL: provider.BaseURL}
	}
	for name, provider := range cfg.Providers {
		key, keyErr := provider.ResolveKey()
		if keyErr != nil || key == "" {
			if keyErr != nil {
				result.Errors[name] = keyErr.Error()
			} else {
				result.Errors[name] = "no API key"
			}
			continue
		}
		models, fetchErr := llm.New(provider.BaseURL, key).Models(ctx)
		if fetchErr != nil {
			result.Errors[name] = fetchErr.Error()
			continue
		}
		catalog := config.Catalog{FetchedAt: time.Now(), BaseURL: provider.BaseURL, Models: modelInfoLites(models)}
		if err := config.UpdateCatalog(name, catalog); err != nil {
			return "", err
		}
	}
	catalogs = config.LoadCatalogs()
	result.Catalogs = catalogs
	return marshalClientOutput(result, nil)
}

func modelInfoLites(values []llm.ModelInfo) []config.ModelInfoLite {
	result := make([]config.ModelInfoLite, 0, len(values))
	for _, value := range values {
		var inputPrice, outputPrice, cacheReadPrice float64
		if value.Pricing != nil {
			inputPrice, outputPrice, cacheReadPrice = value.Pricing.Rates()
		}
		result = append(result, config.ModelInfoLite{
			ID: value.ID, ContextLength: value.ContextLength, MaxCompletionTokens: value.MaxCompletionTokens,
			ReasoningEfforts: value.ReasoningEfforts, InputModalities: value.InputModalities,
			InPrice: inputPrice, OutPrice: outputPrice, CacheReadPrice: cacheReadPrice,
		})
	}
	return result
}

func decodeClientAction(raw json.RawMessage, value *clientActionPayload) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func (replacement *clientReplacement) close() {
	if replacement == nil || replacement.installed {
		return
	}
	replacement.disposed.Do(func() {
		if replacement.components.Runner != nil {
			_ = safeClose("replacement runner", replacement.components.Runner.Close)
		}
		if replacement.components.MCP != nil {
			_ = safeClose("replacement MCP", replacement.components.MCP.Close)
		}
		if replacement.components.Runtime != nil {
			_ = safeClose("replacement runtime", replacement.components.Runtime.Close)
		}
	})
}

// startPendingReload runs on the actor. Deferred reload construction never
// holds up admission, cancellation or subscriptions while providers initialize.
func (s *Session) startPendingReload() {
	if !s.reloadPending || s.hasRunningAgent() || s.clientBusy || s.clientIntegrations > 0 {
		return
	}
	if replaceable, ok := s.runner.(clientReplaceRunner); ok {
		if err := replaceable.CanReplace(); err != nil {
			return
		}
	}
	s.reloadPending = false
	s.clientBusy = true
	s.clientPreparing = true
	factory, rootID, model, provider := s.factory, s.meta.ID, s.meta.Model, s.meta.Provider
	if !s.supervisor.launchWorker("prepare deferred reload", func() {
		var replacement *clientReplacement
		var err error
		if factory == nil {
			err = errors.New("session runner cannot be rebuilt")
		} else {
			replacement, err = s.prepareReplacement(s.supervisor.ctx, factory, rootID, model, provider)
		}
		s.supervisor.post(workerEnvelope{kind: workerClientCommand, client: &clientCommandCompletion{automatic: true, replacement: replacement, err: err}})
	}) {
		s.clientBusy = false
		s.clientPreparing = false
	}
}
