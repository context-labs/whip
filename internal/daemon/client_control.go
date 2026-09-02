package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/schedule"
	sessionstore "github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

type clientActionPayload struct {
	Args                string                      `json:"args,omitempty"`
	Command             string                      `json:"command,omitempty"`
	Cut                 int                         `json:"cut,omitempty"`
	ID                  string                      `json:"id,omitempty"`
	Bytes               []byte                      `json:"bytes,omitempty"`
	System              string                      `json:"system,omitempty"`
	MaxTurns            int                         `json:"max_turns,omitempty"`
	Headless            bool                        `json:"headless,omitempty"`
	Tool                string                      `json:"tool,omitempty"`
	Arguments           json.RawMessage             `json:"arguments,omitempty"`
	DenyPermissions     bool                        `json:"deny_permissions,omitempty"`
	ExternalPermissions bool                        `json:"external_permissions,omitempty"`
	Servers             map[string]mcp.ServerConfig `json:"servers,omitempty"`
}

type clientCommandReply struct {
	result CommandResult
	err    error
}

type clientCommandCompletion struct {
	clientID  string
	commandID string
	operation string
	ingress   int64
	output    string
	err       error
	compact   *clientCompaction
	rewind    *clientRewind
	goal      *clientGoal
	reply     chan clientCommandReply
}

type clientCompaction struct {
	summary      string
	cutoff       int
	rawTailStart int
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
	case "cancel", "goal.set", "goal.run", "goal.from-context", "schedule.manage", "session.fork", "workspace.inspect", "workspace.set",
		"session.effort", "session.model", "session.list", "session.open", "session.rename", "run.configure",
		"history.clear", "history.rewind", "history.compact",
		"agents.list", "agent.start", "agent.control", "agent.delete", "budget.cap", "capability.revoke", "shell.run",
		"context.inspect", "mcp.control", "lsp.control", "browser.control", "computer.control", "terminal.input",
		"tool.configure", "tool.schema", "tool.call", "permission.mode", "mcp.attach":
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

type clientTaskRunner interface {
	StartTask(string) (string, error)
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
	ConfigureRun(system string, maxTurns int, headless bool)
}

type clientRunRuntime interface {
	ConfigureRun(string)
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
	Reconnect(string) bool
	Enable(string) bool
	Disable(string) bool
}

func (r *agentRunner) ResolveWorkingDirectory(path string) (string, error) {
	return r.agent.Services.ResolveWorkingDirectory(path)
}

func (r *agentRunner) SetWorkingDirectory(path string) { r.agent.WorkingDir = path }

func (r *agentRunner) RunShell(ctx context.Context, command string) (string, error) {
	ctx = tools.WithWorkingDirectory(ctx, r.agent.WorkingDir)
	result, err := r.agent.Services.RunBash(ctx, command, 120*time.Second)
	return result.Output, err
}

func (r *agentRunner) ReplaceHistory(history []llm.Message) { r.agent.ReplaceHistory(history) }

func (r *agentRunner) ConfigureRun(system string, maxTurns int, headless bool) {
	if system != "" {
		r.agent.SetSystemPrompt(system)
	}
	r.agent.MaxTurns = maxTurns
	if headless {
		r.agent.ComputerDisabled = true
	}
}

func (r *agentRunner) ToolDefinitions(ctx context.Context) ([]llm.Tool, error) {
	return r.agent.Services.ToolDefinitions(ctx)
}

func (r *agentRunner) CallTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	return r.agent.Services.CallTool(ctx, name, arguments)
}

func (r *agentRunner) DenyToolPermissions() {
	r.agent.Services.SetGate(func(context.Context, tools.GateRequest) (tools.GateDecision, string) {
		return tools.GateReject, "this automation client cannot approve side effects"
	})
}

func (r *agentRunner) SetExternalPermissions(enabled bool) {
	r.agent.Services.SetExternalPermissions(enabled)
}

func (r *agentRunner) ExternalPermissionsEnabled() bool {
	return r.agent.Services.ExternalPermissionsEnabled()
}

func (r *agentRunner) ResolvePermission(permissionID string, decision capability.Decision) error {
	return r.agent.Services.ResolvePermission(permissionID, decision)
}

