package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/context-labs/whip/internal/protocol"
)

const (
	InputRecoveryNamespace = "whip.v4.go.inputs"
	MaxInputRecordBytes    = 4 << 20
)

// InputRecord contains the exact original admission payload. Persist it before
// Send when recovery across a client restart is wanted. It is client-owned data,
// never a second source for host input, turn, or execution state.
type InputRecord struct {
	Namespace string          `json:"namespace"`
	Version   int             `json:"version"`
	RuntimeID protocol.ID     `json:"runtime_id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	Accepted  bool            `json:"accepted"`
}

type inputSend struct {
	done   chan struct{}
	result json.RawMessage
	err    error
}

type InputCommand struct {
	client    *Client
	mu        sync.Mutex
	record    InputRecord
	identity  protocol.RequestIdentity
	owner     protocol.ID
	recovered bool
	pending   *inputSend
	retry     chan struct{}
}

// PrepareInput accepts only native operations that use ordinary input receipts.
// It validates and freezes all params before any transport work. Other typed
// controls retain their own explicit stable identity and CAS semantics.
func (c *Client) PrepareInput(method string, params any) (*InputCommand, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return restoreInput(c, InputRecord{Namespace: InputRecoveryNamespace, Version: 1, RuntimeID: c.Identity(), Method: method, Params: raw}, false)
}

// RestoreInput never transmits. A recovered handle requires Check or explicit
// Retry; Send cannot silently reissue an uncertain admission after restart.
func (c *Client) RestoreInput(raw []byte) (*InputCommand, error) {
	if len(raw) > MaxInputRecordBytes {
		return nil, errors.New("input recovery record exceeds 4 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var record InputRecord
	if err := decoder.Decode(&record); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("trailing input recovery data")
	}
	return restoreInput(c, record, true)
}

func restoreInput(c *Client, record InputRecord, recovered bool) (*InputCommand, error) {
	if record.Namespace != InputRecoveryNamespace || record.Version != 1 || record.RuntimeID != c.Identity() {
		return nil, errors.New("input record belongs to another runtime or namespace")
	}
	if len(record.Params) > MaxInputRecordBytes {
		return nil, errors.New("input recovery payload exceeds 4 MiB")
	}
	var schema string
	switch record.Method {
	case "sessions.submit":
		schema = "SubmitParams"
	case "sessions.compact":
		schema = "CompactParams"
	case "goals.formulate":
		schema = "FormulateGoalParams"
	case "goals.resume":
		schema = "ResumeGoalParams"
	case "tool.call":
		schema = "CallHostToolParams"
	case "shell.run":
		schema = "RunShellParams"
	default:
		return nil, errors.New("operation does not use input receipt recovery")
	}
	if err := protocol.Validate(schema, record.Params); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw) > MaxInputRecordBytes {
		return nil, errors.New("input recovery record exceeds 4 MiB")
	}
	var scope struct {
		Identity  protocol.RequestIdentity `json:"identity"`
		SessionID protocol.ID              `json:"session_id"`
	}
	if err := json.Unmarshal(record.Params, &scope); err != nil {
		return nil, err
	}
	record.Params = bytes.Clone(record.Params)
	return &InputCommand{client: c, record: record, identity: scope.Identity, owner: scope.SessionID, recovered: recovered, retry: make(chan struct{}, 1)}, nil
}

func (c *InputCommand) Record() InputRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	record := c.record
	record.Params = bytes.Clone(record.Params)
	return record
}

// Send shares one bounded transport send. The observing context cancels only
// this waiter; client Close joins its send without sending host cancellation.
func (c *InputCommand) Send(ctx context.Context) (protocol.Admission, error) {
	return c.send(ctx, false)
}

func (c *InputCommand) send(ctx context.Context, retry bool) (protocol.Admission, error) {
	if err := ctx.Err(); err != nil {
		return protocol.Admission{}, err
	}
	c.mu.Lock()
	if c.recovered && !retry {
		c.mu.Unlock()
		return protocol.Admission{}, errors.New("recovered input requires Check or explicit Retry")
	}
	if retry && c.pending != nil {
		select {
		case <-c.pending.done:
			c.pending = nil
		default:
		}
	}
	if c.pending == nil {
		pending := &inputSend{done: make(chan struct{})}
		c.pending = pending
		method, params := c.record.Method, c.record.Params
		if err := c.client.startOwned(len(params), func() {
			var result protocol.Admission
			pending.err = c.client.Call(c.client.lifecycle, method, params, &result)
			if pending.err == nil {
				pending.err = c.validate(result)
			}
			if pending.err == nil {
				pending.result, pending.err = json.Marshal(result)
			}
			c.mu.Lock()
			if pending.err == nil {
				c.record.Accepted = true
			}
			c.mu.Unlock()
			close(pending.done)
		}); err != nil {
			pending.err = err
			close(pending.done)
		}
	}
	pending := c.pending
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return protocol.Admission{}, ctx.Err()
	case <-pending.done:
	}
	var result protocol.Admission
	if pending.err != nil {
		return result, pending.err
	}
	err := json.Unmarshal(pending.result, &result)
	return result, err
}

func (c *InputCommand) validate(value protocol.Admission) error {
	if value.Receipt.DeletedAt == nil && value.Input == nil || value.Receipt.DeletedAt != nil && (value.Input != nil || value.Turn != nil) {
		return errors.New("receipt has inconsistent deletion projection")
	}

	if value.Receipt.Identity != c.identity || value.Input != nil && value.Input.SessionID != c.owner || value.Turn != nil && value.Turn.SessionID != c.owner {
		return errors.New("input receipt ownership mismatch")
	}
	if value.Input != nil && (value.Receipt.InputID == nil || *value.Receipt.InputID != value.Input.ID) {
		return errors.New("input receipt identity mismatch")
	}
	if value.Turn != nil && (value.Input == nil || value.Input.TurnID == nil || *value.Input.TurnID != value.Turn.ID) {
		return errors.New("input receipt turn mismatch")
	}
	return nil
}

// Check verifies exact original bytes against the durable receipt. Missing is
// explicit, and a previously accepted receipt may never disappear into a resend.
func (c *InputCommand) Check(ctx context.Context) (result protocol.Admission, found bool, err error) {
	record := c.Record()
	err = c.client.Call(ctx, "receipts.match", protocol.MatchReceiptParams{Method: record.Method, ParamsBase64: base64.StdEncoding.EncodeToString(record.Params)}, &result)
	var remote *Error
	if errors.As(err, &remote) && remote.Kind == "NOT_FOUND" {
		if record.Accepted {
			return result, false, errors.New("previously accepted input receipt is missing")
		}
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	if err = c.validate(result); err != nil {
		return result, false, err
	}
	c.mu.Lock()
	c.record.Accepted = true
	c.mu.Unlock()
	return result, true, nil
}

// Retry first checks the exact receipt. Only the caller's explicit invocation
// may resend missing work, retaining the original identity and payload.
func (c *InputCommand) Retry(ctx context.Context) (protocol.Admission, error) {
	select {
	case c.retry <- struct{}{}:
	case <-ctx.Done():
		return protocol.Admission{}, ctx.Err()
	}
	defer func() { <-c.retry }()
	c.mu.Lock()
	pending := c.pending
	c.mu.Unlock()
	if pending != nil {
		select {
		case <-ctx.Done():
			return protocol.Admission{}, ctx.Err()
		case <-pending.done:
		}
	}
	result, found, err := c.Check(ctx)
	if err != nil || found {
		return result, err
	}
	return c.send(ctx, true)
}

func (c *InputCommand) Wait(ctx context.Context) (protocol.Admission, error) {
	if _, found, err := c.Check(ctx); err != nil || !found {
		if err == nil {
			err = fmt.Errorf("input %s is not accepted", c.identity.RequestID)
		}
		return protocol.Admission{}, err
	}
	value, err := c.client.Wait(ctx, c.identity)
	if err == nil {
		err = c.validate(value)
	}
	return value, err
}
