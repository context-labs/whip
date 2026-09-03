package daemon

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/context-labs/whip/internal/session"
)

// RootClientState is the synchronization state of a reconnecting daemon
// client. Commands are accepted only after replay or snapshot replacement has
// brought the client live.
type RootClientState uint8

const (
	RootDisconnected RootClientState = iota
	RootReconnecting
	RootSnapshotting
	RootLive
)

func (s RootClientState) String() string {
	switch s {
	case RootDisconnected:
		return "disconnected"
	case RootReconnecting:
		return "reconnecting"
	case RootSnapshotting:
		return "snapshotting"
	case RootLive:
		return "live"
	default:
		return "unknown"
	}
}

// RootConnection is the protocol surface used by RootClient. The interface
// keeps command-line and protocol-adapter tests independent of a real socket.
type RootConnection interface {
	Command(context.Context, CommandParams) (CommandResult, error)
	Replay(context.Context, ReplayParams) (ReplayResult, error)
	Snapshot(context.Context, string) (session.RootSnapshot, error)
	Events() <-chan ProtocolEvent
	Done() <-chan struct{}
	Err() error
	Close() error
}

// RootConnector attaches with the supplied durable cursors.
type RootConnector func(context.Context, map[string]int64) (RootConnection, error)

// RootAction gives one logical command a stable identity across reconnects.
type RootAction struct {
	CommandID string
	Operation string
	RootID    string
	Payload   json.RawMessage
}

// RootUpdate carries synchronization state and authoritative daemon data.
type RootUpdate struct {
	State        RootClientState
	StateChanged bool
	Snapshot     *session.RootSnapshot
	Event        *ProtocolEvent
	Err          error
}

// RootClientOptions contains behavioral inputs for one daemon root.
type RootClientOptions struct {
	ClientID   string
	PrivateKey ed25519.PrivateKey
	RootID     string
	Create     *CreateSession
	Connector  RootConnector
	RetryMin   time.Duration
	RetryMax   time.Duration
}

// RootClient reconnects one daemon-root subscription and retries commands
// with their original identity. It owns no provider, store, tool, or process.
type RootClient struct {
	clientID   string
	instanceID string
	privateKey ed25519.PrivateKey
	create     *CreateSession
	connect    RootConnector
	retryMin   time.Duration
	retryMax   time.Duration

	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	once    sync.Once
	started atomic.Bool

	mu      sync.RWMutex
	state   RootClientState
	err     error
	rootID  string
	cursor  int64
	conn    RootConnection
	changed chan struct{}

	updates chan RootUpdate
	nextID  atomic.Uint64
}

var errReconnectForCreatedRoot = errors.New("reconnect to subscribe to created root")

func NewRootClient(options RootClientOptions) (*RootClient, error) {
	if options.ClientID == "" || options.Connector == nil {
		return nil, errors.New("root client requires an identity and connector")
	}
	if options.RootID == "" && options.Create == nil {
		return nil, errors.New("root client requires a root or session template")
	}
	if options.RootID != "" && options.Create != nil {
		return nil, errors.New("root client cannot resume and create simultaneously")
	}
	if options.RetryMin <= 0 {
		options.RetryMin = 25 * time.Millisecond
	}
	if options.RetryMax < options.RetryMin {
		options.RetryMax = time.Second
	}
	instanceNonce, err := randomNonce()
	if err != nil {
		return nil, fmt.Errorf("create client instance identity: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &RootClient{
		clientID: options.ClientID, privateKey: append(ed25519.PrivateKey(nil), options.PrivateKey...),
		instanceID: hex.EncodeToString(instanceNonce[:16]),
		rootID:     options.RootID, create: options.Create, connect: options.Connector,
		retryMin: options.RetryMin, retryMax: options.RetryMax,
		ctx: ctx, cancel: cancel, done: make(chan struct{}), changed: make(chan struct{}),
		updates: make(chan RootUpdate, MaxOutboundEnvelopes),
	}, nil
}

func (c *RootClient) Start() {
	c.once.Do(func() {
		c.started.Store(true)
		go c.run()
	})
}

func (c *RootClient) Close() error {
	c.cancel()
	c.once.Do(func() {
		close(c.done)
		close(c.updates)
	})
	select {
	case <-c.done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("root client did not stop")
	}
}

func (c *RootClient) Updates() <-chan RootUpdate { return c.updates }

func (c *RootClient) State() RootClientState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *RootClient) RootID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rootID
}

func (c *RootClient) Cursor() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cursor
}

func (c *RootClient) Err() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.err
}

func (c *RootClient) NewAction(operation string, payload any) (RootAction, error) {
	if operation == "" {
		return RootAction{}, errors.New("action operation is required")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return RootAction{}, err
	}
	id := c.nextID.Add(1)
	return RootAction{
		CommandID: c.clientID + "-" + c.instanceID + "-" + strconv.FormatUint(id, 10),
		Operation: operation, RootID: c.RootID(), Payload: raw,
	}, nil
}

