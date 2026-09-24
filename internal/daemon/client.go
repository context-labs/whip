package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

type callResponse struct {
	result json.RawMessage
	err    error
}

const (
	clientPingInterval = 30 * time.Second
	clientPingTimeout  = 10 * time.Second
)

// Client is one initialized JSON-RPC connection to the local daemon.
type Client struct {
	conn messageTransport
	init InitializeResult

	writeMu        sync.Mutex
	mu             sync.Mutex
	nextID         int64
	subscriptions  map[string]string
	pending        map[string]chan callResponse
	commandChanged chan struct{}
	events         chan ProtocolEvent
	done           chan struct{}
	err            error
	once           sync.Once
}

func DialClient(ctx context.Context, paths RuntimePaths, initialize InitializeParams) (*Client, error) {
	timeout := initializationTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
		if timeout <= 0 {
			return nil, ctx.Err()
		}
	}
	conn, err := dialLocal(paths, timeout)
	if err != nil {
		return nil, err
	}
	client, err := NewClient(ctx, conn, initialize)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return client, nil
}

func NewClient(ctx context.Context, conn net.Conn, initialize InitializeParams) (*Client, error) {
	if conn == nil {
		return nil, errors.New("protocol connection is required")
	}
	return newTransportClient(ctx, newUnixMessageTransport(conn), initialize)
}

func newTransportClient(ctx context.Context, conn messageTransport, initialize InitializeParams) (*Client, error) {
	if conn == nil {
		return nil, errors.New("protocol connection is required")
	}
	client := &Client{
		conn: conn, subscriptions: make(map[string]string), nextID: 1, pending: make(map[string]chan callResponse),
		commandChanged: make(chan struct{}), events: make(chan ProtocolEvent, MaxOutboundEnvelopes), done: make(chan struct{}),
	}
	params, err := json.Marshal(initialize)
	if err != nil {
		return nil, err
	}
	frame, err := marshalFrame(rpcMessage{ID: json.RawMessage("1"), Method: "initialize", Params: params})
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(initializationTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetWriteDeadline(deadline)
	if err := conn.WriteMessage(frame); err != nil {
		return nil, err
	}
	replyFrame, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	reply, err := decodeFrame(replyFrame)
	if err != nil {
		return nil, err
	}
	if reply.Error != nil {
		return nil, reply.Error
	}
	raw, err := json.Marshal(reply.Result)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &client.init); err != nil {
		return nil, fmt.Errorf("decode initialize result: %w", err)
	}
	if client.init.ProtocolMajor != ProtocolMajor {
		return nil, fmt.Errorf("daemon selected unsupported protocol major %d", client.init.ProtocolMajor)
	}
	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})
	go client.readLoop()
	go client.heartbeat(context.WithoutCancel(ctx), clientPingInterval, clientPingTimeout)
	for rootID, cursor := range initialize.Cursors {
		if _, err := client.Subscribe(ctx, rootID, cursor); err != nil {
			if failure, ok := errors.AsType[*RPCError](err); ok && failure.Code == -32010 {
				continue
			}
			client.close(err)
			return nil, err
		}
	}
	return client, nil
}

func (c *Client) InitializeResult() InitializeResult { return c.init }

func (c *Client) Events() <-chan ProtocolEvent { return c.events }

func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Client) Close() error {
	c.close(net.ErrClosed)
	return nil
}

func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	if method == "" {
		return errors.New("protocol method is required")
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	c.nextID++
	id := strconv.FormatInt(c.nextID, 10)
	response := make(chan callResponse, 1)
	c.pending[id] = response
	c.mu.Unlock()

	frame, err := marshalFrame(rpcMessage{ID: json.RawMessage(id), Method: method, Params: rawParams})
	if err == nil {
		c.writeMu.Lock()
		err = c.conn.WriteMessage(frame)
		c.writeMu.Unlock()
	}
	if err != nil {
		c.removePending(id)
		c.close(err)
		return err
	}
	select {
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case reply := <-response:
		if reply.err != nil {
			return reply.err
		}
		if result == nil || len(reply.result) == 0 || string(reply.result) == "null" {
			return nil
		}
		return json.Unmarshal(reply.result, result)
	}
}

