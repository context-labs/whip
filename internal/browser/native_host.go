package browser

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/context-labs/whip/internal/browserconfig"
	"github.com/context-labs/whip/internal/capability"
)

var (
	ErrNativeStale = errors.New("external browser generation ended; explicit reconnect required")
	ErrNativeBusy  = errors.New("external browser capacity is full")
)

// NativeHost owns external browser lifetimes. It borrows the host's one process
// manager. Capture only reserves bounded metadata; Run is the sole launch path
// and requires a callback checking the already committed operation dispatch.
// A named browser belongs to a tree root. Children need an explicit SQL grant
// for its exact resource; sharing this map never grants control.
type NativeHost struct {
	changes  sync.Mutex
	mu       sync.Mutex
	options  NativeOptions
	entries  map[string]*nativeEntry
	closed   bool
	changing bool
	group    sync.WaitGroup
	active   chan struct{}
	open     func(context.Context, context.Context, NativeOptions) (*NativeConnection, error)
}
type nativeEntry struct {
	host              *NativeHost
	description       NativeDescription
	ctx               context.Context
	cancel            context.CancelFunc
	retired           bool
	connection        *NativeConnection
	slots             chan struct{}
	active            chan struct{}
	group             sync.WaitGroup
	leases            map[*NativeLease]struct{}
	run               context.Context
	check             func(context.Context) error
	uploadCount       int
	uploadBytes       int64
	uploadDirectories []string
	uploadResources   map[string]struct{}
}

type NativeDescription struct {
	RootID     string `json:"root_id"`
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	Driver     string `json:"driver"`
	Generation string `json:"generation"`
	Resource   string `json:"resource"`
	State      string `json:"state"`
}
type NativeCapture struct {
	entry *nativeEntry
	agent string
}
type NativeLease struct {
	capture *NativeCapture
	ctx     context.Context
	cancel  context.CancelFunc
	stop    func() bool
	stopped chan struct{}
	once    sync.Once
	used    bool
	ran     bool
	closed  bool
	done    chan struct{}
}

func NewNativeHost(directory string, processes *capability.ProcessManager) *NativeHost {
	return &NativeHost{options: NativeOptions{Directory: directory, Processes: processes, Config: browserconfig.Config{Mode: "disabled"}, Driver: DriverRod}, entries: map[string]*nativeEntry{}, active: make(chan struct{}, 4), open: OpenNative}
}

// Update is passive and invalidates captures only when effective settings change.
// Canceled leases and launched processes are joined before replacement is usable.
func (h *NativeHost) Update(settings browserconfig.Config, driver string) error {
	settings, err := settings.Normalize()
	if err != nil {
		return err
	}
	if driver != DriverRod && driver != DriverChromedp {
		return errors.New("invalid external browser driver")
	}
	h.changes.Lock()
	defer h.changes.Unlock()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return ErrNativeStale
	}
	if h.options.Config == settings && h.options.Driver == driver {
		h.mu.Unlock()
		return nil
	}
	h.changing = true
	old := h.takeEntriesLocked()
	h.options.Config, h.options.Driver = settings, driver
	h.mu.Unlock()
	closeNativeEntries(old)
	h.mu.Lock()
	h.changing = false
	h.mu.Unlock()
	return nil
}

func (h *NativeHost) takeEntriesLocked() []*nativeEntry {
	entries := make([]*nativeEntry, 0, len(h.entries))
	for _, entry := range h.entries {
		entry.retired = true
		entry.cancel()
		entries = append(entries, entry)
	}
	h.entries = map[string]*nativeEntry{}
	return entries
}

func closeNativeEntries(entries []*nativeEntry) {
	for _, entry := range entries {
		entry.retire()
	}
	for _, entry := range entries {
		entry.group.Wait()
	}
}

func (h *NativeHost) Close() error {
	h.changes.Lock()
	defer h.changes.Unlock()
	h.mu.Lock()
	h.closed = true
	entries := h.takeEntriesLocked()
	h.mu.Unlock()
	closeNativeEntries(entries)
	h.group.Wait()
	return nil
}

func nativeName(raw, mode string) (string, error) {
	if prefix, name, found := strings.Cut(raw, ":"); found {
		if prefix != mode {
			return "", errors.New("browser session mode must match explicit host configuration")
		}
		raw = name
	}
	if !sessionNameRe.MatchString(raw) {
		return "", errors.New("browser session requires 1-64 letters, digits, dashes or underscores")
	}
	return raw, nil
}

