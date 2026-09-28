package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/content"
	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tool"
)

// A live kernel is a disposable cache owned by its session's runtime. The store
// owns its last execution boundary; no worker image is authoritative in memory.
type sessionKernel struct {
	mu          sync.Mutex
	kernel      *process.Kernel
	checkpoints *cellCheckpoints
}

type cellCheckpoints struct {
	runtime    *Runtime
	sessionID  session.SessionID
	descriptor process.EngineDescriptor
	candidate  *session.Checkpoint
}

func (c *cellCheckpoints) Load(ctx context.Context) (*process.Checkpoint, error) {
	latest, err := c.runtime.store.LatestCell(ctx, c.sessionID)
	if err != nil || latest == nil {
		return nil, err
	}
	if latest.Checkpoint == nil {
		return nil, errors.New("REPL checkpoint unavailable at latest execution boundary; explicit recovery is required")
	}
	saved := latest.Checkpoint
	if err := saved.Validate(); err != nil {
		return nil, err
	}
	data, err := c.runtime.content.ReadVerified(content.Body{Digest: saved.Digest, Size: saved.Size}, process.MaxCheckpointBytes)
	if err != nil {
		return nil, err
	}
	checkpoint := &process.Checkpoint{Data: data}
	if err := json.Unmarshal(saved.Metadata, &checkpoint.Envelope); err != nil {
		return nil, err
	}
	if checkpoint.Envelope.SHA256 != saved.Digest || int64(checkpoint.Envelope.Bytes) != saved.Size || checkpoint.Envelope.Engine != string(saved.Engine) {
		return nil, errors.New("checkpoint metadata does not match committed body")
	}
	if err := checkpoint.Validate(c.descriptor); err != nil {
		return nil, err
	}
	return checkpoint, nil
}

// Save publishes immutable bytes only. The candidate becomes a restore target
// only when SettleCell commits the result message and boundary in one transaction.
func (c *cellCheckpoints) Save(ctx context.Context, checkpoint process.Checkpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkpoint.Validate(c.descriptor); err != nil {
		return err
	}
	metadata, err := json.Marshal(checkpoint.Envelope)
	if err != nil {
		return err
	}
	candidate := &session.Checkpoint{Digest: checkpoint.Envelope.SHA256, Size: int64(len(checkpoint.Data)), Engine: session.Engine(c.descriptor.ID), Metadata: metadata}
	if err := candidate.Validate(); err != nil {
		return err
	}
	if _, err := c.runtime.content.Put(checkpoint.Data); err != nil {
		return err
	}
	c.candidate = candidate
	return nil
}

func (r *Runtime) Instructions(ctx context.Context, id session.SessionID) (string, error) {
	current, err := r.store.Session(ctx, id)
	if err != nil {
		return "", err
	}
	tree, err := r.store.Tree(ctx, current.TreeID)
	if err != nil {
		return "", err
	}
	language := "Starlark (Python-like syntax; print for output)"
	if tree.Engine == session.QuickJS {
		language = "JavaScript (QuickJS; top-level await is supported; console.log for output)"
	}
	instructions := "The execute tool runs " + language + " in a persistent isolated REPL. Variables survive cells and turns. Host operations are separately authorized; no ambient filesystem, network, or process access is available. A failed cell can have partially changed variables or completed effects. Never replay effects merely because a checkpoint or connection failed."
	if tree.Engine == session.Starlark {
		instructions += " Available workspace operations: files.read(path=\"relative/path\", offset=1, limit=2000), files.write(path=\"relative/path\", content=\"text\"), files.patch(path=\"relative/path\", old_text=\"old\", new_text=\"new\", replace_all=False)."
	} else {
		instructions += " Available workspace operations: await files.read({path: \"relative/path\", offset: 1, limit: 2000}), await files.write({path: \"relative/path\", content: \"text\"}), await files.patch({path: \"relative/path\", old_text: \"old\", new_text: \"new\", replace_all: false})."
	}
	instructions += " File operations are confined to the session workspace and may wait for an explicit permission decision. An approval authorizes that operation only."
	return instructions, nil
}

func (r *Runtime) kernel(ctx context.Context, id session.SessionID) (*sessionKernel, error) {
	r.mu.Lock()
	existing := r.kernels[id]
	r.mu.Unlock()
	if existing != nil {
		return existing, nil
	}
	current, err := r.store.Session(ctx, id)
	if err != nil {
		return nil, err
	}
	tree, err := r.store.Tree(ctx, current.TreeID)
	if err != nil {
		return nil, err
	}
	descriptor, err := process.ResolveExecutionEngine(string(tree.Engine))
	if err != nil {
		return nil, err
	}
	checkpoints := &cellCheckpoints{runtime: r, sessionID: id, descriptor: descriptor}
	kernel, err := process.NewKernel(process.KernelOptions{Engine: string(tree.Engine), Checkpoints: checkpoints, Manager: r.engineManager, Command: r.options.EngineCommand, Limits: process.Limits{OutputBytes: 64 << 10}, Host: process.HostFunc(func(ctx context.Context, module, operation string, arguments map[string]any) (any, error) {
		call, ok := process.HostCallFromContext(ctx)
		if !ok {
			return nil, errors.New("missing host invocation identity")
		}
		value, operationID, err := r.tools.Call(ctx, tool.Invocation{SessionID: id, CellID: session.CellID(call.CallID), RequestID: call.InvocationID, Module: module, Name: operation, Arguments: arguments})
		if operationID != "" {
			process.ReportHostOperation(ctx, string(operationID))
		}
		return value, err
	})})
	if err != nil {
		return nil, err
	}
	created := &sessionKernel{kernel: kernel, checkpoints: checkpoints}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		kernel.Close()
		return nil, ErrClosed
	}
	if existing = r.kernels[id]; existing == nil {
		r.kernels[id] = created
	}
	r.mu.Unlock()
	if existing != nil {
		kernel.Close()
		return existing, nil
	}
	return created, nil
}

