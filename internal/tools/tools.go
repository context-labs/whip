// Package tools implements the agent's built-in tools.
package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools/bashrun"
)

// Tool is a named executable tool with a JSON schema.
type Tool struct {
	Def llm.Tool
	Run func(ctx context.Context, args json.RawMessage) (string, error)
}

// InteractiveRunner runs an interactive bash command with PTY-backed live I/O.
// The TUI installs one so the agent's bash tool can hand interactive prompts
// (sudo, ssh, gpg) to the user. ctx caps the whole run; keys feeds keystrokes
// the user types; the returned string is fed back to the model as tool output.
// Implementations must be safe to call from a goroutine that is not the UI
// thread, and must not block forever when no input arrives.
type InteractiveRunner interface {
	Run(ctx context.Context, opts bashrun.Options) string
}

type Diagnostics interface {
	WaitDiagnostics(ctx context.Context, path string) string
}

type Services struct {
	mu                  sync.RWMutex
	interactive         InteractiveRunner
	diagnostics         Diagnostics
	gate                Gate
	dispatcher          *capability.Dispatcher
	authority           capability.Authority
	processes           *capability.ProcessManager
	workspace           *capability.Workspace
	processCwd          string
	processEnv          map[string]string
	browser             *browser.Manager
	allowPrivateURLs    bool
	screenshotSink      func([][]byte)
	computerPolicy      *computer.Policy
	computerApprover    func(string) bool
	computerHelper      *computer.Helper
	appGenerations      map[string]int
	externalPermissions bool
	permissionWaiters   map[string]chan capability.Decision
	permissionEarly     map[string]capability.Decision

	// Background shell jobs owned by this agent; see jobs.go.
	jobs     map[string]*bashrun.Job
	jobOrder []string
}

func NewServices() *Services { return &Services{} }

// SetExternalPermissions makes dispatcher admissions wait for an authenticated
// daemon decision instead of consulting an in-process consent callback.
func (s *Services) SetExternalPermissions(enabled bool) {
	s.mu.Lock()
	s.externalPermissions = enabled
	if enabled && s.permissionWaiters == nil {
		s.permissionWaiters = make(map[string]chan capability.Decision)
		s.permissionEarly = make(map[string]capability.Decision)
	}
	s.mu.Unlock()
}

// ExternalPermissionsEnabled reports whether admissions are delegated to an
// authenticated daemon client instead of the in-process consent callback.
func (s *Services) ExternalPermissionsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.externalPermissions
}

// ResolvePermission delivers one authenticated decision to the dispatcher
// call waiting on that permission ID.
func (s *Services) ResolvePermission(permissionID string, decision capability.Decision) error {
	if permissionID == "" || decision.PrincipalID == "" {
		return errors.New("permission and principal identities are required")
	}
	s.mu.Lock()
	if !s.externalPermissions {
		s.mu.Unlock()
		return errors.New("external permissions are not enabled")
	}
	if waiter := s.permissionWaiters[permissionID]; waiter != nil {
		delete(s.permissionWaiters, permissionID)
		s.mu.Unlock()
		waiter <- decision
		return nil
	}
	s.permissionEarly[permissionID] = decision
	s.mu.Unlock()
	return nil
}

func (s *Services) SetBrowser(manager *browser.Manager, allowPrivateURLs bool) {
	s.mu.Lock()
	s.browser = manager
	s.allowPrivateURLs = allowPrivateURLs
	s.mu.Unlock()
}

func (s *Services) Browser() *browser.Manager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.browser
}

func (s *Services) browserConfig() (*browser.Manager, bool, func([][]byte)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.browser, s.allowPrivateURLs, s.screenshotSink
}

func (s *Services) SetScreenshotSink(sink func([][]byte)) {
	s.mu.Lock()
	s.screenshotSink = sink
	s.mu.Unlock()
}

// ScreenshotsEnabled reports whether browser/computer captures can be sent
// back to the owning model. Authority clones intentionally do not copy the
// callback because it steers one specific agent.
func (s *Services) ScreenshotsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.screenshotSink != nil
}

func (s *Services) screenshots() func([][]byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.screenshotSink
}

func (s *Services) SetComputerPolicy(policy *computer.Policy) {
	s.mu.Lock()
	s.computerPolicy = policy
	s.mu.Unlock()
}

func (s *Services) ComputerPolicy() *computer.Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.computerPolicy
}

func (s *Services) SetComputerApprover(approver func(string) bool) {
	s.mu.Lock()
	s.computerApprover = approver
	s.mu.Unlock()
}

