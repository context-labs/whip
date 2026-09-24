package bashrun

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Bash children carry the agent markers so scripts can detect they're under whip.
func TestChildMarkers(t *testing.T) {
	res := Run(context.Background(), Options{Command: "env", Timeout: 5 * time.Second, Env: Markers("sess123", "kimi-k3-fast")})
	for _, want := range []string{"WHIP=1", "WHIP_SESSION_ID=sess123", "WHIP_MODEL=kimi-k3-fast", "WHIP_PID="} {
		if !strings.Contains(res.Output, want) {
			t.Fatalf("child env missing %q:\n%s", want, res.Output)
		}
	}
}