func (c *RootClient) Command(ctx context.Context, action RootAction) (CommandResult, error) {
	if action.CommandID == "" || action.Operation == "" || action.RootID == "" {
		return CommandResult{}, errors.New("action identity and operation are required")
	}
	for {
		c.mu.RLock()
		state, connection := c.state, c.conn
		c.mu.RUnlock()
		if state != RootLive || connection == nil {
			if !c.started.Load() {
				return CommandResult{}, fmt.Errorf("commands are disabled while client is %s", state)
			}
			if !c.waitForLive(ctx) {
				if err := ctx.Err(); err != nil {
					return CommandResult{}, err
				}
				return CommandResult{}, c.ctx.Err()
			}
			continue
		}
		result, err := connection.Command(ctx, CommandParams{
			CommandID: action.CommandID, Scope: string(session.CommandScopeRoot), RootID: action.RootID,
			Operation: action.Operation, Payload: action.Payload,
		})
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return CommandResult{}, ctx.Err()
		}
		select {
		case <-connection.Done():
			continue
		default:
			return CommandResult{}, err
		}
	}
}

func (c *RootClient) DecidePermission(ctx context.Context, action RootAction, permissionID string, allow bool, reason string) (PermissionDecisionResult, error) {
	if action.CommandID == "" || action.Operation != "permission.decide" || action.RootID == "" || permissionID == "" {
		return PermissionDecisionResult{}, errors.New("permission action requires stable command and permission identities")
	}
	if len(c.privateKey) != ed25519.PrivateKeySize {
		return PermissionDecisionResult{}, errors.New("this client identity cannot approve permissions")
	}
	for {
		c.mu.RLock()
		state, connection := c.state, c.conn
		c.mu.RUnlock()
		if state != RootLive || connection == nil {
			if !c.waitForLive(ctx) {
				if err := ctx.Err(); err != nil {
					return PermissionDecisionResult{}, err
				}
				return PermissionDecisionResult{}, c.ctx.Err()
			}
			continue
		}
		privileged, ok := connection.(interface {
			DecidePermission(context.Context, ed25519.PrivateKey, PermissionDecision) (PermissionDecisionResult, error)
		})
		if !ok {
			return PermissionDecisionResult{}, errors.New("daemon connection cannot approve permissions")
		}
		result, err := privileged.DecidePermission(ctx, c.privateKey, PermissionDecision{
			CommandID: action.CommandID, RootID: action.RootID, PermissionID: permissionID, Allow: allow, Reason: reason,
		})
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return PermissionDecisionResult{}, ctx.Err()
		}
		select {
		case <-connection.Done():
			continue
		default:
			return PermissionDecisionResult{}, err
		}
	}
}

// SetPermissionMode enables external prompts as an ordinary command, but
// requires a paired identity signature before disabling them for automatic
// execution under the session's existing grants.
func (c *RootClient) SetPermissionMode(ctx context.Context, action RootAction, external bool) (CommandResult, error) {
	if action.CommandID == "" || action.Operation != "permission.mode" || action.RootID == "" {
		return CommandResult{}, errors.New("permission mode requires stable command and root identities")
	}
	if external {
		return c.Command(ctx, action)
	}
	if len(c.privateKey) != ed25519.PrivateKeySize {
		return CommandResult{}, errors.New("automatic permission mode requires a paired client identity")
	}
	for {
		c.mu.RLock()
		state, connection := c.state, c.conn
		c.mu.RUnlock()
		if state != RootLive || connection == nil {
			if !c.waitForLive(ctx) {
				if err := ctx.Err(); err != nil {
					return CommandResult{}, err
				}
				return CommandResult{}, c.ctx.Err()
			}
			continue
		}
		privileged, ok := connection.(interface {
			SetPermissionMode(context.Context, ed25519.PrivateKey, CommandParams) (CommandResult, error)
		})
		if !ok {
			return CommandResult{}, errors.New("daemon connection cannot authorize automatic permission mode")
		}
		result, err := privileged.SetPermissionMode(ctx, c.privateKey, CommandParams{
			CommandID: action.CommandID, Scope: string(session.CommandScopeRoot), RootID: action.RootID,
			Operation: action.Operation, Payload: action.Payload,
		})
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return CommandResult{}, ctx.Err()
		}
		select {
		case <-connection.Done():
			continue
		default:
			return CommandResult{}, err
		}
	}
}

func (c *RootClient) Snapshot(ctx context.Context) (session.RootSnapshot, error) {
	c.mu.RLock()
	state, rootID, connection := c.state, c.rootID, c.conn
	c.mu.RUnlock()
	if state != RootLive || connection == nil || rootID == "" {
		return session.RootSnapshot{}, fmt.Errorf("snapshot unavailable while client is %s", state)
	}
	return connection.Snapshot(ctx, rootID)
}

