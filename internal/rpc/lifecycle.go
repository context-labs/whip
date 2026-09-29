package rpc

import (
	"sync"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

// HostLifecycle is command-owned process metadata and an explicit stop hook.
// The server calls stop after attempting the acknowledgement write, including
// lost acknowledgements. Runtime.Close remains the sole execution teardown owner.
type HostLifecycle struct {
	mu       sync.Mutex
	status   protocol.HostStatus
	stop     func()
	stopOnce sync.Once
}

func NewHostLifecycle(runtimeID, epoch protocol.ID, pid int, build string, started time.Time, stop func()) *HostLifecycle {
	return &HostLifecycle{status: protocol.HostStatus{RuntimeID: runtimeID, ProcessEpoch: epoch, PID: pid, Build: build, StartedAt: started.UTC().Format(time.RFC3339Nano)}, stop: stop}
}

func (h *HostLifecycle) SetWebEndpoint(value string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status.WebEndpoint = value
}

func (h *HostLifecycle) snapshot() protocol.HostStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status
}

func (h *HostLifecycle) requestStop() { h.stopOnce.Do(h.stop) }
