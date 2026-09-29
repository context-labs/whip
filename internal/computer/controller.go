package computer

import (
	"context"
	"crypto/rand"
	"errors"
	"maps"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/computerconfig"
)

var (
	ErrControllerClosed  = errors.New("computer controller is closed")
	ErrGenerationRetired = errors.New("computer generation is retired; explicit reconnect required")
	ErrBatchCapacity     = errors.New("computer batch queue is full")
)

// Controller owns one revocable desktop-control lifetime, borrowing the host's
// process manager. Closing it never closes human apps. It has no session map.
// Update and Reconnect invalidate captured work and join it before returning.
type Controller struct {
	changes          sync.Mutex
	mu               sync.Mutex
	processes        *capability.ProcessManager
	owner, directory string
	environment      map[string]string
	scriptCommand    CommandRunner
	epoch            *computerEpoch
	closed           bool
	slots            chan struct{}
	active           chan struct{}
}

type computerEpoch struct {
	config     computerconfig.Config
	id         string
	ctx        context.Context
	cancel     context.CancelFunc
	connection *Connection
	leases     sync.WaitGroup
	retired    bool
}

type ControllerOptions struct {
	Processes        *capability.ProcessManager
	Owner, Directory string
	Environment      map[string]string
	// RunScript injects a host-owned runner; nil uses a bounded managed osascript.
	RunScript CommandRunner
}

func NewController(options ControllerOptions, config computerconfig.Config) (*Controller, error) {
	if options.Processes == nil || options.Owner == "" || !filepath.IsAbs(options.Directory) {
		return nil, errors.New("computer controller requires explicit process ownership")
	}
	config, err := config.Normalize()
	if err != nil {
		return nil, err
	}
	c := &Controller{processes: options.Processes, owner: options.Owner, directory: options.Directory, environment: maps.Clone(options.Environment), scriptCommand: options.RunScript, slots: make(chan struct{}, 5), active: make(chan struct{}, 1)}
	c.epoch = newComputerEpoch(config)
	return c, nil
}

func newComputerEpoch(config computerconfig.Config) *computerEpoch {
	token := rand.Text()
	ctx, cancel := context.WithCancel(context.Background())
	return &computerEpoch{config: config, id: "control_" + token, ctx: ctx, cancel: cancel}
}

type ControllerStatus struct {
	Generation        string
	State             string
	Enabled           bool
	NativeConfigured  bool
	PlatformSupported bool
}

// Status is passive: it neither probes nor launches a helper or app.
func (c *Controller) Status() ControllerStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.epoch
	state := "available"
	switch {
	case c.closed:
		state = "closed"
	case !e.config.Enabled:
		state = "disabled"
	case e.retired || e.ctx.Err() != nil:
		state = "retired"
	case e.connection != nil:
		if e.connection.Lifetime().Err() != nil {
			e.retired = true
			e.cancel()
			state = "retired"
		} else {
			state = "connected"
		}
	}
	return ControllerStatus{Generation: e.id, State: state, Enabled: e.config.Enabled, NativeConfigured: e.config.HelperExecutable != "", PlatformSupported: Available()}
}

// Update changes availability policy, never SQL authority. Equal settings are a
// passive no-op, including after transport loss: they cannot reconnect it.
func (c *Controller) Update(config computerconfig.Config) error {
	config, err := config.Normalize()
	if err != nil {
		return err
	}
	c.changes.Lock()
	defer c.changes.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrControllerClosed
	}
	if reflect.DeepEqual(c.epoch.config, config) {
		c.mu.Unlock()
		return nil
	}
	old := c.epoch
	c.epoch = newComputerEpoch(config)
	old.retired = true
	old.cancel()
	connection := old.connection
	c.mu.Unlock()
	if connection != nil {
		connection.Close()
	}
	old.leases.Wait()
	return nil
}

// Reconnect is an explicit human control action. It changes the authority
// generation but leaves startup lazy until an approved batch acquires it.
func (c *Controller) Reconnect() error {
	c.changes.Lock()
	defer c.changes.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrControllerClosed
	}
	old := c.epoch
	c.epoch = newComputerEpoch(old.config)
	old.retired = true
	old.cancel()
	connection := old.connection
	c.mu.Unlock()
	if connection != nil {
		connection.Close()
	}
	old.leases.Wait()
	return nil
}

// Disconnect revokes this generation without authorizing a replacement.
func (c *Controller) Disconnect() { c.retire(false) }
func (c *Controller) Close()      { c.retire(true) }
func (c *Controller) retire(closeController bool) {
	c.changes.Lock()
	defer c.changes.Unlock()
	c.mu.Lock()
	if closeController {
		c.closed = true
	}
	old := c.epoch
	old.retired = true
	old.cancel()
	connection := old.connection
	c.mu.Unlock()
	if connection != nil {
		connection.Close()
	}
	old.leases.Wait()
}

// Capture is inert and immutable. Its lifetime may cancel a permission wait;
// Acquire still rechecks it after consent and queue admission.
type Capture struct {
	controller   *Controller
	epoch        *computerEpoch
	batch        *Batch
	needsConsent bool
}