func (s *Services) computerApproval() (*computer.Policy, func(string) bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.computerPolicy, s.computerApprover
}

func (s *Services) noteGeneration(app string, generation int) {
	s.mu.Lock()
	if s.appGenerations == nil {
		s.appGenerations = map[string]int{}
	}
	s.appGenerations[strings.ToLower(app)] = generation
	s.mu.Unlock()
}

func (s *Services) generationFor(app string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.appGenerations[strings.ToLower(app)]
}

func (s *Services) SetInteractive(runner InteractiveRunner) {
	s.mu.Lock()
	s.interactive = runner
	s.mu.Unlock()
}

func (s *Services) SetDiagnostics(diagnostics Diagnostics) {
	s.mu.Lock()
	s.diagnostics = diagnostics
	s.mu.Unlock()
}

func (s *Services) Diagnostics() Diagnostics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.diagnostics
}

func (s *Services) SetGate(gate Gate) {
	s.mu.Lock()
	s.gate = gate
	s.mu.Unlock()
}

func (s *Services) SetProcessMarkers(sessionID, model string) {
	s.mu.Lock()
	s.processEnv = bashrun.Markers(sessionID, model)
	s.mu.Unlock()
}

// SetProcessEnvironment adds explicit child-process values without mutating
// the daemon's process-global environment.
func (s *Services) SetProcessEnvironment(values map[string]string) {
	s.mu.Lock()
	if s.processEnv == nil {
		s.processEnv = make(map[string]string)
	}
	maps.Copy(s.processEnv, values)
	s.mu.Unlock()
}

func (s *Services) ProcessOptions() bashrun.Options {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return bashrun.Options{Cwd: s.processCwd, RootID: s.authority.RootID, Processes: s.processes, Env: maps.Clone(s.processEnv)}
}

func (s *Services) ResolveWorkingDirectory(path string) (string, error) {
	s.mu.RLock()
	workspace := s.workspace
	s.mu.RUnlock()
	if workspace == nil {
		return filepath.Abs(path)
	}
	resolved, err := workspace.Resolve(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", path)
	}
	return resolved, nil
}

type workingDirectoryKey struct{}

func WithWorkingDirectory(ctx context.Context, path string) context.Context {
	if path == "" {
		return ctx
	}
	return context.WithValue(ctx, workingDirectoryKey{}, path)
}

func workingDirectory(ctx context.Context) string {
	path, _ := ctx.Value(workingDirectoryKey{}).(string)
	return path
}

type invocationKey struct{}

type invocation struct {
	commandClientID string
	commandID       string
	operationID     string
	traceID         string
}

// WithTurnIdentity attributes all tool calls made from one agent turn to the
// same command and trace.
func WithTurnIdentity(ctx context.Context, clientID string) (context.Context, error) {
	id, err := randomID()
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, invocationKey{}, invocation{
		commandClientID: clientID,
		commandID:       id,
		traceID:         id,
	}), nil
}

// WithOperationIdentity attributes one model tool call within a turn.
func WithOperationIdentity(ctx context.Context, callID string) context.Context {
	identity, _ := ctx.Value(invocationKey{}).(invocation)
	if callID != "" {
		identity.operationID = identity.commandID + ":" + callID
	}
	return context.WithValue(ctx, invocationKey{}, identity)
}

type bashResultKey struct{}

func (s *Services) RunBash(ctx context.Context, command string, timeout time.Duration) (bashrun.Result, error) {
	arguments, err := json.Marshal(struct {
		Command string  `json:"command"`
		Timeout float64 `json:"timeout"`
	}{Command: command, Timeout: timeout.Seconds()})
	if err != nil {
		return bashrun.Result{}, err
	}
	var result bashrun.Result
	ctx = context.WithValue(ctx, bashResultKey{}, &result)
	_, err = s.run(ctx, "bash", arguments, bashTool(s).Run)
	return result, err
}

func (s *Services) RunProcess(ctx context.Context, name string, args ...string) ([]byte, error) {
	opts := s.ProcessOptions()
	if opts.Processes == nil {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = opts.Cwd
		return cmd.CombinedOutput()
	}
	var stdout, stderr bytes.Buffer
	process, err := opts.Processes.Start(ctx, opts.RootID, name, args, capability.ProcessOptions{
		Cwd: opts.Cwd, Env: opts.Env, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
	})
	if err == nil {
		err = process.Wait()
	}
	return append(stdout.Bytes(), stderr.Bytes()...), err
}