// Submit accepts durable work without retaining the request until completion.
func (c *Client) Submit(ctx context.Context, params CommandParams) (CommandResult, error) {
	var result CommandResult
	err := c.Call(ctx, "command.submit", params, &result)
	fillCommandPresentation(&result)
	return result, err
}

func (c *Client) CommandStatus(ctx context.Context, commandID string) (CommandResult, error) {
	var result CommandResult
	err := c.Call(ctx, "command.status", CommandStatusParams{CommandID: commandID}, &result)
	fillCommandPresentation(&result)
	return result, err
}

// SubmitAndWait is a convenience for callers that need the terminal result.
// Cancelling this wait does not cancel the admitted operation.
func (c *Client) SubmitAndWait(ctx context.Context, params CommandParams) (CommandResult, error) {
	result, err := c.Submit(ctx, params)
	if err != nil {
		return result, err
	}
	return c.waitCommand(ctx, params.RootID, result)
}

func (c *Client) waitCommand(ctx context.Context, rootID string, result CommandResult) (CommandResult, error) {
	var err error
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for result.Status == "queued" || result.Status == "running" || result.Status == "waiting" {
		c.mu.Lock()
		changed := c.commandChanged
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-c.done:
			return result, c.Err()
		case <-changed:
		case <-ticker.C:
		}
		result, err = c.CommandStatus(ctx, result.CommandID)
		if err != nil {
			return result, err
		}
	}
	if err := c.commandContent(ctx, rootID, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) Command(ctx context.Context, params CommandParams) (CommandResult, error) {
	if operation, ok := protocol.LookupRuntime(params.Operation); ok && operation.Execution == protocol.Ephemeral {
		reply, err := c.Invoke(ctx, protocol.QueryParams{RootID: params.RootID, Operation: params.Operation, Payload: params.Payload})
		result := CommandResult{Operation: params.Operation, Status: "succeeded", Result: reply.Result}
		fillCommandPresentation(&result)
		return result, err
	}
	if operation, ok := protocol.LookupRuntime(params.Operation); ok && operation.Execution == protocol.Query {
		query, err := c.Query(ctx, protocol.QueryParams{RootID: params.RootID, Operation: params.Operation, Payload: params.Payload})
		result := CommandResult{Operation: params.Operation, CommandID: params.CommandID, Status: "succeeded", Result: query.Result}
		fillCommandPresentation(&result)
		return result, err
	}
	return c.SubmitAndWait(ctx, params)
}

func (c *Client) Replay(ctx context.Context, params ReplayParams) (ReplayResult, error) {
	var result ReplayResult
	err := c.Call(ctx, "events.replay", params, &result)
	return result, err
}

func (c *Client) Snapshot(ctx context.Context, rootID string) (session.RootSnapshot, error) {
	var snapshot session.RootSnapshot
	if err := c.Call(ctx, "root.snapshot", SnapshotParams{RootID: rootID}, &snapshot); err != nil {
		return snapshot, err
	}
	if snapshot.RootID != rootID {
		return session.RootSnapshot{}, errors.New("daemon returned an inconsistent snapshot")
	}
	return snapshot, nil
}

func (c *Client) ValidateProvider(ctx context.Context, params ProviderValidateParams) (ProviderValidateResult, error) {
	var result ProviderValidateResult
	err := c.Call(ctx, "provider.validate", params, &result)
	return result, err
}

func (c *Client) Upload(ctx context.Context, begin UploadBeginParams, data []byte) (ContentHandle, error) {
	if int64(len(data)) != begin.Size {
		return ContentHandle{}, errors.New("upload data size does not match metadata")
	}
	if err := c.Call(ctx, "upload.begin", begin, nil); err != nil {
		return ContentHandle{}, err
	}
	for offset := 0; offset < len(data); offset += MaxContentChunk {
		end := min(offset+MaxContentChunk, len(data))
		if err := c.Call(ctx, "upload.chunk", UploadChunkParams{
			UploadID: begin.UploadID, Offset: int64(offset), Data: data[offset:end],
		}, nil); err != nil {
			return ContentHandle{}, err
		}
	}
	var handle ContentHandle
	if err := c.Call(ctx, "upload.finish", UploadFinishParams{UploadID: begin.UploadID}, &handle); err != nil {
		return ContentHandle{}, err
	}
	return handle, nil
}