func (c *Controller) Capture(batch *Batch) (*Capture, error) {
	if batch == nil {
		return nil, errors.New("computer batch required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.epoch
	if c.closed {
		return nil, ErrControllerClosed
	}
	if e.retired || e.ctx.Err() != nil {
		return nil, ErrGenerationRetired
	}
	if e.connection != nil && e.connection.Lifetime().Err() != nil {
		e.retired = true
		e.cancel()
		return nil, ErrGenerationRetired
	}
	if err := e.config.Check(batch.intent.Applications...); err != nil {
		return nil, err
	}
	if batch.intent.BroadScript && len(e.config.Deny) > 0 {
		return nil, errors.New("broad AppleScript cannot enforce hard application denials")
	}
	if batch.intent.Native && e.config.HelperExecutable == "" {
		return nil, errors.New("native computer helper is not configured")
	}
	return &Capture{controller: c, epoch: e, batch: batch, needsConsent: batch.intent.BroadScript || batch.intent.Permissions || (batch.intent.Inventory && e.config.DefaultDeny) || e.config.NeedsConsent(batch.intent.Applications)}, nil
}
func (c *Capture) Lifetime() context.Context { return c.epoch.ctx }
func (c *Capture) Resource() string          { resource, _ := c.batch.Resource(c.epoch.id); return resource }
func (c *Capture) NeedsConsent() bool        { return c.needsConsent }

// Lease is one whole batch reservation. Run is one-shot, even after failure.
// The holder releases it after durable settlement; it owns no helper restart.
type Lease struct {
	capture *Capture
	ctx     context.Context
	cancel  context.CancelFunc
	stop    func() bool
	close   sync.Once
	runMu   sync.Mutex
	ran     bool
	closed  bool
	done    chan struct{}
}

func (capture *Capture) Acquire(ctx context.Context) (*Lease, error) {
	c, e := capture.controller, capture.epoch
	c.mu.Lock()
	if c.closed || c.epoch != e || e.retired || e.ctx.Err() != nil {
		c.mu.Unlock()
		return nil, ErrGenerationRetired
	}
	e.leases.Add(1)
	c.mu.Unlock()
	failed := true
	defer func() {
		if failed {
			e.leases.Done()
		}
	}()
	select {
	case c.slots <- struct{}{}:
	default:
		return nil, ErrBatchCapacity
	}
	queued := true
	defer func() {
		if queued {
			<-c.slots
		}
	}()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.ctx, cancel)
	cleanup := true
	defer func() {
		if cleanup {
			stop()
			cancel()
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case c.active <- struct{}{}:
	}
	active := true
	defer func() {
		if active {
			<-c.active
		}
	}()
	c.mu.Lock()
	if c.closed || c.epoch != e || e.retired || ctx.Err() != nil {
		c.mu.Unlock()
		return nil, ErrGenerationRetired
	}
	connection := e.connection
	c.mu.Unlock()
	if capture.batch.intent.Native {
		if connection == nil {
			var err error
			stopStartup := context.AfterFunc(ctx, e.cancel)
			connection, err = OpenConnection(e.ctx, ConnectionOptions{Processes: c.processes, Owner: c.owner, Executable: e.config.HelperExecutable, Directory: c.directory, Environment: c.environment})
			stopStartup()
			if err != nil {
				c.mu.Lock()
				e.retired = true
				e.cancel()
				c.mu.Unlock()
				return nil, err
			}
			c.mu.Lock()
			if c.closed || c.epoch != e || e.retired || ctx.Err() != nil {
				c.mu.Unlock()
				connection.Close()
				return nil, ErrGenerationRetired
			}
			e.connection = connection
			context.AfterFunc(connection.Lifetime(), e.cancel)
			c.mu.Unlock()
		}
		if connection.Lifetime().Err() != nil {
			c.mu.Lock()
			e.retired = true
			e.cancel()
			c.mu.Unlock()
			return nil, ErrGenerationRetired
		}
	}
	failed, queued, active, cleanup = false, false, false, false
	return &Lease{capture: capture, ctx: ctx, cancel: cancel, stop: stop}, nil
}

func (l *Lease) Close() {
	l.close.Do(func() {
		l.runMu.Lock()
		l.closed = true
		done := l.done
		l.runMu.Unlock()
		l.stop()
		l.cancel()
		if done != nil {
			<-done
		}
		<-l.capture.controller.active
		<-l.capture.controller.slots
		l.capture.epoch.leases.Done()
	})
}

// CheckConfig is safe inside the lease: it never joins itself or refreshes
// authority. A changed policy fails this invocation; the next preparation may
// install a new generation through Update.
func (capture *Capture) CheckConfig(config computerconfig.Config) error {
	config, err := config.Normalize()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(config, capture.epoch.config) {
		return errors.New("computer availability policy changed")
	}
	return nil
}

func (l *Lease) check() error {
	if err := l.ctx.Err(); err != nil {
		return err
	}
	c, e := l.capture.controller, l.capture.epoch
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.epoch != e || e.retired {
		return ErrGenerationRetired
	}
	return nil
}