func (s *Services) RunWorkspaceProcess(ctx context.Context, name string, args ...string) ([]byte, error) {
	arguments, err := json.Marshal(struct {
		Name    string   `json:"name"`
		Args    []string `json:"args"`
		Command string   `json:"command"`
	}{Name: name, Args: args, Command: strings.Join(append([]string{name}, args...), " ")})
	if err != nil {
		return nil, err
	}
	out, err := s.run(ctx, "workspace_process", arguments, workspaceProcessTool(s).Run)
	return []byte(out), err
}

func workspaceProcessTool(services *Services) Tool {
	return Tool{Def: llm.NewTool("workspace_process", "Run an internal workspace-wide process.", `{}`), Run: func(ctx context.Context, arguments json.RawMessage) (string, error) {
		var args struct {
			Name string   `json:"name"`
			Args []string `json:"args"`
		}
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}
		out, err := services.RunProcess(ctx, args.Name, args.Args...)
		return string(out), err
	}}
}

func (s *Services) computerAutomation(ctx context.Context) computer.Automation {
	return computer.Automation{Context: ctx, Run: s.RunProcess}
}

func (s *Services) nativeComputerHelper() (*computer.Helper, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.computerHelper != nil {
		return s.computerHelper, nil
	}
	if s.processes == nil {
		return computer.Shared()
	}
	helper, err := computer.NewManagedHelper(s.processes, s.authority.RootID, s.processCwd, maps.Clone(s.processEnv))
	if err != nil {
		return nil, err
	}
	s.computerHelper = helper
	return helper, nil
}

func (s *Services) Close() {
	s.killJobs()
	s.mu.RLock()
	diagnostics, browserManager, computerHelper := s.diagnostics, s.browser, s.computerHelper
	s.mu.RUnlock()
	if closer, ok := diagnostics.(interface{ Close() }); ok {
		closer.Close()
	}
	if browserManager != nil {
		browserManager.CloseAll()
	}
	if computerHelper != nil {
		computerHelper.Close()
	}
}

// All returns an unbound built-in tool set. Execution fails closed until its
// Services is bound to dispatcher authority.
func All() []Tool {
	return AllWithServices(NewServices())
}

func AllWithServices(services *Services) []Tool {
	if services == nil {
		services = NewServices()
	}
	var toolset []Tool
	for _, spec := range hostToolSpecs {
		if spec.advertised {
			toolset = append(toolset, services.wrap(spec.build(services)))
		}
	}
	return toolset
}

// ToolDefinitions returns the public built-in schemas without exposing
// concrete handlers to protocol adapters.
func (s *Services) ToolDefinitions(context.Context) ([]llm.Tool, error) {
	return Defs(AllWithServices(s)), nil
}

// CallTool routes one public built-in through the bound dispatcher.
func (s *Services) CallTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	return s.Invoke(ctx, name, arguments)
}

type hostToolSpec struct {
	build      func(*Services) Tool
	advertised bool
	shell      bool
	writer     bool
	mutation   capability.Mutation
	permission bool
	path       func(json.RawMessage) (string, error)
}

var hostToolSpecs = []hostToolSpec{
	{build: bashTool, advertised: true, shell: true, writer: true, mutation: capability.MutationWorkspace, permission: true},
	{build: func(*Services) Tool { return readTool() }, advertised: true, path: toolPath},
	{build: writeTool, advertised: true, mutation: capability.MutationPath, permission: true, path: toolPath},
	{build: editTool, advertised: true, mutation: capability.MutationPath, permission: true, path: toolPath},
	{build: browserExec, shell: true},
	{build: computerExec, shell: true},
	{build: workspaceProcessTool, shell: true, writer: true, mutation: capability.MutationWorkspace, permission: true},
	{build: shellStartTool, shell: true, writer: true, mutation: capability.MutationWorkspace, permission: true},
}

func hostTool(services *Services, operation string) Tool {
	for _, spec := range hostToolSpecs {
		tool := spec.build(services)
		if tool.Def.Function.Name == operation {
			return services.wrap(tool)
		}
	}
	panic("unknown host tool: " + operation)
}

func hostSpec(operation string) (hostToolSpec, bool) {
	for _, spec := range hostToolSpecs {
		if spec.build(nil).Def.Function.Name == operation {
			return spec, true
		}
	}
	return hostToolSpec{}, false
}

func (s *Services) wrap(tool Tool) Tool {
	direct := tool.Run
	operation := tool.Def.Function.Name
	tool.Run = func(ctx context.Context, arguments json.RawMessage) (string, error) {
		if _, dispatched := dispatchCall(ctx); dispatched {
			return direct(ctx, arguments)
		}
		return s.run(ctx, operation, arguments, direct)
	}
	return tool
}