func (h *NativeHost) Capture(root, agent, name string) (*NativeCapture, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.changing || h.options.Config.Mode == "disabled" {
		return nil, ErrNativeStale
	}
	if root == "" || agent == "" || len(root) > 128 || len(agent) > 128 {
		return nil, errors.New("invalid browser owner")
	}
	name, err := nativeName(name, h.options.Config.Mode)
	if err != nil {
		return nil, err
	}
	key := root + ":" + name
	if existing := h.entries[key]; existing != nil {
		if existing.retired || existing.ctx.Err() != nil {
			return nil, ErrNativeStale
		}
		return &NativeCapture{entry: existing, agent: agent}, nil
	}
	count := 0
	for _, entry := range h.entries {
		if entry.description.RootID == root {
			count++
		}
		if h.options.Config.Mode == "live" || h.options.Config.Mode == "extension" {
			return nil, ErrNativeBusy
		}
	}
	if count >= 4 || len(h.entries) >= 16 {
		return nil, ErrNativeBusy
	}
	entry := h.newEntryLocked(root, name)
	h.entries[key] = entry
	return &NativeCapture{entry: entry, agent: agent}, nil
}

func (h *NativeHost) newEntryLocked(root, name string) *nativeEntry {
	generation := rand.Text()
	sum := sha256.Sum256([]byte(root + "\x00" + name + "\x00" + generation))
	lifetime, cancel := context.WithCancel(context.Background())
	d := NativeDescription{RootID: root, Name: name, Mode: h.options.Config.Mode, Driver: h.options.Driver, Generation: generation, Resource: "browser-external:" + hex.EncodeToString(sum[:]), State: "prepared"}
	return &nativeEntry{host: h, description: d, ctx: lifetime, cancel: cancel, slots: make(chan struct{}, 5), active: make(chan struct{}, 1), leases: map[*NativeLease]struct{}{}}
}

func (h *NativeHost) List(root string) []NativeDescription {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []NativeDescription{}
	for _, entry := range h.entries {
		if root == "" || entry.description.RootID == root {
			out = append(out, entry.descriptionLocked())
		}
	}
	slices.SortFunc(out, func(a, b NativeDescription) int { return strings.Compare(a.RootID+":"+a.Name, b.RootID+":"+b.Name) })
	return out
}

func (e *nativeEntry) descriptionLocked() NativeDescription {
	d := e.description
	if e.retired || e.ctx.Err() != nil {
		d.State = "ended"
	} else if e.connection != nil {
		d.State = "connected"
	}
	return d
}

func (c *NativeCapture) Description() NativeDescription {
	h := c.entry.host
	h.mu.Lock()
	defer h.mu.Unlock()
	return c.entry.descriptionLocked()
}
func (c *NativeCapture) Lifetime() context.Context { return c.entry.ctx }

// Reconnect changes only the ephemeral generation. A subsequent approved Run
// acquires the replacement; this explicit action does not launch or replay work.
func (h *NativeHost) Reconnect(root, name, generation string) (NativeDescription, error) {
	h.changes.Lock()
	defer h.changes.Unlock()
	h.mu.Lock()
	entry := h.entries[root+":"+name]
	if h.closed || entry == nil || entry.description.Generation != generation {
		h.mu.Unlock()
		return NativeDescription{}, ErrNativeStale
	}
	entry.retired = true
	entry.cancel()
	h.mu.Unlock()
	entry.retire()
	entry.group.Wait()
	h.mu.Lock()
	defer h.mu.Unlock()
	replacement := h.newEntryLocked(root, name)
	h.entries[root+":"+name] = replacement
	return replacement.description, nil
}

func (h *NativeHost) RevokeResource(resource string) {
	h.mu.Lock()
	var affected []*nativeEntry
	for _, entry := range h.entries {
		_, upload := entry.uploadResources[resource]
		if entry.description.Resource == resource || upload {
			entry.retired = true
			entry.cancel()
			affected = append(affected, entry)
		}
	}
	h.mu.Unlock()
	closeNativeEntries(affected)
}