func (c *RootClient) WaitLive(ctx context.Context) error {
	if c.waitForLive(ctx) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.Err(); err != nil {
		return err
	}
	return c.ctx.Err()
}

func (c *RootClient) waitForLive(ctx context.Context) bool {
	for {
		c.mu.RLock()
		live := c.state == RootLive && c.conn != nil
		changed := c.changed
		c.mu.RUnlock()
		if live {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-c.ctx.Done():
			return false
		case <-c.done:
			return false
		case <-changed:
		}
	}
}

func (c *RootClient) run() {
	defer close(c.done)
	defer close(c.updates)
	delay := c.retryMin
	for c.ctx.Err() == nil {
		c.transition(RootReconnecting, nil)
		rootID, cursor := c.position()
		cursors := map[string]int64{}
		if rootID != "" {
			cursors[rootID] = cursor
		}
		connection, err := c.connect(c.ctx, cursors)
		if err != nil {
			c.transition(RootDisconnected, err)
			if !c.retry(delay) {
				return
			}
			delay = min(delay*2, c.retryMax)
			continue
		}
		delay = c.retryMin
		c.setConnection(connection)
		if err := c.synchronize(connection); err != nil {
			if errors.Is(err, errReconnectForCreatedRoot) {
				_ = connection.Close()
				c.clearConnection(connection)
				continue
			}
			select {
			case <-connection.Done():
				_ = connection.Close()
				c.clearConnection(connection)
				c.transition(RootDisconnected, err)
				continue
			default:
			}
			_ = connection.Close()
			c.clearConnection(connection)
			c.transition(RootDisconnected, err)
			return
		}
		c.transition(RootLive, nil)
		if !c.consume(connection) {
			return
		}
		c.clearConnection(connection)
		c.transition(RootDisconnected, connection.Err())
	}
}

func (c *RootClient) synchronize(connection RootConnection) error {
	rootID, cursor := c.position()
	if rootID == "" {
		payload, err := json.Marshal(c.create)
		if err != nil {
			return err
		}
		result, err := connection.Command(c.ctx, CommandParams{
			CommandID: c.clientID + "-session-" + c.instanceID, Scope: string(session.CommandScopeDaemon),
			Operation: "session.create", Payload: payload,
		})
		if err != nil {
			return err
		}
		if result.Status != "succeeded" || result.Output == "" {
			return fmt.Errorf("session creation is %s: %s", result.Status, result.Error)
		}
		c.mu.Lock()
		c.rootID, c.cursor = result.Output, 0
		c.mu.Unlock()
		return errReconnectForCreatedRoot
	}

	c.transition(RootSnapshotting, nil)
	if cursor > 0 {
		replay, err := connection.Replay(c.ctx, ReplayParams{RootID: rootID, Cursor: cursor})
		if err != nil {
			return err
		}
		if !replay.Expired {
			for i := range replay.Events {
				c.emitEvent(replay.Events[i])
			}
			return nil
		}
	}
	snapshot, err := connection.Snapshot(c.ctx, rootID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.cursor = snapshot.Cursor
	c.mu.Unlock()
	c.emit(RootUpdate{Snapshot: &snapshot})
	return nil
}

func (c *RootClient) consume(connection RootConnection) bool {
	for {
		select {
		case <-c.ctx.Done():
			_ = connection.Close()
			return false
		case <-connection.Done():
			return true
		case event, ok := <-connection.Events():
			if !ok {
				return true
			}
			c.emitEvent(event)
		}
	}
}

func (c *RootClient) emitEvent(event ProtocolEvent) {
	c.mu.Lock()
	if event.RootID != c.rootID || event.Seq <= c.cursor {
		c.mu.Unlock()
		return
	}
	c.cursor = event.Seq
	c.mu.Unlock()
	c.emit(RootUpdate{Event: &event})
}

func (c *RootClient) transition(state RootClientState, err error) {
	c.mu.Lock()
	c.state = state
	if state == RootLive {
		c.err = nil
	} else if err != nil {
		c.err = err
	}
	c.notifyLocked()
	c.mu.Unlock()
	c.emit(RootUpdate{State: state, StateChanged: true, Err: err})
}

func (c *RootClient) emit(update RootUpdate) {
	select {
	case <-c.ctx.Done():
	case c.updates <- update:
	}
}

func (c *RootClient) retry(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *RootClient) position() (string, int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rootID, c.cursor
}

func (c *RootClient) setConnection(connection RootConnection) {
	c.mu.Lock()
	c.conn = connection
	c.notifyLocked()
	c.mu.Unlock()
}

func (c *RootClient) clearConnection(connection RootConnection) {
	c.mu.Lock()
	if c.conn == connection {
		c.conn = nil
		c.notifyLocked()
	}
	c.mu.Unlock()
}

func (c *RootClient) notifyLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}
