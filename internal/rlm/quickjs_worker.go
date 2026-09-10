package rlm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/rlm/engine"
	"github.com/context-labs/whip/internal/rlm/engine/quickjs"
)

const maxOutstandingCalls = 16
const maxQuickJSJobs = 100000

func runQuickJSWorker(input io.Reader, output io.Writer, limits Limits, modules, tools []string) error {
	ctx := context.Background() // The supervisor owns the subprocess lifetime.
	allowed := make([]string, 0)
	for module, operations := range selectedOperations(modules, tools) {
		for _, operation := range operations {
			allowed = append(allowed, module+"."+operation)
		}
	}
	factory, err := quickjs.NewFactory(ctx, engine.Options{AllowedTools: allowed, Limits: engine.Limits{
		MemoryBytes: min(limits.MemoryBytes/4, 32<<20), MemoryPages: 1024, MaxQueuedRequests: maxOutstandingCalls,
		MaxRequestBytes: limits.FrameBytes / 2, MaxResultBytes: limits.FrameBytes / 2, MaxOutputBytes: limits.OutputBytes, MaxSnapshotBytes: MaxCheckpointBytes,
	}})
	if err != nil {
		return err
	}
	defer factory.Close(ctx)
	runtime, err := factory.New(ctx, "whip-kernel")
	if err != nil {
		return err
	}
	defer func() { _ = runtime.Close(ctx) }()
	reader := bufio.NewReaderSize(input, min(limits.FrameBytes, 64<<10))
	descriptor, _ := ResolveEngine(EngineQuickJS)
	for {
		request, err := readFrame(reader, limits.FrameBytes, true)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if request.ID == 0 {
			return errors.New("missing RLM request ID")
		}
		result := frame{Type: "result", ID: request.ID}
		switch request.Type {
		case "hello":
			if request.Engine != descriptor.ID || request.Build != descriptor.Build || request.ABI != descriptor.ABI || request.Profile != descriptor.Profile {
				return errors.New("RLM engine handshake mismatch")
			}
			result.Engine, result.Build, result.ABI, result.Profile = descriptor.ID, descriptor.Build, descriptor.ABI, descriptor.Profile
		case "eval":
			result, err = evaluateQuickJS(ctx, runtime, reader, output, request, limits)
			if err != nil {
				result.Error, result.Termination = err.Error(), "worker_terminated"
				if strings.Contains(err.Error(), "stalled") {
					result.Termination = "stalled"
				}
				if strings.Contains(err.Error(), "limit") {
					result.Termination = "resource_limit"
				}
				if writeErr := writeFrame(output, limits.FrameBytes, result); writeErr != nil {
					return writeErr
				}
				return err
			}
		case "checkpoint":
			data, captureErr := runtime.Checkpoint(ctx)
			if captureErr != nil {
				result.Error = captureErr.Error()
				break
			}
			if err := writeBlob(output, limits.FrameBytes, request.ID, data); err != nil {
				return err
			}
			result.Value = SnapshotManifest{Saved: []string{"<complete JavaScript heap>"}, Bytes: len(data)}
		case "restore_checkpoint":
			data, readErr := readBlob(reader, limits.FrameBytes, request)
			if readErr != nil {
				return readErr
			}
			restored, restoreErr := factory.Restore(ctx, "whip-kernel", data)
			if restoreErr != nil {
				result.Error = restoreErr.Error()
				break
			}
			_ = runtime.Close(ctx)
			runtime = restored
			result.Value = RestoreReport{Restored: []string{"<complete JavaScript heap>"}}
		default:
			return fmt.Errorf("unexpected RLM worker frame %q", request.Type)
		}
		result.ID, result.Type = request.ID, "result"
		if err := writeFrame(output, limits.FrameBytes, result); err != nil {
			return err
		}
	}
}

