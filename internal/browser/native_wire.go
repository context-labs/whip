package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/go-rod/rod/lib/cdp"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

const nativeMessageBytes = 12 << 20

var errNativeConnection = errors.New("external browser connection ended; explicit reconnect required")

// nativeWire is one non-reconnecting CDP connection. A canceled delivered call,
// invalid/oversized frame or overflowing event queue retires the connection.
// It never resends a command, logs its private endpoint or closes human Chrome.
type nativeWire struct {
	ctx        context.Context
	cancel     context.CancelFunc
	conn       net.Conn
	reader     io.Reader
	events     chan *cdp.Event
	slot       chan struct{}
	writes     sync.Mutex
	mu         sync.Mutex
	closed     bool
	waiting    int
	next       int64
	pending    chan nativeReply
	pendingID  int64
	group      sync.WaitGroup
	parentStop func() bool
	parentDone chan struct{}
	closeOnce  sync.Once
	authorize  func(context.Context) (context.Context, func(), error)
}

type nativeReply struct {
	value json.RawMessage
	err   error
}

func dialNativeWire(ctx, lifetime context.Context, endpoint string) (*nativeWire, error) {
	conn, buffer, _, err := ws.Dial(ctx, endpoint)
	if err != nil {
		return nil, errors.New("external browser CDP connection failed")
	}
	var reader io.Reader = buffer
	if buffer == nil {
		reader = bufio.NewReader(conn)
	}
	return newNativeWire(lifetime, conn, reader), nil
}

func newNativeWire(parent context.Context, conn net.Conn, reader io.Reader) *nativeWire {
	lifetime, cancel := context.WithCancel(parent)
	c := &nativeWire{ctx: lifetime, cancel: cancel, conn: conn, reader: reader, events: make(chan *cdp.Event, 32), slot: make(chan struct{}, 1), parentDone: make(chan struct{})}
	c.parentStop = context.AfterFunc(parent, func() { defer close(c.parentDone); c.retire() })
	c.group.Go(c.read)
	return c
}

func (c *nativeWire) Event() <-chan *cdp.Event  { return c.events }
func (c *nativeWire) Lifetime() context.Context { return c.ctx }
func (c *nativeWire) retire() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	_ = c.conn.Close()
}

func (c *nativeWire) Close() error {
	c.closeOnce.Do(func() {
		c.retire()
		if !c.parentStop() {
			<-c.parentDone
		}
		c.group.Wait()
	})
	return nil
}

func (c *nativeWire) Call(ctx context.Context, sessionID, method string, params any) ([]byte, error) {
	if len(sessionID) > 256 || method == "" || len(method) > 256 {
		return nil, errors.New("invalid CDP method or session")
	}
	raw, err := json.Marshal(params)
	if err != nil || len(raw) > 256<<10 {
		return nil, errors.New("CDP parameters exceed bounds")
	}
	c.mu.Lock()
	if c.closed || c.ctx.Err() != nil {
		c.mu.Unlock()
		return nil, errNativeConnection
	}
	if c.waiting >= 32 {
		c.mu.Unlock()
		return nil, errors.New("external browser CDP call queue is full")
	}
	c.waiting++
	c.group.Add(1)
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.waiting--; c.mu.Unlock(); c.group.Done() }()
	select {
	case c.slot <- struct{}{}:
		defer func() { <-c.slot }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, errNativeConnection
	}
	if c.authorize != nil {
		var release func()
		ctx, release, err = c.authorize(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.closed || c.ctx.Err() != nil || c.next >= 1<<53-1 {
		c.mu.Unlock()
		return nil, errNativeConnection
	}
	c.next++
	id := c.next
	reply := make(chan nativeReply, 1)
	c.pending, c.pendingID = reply, id
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.pendingID == id {
			c.pending = nil
			c.pendingID = 0
		}
		c.mu.Unlock()
	}()
	data, err := json.Marshal(struct {
		ID        int64           `json:"id"`
		SessionID string          `json:"sessionId,omitempty"`
		Method    string          `json:"method"`
		Params    json.RawMessage `json:"params"`
	}{id, sessionID, method, raw})
	if err != nil {
		return nil, err
	}
	canceled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(canceled); c.retire() })
	defer func() {
		if !stop() {
			<-canceled
		}
	}()
	if err := c.write(data, ws.OpText); err != nil {
		c.retire()
		return nil, errNativeConnection
	}
	select {
	case result := <-reply:
		return result.value, result.err
	case <-ctx.Done():
		c.retire()
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, errNativeConnection
	}
}

func (c *nativeWire) write(data []byte, op ws.OpCode) error {
	c.writes.Lock()
	defer c.writes.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = c.conn.SetWriteDeadline(time.Time{}) }()
	return wsutil.WriteClientMessage(c.conn, op, data)
}

func (c *nativeWire) read() {
	defer c.retire()
	defer close(c.events)
	// Control replies use the same whole-frame write lock as CDP commands.
	control := func(header ws.Header, reader io.Reader) error {
		value, err := io.ReadAll(io.LimitReader(reader, 126))
		if err != nil {
			return err
		}
		if len(value) > 125 {
			return errors.New("invalid control frame")
		}
		switch header.OpCode {
		case ws.OpPing:
			return c.write(value, ws.OpPong)
		case ws.OpClose:
			return io.EOF
		case ws.OpPong:
			return nil
		default:
			return errors.New("invalid control opcode")
		}
	}
	fragments := 0
	reader := wsutil.Reader{Source: c.reader, State: ws.StateClientSide, CheckUTF8: true, MaxFrameSize: nativeMessageBytes, OnIntermediate: control, OnContinuation: func(ws.Header, io.Reader) error {
		fragments++
		if fragments > 256 {
			return errors.New("too many CDP fragments")
		}
		return nil
	}}
	for {
		fragments = 0
		header, err := reader.NextFrame()
		if err != nil {
			return
		}
		if header.OpCode.IsControl() {
			if control(header, &reader) != nil {
				return
			}
			continue
		}
		if header.OpCode != ws.OpText {
			return
		}
		data, err := io.ReadAll(io.LimitReader(&reader, nativeMessageBytes+1))
		if err != nil || len(data) > nativeMessageBytes {
			return
		}
		var value struct {
			ID        int64           `json:"id"`
			Method    string          `json:"method"`
			SessionID string          `json:"sessionId"`
			Params    json.RawMessage `json:"params"`
			Result    json.RawMessage `json:"result"`
			Error     *cdp.Error      `json:"error"`
		}
		if json.Unmarshal(data, &value) != nil {
			return
		}
		if value.ID == 0 {
			if value.Method == "" || len(value.Method) > 256 || len(value.SessionID) > 256 || len(data) > 256<<10 {
				return
			}
			event := &cdp.Event{Method: value.Method, SessionID: value.SessionID, Params: value.Params}
			select {
			case c.events <- event:
			case <-c.ctx.Done():
				return
			default:
				return
			}
		} else {
			c.mu.Lock()
			reply, expected := c.pending, c.pendingID
			c.mu.Unlock()
			if reply == nil || value.ID != expected {
				return
			}
			result := nativeReply{value: value.Result}
			if value.Error != nil {
				result.err = errors.New("chrome debugger command failed")
			}
			if result.err == nil && len(result.value) == 0 {
				return
			}
			select {
			case reply <- result:
			default:
				return
			}
		}
	}
}