func (r *Runtime) discardKernel(id session.SessionID, entry *sessionKernel) {
	r.mu.Lock()
	if r.kernels[id] == entry {
		delete(r.kernels, id)
	}
	r.mu.Unlock()
	entry.kernel.Close()
}

// Execute is called only after the assistant's call is durably committed. It
// holds capacity through SQL settlement, then releases it before another model
// request or child wait can consume the turn's lifetime.
func (r *Runtime) Execute(ctx context.Context, turn session.Turn, messageID session.MessageID, call session.ToolCall) (session.ToolResult, error) {
	result := session.ToolResult{CallID: call.ID}
	if call.Name != "execute" {
		return result, errors.New("unsupported tool")
	}
	var arguments struct {
		Code string `json:"code"`
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&arguments); err != nil {
		return result, fmt.Errorf("invalid execute arguments: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, errors.New("trailing execute arguments")
	}
	if err := session.ValidateText(arguments.Code, 256<<10); err != nil {
		return result, err
	}
	entry, err := r.kernel(ctx, turn.SessionID)
	if err != nil {
		return result, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	latest, err := r.store.LatestCell(ctx, turn.SessionID)
	if err != nil {
		return result, err
	}
	entry.checkpoints.candidate = nil
	if latest != nil {
		if latest.Checkpoint == nil {
			return result, errors.New("REPL checkpoint unavailable at latest execution boundary; explicit recovery is required")
		}
		entry.checkpoints.candidate = latest.Checkpoint
	}
	cellCtx, start, release, err := entry.kernel.AcquireTurn(ctx)
	if err != nil {
		r.discardKernel(turn.SessionID, entry)
		return result, err
	}
	defer release()
	digest := sha256.Sum256([]byte(string(messageID) + "\x00" + call.ID))
	id := session.CellID("cell_" + hex.EncodeToString(digest[:]))
	_, dispatch, err := r.store.BeginCell(ctx, session.CellSpec{ID: id, TurnID: turn.ID, CallMessageID: messageID, CallID: call.ID})
	if err != nil || !dispatch {
		if err == nil {
			err = errors.New("cell already admitted; automatic replay prohibited")
		}
		return result, err
	}
	evaluated, executionErr := entry.kernel.Exec(cellCtx, process.Cell{Code: arguments.Code, CallID: string(id)})
	if evaluated.Restored == nil {
		evaluated.Restored = start.Restore
	}
	state := session.CellSucceeded
	if executionErr != nil {
		state = session.CellFailed
		result.IsError = true
	}
	checkpoint := entry.checkpoints.candidate
	if !evaluated.Settled {
		state = session.CellUncertain
		result.IsError = true
		checkpoint = nil
	}
	if evaluated.Termination != "" || (evaluated.Scratch != nil && evaluated.Scratch.Warning != "") {
		checkpoint = nil
	}
	result.Output = cellOutput(evaluated, executionErr)
	if err := r.settleCell(ctx, id, state, result, checkpoint); err != nil {
		r.discardKernel(turn.SessionID, entry)
		return result, err
	}
	if checkpoint == nil {
		r.discardKernel(turn.SessionID, entry)
		return result, errors.New("cell result recorded but its REPL checkpoint is unavailable; explicit recovery is required")
	}
	return result, nil
}

func cellOutput(result process.Result, err error) string {
	var failure *string
	if err != nil {
		failure = runner.Failure(err).Failure
	}
	raw, encodeErr := json.Marshal(struct {
		Result process.Result `json:"result"`
		Error  *string        `json:"error,omitempty"`
	}{result, failure})
	if encodeErr != nil {
		return `{"error":"cell output could not be encoded"}`
	}
	if len(raw) > 256<<10 {
		return `{"error":"cell output exceeded the response limit; values were omitted"}`
	}
	return string(raw)
}

func (r *Runtime) settleCell(parent context.Context, id session.CellID, state session.CellState, result session.ToolResult, checkpoint *session.Checkpoint) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, err := r.store.SettleCell(ctx, id, state, result, checkpoint)
		if err == nil || errors.Is(err, session.ErrInvalid) {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), err)
		case <-ticker.C:
		}
	}
}