func (h *NativeHost) RevokeOwner(root, agent string) {
	h.mu.Lock()
	var affected []*nativeEntry
	for _, entry := range h.entries {
		if entry.description.RootID != root {
			continue
		}
		if root == agent {
			entry.retired = true
			entry.cancel()
			affected = append(affected, entry)
			continue
		}
		for lease := range entry.leases {
			if lease.capture.agent == agent {
				lease.cancel()
			}
		}
	}
	h.mu.Unlock()
	closeNativeEntries(affected)
	h.mu.Lock()
	for _, entry := range affected {
		key := entry.description.RootID + ":" + entry.description.Name
		if h.entries[key] == entry {
			delete(h.entries, key)
		}
	}
	h.mu.Unlock()
}

func (e *nativeEntry) retire() {
	h := e.host
	h.mu.Lock()
	e.retired = true
	e.cancel()
	connection := e.connection
	directories := e.uploadDirectories
	e.uploadDirectories = nil
	h.mu.Unlock()
	if connection != nil {
		_ = connection.Close()
	}
	for _, directory := range directories {
		_ = os.RemoveAll(directory)
	}
}

func (c *NativeCapture) Acquire(ctx context.Context) (*NativeLease, error) {
	e, h := c.entry, c.entry.host
	h.mu.Lock()
	if h.closed || e.retired || e.ctx.Err() != nil {
		h.mu.Unlock()
		return nil, ErrNativeStale
	}
	select {
	case e.slots <- struct{}{}:
	default:
		h.mu.Unlock()
		return nil, ErrNativeBusy
	}
	owned, cancel := context.WithCancel(ctx)
	lease := &NativeLease{capture: c, ctx: owned, cancel: cancel, stopped: make(chan struct{}), done: make(chan struct{})}
	lease.stop = context.AfterFunc(e.ctx, func() { defer close(lease.stopped); cancel() })
	e.leases[lease] = struct{}{}
	e.group.Add(1)
	h.group.Add(1)
	h.mu.Unlock()
	fail := func(err error) (*NativeLease, error) { lease.Close(); return nil, err }
	select {
	case e.active <- struct{}{}:
	case <-owned.Done():
		return fail(owned.Err())
	}
	// Holding the entry slot while waiting for the global slot prevents interleaved
	// batches, while both queues remain bounded and cancellation releases them.
	select {
	case h.active <- struct{}{}:
		lease.used = true
	case <-owned.Done():
		<-e.active
		return fail(owned.Err())
	}
	if err := owned.Err(); err != nil {
		return fail(err)
	}
	return lease, nil
}

func (l *NativeLease) Close() {
	l.once.Do(func() {
		l.cancel()
		if !l.stop() {
			<-l.stopped
		}
		e, h := l.capture.entry, l.capture.entry.host
		h.mu.Lock()
		l.closed = true
		ran := l.ran
		h.mu.Unlock()
		if ran {
			<-l.done
		}
		h.mu.Lock()
		delete(e.leases, l)
		h.mu.Unlock()
		if l.used {
			<-h.active
			<-e.active
		}
		<-e.slots
		e.group.Done()
		h.group.Done()
	})
}

func (e *nativeEntry) authorize(ctx context.Context) (context.Context, func(), error) {
	h := e.host
	h.mu.Lock()
	run, check := e.run, e.check
	retired := e.retired
	h.mu.Unlock()
	if retired || run == nil || check == nil || run.Err() != nil {
		return nil, nil, ErrNativeStale
	}
	owned, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	stop := context.AfterFunc(run, func() { defer close(done); cancel() })
	release := func() {
		cancel()
		if !stop() {
			<-done
		}
	}
	if err := check(owned); err != nil {
		release()
		return nil, nil, err
	}
	if err := owned.Err(); err != nil {
		release()
		return nil, nil, err
	}
	return owned, release, nil
}

