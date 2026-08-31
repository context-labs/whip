package bashrun

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// Bash children carry the agent markers so scripts can detect they're under whip.
func TestChildMarkers(t *testing.T) {
	SetMarkers("sess123", "kimi-k3-fast")
	res := Run(context.Background(), Options{Command: "env", Timeout: 5 * time.Second})
	for _, want := range []string{"WHIP=1", "WHIP_SESSION_ID=sess123", "WHIP_MODEL=kimi-k3-fast", "WHIP_PID="} {
		if !strings.Contains(res.Output, want) {
			t.Fatalf("child env missing %q:\n%s", want, res.Output)
		}
	}
}

func TestMarkerSnapshotsStayAtomicDuringSessionPublication(t *testing.T) {
	const iterations = 1_000
	SetMarkers("session-initial", "model-initial")
	start := make(chan struct{})
	errCh := make(chan string, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := range iterations {
			SetMarkers(fmt.Sprintf("session-%d", i), fmt.Sprintf("model-%d", i))
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range iterations {
			markers := markersSnapshot()
			if len(markers) == 1 { // the safe zero-session value
				continue
			}
			if len(markers) != 4 {
				select {
				case errCh <- fmt.Sprintf("marker count = %d", len(markers)):
				default:
				}
				return
			}
			session := strings.TrimPrefix(markers[1], "WHIP_SESSION_ID=session-")
			model := strings.TrimPrefix(markers[2], "WHIP_MODEL=model-")
			if session != model {
				select {
				case errCh <- fmt.Sprintf("mixed marker snapshot: %v", markers):
				default:
				}
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}
