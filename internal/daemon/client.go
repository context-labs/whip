package daemon

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/session"
)

type callResponse struct {
	result json.RawMessage
	err    error
}

// Client is one initialized JSON-RPC connection to the local daemon.
type Client struct {
	conn net.Conn
	init InitializeResult
	self InitializeParams

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int64
	nonce   []byte
	pending map[string]chan callResponse
	events  chan ProtocolEvent
	done    chan struct{}
	err     error
	once    sync.Once
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
	client := &Client{
		conn: conn, self: initialize, nextID: 1, pending: make(map[string]chan callResponse),
		events: make(chan ProtocolEvent, MaxOutboundEnvelopes), done: make(chan struct{}),
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
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(frame); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(conn, MaxFrameSize)
	replyFrame, err := readProtocolFrame(reader)
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
	client.nonce = append([]byte(nil), client.init.Nonce...)
	_ = conn.SetDeadline(time.Time{})
	go client.readLoop(reader)
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
		_, err = c.conn.Write(frame)
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

func (c *Client) Command(ctx context.Context, params CommandParams) (CommandResult, error) {
	var result CommandResult
	err := c.Call(ctx, "command", params, &result)
	return result, err
}

func (c *Client) Replay(ctx context.Context, params ReplayParams) (ReplayResult, error) {
	var result ReplayResult
	err := c.Call(ctx, "events.replay", params, &result)
	return result, err
}

func (c *Client) Snapshot(ctx context.Context, rootID string) (session.RootSnapshot, error) {
	var result SnapshotResult
	if err := c.Call(ctx, "snapshot", SnapshotParams{RootID: rootID}, &result); err != nil {
		return session.RootSnapshot{}, err
	}
	if result.SnapshotID == "" || result.Count < 1 {
		return session.RootSnapshot{}, errors.New("daemon returned an empty snapshot")
	}
	var data []byte
	for index := range result.Count {
		var chunk SnapshotChunk
		if err := c.Call(ctx, "snapshot.chunk", SnapshotChunkParams{SnapshotID: result.SnapshotID, Index: index}, &chunk); err != nil {
			return session.RootSnapshot{}, err
		}
		if chunk.Index != index || chunk.Count != result.Count || chunk.Cursor != result.Cursor || len(chunk.Data) > MaxSnapshotChunk {
			return session.RootSnapshot{}, errors.New("daemon returned invalid snapshot chunks")
		}
		data = append(data, chunk.Data...)
	}
	var snapshot session.RootSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return session.RootSnapshot{}, err
	}
	if snapshot.RootID != rootID || snapshot.Cursor != result.Cursor {
		return session.RootSnapshot{}, errors.New("daemon returned an inconsistent snapshot")
	}
	return snapshot, nil
}

func (c *Client) Upload(ctx context.Context, begin UploadBeginParams, data []byte) (ContentHandle, error) {
	if int64(len(data)) != begin.Size {
		return ContentHandle{}, errors.New("upload data size does not match metadata")
	}
	if err := c.Call(ctx, "upload.begin", begin, nil); err != nil {
		return ContentHandle{}, err
	}
	for offset := 0; offset < len(data); offset += MaxSnapshotChunk {
		end := min(offset+MaxSnapshotChunk, len(data))
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

func (c *Client) EnrollIdentity(ctx context.Context, private ed25519.PrivateKey, ttyConfirmed bool, authorizedBy string, authorizer ed25519.PrivateKey) (IdentityResult, error) {
	if len(private) != ed25519.PrivateKeySize {
		return IdentityResult{}, errors.New("identity enrollment requires an Ed25519 private key")
	}
	params := EnrollIdentityParams{
		PublicKey:    append([]byte(nil), private.Public().(ed25519.PublicKey)...),
		TTYConfirmed: ttyConfirmed, AuthorizedBy: authorizedBy,
	}
	if authorizedBy != "" {
		if len(authorizer) != ed25519.PrivateKeySize {
			return IdentityResult{}, errors.New("later enrollment requires the authorizer private key")
		}
		c.mu.Lock()
		nonce := append([]byte(nil), c.nonce...)
		c.mu.Unlock()
		params.Signature = ed25519.Sign(authorizer, enrollmentMessage(c.init.Generation, nonce, c.self.ClientID, c.self.ClientKind, params.PublicKey))
	}
	var result IdentityResult
	if err := c.Call(ctx, "identity.enroll", params, &result); err != nil {
		return IdentityResult{}, err
	}
	c.mu.Lock()
	c.nonce = append([]byte(nil), result.Nonce...)
	c.mu.Unlock()
	return result, nil
}

func (c *Client) DecidePermission(ctx context.Context, private ed25519.PrivateKey, decision PermissionDecision) (PermissionDecisionResult, error) {
	if len(private) != ed25519.PrivateKeySize {
		return PermissionDecisionResult{}, errors.New("permission decision requires an Ed25519 private key")
	}
	c.mu.Lock()
	nonce := append([]byte(nil), c.nonce...)
	c.mu.Unlock()
	message, err := authorizationMessage("permission.decide", c.init.Generation, nonce, decision)
	if err != nil {
		return PermissionDecisionResult{}, err
	}
	params := PermissionDecisionParams{Decision: decision, Signature: ed25519.Sign(private, message)}
	var result PermissionDecisionResult
	if err := c.Call(ctx, "permission.decide", params, &result); err != nil {
		return PermissionDecisionResult{}, err
	}
	c.mu.Lock()
	c.nonce = append([]byte(nil), result.Nonce...)
	c.mu.Unlock()
	return result, nil
}

func (c *Client) RequestRestart(ctx context.Context, generation int64) error {
	params, err := json.Marshal(RestartParams{Generation: generation})
	if err != nil {
		return err
	}
	frame, err := marshalFrame(rpcMessage{Method: "daemon.restart", Params: params})
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	_, err = c.conn.Write(frame)
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

func (c *Client) readLoop(reader *bufio.Reader) {
	for {
		frame, err := readProtocolFrame(reader)
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
			if message.Method == "event" {
				var notification eventNotification
				if err := json.Unmarshal(message.Params, &notification); err != nil {
					c.close(err)
					return
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