type dispatchCallKey struct{}

func (s *Services) BindDispatcher(ledger capability.Ledger, workspaces *capability.Workspaces, processes *capability.ProcessManager, authority capability.Authority) error {
	if ledger == nil || workspaces == nil || processes == nil || authority.RootID == "" || authority.AgentID == "" || authority.Files.ID == "" || authority.Shell.ID == "" {
		return errors.New("host dispatcher authority is incomplete")
	}
	s.mu.RLock()
	bound := s.dispatcher != nil && s.processes == processes && s.authority == authority
	s.mu.RUnlock()
	if bound {
		return nil
	}
	dispatcher := capability.NewDispatcher(ledger, workspaces, s)
	root, err := ledger.WorkspaceRoot(context.Background(), authority.RootID)
	if err != nil {
		return err
	}
	workspace, err := workspaces.Open(root)
	if err != nil {
		return err
	}
	for _, spec := range hostToolSpecs {
		tool := spec.build(s)
		operation := tool.Def.Function.Name
		registration := capability.Registration{Operation: operation, Handler: func(ctx context.Context, call capability.Call) (string, error) {
			return tool.Run(context.WithValue(ctx, dispatchCallKey{}, call), call.Arguments)
		}, Mutation: spec.mutation, Permission: spec.permission, Path: spec.path}
		if err := dispatcher.Register(registration); err != nil {
			return err
		}
	}
	s.mu.RLock()
	env := maps.Clone(s.processEnv)
	browserManager := s.browser
	s.mu.RUnlock()
	var childEnv []string
	if browserManager != nil {
		childEnv, err = processes.ChildEnvironment(env)
		if err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.dispatcher = dispatcher
	s.authority = authority
	s.processes = processes
	s.workspace = workspace
	s.processCwd = workspace.Root()
	diagnostics := s.diagnostics
	computerHelper := s.computerHelper
	s.computerHelper = nil
	s.mu.Unlock()
	if computerHelper != nil {
		computerHelper.Close()
	}
	if browserManager != nil {
		browserManager.SetProcessOptions(processes, authority.RootID, childEnv)
	}
	if scoped, ok := diagnostics.(interface {
		SetProcessOptions(*capability.ProcessManager, string, string, map[string]string)
	}); ok {
		scoped.SetProcessOptions(processes, authority.RootID, workspace.Root(), env)
	}
	return nil
}

// CloneForAuthority keeps the parent's host integrations while binding tool
// calls to a distinct descendant capability set.
func (s *Services) CloneForAuthority(ledger capability.Ledger, workspaces *capability.Workspaces, processes *capability.ProcessManager, authority capability.Authority) (*Services, error) {
	s.mu.RLock()
	clone := &Services{
		interactive:         s.interactive,
		diagnostics:         s.diagnostics,
		gate:                s.gate,
		processEnv:          maps.Clone(s.processEnv),
		browser:             s.browser,
		allowPrivateURLs:    s.allowPrivateURLs,
		computerPolicy:      s.computerPolicy,
		computerApprover:    s.computerApprover,
		appGenerations:      maps.Clone(s.appGenerations),
		externalPermissions: s.externalPermissions,
	}
	s.mu.RUnlock()
	if clone.externalPermissions {
		clone.permissionWaiters = make(map[string]chan capability.Decision)
		clone.permissionEarly = make(map[string]capability.Decision)
	}
	if err := clone.BindDispatcher(ledger, workspaces, processes, authority); err != nil {
		return nil, err
	}
	return clone, nil
}

func toolPath(arguments json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return "", err
	}
	if args.Path == "" {
		return "", errors.New("path is required")
	}
	return args.Path, nil
}

