package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/bashrun"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/shell"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

const shellInlineBytes = 8 << 10

func (r *Runtime) ShellInteraction(ctx context.Context, owner session.SessionID, cursor int64) (*shell.InteractiveView, error) {
	if _, err := r.store.Session(ctx, owner); err != nil {
		return nil, err
	}
	return r.shells.Interaction(string(owner), cursor)
}

// ShellInput is a human control, independent of model capability grants. It
// cannot address another operation or carry keystrokes across process restart.
func (r *Runtime) ShellInput(ctx context.Context, owner session.SessionID, operation session.OperationID, sequence int64, data []byte) error {
	if _, err := r.store.Session(ctx, owner); err != nil {
		return err
	}
	return r.shells.Input(string(owner), string(operation), sequence, data)
}

type shellRequest struct {
	Command     string  `json:"command,omitempty"`
	Timeout     float64 `json:"timeout,omitempty"`
	Interactive bool    `json:"interactive,omitempty"`
	ID          string  `json:"id,omitempty"`
	WaitMillis  int     `json:"timeout_ms,omitempty"`
	Bytes       int     `json:"bytes,omitempty"`
}

func parseShellRequest(name string, args map[string]any) (shellRequest, error) {
	var request shellRequest
	if err := decodeArguments(args, &request); err != nil {
		return request, err
	}
	allowed := map[string]bool{}
	switch name {
	case "run", "start":
		allowed["command"], allowed["timeout"] = true, true
		if name == "run" {
			allowed["interactive"] = true
		}
		if err := session.ValidateText(request.Command, 64<<10); err != nil || strings.TrimSpace(request.Command) == "" || strings.ContainsRune(request.Command, 0) {
			return request, session.ErrInvalid
		}
		maxTimeout := float64(86400)
		if name == "run" {
			maxTimeout = 120
			if request.Timeout == 0 {
				request.Timeout = 120
			}
		}
		if math.IsNaN(request.Timeout) || math.IsInf(request.Timeout, 0) || request.Timeout < 0 || request.Timeout > 0 && request.Timeout < 0.001 || request.Timeout > maxTimeout {
			return request, session.ErrInvalid
		}
	case "poll", "kill", "wait", "tail":
		allowed["id"] = true
		if err := session.ValidateID(request.ID); err != nil {
			return request, err
		}
		if name == "wait" {
			allowed["timeout_ms"] = true
			if _, ok := args["timeout_ms"]; !ok {
				request.WaitMillis = 10000
			}
			if request.WaitMillis < 0 || request.WaitMillis > 25000 {
				return request, session.ErrInvalid
			}
		}
		if name == "tail" {
			allowed["bytes"] = true
			if _, ok := args["bytes"]; !ok {
				request.Bytes = 4096
			}
			if request.Bytes < 1 || request.Bytes > shellInlineBytes {
				return request, session.ErrInvalid
			}
		}
	case "list":
	default:
		return request, session.ErrInvalid
	}
	for key := range args {
		if !allowed[key] {
			return request, session.ErrInvalid
		}
	}
	return request, nil
}

func (r *Runtime) prepareShell(current session.Session, call tool.Invocation) (tool.Prepared, error) {
	if call.Name == "read" {
		return r.prepareArtifactRead(current, call)
	}
	request, err := parseShellRequest(call.Name, call.Arguments)
	if err != nil {
		return tool.Prepared{}, err
	}
	arguments, err := json.Marshal(request)
	if err != nil {
		return tool.Prepared{}, err
	}
	scope, err := r.shells.Capture(string(current.ID))
	if err != nil {
		return tool.Prepared{}, err
	}
	prepared := tool.Prepared{Capability: "shell." + call.Name, Resource: current.WorkingDirectory, Arguments: arguments, Lifetime: scope.Context(), Mutating: call.Name == "run" || call.Name == "start" || call.Name == "kill"}
	if call.Name != "run" && call.Name != "start" {
		prepared.Acquire = func(ctx context.Context) (func(), error) { return func() {}, ctx.Err() }
		prepared.Run = func(ctx context.Context, id session.OperationID) (any, error) {
			return r.shellJobOperation(ctx, current.ID, id, scope, call.Name, request)
		}
		return prepared, nil
	}
	cwd, err := filepath.EvalSymlinks(current.WorkingDirectory)
	if err != nil {
		return tool.Prepared{}, err
	}
	identity, err := os.Stat(cwd)
	if err != nil || !identity.IsDir() {
		return tool.Prepared{}, errors.Join(session.ErrInvalid, err)
	}
	var reservation *shell.Reservation
	prepared.Acquire = func(ctx context.Context) (func(), error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rechecked, err := filepath.EvalSymlinks(current.WorkingDirectory)
		if err != nil || rechecked != cwd {
			return nil, errors.Join(session.ErrInvalid, err)
		}
		info, err := os.Stat(cwd)
		if err != nil || !os.SameFile(identity, info) {
			return nil, errors.Join(session.ErrInvalid, err)
		}
		reservation, err = scope.Reserve(call.Name == "start")
		if err != nil {
			return nil, err
		}
		return reservation.Release, nil
	}
	if call.Name == "run" {
		prepared.Timeout = time.Duration(request.Timeout * float64(time.Second))
	}
	prepared.Run = func(ctx context.Context, id session.OperationID) (any, error) {
		options := bashrun.Options{Command: request.Command, Cwd: cwd, CwdIdentity: identity, Timeout: time.Duration(request.Timeout * float64(time.Second)), Env: bashrun.Markers(string(current.ID), current.Config.Model.Name)}
		if call.Name == "start" {
			job, err := reservation.Start(ctx, string(id), options)
			if err != nil {
				return nil, tool.SettledFailure(err)
			}
			return shellJobView(string(id), job), nil
		}
		if request.Interactive {
			interaction, err := reservation.Interact(string(id))
			if err != nil {
				return nil, tool.SettledFailure(err)
			}
			defer interaction.Close()
			options.Interactive, options.Keys = true, interaction.Keys()
			options.OnOutput, options.OnAwaitInput = interaction.Output, interaction.AwaitInput
		}
		result, err := reservation.Run(ctx, options)
		if err != nil {
			return nil, tool.SettledFailure(err)
		}
		if result.StartError != nil {
			return nil, tool.SettledFailure(result.StartError)
		}
		view := map[string]any{"exit": result.Exit, "timed_out": result.TimedOut, "killed": result.Killed, "interactive": result.Interactive}
		r.shellOutput(ctx, current.ID, id, view, result.Output, result.TotalBytes)
		if result.Killed || result.TimedOut {
			return view, errors.New("shell process stopped; prior external effects may have completed")
		}
		return view, nil
	}
	return prepared, nil
}