func (r *agentRunner) StartTask(arguments string) (string, error) {
	prompt := strings.TrimSpace(arguments)
	model, provider := "", ""
	if strings.HasPrefix(prompt, "-m ") {
		fields := strings.Fields(prompt)
		if len(fields) < 3 {
			return "", errors.New("subagent requires -m model[@provider] and a prompt")
		}
		model, provider, _ = strings.Cut(fields[1], "@")
		prompt = strings.TrimSpace(strings.TrimPrefix(prompt, strings.Join(fields[:2], " ")))
	}
	if prompt == "" {
		return "", errors.New("subagent prompt is required")
	}
	var route agent.SubModel
	if model != "" {
		if r.agent.ResolveModel == nil {
			return "", errors.New("subagent model overrides are unavailable")
		}
		var err error
		route, err = r.agent.ResolveModel(model, provider)
		if err != nil {
			return "", err
		}
	}
	description := strings.Join(strings.Fields(prompt), " ")
	if fields := strings.Fields(description); len(fields) > 8 {
		description = strings.Join(fields[:8], " ") + "…"
	}
	task := r.agent.StartBackground(description, prompt, route)
	if task.Status == agent.TaskError || task.Status == agent.TaskCancelled {
		return "", errors.New(task.Report)
	}
	return task.ID, nil
}

func (r *agentRunner) FormGoal(ctx context.Context, window int) (string, llm.Usage, error) {
	tail, err := agent.GoalFromContextMessages(r.agent.MessagesSnapshot(), window)
	if err != nil {
		return "", r.agent.Usage(), err
	}
	goal, usage, err := r.agent.Client.Complete(ctx, llm.Request{
		Model: r.agent.Model, MaxTokens: 8192,
		Messages: []llm.Message{{Role: "user", Content: agent.BuildGoalFromContextPrompt(tail)}},
	})
	r.agent.AddUsage(usage)
	goal = strings.TrimSpace(goal)
	if err == nil && goal == "" {
		err = errors.New("model returned an empty goal")
	}
	return goal, r.agent.Usage(), err
}

func (r *agentRunner) CompactNow(ctx context.Context) (clientCompaction, error) {
	before := r.agent.MessagesSnapshot()
	summary, cutoff, info, err := r.agent.CompactNow(ctx)
	return clientCompaction{
		summary: summary, cutoff: cutoff, rawTailStart: agent.CompactionRawTailStart(before, cutoff),
		model: info.Model, usage: r.agent.Usage(), before: before,
	}, err
}

func (r *agentRunner) CanReplace() error {
	switch {
	case r.agent.TurnRunning():
		return errors.New("a turn is running")
	case r.agent.HasRunningTasks():
		return errors.New("a child task is running")
	case r.agent.HasRunningWaits():
		return errors.New("a wait is running")
	default:
		return nil
	}
}

func (r *agentRunner) browserManager() *browser.Manager {
	if r.agent.Services == nil {
		return nil
	}
	return r.agent.Services.Browser()
}

func (r *agentRunner) lspManager() *lsp.Manager {
	if r.agent.Services == nil {
		return nil
	}
	manager, _ := r.agent.Services.Diagnostics().(*lsp.Manager)
	return manager
}

func (r *agentRunner) computerPolicy() *computer.Policy {
	if r.agent.Services == nil {
		return nil
	}
	return r.agent.Services.ComputerPolicy()
}