func (s *Services) run(ctx context.Context, operation string, arguments json.RawMessage, direct func(context.Context, json.RawMessage) (string, error)) (string, error) {
	s.mu.RLock()
	dispatcher, authority := s.dispatcher, s.authority
	s.mu.RUnlock()
	if dispatcher == nil {
		return "", errors.New("tool services are not bound to dispatcher authority")
	}
	spec, ok := hostSpec(operation)
	if !ok {
		return "", fmt.Errorf("unknown host operation %q", operation)
	}
	capabilityRef := authority.Files
	identity, ok := ctx.Value(invocationKey{}).(invocation)
	if !ok || identity.traceID == "" {
		var err error
		ctx, err = WithTurnIdentity(ctx, "runtime")
		if err != nil {
			return "", err
		}
		identity = ctx.Value(invocationKey{}).(invocation)
	}
	request := capability.Request{
		RootID: authority.RootID, AgentID: authority.AgentID, Operation: operation, Arguments: arguments,
		CommandClientID: identity.commandClientID, CommandID: identity.commandID, TraceID: identity.traceID,
	}
	if spec.shell {
		capabilityRef = authority.Shell
	}
	if spec.writer {
		request.WriterCapabilityID = authority.Files.ID
		request.WriterCapabilityGeneration = authority.Files.Generation
	}
	request.CapabilityID = capabilityRef.ID
	request.CapabilityGeneration = capabilityRef.Generation
	request.WorkingDirectory = workingDirectory(ctx)
	request.OperationID = identity.operationID
	if request.OperationID == "" {
		var err error
		request.OperationID, err = randomID()
		if err != nil {
			return "", err
		}
	}
	response, err := dispatcher.Dispatch(ctx, request)
	return response.Output, err
}

// Invoke routes a named built-in operation through the same dispatcher,
// authority, permission, budget, mutation-ordering, and trace path used by
// runtime modules.
func (s *Services) Invoke(ctx context.Context, operation string, arguments json.RawMessage) (string, error) {
	spec, ok := hostSpec(operation)
	if !ok {
		return "", fmt.Errorf("unknown host operation %q", operation)
	}
	return s.run(ctx, operation, arguments, spec.build(s).Run)
}

func randomID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func dispatchCall(ctx context.Context) (capability.Call, bool) {
	call, ok := ctx.Value(dispatchCallKey{}).(capability.Call)
	return call, ok
}

// updateKey carries a per-tool-call partial-output callback. The agent layer
// attaches it to the ctx for one call (a context value, not a package var, so
// parallel tool calls can't cross wires); the bash tool forwards it to
// bashrun's OnUpdate. Non-callers (whip run, tests) simply don't set it.
type updateKey struct{}

// WithOnUpdate returns a ctx that makes the bash tool report throttled partial
// output snapshots for this one call.
func WithOnUpdate(ctx context.Context, onUpdate func(outputSoFar string)) context.Context {
	return context.WithValue(ctx, updateKey{}, onUpdate)
}

// OnUpdate returns the partial-output callback installed by WithOnUpdate, or
// nil. The RLM kernel uses it to stream a cell's print output.
func OnUpdate(ctx context.Context) func(outputSoFar string) {
	callback, _ := ctx.Value(updateKey{}).(func(string))
	return callback
}

type toolCallKey struct{}

// WithToolCallID names the model tool call a ctx belongs to, so runtime
// events emitted while it runs (host calls inside a Starlark cell) can be
// attributed to it in the presentation stream.
func WithToolCallID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, toolCallKey{}, id)
}

// ToolCallID returns the id set by WithToolCallID, or "".
func ToolCallID(ctx context.Context) string {
	id, _ := ctx.Value(toolCallKey{}).(string)
	return id
}

// Defs returns the llm.Tool definitions for a tool set.
func Defs(ts []Tool) []llm.Tool {
	defs := make([]llm.Tool, len(ts))
	for i, t := range ts {
		defs[i] = t.Def
	}
	return defs
}

// Execute runs the named tool. Errors are returned as strings so they can be
// fed back to the model rather than aborting the loop.
func Execute(ctx context.Context, ts []Tool, name string, args json.RawMessage) string {
	return ExecuteWithSuggester(ctx, ts, name, args, nil)
}

func ExecuteWithSuggester(ctx context.Context, ts []Tool, name string, args json.RawMessage, suggest func(string) []string) string {
	for _, t := range ts {
		if t.Def.Function.Name == name {
			out, err := t.Run(ctx, args)
			if err != nil {
				return "Error: " + err.Error()
			}
			if out == "" {
				out = "(no output)"
			}
			return out
		}
	}
	msg := fmt.Sprintf("Error: unknown tool %q", name)
	if suggest != nil {
		if hints := suggest(name); len(hints) > 0 {
			msg += " — did you mean " + strings.Join(hints, " or ") + "?"
		}
	}
	return msg
}

const maxOutput = 50_000 // bytes of tool output fed back to the model

// Truncate caps tool output at maxOutput with a marker; exported for the MCP
// bridge, which flattens remote results into the same budget.
func Truncate(s string) string {
	return truncate(s)
}

// truncate keeps head and tail with a middle elision: the first lines usually
// orient (headers, imports, the command's first output) and the last lines
// carry the error; the middle is what repeats. The full output spills to a
// file so nothing is unrecoverable — the decay layer reuses the same marker
// to point its placeholders at the spill.
func truncate(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	return middleElide(s)
}

