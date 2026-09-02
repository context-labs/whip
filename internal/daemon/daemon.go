package daemon

import (
	"context"
	"errors"
	"sync"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

var ErrClosed = errors.New("daemon closed")

type Factory func(context.Context, session.Meta, []llm.Message) (Components, error)

type rootEntry struct {
	ready chan struct{}
	root  *Session
	err   error
}

// Daemon owns the durable store and exactly one live actor per opened root.
type Daemon struct {
	store   *session.Store
	factory Factory
	control *Control
	ctx     context.Context
	cancel  context.CancelFunc

	mu      sync.Mutex
	wg      sync.WaitGroup
	roots   map[string]*rootEntry
	closing bool
	once    sync.Once
	err     error
}

// New applies daemon-startup recovery before any root can be opened.
func New(store *session.Store, factory Factory) (*Daemon, error) {
	if store == nil || factory == nil {
		return nil, errors.New("daemon requires a store and root factory")
	}
	if !store.AcquireDaemon() {
		return nil, errors.New("store already has a daemon owner")
	}
	if err := store.Recover(context.Background()); err != nil {
		store.ReleaseDaemon()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	daemon := &Daemon{store: store, factory: factory, ctx: ctx, cancel: cancel, roots: make(map[string]*rootEntry)}
	daemon.control = newControl(ctx, store)
	return daemon, nil
}

// Open returns the existing live root or constructs it without holding the
// registry lock across store, factory, or actor work.
func (d *Daemon) Open(rootID string) (*Session, error) {
	if rootID == "" {
		return nil, errors.New("root ID is required")
	}
	d.mu.Lock()
	if d.closing {
		d.mu.Unlock()
		return nil, ErrClosed
	}
	d.mu.Unlock()
	meta, history, err := d.store.Load(rootID)
	if err != nil {
		return nil, err
	}
	rootID = meta.ID

	d.mu.Lock()
	if d.closing {
		d.mu.Unlock()
		return nil, ErrClosed
	}
	if entry := d.roots[rootID]; entry != nil {
		ready := entry.ready
		d.mu.Unlock()
		select {
		case <-ready:
		case <-d.ctx.Done():
			return nil, ErrClosed
		}
		return d.opened(entry)
	}
	entry := &rootEntry{ready: make(chan struct{})}
	d.roots[rootID] = entry
	d.mu.Unlock()

	func() {
		defer func() {
			if value := recover(); value != nil {
				entry.err = panicError("root construction", value)
			}
		}()
		entry.root, entry.err = d.open(meta, history)
	}()
	d.mu.Lock()
	root := entry.root
	if entry.err != nil {
		delete(d.roots, rootID)
	} else if root != nil {
		d.wg.Add(1)
	}
	close(entry.ready)
	d.mu.Unlock()
	if root != nil {
		go func() {
			defer d.wg.Done()
			d.tombstone(rootID, entry, root)
		}()
	}
	return d.opened(entry)
}

// ResumeActive reconstructs detached roots that own durable schedules or
// subscriptions. It is called after the protocol server owns the process.
func (d *Daemon) ResumeActive(ctx context.Context) error {
	rootIDs, err := d.store.ActiveRootIDs(ctx)
	if err != nil {
		return err
	}
	for _, rootID := range rootIDs {
		if _, err := d.Open(rootID); err != nil {
			return err
		}
	}
	return nil
}

func (d *Daemon) tombstone(rootID string, entry *rootEntry, root *Session) {
	<-root.Done()
	d.mu.Lock()
	if d.roots[rootID] == entry {
		entry.root = nil
		entry.err = root.Err()
	}
	d.mu.Unlock()
}

func (d *Daemon) opened(entry *rootEntry) (*Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if entry.err != nil || entry.root == nil {
		return entry.root, entry.err
	}
	select {
	case <-entry.root.Done():
		return entry.root, entry.root.Err()
	default:
		return entry.root, nil
	}
}

func (d *Daemon) entryRoot(entry *rootEntry) *Session {
	d.mu.Lock()
	defer d.mu.Unlock()
	return entry.root
}

func (d *Daemon) open(meta session.Meta, history []llm.Message) (_ *Session, err error) {
	authority, err := d.store.EnsureClassicAuthority(d.ctx, meta.ID)
	if err != nil {
		return nil, err
	}
	components, err := d.factory(d.ctx, meta, history)
	if err != nil {
		return nil, err
	}
	started := false
	var root *Session
	defer func() {
		if started {
			return
		}
		if components.Runner != nil {
			err = errors.Join(err, safeClose("runner", components.Runner.Close))
		}
		if components.MCP != nil {
			err = errors.Join(err, safeClose("mcp", components.MCP.Close))
		}
		if components.Runtime != nil {
			err = errors.Join(err, safeClose("runtime", components.Runtime.Close))
		}
		err = errors.Join(err, d.store.Processes().StopRoot(meta.ID))
		if root != nil {
			root.supervisor.stop()
			root.supervisor.wait()
		}
	}()
	if components.Runner == nil {
		return nil, errors.New("root factory returned no runner")
	}
	root = newSession(d.store, meta, authority, components)
	if binder, ok := components.Runner.(interface{ bind(*Session) error }); ok {
		if err := binder.bind(root); err != nil {
			return nil, err
		}
	}
	if components.Bind != nil {
		if err := components.Bind(root); err != nil {
			return nil, err
		}
	}
	if manager, ok := components.MCP.(interface {
		SetProcessOptions(*capability.ProcessManager, string, string, map[string]string)
		SetOnChange(func())
		Tools() []tools.Tool
	}); ok {
		manager.SetProcessOptions(d.store.Processes(), meta.ID, meta.CWD, nil)
		if runner, ok := components.Runner.(*agentRunner); ok {
			updateTools := func() { runner.agent.SetMCPTools(manager.Tools()) }
			manager.SetOnChange(updateTools)
			updateTools()
		}
	}
	if supervised, ok := components.MCP.(interface {
		SetLauncher(func(string, func()) bool)
	}); ok {
		supervised.SetLauncher(root.supervisor.launchWorker)
	}
	if lifecycle, ok := components.MCP.(interface{ Start(context.Context) }); ok {
		lifecycle.Start(root.supervisor.ctx)
	}
	root.supervisor.startActor(root.run)
	root.startScheduler()
	root.notify()
	started = true
	return root, nil
}

// Close stops roots outside the registry lock, then closes the shared store.
func (d *Daemon) Close() error {
	d.once.Do(func() {
		defer d.store.ReleaseDaemon()
		d.mu.Lock()
		d.closing = true
		d.cancel()
		entries := make([]*rootEntry, 0, len(d.roots))
		for _, entry := range d.roots {
			entries = append(entries, entry)
		}
		d.mu.Unlock()
		for _, entry := range entries {
			select {
			case <-entry.ready:
				if root := d.entryRoot(entry); root != nil {
					root.requestStop(false)
				}
			default:
			}
		}
		for _, entry := range entries {
			<-entry.ready
			if root := d.entryRoot(entry); root != nil {
				root.requestStop(false)
			}
		}
		for _, entry := range entries {
			if root := d.entryRoot(entry); root != nil {
				<-root.Done()
			}
		}
		d.wg.Wait()
		<-d.control.done
		d.err = d.store.Close()
	})
	return d.err
}
