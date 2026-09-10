package rlm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/context-labs/whip/internal/tools"
)

type hostCompletion struct {
	request frame
	call    HostCall
	value   any
	err     error
}

func (kernel *Kernel) completeHost(completion hostCompletion) {
	if kernel.onHostCall == nil {
		return
	}
	call := completion.call
	call.Status = "completed"
	if completion.err != nil {
		call.Err = completion.err.Error()
		call.Status = "failed"
		if errors.Is(completion.err, context.Canceled) {
			call.Status = "cancelled"
		}
	}
	kernel.onHostCall(call)
}

// The pump owns callbacks and the writer. A single process reader continues to
// receive output and independent requests while bounded host goroutines run.
func (kernel *Kernel) evalQuickJSLocked(ctx context.Context, code string) (Result, error) {
	kernel.nextID++
	id := kernel.nextID
	if err := writeFrame(kernel.worker.input, kernel.limits.FrameBytes, frame{Type: "eval", ID: id, Code: code}); err != nil {
		kernel.stop()
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	pending := make(map[uint64]bool)
	seen := make(map[uint64]bool)
	completed := make(chan hostCompletion, maxOutstandingCalls)
	serial := make(chan struct{}, 1)
	admission := make(chan struct{}, kernel.limits.MaxConcurrentHostCalls)
	// Accepted calls always settle under their cancellation context; never replay
	// them to repair a lost guest or checkpoint. The bounded channel lets every
	// producer exit even while worker shutdown completes.
	defer func() {
		cancel()
		for len(pending) > 0 {
			out := <-completed
			delete(pending, out.request.ID)
			kernel.completeHost(out)
		}
	}()
	process := kernel.worker
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	last := time.Now()
	var compute time.Duration
	onUpdate, callID := tools.OnUpdate(ctx), tools.ToolCallID(ctx)
	fail := func(err error) (Result, error) { kernel.stop(); return Result{}, err }
	for {
		now := time.Now()
		if len(pending) == 0 {
			compute += now.Sub(last)
		}
		last = now
		if compute > kernel.limits.Wall {
			return fail(errors.New("QuickJS cell compute deadline exceeded"))
		}
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-ticker.C:
			if resident, err := residentBytes(process.command.Process.Pid); err == nil && resident > kernel.limits.MemoryBytes {
				return fail(ErrMemoryLimit)
			}
		case out := <-completed:
			delete(pending, out.request.ID)
			kernel.completeHost(out)
			reply := frame{Type: "host_response", ID: out.request.ID, CellID: id, Value: out.value}
			if out.err != nil {
				reply.Value = nil
				reply.Error = out.err.Error()
			}
			if err := writeFrame(process.input, kernel.limits.FrameBytes, reply); err != nil {
				return fail(err)
			}
		case incoming := <-process.frames:
			if incoming.err != nil {
				if errors.Is(incoming.err, io.EOF) {
					<-process.done
				}
				if detail := process.stderr.String(); detail != "" {
					return fail(fmt.Errorf("RLM worker exited: %s", detail))
				}
				return fail(incoming.err)
			}
			response := incoming.frame
			switch response.Type {
			case "output":
				if response.ID != id {
					return fail(errors.New("stale QuickJS output"))
				}
				if onUpdate != nil {
					onUpdate(response.Output)
				}
			case "host_request":
				if response.CellID != id || response.ID == 0 || seen[response.ID] {
					return fail(errors.New("stale or duplicate QuickJS host request"))
				}
				if len(pending) >= maxOutstandingCalls || len(seen) >= kernel.limits.HostRequests {
					return fail(errors.New("QuickJS host request limit exceeded"))
				}
				if err := validateHostOperation(response.Module, response.Operation, kernel.tools); err != nil {
					return fail(err)
				}
				seen[response.ID], pending[response.ID] = true, true
				call := HostCall{CallID: callID, InvocationID: fmt.Sprintf("%d:%d", id, response.ID), Module: response.Module, Operation: response.Operation, Summary: hostCallSummary(response.Arguments)}
				if kernel.onHostStart != nil {
					kernel.onHostStart(call)
				}
				go func() {
					start := time.Now()
					out := hostCompletion{request: response, call: call}
					select {
					case admission <- struct{}{}:
						defer func() { <-admission }()
					case <-ctx.Done():
						out.err = ctx.Err()
					}
					if out.err == nil && serializedHostOperation(response.Module, response.Operation) {
						select {
						case serial <- struct{}{}:
							defer func() { <-serial }()
						case <-ctx.Done():
							out.err = ctx.Err()
						}
					}
					if out.err == nil {
						if kernel.host == nil {
							out.err = errors.New("RLM host is not bound")
						} else {
							out.value, out.err = kernel.host.Call(ctx, response.Module, response.Operation, response.Arguments)
						}
					}
					out.call.Duration = time.Since(start)
					completed <- out
				}()
			case "result":
				if response.ID != id || len(pending) != 0 {
					return fail(errors.New("QuickJS result before owned host calls settled"))
				}
				result := Result{Termination: response.Termination, Value: response.Value, Output: response.Output, HasValue: response.HasValue, Metrics: map[string]uint64{"quickjs_jobs": response.Jobs, "guest_compute_ns": response.ComputeNanos, "host_wait_ns": response.HostWaitNanos}}
				if response.Error != "" {
					if response.Termination != "" {
						kernel.stop()
					}
					return result, errors.New(response.Error)
				}
				return result, nil
			default:
				return fail(fmt.Errorf("unexpected QuickJS worker frame %q", response.Type))
			}
		}
	}
}

func serializedHostOperation(module, operation string) bool {
	switch module {
	case "browser", "computer", "user", "permissions", "state", "messages", "schedules":
		return true
	case "agents":
		return operation != "wait" && operation != "inspect" && operation != "list"
	}
	return false
}
