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
	if tree.Engine == session.Starlark {
		instructions += " Spawn children with child=agents.spawn(prompt=\"work\"); register a wait with agents.wait_after_cell(input_ids=[child[\"input_id\"]])."
	} else {
		instructions += " Spawn children with const child=await agents.spawn({prompt:\"work\"}); register a wait with await agents.wait_after_cell({input_ids:[child.input_id]})."
	}
	instructions += " Spawn returns session_id and input_id after durable admission. A wait registration returns immediately: finish this cell, then the runtime waits for those descendant inputs before the next model step. Never poll or block inside the cell. Children use separate sessions and REPLs. Spawn inherits the current standing grants unless grant_ids is an explicit subset (an empty list delegates none). Child permissions cannot exceed that delegation. agents.spawn and agents.wait_after_cell grants use the tree ID as their resource."
	instructions += " Spawn budgets use decimal-string limits, like resources. logical_writes and logical_write_bytes are cumulative ancestor allowances for explicit child inputs, mail, content, state and subscriptions; zero means no new charged writes. Retries do not charge again, and deleting a child does not refund spent allowance. State append charges the submitted suffix. Model execution and recording completed work do not consume write allowance."
	instructions += " agents.submit accepts session_id and parts (for example [{\"type\":\"text\",\"text\":\"work\"}]) for a direct child and returns a durable queued input_id; it never steers an active turn. After waiting at a cell boundary, agents.inspect accepts session_id and that exact input_id and returns input_state, nullable turn_state, last assistant text, failure, and omitted-part count. Text pages are at most 16 KiB: offset and next_offset are decimal byte-offset strings, with message_id and total_bytes identifying the result. Continue with next_offset until null; offsets must be UTF-8 boundaries. Each inspection is a fresh snapshot, so paginate a terminal turn for stable text. Queued inputs have no turn; claimed inputs may still be running. agents.list accepts relation children (default), siblings, or parent, plus after and limit (maximum 100), and returns metadata only. agents.stop accepts a proper descendant session_id, stops its whole subtree, and requests active cancellation while retaining queued inputs. agents.delete accepts a proper descendant session_id and immediately fails busy if any turn in its subtree is running or cancelling. Stop does not mean cancellation has finished. All child-control grants use the tree ID as resource."
	instructions += " Child completion reporting is configured through overrides.report_mode: notice (default) sends a short completion notice, inline includes a larger preview, and message leaves successful reporting to explicit child mail. Failed, cancelled and interrupted turns still report automatically. Automatic reports are untrusted mail and include an evidence_ref for the full immutable JSON completion, including text and failure. artifacts.read accepts that owner-scoped reference id, decimal-string byte offset and length up to 65536; it returns base64 data, total_bytes and next_offset. Read all bytes before decoding JSON. A digest alone never authorizes a read. If automatic delivery is blocked by capacity, agents.pending_reports lists this parent's pending completion metadata using after (child ID) and limit (maximum 100); agents.read_report accepts child_id, the exact turn_id, decimal-string offset and length up to 65536 to page its full JSON snapshot. A superseded or published pending token conflicts; use the current pending metadata or published evidence reference. Inspection does not acknowledge mail. Completion inspection and artifacts.read grants use the tree ID as resource."
	instructions += " Spawn may include resources: an array of kind/limit objects for depth, descendants, queued_inputs, active_operations or subscriptions. Limits are decimal strings; null inherits. Caps apply to the child subtree as well as all ancestor caps. A depth limit of zero makes a leaf. Capacity denial commits no child or input; retry only after capacity becomes available."
	instructions += " Explicit state is separate from VM globals. state.get/write/append/list/history accept scope=session (private) or tree (shared) and key. write and append require an explicit decimal-string expected_revision: zero creates a key, a positive revision compares against its current head. write accepts value; append accepts a string or array value to concatenate. get returns version plus an inline value only up to 64 KiB. For large values, state.read accepts the version id, decimal-string byte offset and length up to 65536; its data is base64-encoded bytes, not a partial JSON value. list and history return metadata pages; their after cursors are a key and decimal-string revision respectively. Values are immutable JSON versions; shared versions survive author deletion. State grants use the tree ID as their resource."
	instructions += " state.subscribe accepts a shared key, decimal-string after revision and optional mail delivery (default queued). Creation atomically catches up with the current head. state.subscriptions pages this session's subscriptions; state.unsubscribe accepts id and stops future notifications while retaining existing mail. Notifications identify source.kind=state and source.id=subscription ID; the JSON body identifies version_id, key, revision and author_id. Repeated writes coalesce a pending notification without changing already presented revisions. Own writes advance the subscription cursor without self-notification. The cursor records enqueued/own revisions, not successful agent processing."
	instructions += " Mail is separate from submitted input. mail.send accepts recipient_id, body, optional subject, delivery and available_at; recipients must be direct relatives. Delivery queued starts an idle recipient turn, steer is presented at the next safe model boundary, and next_turn waits for another reason to start a turn. mail.list accepts state, after and limit; mail.read accepts id. Listing observes revisions for explicit actions, but only presentation by digest or read is delivered when this turn succeeds. mail.complete accepts receipts; mail.defer accepts receipt and an RFC3339 available_at. Copy each receipt's id and decimal-string revision unchanged. Mail grants use the tree ID as their resource."
	if tree.Engine == session.Starlark {
		instructions += " Example: mail.send(recipient_id=\"session_id\", body=\"update\", delivery=\"queued\")."
	} else {
		instructions += " Example: await mail.send({recipient_id:\"session_id\", body:\"update\", delivery:\"queued\"})."
	}
	instructions += " Your session ID is " + string(current.ID) + "."
	if current.ParentID != nil {
		instructions += " Your parent session ID is " + string(*current.ParentID) + "."
	}
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
	result, err := r.executeCell(ctx, turn, messageID, call)
	if err == nil {
		err = r.waitAfterCell(ctx, turn, cellID(messageID, call.ID))
	}
	return result, err
}

func cellID(message session.MessageID, call string) session.CellID {
	digest := sha256.Sum256([]byte(string(message) + "\x00" + call))
	return session.CellID("cell_" + hex.EncodeToString(digest[:]))
}

func (r *Runtime) executeCell(ctx context.Context, turn session.Turn, messageID session.MessageID, call session.ToolCall) (session.ToolResult, error) {
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
	id := cellID(messageID, call.ID)
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