// ClientCommand admits a non-turn user action on the root actor. The durable
// command row is the idempotency boundary; matching retries never re-execute
// the action and instead return its authoritative terminal outcome.
func (s *Session) ClientCommand(ctx context.Context, admission sessionstore.CommandAdmission, operation string, payload json.RawMessage) (result CommandResult, err error) {
	admission.Scope = sessionstore.CommandScopeRoot
	admission.RootID = s.meta.ID
	admission.AgentID = s.authority.AgentID
	admission.Kind = operation
	var asyncReply chan clientCommandReply
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		admitted, admitErr := s.store.AdmitControlCommand(actorCtx, admission)
		if admitErr != nil {
			return admitErr
		}
		result = CommandResult{
			CommandID: admitted.Command.CommandID, IngressSeq: admitted.Command.IngressSeq,
			Status: admitted.Command.Status,
		}
		if !admitted.New {
			if admitted.Command.Status == "queued" || admitted.Command.Status == "running" || admitted.Command.Status == "waiting" {
				return nil
			}
			body, resolveErr := s.store.ResolveRuntimeValue(actorCtx, s.meta.ID, admitted.Command.Outcome)
			if resolveErr != nil {
				return resolveErr
			}
			if admitted.Command.Status == "failed" || admitted.Command.Status == "cancelled" || admitted.Command.Status == "interrupted" {
				result.Error = string(body)
			} else {
				result.Output = string(body)
			}
			return nil
		}
		if operation == "permission.mode" && s.running != nil {
			return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("permission mode cannot change while a turn is running"), &result)
		}
		if operation == "tool.call" {
			if s.clientBusy || s.running != nil {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("another root operation is already running"), &result)
			}
			var action clientActionPayload
			if err := json.Unmarshal(payload, &action); err != nil || action.Tool == "" {
				if err == nil {
					err = errors.New("tool name is required")
				}
				return s.finishClientCommandInline(actorCtx, admission, operation, "", err, &result)
			}
			runner, ok := s.runner.(clientToolRunner)
			if !ok {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner does not support tool calls"), &result)
			}
			asyncReply = make(chan clientCommandReply, 1)
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
				return s.finishClientCommandInline(actorCtx, admission, operation, "", ErrStopped, &result)
			}
			return nil
		}
		if operation == "shell.run" {
			if s.clientBusy || s.running != nil {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("another root operation is already running"), &result)
			}
			var action clientActionPayload
			if err := json.Unmarshal(payload, &action); err != nil || strings.TrimSpace(action.Command) == "" {
				if err == nil {
					err = errors.New("shell command is required")
				}
				return s.finishClientCommandInline(actorCtx, admission, operation, "", err, &result)
			}
			runner, ok := s.runner.(clientShellRunner)
			if !ok {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner does not support shell commands"), &result)
			}
			asyncReply = make(chan clientCommandReply, 1)
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
				return s.finishClientCommandInline(actorCtx, admission, operation, "", ErrStopped, &result)
			}
			return nil
		}
		if operation == "history.compact" {
			if s.clientBusy || s.running != nil {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("history cannot compact while a root operation is running"), &result)
			}
			runner, ok := s.runner.(clientCompactRunner)
			if !ok {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner does not support compaction"), &result)
			}
			asyncReply = make(chan clientCommandReply, 1)
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
				return s.finishClientCommandInline(actorCtx, admission, operation, "", ErrStopped, &result)
			}
			return nil
		}
		if operation == "goal.from-context" {
			if s.clientBusy || s.running != nil {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("a goal cannot be formulated while a root operation is running"), &result)
			}
			var action clientActionPayload
			if err := json.Unmarshal(payload, &action); err != nil {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("invalid goal context payload"), &result)
			}
			window := agent.GoalFromContextDefaultWindow
			if strings.TrimSpace(action.Args) != "" {
				var err error
				window, err = strconv.Atoi(strings.TrimSpace(action.Args))
				if err != nil || window < 2 {
					return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("goal context window must be at least 2"), &result)
				}
			}
			runner, ok := s.runner.(clientGoalRunner)
			if !ok {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner does not support goal formulation"), &result)
			}
			asyncReply = make(chan clientCommandReply, 1)
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
				return s.finishClientCommandInline(actorCtx, admission, operation, "", ErrStopped, &result)
			}
			return nil
		}
		if operation == "history.rewind" {
			if s.clientBusy || s.running != nil {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("history cannot rewind while a root operation is running"), &result)
			}
			var action clientActionPayload
			if err := json.Unmarshal(payload, &action); err != nil {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("invalid rewind payload"), &result)
			}
			cut, err := strconv.Atoi(strings.TrimSpace(action.Args))
			if err != nil || cut < 1 {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("rewind requires a positive conversation index"), &result)
			}
			if _, ok := s.runner.(clientHistoryRunner); !ok {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner does not support history replacement"), &result)
			}
			snapshots, err := s.store.WorkspaceSnapshotsFrom(actorCtx, s.meta.ID, cut)
			if err != nil {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", err, &result)
			}
			workspace, canRestore := s.runner.(workspaceSnapshotRunner)
			if len(snapshots) > 0 && !canRestore {
				return s.finishClientCommandInline(actorCtx, admission, operation, "", errors.New("session runner cannot restore workspace snapshots"), &result)
			}
			asyncReply = make(chan clientCommandReply, 1)
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
				return s.finishClientCommandInline(actorCtx, admission, operation, "", ErrStopped, &result)
			}
			return nil
		}

		output, actionErr := s.applyClientCommand(actorCtx, operation, payload)
		return s.finishClientCommandInline(actorCtx, admission, operation, output, actionErr, &result)
	})
	if err == nil && asyncReply != nil {
		reply := <-asyncReply
		return reply.result, reply.err
	}
	return result, err
}

