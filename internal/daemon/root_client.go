package daemon

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/context-labs/whip/internal/protocol"
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
	Subscribe(context.Context, string, int64) (SubscribeResult, error)
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
	ClientID string
	RootID   string
	Create   *CreateSession
	// DeferCreate connects host services first; StartSession admits the template.
	DeferCreate bool
	Connector   RootConnector
	RetryMin    time.Duration
	RetryMax    time.Duration
}

// RootClient reconnects one daemon-root subscription and retries commands
// with their original identity. It owns no provider, store, tool, or process.
type RootClient struct {
	clientID    string
	instanceID  string
	create      *CreateSession
	deferCreate bool
	connect     RootConnector
	retryMin    time.Duration
	retryMax    time.Duration

	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	once    sync.Once
	started atomic.Bool

	mu                sync.RWMutex
	state             RootClientState
	err               error
	rootID            string
	cursor            int64
	subscriptionID    string
	activeTurns       map[string]string
	submittedCommands map[string]string
	conn              RootConnection
	changed           chan struct{}

	updates chan RootUpdate
	nextID  atomic.Uint64
}

func NewRootClient(options RootClientOptions) (*RootClient, error) {
	if options.ClientID == "" || options.Connector == nil {
		return nil, errors.New("root client requires a client ID and connector")
	}
	if options.RootID == "" && options.Create == nil {
		return nil, errors.New("root client requires a root or session template")
	}
	if options.RootID != "" && options.Create != nil {
		return nil, errors.New("root client cannot resume and create simultaneously")
	}
	if options.DeferCreate && options.Create == nil {
		return nil, errors.New("deferred creation requires a session template")
	}
	if options.RetryMin <= 0 {
		options.RetryMin = 25 * time.Millisecond
	}
	if options.RetryMax < options.RetryMin {
		options.RetryMax = time.Second
	}
	if options.Create != nil {
		create := *options.Create
		options.Create = &create
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &RootClient{
		clientID: options.ClientID, instanceID: rand.Text(),
		rootID: options.RootID, create: options.Create, deferCreate: options.DeferCreate, connect: options.Connector,
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

// StartSession admits a deferred template once, freezing its route before the
// reconnect loop sends the existing stable session-create command identity.
func (c *RootClient) StartSession(model, provider string) error {
	if model == "" || provider == "" {
		return errors.New("a model and provider are required")
	}
	c.mu.Lock()
	if c.ctx.Err() != nil || c.state != RootLive || c.conn == nil || !c.deferCreate || c.create == nil {
		c.mu.Unlock()
		return errors.New("session creation is not available")
	}
	create := *c.create
	create.Model, create.Provider = model, provider
	c.create, c.deferCreate, c.state = &create, false, RootReconnecting
	connection := c.conn
	c.notifyLocked()
	c.mu.Unlock()
	// As with SwitchRoot, the owner loop performs synchronization after close.
	return connection.Close()
}

// SwitchRoot changes the subscribed session after a successful daemon action.
// Closing the current connection makes the run loop synchronize the new root
// before it accepts another command.
func (c *RootClient) SwitchRoot(rootID string) error {
	if rootID == "" {
		return errors.New("session root is required")
	}
	c.mu.Lock()
	c.rootID, c.cursor, c.create = rootID, 0, nil
	c.state = RootSnapshotting
	connection := c.conn
	c.notifyLocked()
	c.mu.Unlock()
	c.emit(RootUpdate{State: RootSnapshotting, StateChanged: true})
	if connection != nil {
		return connection.Close()
	}
	return nil
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
	if operation == "cancel" || operation == "agent.turn.cancel" {
		var target struct {
			ID              string `json:"id"`
			TurnID          string `json:"turn_id"`
			TargetCommandID string `json:"target_command_id,omitempty"`
		}
		if err := json.Unmarshal(raw, &target); err != nil {
			return RootAction{}, err
		}
		c.mu.RLock()
		agentID := target.ID
		if agentID == "" {
			agentID = c.rootID
		}
		if target.TurnID == "" && target.TargetCommandID == "" {
			if operation == "cancel" {
				target.TargetCommandID = c.submittedCommands[agentID]
			}
			if target.TargetCommandID == "" {
				target.TurnID = c.activeTurns[agentID]
			}
		}
		c.mu.RUnlock()
		if operation == "cancel" {
			raw, _ = json.Marshal(struct {
				TurnID          string `json:"turn_id"`
				TargetCommandID string `json:"target_command_id,omitempty"`
			}{TurnID: target.TurnID, TargetCommandID: target.TargetCommandID})
		} else {
			raw, _ = json.Marshal(target)
		}
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
	tracked := false
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
		if !tracked && (action.Operation == "submit" || action.Operation == "steer") {
			c.mu.Lock()
			if c.submittedCommands == nil {
				c.submittedCommands = make(map[string]string)
			}
			c.submittedCommands[action.RootID] = action.CommandID
			tracked = true
			c.mu.Unlock()
		}
		result, err := connection.Command(ctx, CommandParams{
			CommandID: action.CommandID, Scope: string(session.CommandScopeRoot), RootID: action.RootID,
			Operation: action.Operation, Payload: action.Payload,
		})
		if tracked && (result.Status == "succeeded" || result.Status == "failed" || result.Status == "cancelled" || result.Status == "interrupted") {
			c.mu.Lock()
			if c.submittedCommands[action.RootID] == action.CommandID {
				delete(c.submittedCommands, action.RootID)
			}
			c.mu.Unlock()
		}
		if err == nil {
			return result, nil
		}
		if operation, ok := protocol.LookupRuntime(action.Operation); ok && operation.Execution == protocol.Ephemeral {
			return result, err
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

// ValidateProvider performs an ephemeral daemon request. Unlike Command, its
// credential-bearing payload is never admitted to the durable command log.
func (c *RootClient) ValidateProvider(ctx context.Context, params ProviderValidateParams) (ProviderValidateResult, error) {
	type validator interface {
		ValidateProvider(context.Context, ProviderValidateParams) (ProviderValidateResult, error)
	}
	for {
		c.mu.RLock()
		state, connection := c.state, c.conn
		c.mu.RUnlock()
		if state != RootLive || connection == nil {
			if !c.started.Load() || !c.waitForLive(ctx) {
				if err := ctx.Err(); err != nil {
					return ProviderValidateResult{}, err
				}
				return ProviderValidateResult{}, c.ctx.Err()
			}
			continue
		}
		provider, ok := connection.(validator)
		if !ok {
			return ProviderValidateResult{}, errors.New("daemon connection does not support provider validation")
		}
		result, err := provider.ValidateProvider(ctx, params)
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return ProviderValidateResult{}, ctx.Err()
		}
		select {
		case <-connection.Done():
			continue
		default:
			return ProviderValidateResult{}, err
		}
	}
}

func (c *RootClient) DecidePermission(ctx context.Context, action RootAction, permissionID string, allow bool, reason, remember string) (PermissionDecisionResult, error) {
	if action.CommandID == "" || action.Operation != "permission.decide" || action.RootID == "" || permissionID == "" {
		return PermissionDecisionResult{}, errors.New("permission action requires stable command and permission identities")
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
		approver, ok := connection.(interface {
			DecidePermission(context.Context, PermissionDecision) (PermissionDecisionResult, error)
		})
		if !ok {
			return PermissionDecisionResult{}, errors.New("daemon connection cannot approve permissions")
		}
		result, err := approver.DecidePermission(ctx, PermissionDecision{
			CommandID: action.CommandID, RootID: action.RootID, PermissionID: permissionID, Allow: allow, Reason: reason, Remember: remember,
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

// SetPermissionMode changes prompting behavior through the durable command path.
func (c *RootClient) SetPermissionMode(ctx context.Context, action RootAction) (CommandResult, error) {
	if action.CommandID == "" || action.Operation != "permission.mode" || action.RootID == "" {
		return CommandResult{}, errors.New("permission mode requires stable command and root identities")
	}
	return c.Command(ctx, action)
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
			if failure, ok := errors.AsType[*RPCError](err); ok && failure.Code != -32002 && failure.Code != -32004 {
				return
			}
			if !c.retry(delay) {
				return
			}
			delay = min(delay*2, c.retryMax)
			continue
		}
		delay = c.retryMin
		c.setConnection(connection)
		if err := c.synchronize(connection); err != nil {

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
		c.mu.Lock()
		deferred := c.deferCreate
		if !deferred && c.create != nil && c.create.ExecutionEngine == "" {
			if initialized, ok := connection.(interface{ InitializeResult() InitializeResult }); ok {
				c.create.ExecutionEngine = initialized.InitializeResult().DefaultExecutionEngine
			}
			if c.create.ExecutionEngine == "" {
				c.create.ExecutionEngine = "starlark"
			}
		}
		create := c.create
		c.mu.Unlock()
		if deferred {
			return nil
		}
		payload, err := json.Marshal(create)
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
		rootID, cursor = result.Output, 0
	}

	c.transition(RootSnapshotting, nil)
	if cursor > 0 {
		replay, err := connection.Replay(c.ctx, ReplayParams{RootID: rootID, Cursor: cursor})
		if err != nil {
			return err
		}
		if !replay.Expired {
			contiguous := true
			for i := range replay.Events {
				if !c.emitEvent(replay.Events[i]) {
					contiguous = false
					break
				}
			}
			if contiguous {
				subscription, err := connection.Subscribe(c.ctx, rootID, c.Cursor())
				c.mu.Lock()
				c.subscriptionID = subscription.SubscriptionID
				c.mu.Unlock()
				return err
			}
		}
	}
	snapshot, err := connection.Snapshot(c.ctx, rootID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.cursor = snapshot.Cursor
	c.activeTurns = snapshot.ActiveTurns
	c.mu.Unlock()
	c.emit(RootUpdate{Snapshot: &snapshot})
	subscription, err := connection.Subscribe(c.ctx, rootID, snapshot.Cursor)
	c.mu.Lock()
	c.subscriptionID = subscription.SubscriptionID
	c.mu.Unlock()
	return err
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
			if !c.emitEvent(event) {
				_ = connection.Close()
				return true
			}
		}
	}
}

func (c *RootClient) emitEvent(event ProtocolEvent) bool {
	c.mu.Lock()
	if (event.SubscriptionID != "" && event.SubscriptionID != c.subscriptionID) || event.RootID != c.rootID || event.Seq <= c.cursor {
		c.mu.Unlock()
		return true
	}
	if c.cursor > 0 && event.Seq != c.cursor+1 {
		c.mu.Unlock()
		return false
	}
	var lifecycle session.LifecycleEvent
	if json.Unmarshal(event.Payload, &lifecycle) == nil {
		if c.activeTurns == nil {
			c.activeTurns = map[string]string{}
		}
		if event.Kind == "turn.started" || event.Kind == "agent.turn.started" {
			c.activeTurns[lifecycle.AgentID] = lifecycle.TurnID
		}
		if strings.HasPrefix(event.Kind, "turn.") || strings.HasPrefix(event.Kind, "agent.turn.") {
			switch lifecycle.Status {
			case "succeeded", "failed", "cancelled", "interrupted":
				delete(c.activeTurns, lifecycle.AgentID)
			}
		}
	}
	c.cursor = event.Seq
	c.mu.Unlock()
	c.emit(RootUpdate{Event: &event})
	return true
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
