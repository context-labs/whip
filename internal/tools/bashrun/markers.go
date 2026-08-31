package bashrun

import (
	"os"
	"strconv"
	"sync"
)

var (
	markersMu    sync.RWMutex
	childMarkers = []string{"WHIP=1"}
)

// SetMarkers records the session/model markers appended to every child env.
// Called once the session exists; idempotent.
func SetMarkers(sessionID, model string) {
	markersMu.Lock()
	childMarkers = []string{"WHIP=1", "WHIP_SESSION_ID=" + sessionID, "WHIP_MODEL=" + model, "WHIP_PID=" + strconv.Itoa(os.Getpid())}
	markersMu.Unlock()
}

func markersSnapshot() []string {
	markersMu.RLock()
	defer markersMu.RUnlock()
	return append([]string(nil), childMarkers...)
}