// middleElide keeps the first and last quarters of maxOutput and replaces the
// middle with a marker naming the dropped byte count (and the spill path when
// writing it succeeded). Exported pieces of the result format are parsed by
// the agent's decay pass (spillPathOf) — keep the marker shape stable.
func middleElide(s string) string {
	keep := maxOutput / 2
	head, tail := s[:keep], s[len(s)-keep:]
	elided := len(s) - 2*keep
	marker := fmt.Sprintf("\n... [%d bytes elided from the middle", elided)
	if path := bashrun.Spill(s); path != "" {
		marker += fmt.Sprintf(" — full output (%d bytes): %s", len(s), path)
	}
	marker += "] ...\n"
	return head + marker + tail
}

// lspDiagnostics appends the LSP diagnostics block for a just-written file.
// Never fails the tool: a nil hook, an uncovered file, or a slow server all
// yield "" (the wait is capped inside internal/lsp).
func (s *Services) lspDiagnostics(ctx context.Context, path string) string {
	s.mu.RLock()
	diagnostics := s.diagnostics
	s.mu.RUnlock()
	if diagnostics == nil {
		return ""
	}
	return diagnostics.WaitDiagnostics(ctx, path)
}

// TruncateTail caps tool output at maxOutput bytes, keeping the tail (the end
// is usually where the error is). Exported for the TUI's `!` shell escape,
// which formats output exactly like the bash tool.
func TruncateTail(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	return fmt.Sprintf("[... first %d bytes truncated]\n", len(s)-maxOutput) + s[len(s)-maxOutput:]
}

func bashTool(services *Services) Tool {
	return Tool{
		Def: llm.NewTool("bash",
			"Execute a bash command in the current working directory and return its combined stdout/stderr. Use for running programs, git, searching (grep/rg), listing files, etc.",
			`{"type":"object","properties":{"command":{"type":"string","description":"The bash command to execute"},"timeout":{"type":"number","description":"Timeout in seconds (default 120)"},"interactive":{"type":"boolean","description":"Run in a PTY so sudo/ssh-style password prompts work. Whip stays in control of the terminal and forwards your keystrokes; the command is killed after 15s of no input. Use only for commands that genuinely need a password."}},"required":["command"]}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Command     string  `json:"command"`
				Timeout     float64 `json:"timeout"`
				Interactive bool    `json:"interactive"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return "", err
			}
			if a.Timeout <= 0 {
				a.Timeout = 120
			}
			call, dispatched := dispatchCall(ctx)
			if !dispatched {
				if deny := services.CheckGate(ctx, "bash", a.Command); deny != "" {
					return "", errors.New(deny)
				}
			}
			dur := time.Duration(a.Timeout * float64(time.Second))

			// Interactive mode hands the live terminal to the user only when the
			// TUI has wired a runner. Without it we run non-interactively, which
			// fails sudo-style prompts fast instead of hanging on whip's tty.
			services.mu.RLock()
			interactive := services.interactive
			services.mu.RUnlock()
			processOpts := services.ProcessOptions()
			processOpts.Command = a.Command
			processOpts.Cwd = call.WorkingDir
			if processOpts.Cwd == "" {
				processOpts.Cwd = call.CanonicalRoot
			}
			processOpts.Timeout = dur
			if a.Interactive && interactive != nil {
				keys := make(chan []byte, 16)
				processOpts.Keys = keys
				out := interactive.Run(ctx, processOpts)
				return TruncateTail(out), nil
			}

			var onUpdate func(string)
			if cb, ok := ctx.Value(updateKey{}).(func(string)); ok {
				onUpdate = cb
			}
			processOpts.OnUpdate = onUpdate
			res := bashrun.Run(ctx, processOpts)
			if result, ok := ctx.Value(bashResultKey{}).(*bashrun.Result); ok {
				*result = res
			}

			s := TruncateTail(res.Output)
			if len(res.Output) > maxOutput {
				// The model only sees the tail; give it a way to reach the
				// rest (pi spills truncated bash output to a file too).
				if path := bashrun.Spill(res.Output); path != "" {
					s += fmt.Sprintf("\n[full output (%d bytes): %s]", len(res.Output), path)
				}
			}
			if res.TimedOut {
				return s + "\n(command timed out)", nil
			}
			if res.Exit != "" {
				return fmt.Sprintf("%s\n(%s)", s, res.Exit), nil
			}
			if s == "" {
				return "(no output)", nil
			}
			return s, nil
		},
	}
}