func (c *Client) DecidePermission(ctx context.Context, decision PermissionDecision) (PermissionDecisionResult, error) {
	if decision.CommandID == "" || decision.RootID == "" || decision.PermissionID == "" {
		return PermissionDecisionResult{}, errors.New("permission decision requires command, root, and permission identities")
	}
	var result PermissionDecisionResult
	err := c.Call(ctx, "permission.decide", PermissionDecisionParams{Decision: decision}, &result)
	return result, err
}

func (c *Client) RequestRestart(ctx context.Context, generation int64) error {
	return c.requestLifecycle(ctx, "daemon.restart", generation)
}

func (c *Client) RequestStop(ctx context.Context, generation int64) error {
	return c.requestLifecycle(ctx, "daemon.stop", generation)
}

func (c *Client) requestLifecycle(ctx context.Context, method string, generation int64) error {
	params, err := json.Marshal(RestartParams{Generation: generation})
	if err != nil {
		return err
	}
	frame, err := marshalFrame(rpcMessage{Method: method, Params: params})
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	err = c.conn.WriteMessage(frame)
	c.writeMu.Unlock()
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return nil
	}
}

func (c *Client) readLoop() {
	for {
		frame, err := c.conn.ReadMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = net.ErrClosed
			}
			c.close(err)
			return
		}
		message, err := decodeFrame(frame)
		if err != nil {
			c.close(err)
			return
		}
		if message.Method != "" {
			if message.Method == "subscription.failed" {
				var failure protocol.SubscriptionFailure
				if err := json.Unmarshal(message.Params, &failure); err != nil {
					c.close(err)
					return
				}
				c.mu.Lock()
				active := c.subscriptions[failure.RootID] == failure.SubscriptionID
				c.mu.Unlock()
				if !active {
					continue
				}
				if failure.Error != nil {
					c.close(failure.Error)
				} else {
					c.close(errors.New("subscription requires resynchronization"))
				}
				return
			}
			if message.Method == "event" {
				var notification eventNotification
				if err := json.Unmarshal(message.Params, &notification); err != nil {
					c.close(err)
					return
				}
				c.mu.Lock()
				active := c.subscriptions[notification.Event.RootID]
				c.mu.Unlock()
				if active != notification.Event.SubscriptionID {
					continue
				}
				if !strings.HasPrefix(notification.Event.Kind, "stream.") {
					c.mu.Lock()
					close(c.commandChanged)
					c.commandChanged = make(chan struct{})
					c.mu.Unlock()
				}
				select {
				case c.events <- notification.Event:
				default:
					c.close(errors.New("client event buffer exceeded"))
					return
				}
			}
			continue
		}
		id := string(message.ID)
		c.mu.Lock()
		response := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if response == nil {
			continue
		}
		if message.Error != nil {
			response <- callResponse{err: message.Error}
			continue
		}
		raw, err := json.Marshal(message.Result)
		response <- callResponse{result: raw, err: err}
	}
}

func (c *Client) heartbeat(parent context.Context, interval, timeout time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(parent, timeout)
			var result struct {
				Generation int64 `json:"generation,string"`
			}
			err := c.Call(ctx, "daemon.ping", struct{}{}, &result)
			cancel()
			if err != nil {
				c.close(fmt.Errorf("daemon heartbeat: %w", err))
				return
			}
		}
	}
}

func (c *Client) removePending(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) close(err error) {
	c.once.Do(func() {
		_ = c.conn.Close()
		c.mu.Lock()
		c.err = err
		pending := c.pending
		c.pending = make(map[string]chan callResponse)
		c.mu.Unlock()
		for _, response := range pending {
			response <- callResponse{err: err}
		}
		close(c.done)
	})
}