// Run is one use. No callback can run before the dispatch check, including
// startup CDP commands. Any failed batch retires its connection; none is replayed.
func (l *NativeLease) Run(ctx context.Context, check func(context.Context) error, fn func(context.Context, Backend) error) error {
	e, h := l.capture.entry, l.capture.entry.host
	if check == nil || fn == nil {
		return errors.New("browser dispatch check is required")
	}
	h.mu.Lock()
	if e.retired || e.run != nil || !l.used || l.ran || l.closed || l.ctx.Err() != nil {
		h.mu.Unlock()
		return ErrNativeStale
	}
	run, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})
	stop := context.AfterFunc(l.ctx, func() { defer close(stopped); cancel() })
	l.ran = true
	e.run, e.check = run, check
	options := h.options
	options.Profile = fmt.Sprintf("%x", sha256.Sum256([]byte(e.description.RootID+"\x00"+e.description.Name)))
	options.ProcessOwner = "external-browser:" + e.description.Generation
	options.Authorize = e.authorize
	connection := e.connection

	h.mu.Unlock()
	defer func() {
		cancel()
		if !stop() {
			<-stopped
		}
		h.mu.Lock()
		e.run, e.check = nil, nil
		h.mu.Unlock()
		close(l.done)
	}()
	if err := check(run); err != nil {
		return err
	}
	if connection == nil {
		var err error
		connection, err = h.open(run, e.ctx, options)
		if err != nil {
			e.retire()
			return err
		}
		h.mu.Lock()
		if e.retired || e.ctx.Err() != nil {
			h.mu.Unlock()
			_ = connection.Close()
			return ErrNativeStale
		}
		e.connection = connection
		e.group.Add(1)
		h.group.Add(1)
		h.mu.Unlock()
		go func() { defer e.group.Done(); defer h.group.Done(); <-connection.Lifetime().Done(); e.retire() }()
	}
	err := fn(run, connection)
	if err != nil || run.Err() != nil {
		e.retire()
		return errors.Join(err, run.Err())
	}
	return nil
}

func (c *NativeCapture) CheckConfig(settings browserconfig.Config, driver string) error {
	settings, err := settings.Normalize()
	if err != nil {
		return err
	}
	h := c.entry.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || c.entry.retired || c.entry.ctx.Err() != nil || settings != h.options.Config || driver != h.options.Driver {
		return ErrNativeStale
	}
	return nil
}

// Detach retires the exact resource without opening it. It requires committed
// dispatch just like Run; an ended name is not silently recycled by Capture.
func (l *NativeLease) Detach(ctx context.Context, check func(context.Context) error) error {
	e, h := l.capture.entry, l.capture.entry.host
	h.mu.Lock()
	if l.ran || l.closed || !l.used || e.retired {
		h.mu.Unlock()
		return ErrNativeStale
	}
	l.ran = true
	h.mu.Unlock()
	defer close(l.done)
	if err := l.ctx.Err(); err != nil {
		return err
	}
	if check == nil {
		return errors.New("browser dispatch check is required")
	}
	if err := check(ctx); err != nil {
		return err
	}
	e.retire()
	return nil
}

// Disconnect is an explicit human generation-CAS release. It does not recycle
// the name or make an old grant valid for a later browser connection.
func (h *NativeHost) Disconnect(root, name, generation string) (NativeDescription, error) {
	h.changes.Lock()
	defer h.changes.Unlock()
	h.mu.Lock()
	entry := h.entries[root+":"+name]
	if h.closed || entry == nil || entry.description.Generation != generation {
		h.mu.Unlock()
		return NativeDescription{}, ErrNativeStale
	}
	entry.retired = true
	entry.cancel()
	h.mu.Unlock()
	entry.retire()
	entry.group.Wait()
	h.mu.Lock()
	defer h.mu.Unlock()
	return entry.descriptionLocked(), nil
}

func (l *NativeLease) reserveUploads(count int, size int64) error {
	e, h := l.capture.entry, l.capture.entry.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if l.closed || e.retired || l.ctx.Err() != nil {
		return ErrNativeStale
	}
	if count < 0 || size < 0 || e.uploadCount+count > 16 || e.uploadBytes+size > 16<<20 {
		return errors.New("external browser retained upload capacity is full; reconnect explicitly")
	}
	e.uploadCount += count
	e.uploadBytes += size
	return nil
}

func (l *NativeLease) keepUploadDirectory(directory string) error {
	e, h := l.capture.entry, l.capture.entry.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if l.closed || e.retired || l.ctx.Err() != nil {
		return ErrNativeStale
	}
	e.uploadDirectories = append(e.uploadDirectories, directory)
	return nil
}

func (c *NativeCapture) UploadResource(uploads *NativeUploads) (string, error) {
	e, h := c.entry, c.entry.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if e.retired || e.ctx.Err() != nil {
		return "", ErrNativeStale
	}
	resource := uploads.Resource(e.description.Resource)
	if e.uploadResources == nil {
		e.uploadResources = map[string]struct{}{}
	}
	if _, exists := e.uploadResources[resource]; !exists {
		if len(e.uploadResources) >= 16 {
			return "", ErrNativeBusy
		}
		e.uploadResources[resource] = struct{}{}
	}
	return resource, nil
}