// All VM calls happen here, on one goroutine and outside imported host stacks.
// The daemon receives owned passive JSON and may reply in any order.
func evaluateQuickJS(ctx context.Context, runtime engine.Runtime, reader *bufio.Reader, output io.Writer, request frame, limits Limits) (frame, error) {
	result := frame{Type: "result", ID: request.ID}
	var compute, hostWait time.Duration
	guestContext := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(ctx, limits.normalized().Wall-compute)
	}
	runCtx, cancel := guestContext()
	started := time.Now()
	runErr := runtime.RunCell(runCtx, strconv.FormatUint(request.ID, 10), request.Code)
	compute += time.Since(started)
	cancel()
	pending := make(map[uint64]string)
	var next uint64
	lastOutput := ""
	for {
		sliceCtx, cancel := guestContext()
		started = time.Now()
		jobs, drainErr := runtime.Drain(sliceCtx, 256)
		compute += time.Since(started)
		cancel()
		if compute >= limits.normalized().Wall {
			return result, errors.New("QuickJS compute limit exceeded; worker discarded")
		}
		result.Jobs += uint64(jobs)
		if drainErr != nil && !errors.Is(drainErr, engine.ErrJobBudget) {
			return result, drainErr
		}
		if result.Jobs > maxQuickJSJobs {
			return result, errors.New("QuickJS job limit exceeded; worker discarded")
		}
		rejected := false
		for _, queued := range runtime.TakeRequests() {
			next++
			if next > uint64(limits.HostRequests) {
				_, err := runtime.Deliver(ctx, engine.Outcome{ID: queued.ID, Error: &engine.RemoteError{Code: "E_LIMIT", Message: "host request limit exceeded"}})
				if err != nil {
					return result, err
				}
				rejected = true
				continue
			}
			var arguments map[string]any
			decoder := json.NewDecoder(bytes.NewReader(queued.Args))
			decoder.UseNumber()
			if err := decoder.Decode(&arguments); err != nil {
				return result, err
			}
			module, operation, _ := strings.Cut(queued.Tool, ".")
			pending[next] = queued.ID
			if err := writeFrame(output, limits.FrameBytes, frame{Type: "host_request", ID: next, CellID: request.ID, Module: module, Operation: operation, Arguments: arguments}); err != nil {
				return result, err
			}
		}
		view, err := runtime.Inspect(ctx)
		if err != nil {
			return result, err
		}
		if view.Output != lastOutput {
			lastOutput = view.Output
			if err := writeFrame(output, limits.FrameBytes, frame{Type: "output", ID: request.ID, Output: view.Output}); err != nil {
				return result, err
			}
		}
		if len(pending) == 0 {
			if rejected {
				// Deliver queues the Promise rejection; drain it before checking for a stall.
				continue
			}
			if errors.Is(drainErr, engine.ErrJobBudget) {
				continue
			}
			if len(view.Pending) > 0 {
				continue
			} // Quota rejections queued local jobs.
			if view.Status == "running" {
				return result, errors.New("QuickJS stalled: unresolved Promise has no owned host work; worker discarded")
			}
			if err := runtime.Finish(ctx); err != nil {
				return result, err
			}
			view, err = runtime.Inspect(ctx)
			if err != nil {
				return result, err
			}
			result.HasValue, result.Output = view.HasValue, view.Output
			result.ComputeNanos, result.HostWaitNanos = uint64(compute), uint64(hostWait)
			if len(view.Value) > 0 {
				if err := json.Unmarshal(view.Value, &result.Value); err != nil {
					return result, err
				}
			}
			if view.Error != nil {
				result.Error = view.Error.Code + ": " + view.Error.Message
			} else if runErr != nil {
				result.Error = runErr.Error()
			}
			return result, nil
		}
		started = time.Now()
		response, err := readFrame(reader, limits.FrameBytes, true)
		hostWait += time.Since(started)
		if err != nil {
			return result, err
		}
		guestID, exists := pending[response.ID]
		if response.Type != "host_response" || response.CellID != request.ID || !exists {
			return result, errors.New("stale, duplicate, or mismatched QuickJS host response")
		}
		delete(pending, response.ID)
		outcome := engine.Outcome{ID: guestID, OK: response.Error == ""}
		if outcome.OK {
			outcome.Value, err = json.Marshal(response.Value)
			if err != nil {
				return result, err
			}
		} else {
			outcome.Error = &engine.RemoteError{Code: "E_HOST", Message: response.Error}
		}
		if err := runtime.ValidateOutcome(ctx, outcome); err != nil {
			outcome = engine.Outcome{ID: guestID, Error: &engine.RemoteError{Code: "E_LIMIT", Message: "host result exceeds guest payload limit"}}
		}
		sliceCtx, cancel = guestContext()
		started = time.Now()
		_, err = runtime.Deliver(sliceCtx, outcome)
		compute += time.Since(started)
		cancel()
		if err != nil {
			return result, err
		}
	}
}