func shellJobView(id string, job *bashrun.Job) map[string]any {
	_, total := job.Output(1)
	command := job.Command
	if len(command) > 256 {
		command = command[:256]
	}
	view := map[string]any{"id": id, "pid": job.PID(), "command": strings.ToValidUTF8(command, "�"), "command_truncated": len(command) < len(job.Command), "running": job.Running(), "bytes": strconv.Itoa(total), "started_at": job.Started.UTC().Format(time.RFC3339Nano), "exit": job.Exit(), "killed": job.Killed(), "ended_at": nil}
	if ended := job.Ended(); !ended.IsZero() {
		view["ended_at"] = ended.UTC().Format(time.RFC3339Nano)
	}
	return view
}

func (r *Runtime) shellJobOperation(ctx context.Context, owner session.SessionID, _ session.OperationID, scope *shell.Scope, name string, request shellRequest) (any, error) {
	if name == "list" {
		ids, err := scope.JobIDs()
		if err != nil {
			return nil, err
		}
		items := make([]any, 0, len(ids))
		for _, id := range ids {
			job, err := scope.Job(id)
			if errors.Is(err, shell.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			items = append(items, shellJobView(id, job))
		}
		return map[string]any{"items": items}, nil
	}
	job, err := scope.Job(request.ID)
	if err != nil {
		return nil, err
	}
	timedOut := false
	if name == "kill" {
		if err := job.Kill(); err != nil {
			return nil, err
		}
	}
	if name == "wait" && job.Running() {
		timer := time.NewTimer(time.Duration(request.WaitMillis) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-job.Done():
		case <-timer.C:
			timedOut = true
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	view := shellJobView(request.ID, job)
	if name == "wait" {
		view["timed_out"] = timedOut
	}
	if name == "tail" {
		text, total := job.Output(request.Bytes)
		view["tail"] = strings.ToValidUTF8(text, "�")
		view["bytes"] = strconv.Itoa(total)
		view["truncated"] = total > len(text)
	} else if !job.Running() {
		text, total := job.Output(0)
		r.shellOutput(ctx, owner, session.OperationID(request.ID), view, text, int64(total))
	}
	return view, nil
}

// Output retention failure does not turn a completed command into a replayable
// effect. Keep its exit/evidence and say exactly which bytes were retained.
func (r *Runtime) shellOutput(ctx context.Context, owner session.SessionID, id session.OperationID, view map[string]any, text string, total int64) {
	view["bytes"], view["retained_bytes"], view["truncated"], view["content_ref"] = strconv.FormatInt(total, 10), strconv.Itoa(len(text)), total > int64(len(text)), nil
	inline := text
	if len(inline) > shellInlineBytes {
		inline = inline[len(inline)-shellInlineBytes:]
	}
	view["output"] = strings.ToValidUTF8(inline, "�")
	view["inline_truncated"] = len(inline) < len(text)
	if len(text) <= shellInlineBytes {
		return
	}
	retention, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	ref, err := r.PutContent(retention, owner, string(id)+"_output", "application/octet-stream", []byte(text))
	if err != nil {
		view["output_notice"] = "retained output could not be published; only the displayed tail is available"
		return
	}
	view["content_ref"] = ref.ID
}

func (r *Runtime) cleanupShellOwners(ctx context.Context) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, id := range r.shells.Owners() {
		current, err := r.store.Session(cleanup, session.SessionID(id))
		if errors.Is(err, store.ErrNotFound) || err == nil && current.Lifecycle == session.Stopped {
			r.shells.Retire(id)
		} else if err != nil {
			return err
		}
	}
	return nil
}