func readTool() Tool {
	return Tool{
		Def: llm.NewTool("read",
			"Read a file and return its contents with line numbers.",
			`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file"},"offset":{"type":"number","description":"1-based line to start from"},"limit":{"type":"number","description":"Max lines to return (default 2000)"}},"required":["path"]}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Path   string `json:"path"`
				Offset int    `json:"offset"`
				Limit  int    `json:"limit"`
				Mode   string `json:"_rlm_mode"`
				Query  string `json:"query"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return "", err
			}
			actualPath := a.Path
			if call, ok := dispatchCall(ctx); ok {
				actualPath = call.CanonicalPath
			}
			switch a.Mode {
			case "list":
				entries, err := os.ReadDir(actualPath)
				if err != nil {
					return "", err
				}
				var output strings.Builder
				for _, entry := range entries[:min(len(entries), 2_000)] {
					name := entry.Name()
					if entry.IsDir() {
						name += "/"
					}
					output.WriteString(name + "\n")
				}
				return truncate(output.String()), nil
			case "search":
				if a.Query == "" {
					return "", errors.New("query is required")
				}
				return searchFiles(actualPath, a.Query)
			case "":
			default:
				return "", fmt.Errorf("unknown internal read mode %q", a.Mode)
			}
			data, err := os.ReadFile(actualPath) //nolint:gosec // dispatched paths are canonical and capability-authorized
			if err != nil {
				return "", err
			}
			lines := strings.Split(string(data), "\n")
			start := max(a.Offset-1, 0)
			if start >= len(lines) {
				return "", fmt.Errorf("offset %d past end of file (%d lines)", a.Offset, len(lines))
			}
			limit := a.Limit
			if limit <= 0 {
				limit = 2000
			}
			end := min(start+limit, len(lines))
			var b strings.Builder
			for i := start; i < end; i++ {
				fmt.Fprintf(&b, "%d\t%s\n", i+1, lines[i])
			}
			return truncate(b.String()), nil
		},
	}
}