func (s *Session) finishClientCommandInline(ctx context.Context, admission sessionstore.CommandAdmission, operation, output string, actionErr error, result *CommandResult) error {
	status := "succeeded"
	if actionErr != nil {
		status = "failed"
		output = actionErr.Error()
	}
	_, finishErr := s.store.FinishCommand(ctx, admission.ClientID, admission.CommandID, status, sessionstore.RuntimePayload{
		Data: []byte(output), MediaType: "text/plain", Source: operation + " outcome",
	})
	if finishErr != nil {
		return errors.Join(actionErr, finishErr)
	}
	result.Status = status
	if status == "failed" {
		result.Error = output
	} else {
		result.Output = output
	}
	return nil
}

func (s *Session) completeClientCommand(completion *clientCommandCompletion) error {
	if completion == nil || !s.clientBusy {
		return errors.New("client command completion has no running operation")
	}
	s.clientBusy = false
	if completion.compact != nil && completion.err == nil {
		compaction := completion.compact
		rawCutoff := s.rawCompactionCutoff(compaction.cutoff, compaction.rawTailStart)
		if err := s.store.RecordCompaction(s.meta.ID, rawCutoff, compaction.summary); err != nil {
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
			completion.output = fmt.Sprintf("compacted through message %d", rawCutoff)
			if compaction.model != "" {
				completion.output += " using " + compaction.model
			}
		}
	}
	if completion.rewind != nil && completion.err == nil {
		history, err := s.store.RewindHistory(s.supervisor.ctx, s.meta.ID, completion.rewind.cut)
		if err != nil {
			completion.err = err
		} else {
			s.runner.(clientHistoryRunner).ReplaceHistory(history)
			completion.output = strconv.Itoa(completion.rewind.cut)
			if completion.rewind.restored > 0 {
				completion.output += fmt.Sprintf("; restored %d workspace file(s)", completion.rewind.restored)
			}
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
	completion.reply <- clientCommandReply{result: result, err: err}
	return err
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
	var payload clientActionPayload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return "", errors.New("invalid client action payload")
		}
	}
	switch operation {
	case "mcp.attach":
		manager := mcp.NewManager(payload.Servers)
		previous := s.mcp
		s.mcp = manager
		configureMCP(s, Components{MCP: manager})
		if previous != nil {
			_ = safeClose("previous mcp", previous.Close)
		}
		return "configured", nil
	case "permission.mode":
		runner, ok := s.runner.(clientPermissionRunner)
		if !ok {
			return "", errors.New("session runner does not support external permissions")
		}
		runner.SetExternalPermissions(payload.ExternalPermissions)
		return "configured", nil
	case "tool.configure":
		runner, ok := s.runner.(clientToolRunner)
		if !ok {
			return "", errors.New("session runner does not support tool configuration")
		}
		if payload.DenyPermissions {
			runner.DenyToolPermissions()
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
		if payload.MaxTurns < 0 {
			return "", errors.New("max turns cannot be negative")
		}
		runner, ok := s.runner.(clientRunRunner)
		if !ok {
			return "", errors.New("session runner does not support run configuration")
		}
		runner.ConfigureRun(payload.System, payload.MaxTurns, payload.Headless)
		if runtime, ok := s.runtime.(clientRunRuntime); ok {
			runtime.ConfigureRun(payload.System)
		}
		return "configured", nil
	case "cancel":
		if s.turnCancel == nil {
			return "already idle", nil
		}
		s.turnCancel()
		return "cancellation requested", nil
	case "goal.set", "goal.run":
		goal := strings.TrimSpace(payload.Args)
		if goal == "clear" {
			goal = ""
		}
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
	case "schedule.manage":
		return s.clientSchedule(ctx, payload.Args)
	case "session.fork":
		cut := payload.Cut
		if cut <= 0 {
			cut = int(^uint(0) >> 1)
		}
		id, err := s.store.Fork(s.meta.ID, cut, strings.TrimSpace(payload.Args))
		return id, err
	case "workspace.inspect":
		if runner, ok := s.runner.(clientWorkspaceRunner); ok {
			return runner.ResolveWorkingDirectory(".")
		}
		return s.meta.CWD, nil
	case "workspace.set":
		runner, ok := s.runner.(clientWorkspaceRunner)
		if !ok {
			return "", errors.New("session runner does not support working-directory changes")
		}
		path, err := runner.ResolveWorkingDirectory(strings.TrimSpace(payload.Args))
		if err != nil {
			return "", err
		}
		if err := s.store.SetWorkingDirectory(s.meta.ID, path); err != nil {
			return "", err
		}
		runner.SetWorkingDirectory(path)
		s.meta.CWD = path
		return path, nil
	case "session.effort":
		level := strings.TrimSpace(payload.Args)
		if setter, ok := s.runner.(interface{ SetEffort(string) }); ok {
			setter.SetEffort(level)
		}
		if err := s.store.SetEffort(s.meta.ID, level); err != nil {
			return "", err
		}
		s.meta.Effort = level
		return level, nil
	case "session.model":
		if strings.TrimSpace(payload.Args) == "" {
			return s.meta.Model + " @ " + s.meta.Provider, nil
		}
		fields := strings.Fields(payload.Args)
		provider := s.meta.Provider
		if len(fields) > 1 {
			provider = fields[1]
		}
		return s.replaceModel(ctx, fields[0], provider)
	case "session.list":
		metas, err := s.store.RecentContext(ctx, 50)
		return marshalClientOutput(metas, err)
	case "session.open":
		meta, _, err := s.store.Load(strings.TrimSpace(payload.Args))
		if err != nil {
			return "", err
		}
		return meta.ID, nil
	case "session.rename":
		title := strings.TrimSpace(payload.Args)
		if title == "" {
			return "", errors.New("session title is required")
		}
		if err := s.store.SetTitle(s.meta.ID, title); err != nil {
			return "", err
		}
		s.meta.Title = title
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
	case "agents.list":
		relatives, err := s.store.ListAgentRelatives(ctx, s.meta.ID, s.authority.AgentID)
		return marshalClientOutput(relatives, err)
	case "agent.start":
		runner, ok := s.runner.(clientTaskRunner)
		if !ok {
			return "", errors.New("session runner does not support background subagents")
		}
		return runner.StartTask(payload.Args)
	case "agent.control":
		return s.clientAgentControl(ctx, payload.Args, "stopped")
	case "agent.delete":
		return s.clientAgentControl(ctx, payload.Args, "deleted")
	case "budget.cap":
		return s.clientBudget(ctx, payload.Args)
	case "capability.revoke":
		record, err := s.store.RevokeCapabilityFor(ctx, s.meta.ID, s.authority.AgentID, strings.TrimSpace(payload.Args))
		return marshalClientOutput(record, err)
	case "context.inspect":
		snapshot, err := s.store.SnapshotRoot(ctx, s.meta.ID)
		return marshalClientOutput(snapshot, err)
	case "terminal.input":
		runner, ok := s.runner.(clientTerminalRunner)
		if !ok {
			return "", errors.New("session runner does not support interactive terminal input")
		}
		if payload.ID == "" || len(payload.Bytes) == 0 || len(payload.Bytes) > 4<<10 {
			return "", errors.New("terminal input requires an active terminal and at most 4 KiB")
		}
		return "input delivered", runner.SendTerminalInput(payload.ID, payload.Bytes)
	case "mcp.control":
		return s.clientMCP(payload.Args)
	case "lsp.control":
		runner, ok := s.runner.(*agentRunner)
		if !ok || runner.lspManager() == nil {
			return "[]", nil
		}
		if args := strings.TrimSpace(payload.Args); args != "" && args != "list" && args != "status" {
			return "", errors.New("lsp supports status only")
		}
		return marshalClientOutput(runner.lspManager().Statuses(), nil)
	case "browser.control":
		return s.clientBrowser(payload.Args)
	case "computer.control":
		return s.clientComputer(payload.Args)
	default:
		return "", fmt.Errorf("unsupported root command %q", operation)
	}
}

func (s *Session) clientMCP(args string) (string, error) {
	manager, ok := s.mcp.(clientMCPManager)
	if !ok {
		return "[]", nil
	}
	fields := strings.Fields(args)
	if len(fields) == 0 || fields[0] == "list" || fields[0] == "status" {
		return marshalClientOutput(manager.Statuses(), nil)
	}
	action := "reconnect"
	if len(fields) > 1 {
		action = fields[1]
	}
	var changed bool
	switch action {
	case "reconnect":
		changed = manager.Reconnect(fields[0])
	case "enable":
		changed = manager.Enable(fields[0])
	case "disable":
		changed = manager.Disable(fields[0])
	default:
		return "", errors.New("mcp action must be reconnect, enable, or disable")
	}
	if !changed {
		return "", fmt.Errorf("no MCP server named %s", fields[0])
	}
	return fields[0] + ": " + action, nil
}

func (s *Session) clientBrowser(args string) (string, error) {
	runner, ok := s.runner.(*agentRunner)
	if !ok || runner.browserManager() == nil {
		return "browser automation is disabled", nil
	}
	manager := runner.browserManager()
	fields := strings.Fields(args)
	if len(fields) == 0 || fields[0] == "status" {
		return manager.Driver(), nil
	}
	driver := fields[0]
	if fields[0] == "driver" && len(fields) > 1 {
		driver = fields[1]
	}
	if driver != browser.DriverRod && driver != browser.DriverChromedp {
		return "", errors.New("browser driver must be rod or chromedp")
	}
	manager.SwitchDriver(driver)
	return manager.Driver(), nil
}

func (s *Session) clientComputer(args string) (string, error) {
	runner, ok := s.runner.(*agentRunner)
	if !ok || runner.computerPolicy() == nil {
		return "computer automation is disabled", nil
	}
	policy := runner.computerPolicy()
	fields := strings.Fields(args)
	if len(fields) == 0 || fields[0] == "status" {
		return policy.Summary(), nil
	}
	if len(fields) < 2 || fields[0] != "allow" && fields[0] != "deny" {
		return "", errors.New("computer action must be status, allow <app>, or deny <app>")
	}
	app := strings.Join(fields[1:], " ")
	if fields[0] == "allow" {
		policy.Approve(app)
	} else {
		policy.Deny(app)
	}
	return app + ": " + fields[0], nil
}

func (s *Session) replaceModel(ctx context.Context, model, provider string) (string, error) {
	if s.running != nil || s.clientBusy {
		return "", errors.New("model cannot change while a root operation is running")
	}
	if model == s.meta.Model && provider == s.meta.Provider {
		return model + " @ " + provider, nil
	}
	if s.factory == nil {
		return "", errors.New("session runner cannot be rebuilt")
	}
	if replaceable, ok := s.runner.(clientReplaceRunner); ok {
		if err := replaceable.CanReplace(); err != nil {
			return "", fmt.Errorf("model cannot change while %w", err)
		}
	}
	meta, history, err := s.store.Load(s.meta.ID)
	if err != nil {
		return "", err
	}
	meta.Model, meta.Provider = model, provider
	components, err := s.factory(ctx, meta, history)
	if err != nil {
		return "", err
	}
	cleanup := func() {
		if components.Runner != nil {
			_ = safeClose("replacement runner", components.Runner.Close)
		}
		if components.MCP != nil {
			_ = safeClose("replacement MCP", components.MCP.Close)
		}
		if components.Runtime != nil {
			_ = safeClose("replacement runtime", components.Runtime.Close)
		}
	}
	if components.Runner == nil {
		cleanup()
		return "", errors.New("root factory returned no replacement runner")
	}
	if binder, ok := components.Runner.(interface{ bind(*Session) error }); ok {
		if err := binder.bind(s); err != nil {
			cleanup()
			return "", err
		}
	}
	if components.Bind != nil {
		if err := components.Bind(s); err != nil {
			cleanup()
			return "", err
		}
	}
	configureMCP(s, components)
	if err := s.store.SetModelProvider(s.meta.ID, model, provider); err != nil {
		cleanup()
		return "", err
	}
	oldRunner, oldMCP, oldRuntime := s.runner, s.mcp, s.runtime
	s.runner, s.mcp, s.runtime = components.Runner, components.MCP, components.Runtime
	s.meta.Model, s.meta.Provider = model, provider
	_ = safeClose("previous runner", oldRunner.Close)
	if oldMCP != nil {
		_ = safeClose("previous MCP", oldMCP.Close)
	}
	if oldRuntime != nil {
		_ = safeClose("previous runtime", oldRuntime.Close)
	}
	return model + " @ " + provider, nil
}

func (r *agentRunner) SetEffort(level string) { r.agent.Effort = level }

func (s *Session) clientSchedule(ctx context.Context, args string) (string, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 || fields[0] == "list" {
		schedules, err := s.store.SchedulesContext(ctx, s.meta.ID)
		return marshalClientOutput(schedules, err)
	}
	if fields[0] == "cancel" {
		if len(fields) != 2 {
			return "", errors.New("schedule cancel requires an ID")
		}
		id, err := strconv.Atoi(fields[1])
		if err != nil {
			return "", errors.New("schedule ID must be a number")
		}
		return "", s.store.DeleteSchedule(s.meta.ID, id)
	}
	if len(fields) < 3 || fields[0] != "@every" && fields[0] != "@at" {
		return "", errors.New("schedule requires @every <duration> or @at <time> and a prompt")
	}
	expression := strings.Join(fields[:2], " ")
	parsed, err := schedule.Parse(expression)
	if err != nil {
		return "", err
	}
	prompt := strings.Join(fields[2:], " ")
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
	return strconv.Itoa(id), nil
}

func (s *Session) clientAgentControl(ctx context.Context, args, status string) (string, error) {
	fields := strings.Fields(args)
	if status == "stopped" && len(fields) == 2 && fields[0] == "stop" {
		fields = fields[1:]
	}
	if len(fields) != 1 {
		return "", errors.New("agent control requires one child ID")
	}
	target, taskID := fields[0], ""
	for id, child := range s.children {
		if child.agentID == target || id == target {
			taskID, target = id, child.agentID
			child.running = false
			break
		}
	}
	_, err := s.store.TerminalizeSubtree(ctx, s.meta.ID, s.authority.AgentID, target, status)
	if errors.Is(err, sessionstore.ErrAgentTerminal) {
		err = nil
	}
	if err == nil && taskID != "" {
		if runner, ok := s.runner.(*agentRunner); ok {
			s.supervisor.launchWorker("cancel controlled subagent", func() { runner.agent.Tasks().Cancel(taskID) })
		}
	}
	return status, err
}

func (s *Session) clientBudget(ctx context.Context, args string) (string, error) {
	fields := strings.Fields(args)
	if len(fields) != 3 {
		return "", errors.New("budget cap requires <agent> <kind> <limit>")
	}
	limit, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || limit < 0 {
		return "", errors.New("budget limit must be a nonnegative integer")
	}
	state, err := s.store.CapBudget(ctx, s.meta.ID, s.authority.AgentID, fields[0], sessionstore.BudgetKind(fields[1]), limit)
	return marshalClientOutput(state, err)
}

func marshalClientOutput(value any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(value)
	return string(raw), err
}
