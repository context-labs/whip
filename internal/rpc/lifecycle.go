package rpc

import (
	"strings"
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

// SetWebStatus publishes one command-owned gateway transition atomically.
func (h *HostLifecycle) SetWebStatus(state, endpoint string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status.WebState, h.status.WebEndpoint, h.status.WebError = state, endpoint, ""
	if err != nil {
		text := []rune(strings.ToValidUTF8(err.Error(), "�"))
		if len(text) > 1024 {
			text = text[:1024]
		}
		h.status.WebError = string(text)
	}
}

func (h *HostLifecycle) snapshot() protocol.HostStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status
}

func (h *HostLifecycle) requestStop() { h.stopOnce.Do(h.stop) }