func searchFiles(root, query string) (string, error) {
	const maxScanned = 8 << 20
	var output strings.Builder
	var scanned, matches int
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || scanned >= maxScanned || matches >= 100 {
			return nil
		}
		remaining := maxScanned - scanned
		file, err := os.Open(path) //nolint:gosec // root is dispatcher-canonical and WalkDir does not follow symlinks
		if err != nil {
			return nil //nolint:nilerr // an unreadable search entry does not invalidate other matches
		}
		data, readErr := io.ReadAll(io.LimitReader(file, int64(remaining)))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return nil //nolint:nilerr // an unreadable search entry does not invalidate other matches
		}
		scanned += len(data)
		relative, _ := filepath.Rel(root, path)
		for index, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, query) {
				fmt.Fprintf(&output, "%s:%d:%s\n", relative, index+1, line)
				matches++
				if matches >= 100 {
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return truncate(output.String()), nil
}

func writeTool(services *Services) Tool {
	return Tool{
		Def: llm.NewTool("write",
			"Write content to a file, creating it (and parent directories) or overwriting it.",
			`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file"},"content":{"type":"string","description":"Full file content"}},"required":["path","content"]}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return "", err
			}
			call, dispatched := dispatchCall(ctx)
			if !dispatched {
				if deny := services.CheckGate(ctx, "write", a.Path); deny != "" {
					return "", errors.New(deny)
				}
			}
			actualPath := a.Path
			if dispatched {
				actualPath = call.CanonicalPath
			}
			// old content (if any) so an overwrite reports what changed
			old, oldErr := os.ReadFile(actualPath) //nolint:gosec // dispatched paths are canonical and capability-authorized
			//nolint:gosec // workspace files get the user default perms
			if err := os.MkdirAll(filepath.Dir(actualPath), 0o755); err != nil {
				return "", err
			}
			//nolint:gosec // workspace files get the user default perms
			if err := os.WriteFile(actualPath, []byte(a.Content), 0o644); err != nil {
				return "", err
			}
			out := fmt.Sprintf("Wrote %d bytes to %s", len(a.Content), a.Path)
			// overwrites carry a diff so the change is reviewable (a fresh file
			// is all-new — the content itself is right above in the call args).
			// The whole file diffs from line 1, so the numbers are absolute.
			if oldErr == nil {
				if d := editDiff(string(old), a.Content, 1); d != "" {
					out += "\n```diff\n" + d + "\n```"
				}
			}
			return out + services.lspDiagnostics(ctx, actualPath), nil
		},
	}
}

func editTool(services *Services) Tool {
	return Tool{
		Def: llm.NewTool("edit",
			"Replace an exact string in a file. old_string must appear exactly once unless replace_all is true.",
			`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file"},"old_string":{"type":"string","description":"Exact text to replace"},"new_string":{"type":"string","description":"Replacement text"},"replace_all":{"type":"boolean","description":"Replace every occurrence"}},"required":["path","old_string","new_string"]}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Path       string `json:"path"`
				OldString  string `json:"old_string"`
				NewString  string `json:"new_string"`
				ReplaceAll bool   `json:"replace_all"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return "", err
			}
			call, dispatched := dispatchCall(ctx)
			if !dispatched {
				if deny := services.CheckGate(ctx, "edit", a.Path); deny != "" {
					return "", errors.New(deny)
				}
			}
			actualPath := a.Path
			if dispatched {
				actualPath = call.CanonicalPath
			}
			data, err := os.ReadFile(actualPath) //nolint:gosec // dispatched paths are canonical and capability-authorized
			if err != nil {
				return "", err
			}
			s := string(data)
			n := strings.Count(s, a.OldString)
			switch {
			case n == 0:
				return "", fmt.Errorf("old_string not found in %s", a.Path)
			case n > 1 && !a.ReplaceAll:
				return "", fmt.Errorf("old_string appears %d times in %s; make it unique or set replace_all", n, a.Path)
			}
			s = strings.ReplaceAll(s, a.OldString, a.NewString)
			//nolint:gosec // workspace files get the user default perms
			if err := os.WriteFile(actualPath, []byte(s), 0o644); err != nil {
				return "", err
			}
			out := fmt.Sprintf("Replaced %d occurrence(s) in %s", n, a.Path)
			// line numbers are only meaningful for a single occurrence; a
			// replace_all diff renders unnumbered (startLine 0)
			startLine := 0
			if n == 1 {
				startLine = 1 + strings.Count(string(data)[:strings.Index(string(data), a.OldString)], "\n")
			}
			if d := editDiff(a.OldString, a.NewString, startLine); d != "" {
				out += "\n```diff\n" + d + "\n```"
			}
			return out + services.lspDiagnostics(ctx, actualPath), nil
		},
	}
}

// editDiff renders the changed region of an edit as a compact unified-ish
// diff: one line of common context on each side of the first/last changed
// lines, "- old"/"+ new" pairs in between. "" when old and new are identical.
//
// startLine is the 1-based file line the old snippet starts on; when > 0
// every row is prefixed with its absolute line number ("1528 - old",
// "1528 + new", "1527   ctx" — removed lines numbered in the old file, added
// lines in the new one). 0 renders the unnumbered form ("- old" / "+ new").
// The diff is capped at editDiffMaxLines rows; the marker names the rest.
func editDiff(oldS, newS string, startLine int) string {
	o := strings.Split(strings.TrimSuffix(oldS, "\n"), "\n")
	n := strings.Split(strings.TrimSuffix(newS, "\n"), "\n")
	p := 0
	for p < len(o) && p < len(n) && o[p] == n[p] {
		p++
	}
	s := 0
	for s < len(o)-p && s < len(n)-p && o[len(o)-1-s] == n[len(n)-1-s] {
		s++
	}
	if p == len(o) && p == len(n) {
		return ""
	}
	var b strings.Builder
	rows := 0
	row := func(num int, mark, line string) {
		rows++
		if rows > editDiffMaxLines {
			return
		}
		if len(line) > 200 {
			line = line[:200] + "…"
		}
		if startLine > 0 {
			fmt.Fprintf(&b, "%d %s %s\n", num, mark, line)
		} else {
			fmt.Fprintf(&b, "%s %s\n", mark, line)
		}
	}
	if p > 0 {
		row(startLine+p-1, " ", o[p-1])
	}
	for i, l := range o[p : len(o)-s] {
		row(startLine+p+i, "-", l)
	}
	for i, l := range n[p : len(n)-s] {
		row(startLine+p+i, "+", l)
	}
	if s > 0 {
		row(startLine+len(o)-1, " ", o[len(o)-1])
	}
	out := strings.TrimSuffix(b.String(), "\n")
	if rows > editDiffMaxLines {
		out += fmt.Sprintf("\n… +%d more lines", rows-editDiffMaxLines)
	}
	return out
}

// editDiffMaxLines bounds a diff so a whole-file rewrite can't flood the tool
// result (the full content is already in the call args).
const editDiffMaxLines = 200
