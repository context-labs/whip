package inferenceauth

import (
	"context"
	"errors"
	"math"
	"sync"
)

// CapturedMachineKey contains inference authorization only. It never includes a
// management token or user identity. Captures cannot be moved between managers.
type CapturedMachineKey struct {
	Key        string
	KeyID      string
	TeamID     string
	ProjectID  string
	Generation uint64
	owner      *Manager
}

// Manager is the sole writer of its private record in an already existing,
// explicit host directory. The command must prevent other managers/processes
// from owning that directory concurrently. Short local publications hold mu;
// there are no network requests or background goroutines to join.
type Manager struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	storage    *storage
	credential Credentials
	generation uint64
	pending    bool
	deleting   bool
	closed     bool
	closeErr   error
}

// New anchors an owned directory, validates the record, and synchronizes the
// directory before authorizing surviving bytes from an earlier process.
func New(ctx context.Context, directory string) (*Manager, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	disk, err := openStorage(directory)
	if err != nil {
		return nil, err
	}
	credential, err := disk.read()
	if err == nil {
		err = disk.syncDirectory(disk.directory)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = disk.directory.Close()
		return nil, errors.Join(ErrStorage, safeContext(err))
	}
	ctx, cancel := context.WithCancel(ctx)
	return &Manager{ctx: ctx, cancel: cancel, storage: disk, credential: credential}, nil
}

// Snapshot never repairs storage or refreshes credentials. A pending publication
// returns the replacement plus ErrStoragePending; pending logout returns an empty
// record plus that error. Returned expiry pointers are independently owned.
func (m *Manager) Snapshot() (Credentials, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.active(context.Background()); err != nil {
		return Credentials{}, err
	}
	if m.deleting {
		return Credentials{}, ErrStoragePending
	}
	if m.pending {
		return m.credential.clone(), ErrStoragePending
	}
	return m.credential.clone(), nil
}

func (m *Manager) Generation() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generation
}

// Install uses a captured generation even for same-account replacement/rotation.
// A failed pre-publication write leaves existing authorization intact. Once
// rename succeeds, old captures are invalid even if directory sync fails.
func (m *Manager) Install(ctx context.Context, expected uint64, credential Credentials) error {
	credential = credential.clone()
	if err := credential.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.active(ctx); err != nil {
		return err
	}
	if expected != m.generation {
		return ErrChanged
	}
	if m.pending || m.deleting {
		return ErrStoragePending
	}
	if m.generation == math.MaxUint64 {
		return ErrChanged
	}
	published, err := m.storage.save(credential, func() error { return m.active(ctx) })
	if !published {
		return err
	}
	m.generation++
	m.credential = credential
	m.pending = err != nil
	if m.pending {
		return ErrStoragePending
	}
	return nil
}

// Capture may retry only a known local replacement publication. Management
// expiry does not determine whether the independent machine key is usable.
func (m *Manager) Capture(ctx context.Context) (CapturedMachineKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.active(ctx); err != nil {
		return CapturedMachineKey{}, err
	}
	if m.deleting {
		return CapturedMachineKey{}, ErrStoragePending
	}
	if m.pending {
		if _, err := m.storage.save(m.credential, func() error { return m.active(ctx) }); err != nil {
			return CapturedMachineKey{}, errors.Join(ErrStoragePending, safeContext(err))
		}
		m.pending = false
	}
	if m.credential.MachineKey.Value == "" {
		return CapturedMachineKey{}, ErrKeyRequired
	}
	return m.capture(), nil
}

func (m *Manager) capture() CapturedMachineKey {
	return CapturedMachineKey{Key: m.credential.MachineKey.Value, KeyID: m.credential.MachineKey.ID, TeamID: m.credential.Scope.TeamID, ProjectID: m.credential.Scope.ProjectID, Generation: m.generation, owner: m}
}

// Check performs no persistence or network work. It proves authorization at this
// locked point, not atomically with a subsequent HTTP request. Check again after
// any reservation wait and immediately before execution.
func (m *Manager) Check(ctx context.Context, captured CapturedMachineKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.active(ctx); err != nil {
		return err
	}
	if captured.owner != m || captured.Generation != m.generation {
		return ErrChanged
	}
	if m.pending || m.deleting {
		return ErrStoragePending
	}
	if captured.Key == "" || captured != m.capture() {
		return ErrChanged
	}
	return nil
}

// Logout revokes current authorization before touching disk, returning the prior
// private record for later remote cleanup. Failure remains unresolved until an
// explicit Logout retry; Capture and Snapshot never complete deletion. A retry
// returns the same prior record while deletion is pending. No remote work occurs.
func (m *Manager) Logout() (Credentials, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.active(context.Background()); err != nil {
		return Credentials{}, err
	}
	prior := m.credential.clone()
	if !m.deleting {
		// Closed/cleared state also prevents capture reuse if the counter is exhausted.
		if m.generation != math.MaxUint64 {
			m.generation++
		}
		m.deleting = true
		m.pending = false
	}
	if err := m.storage.remove(); err != nil {
		return prior, ErrStoragePending
	}
	m.credential = Credentials{}
	m.deleting = false
	return prior, nil
}

// Close requests cancellation, waits for any local publication, and releases
// the anchored descriptor. It does not conceal or retry unresolved persistence.
func (m *Manager) Close() error {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return m.closeErr
	}
	m.closed = true
	if m.pending || m.deleting {
		m.closeErr = ErrStoragePending
	}
	if err := m.storage.directory.Close(); err != nil {
		m.closeErr = errors.Join(m.closeErr, ErrStorage)
	}
	m.credential = Credentials{}
	return m.closeErr
}

func (m *Manager) active(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.closed {
		return ErrClosed
	}
	return m.ctx.Err()
}

// Filesystem diagnostics may contain private host paths; only cancellation
// crosses the boundary alongside a safe storage category.
func safeContext(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}
