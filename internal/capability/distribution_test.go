package capability

import (
	"testing"

	"github.com/context-labs/whip/internal/buildinfo"
)

func TestDistributionChildEnvironment(t *testing.T) {
	other := "WHIPCODE_HOME"
	if buildinfo.Name == "whipcode" {
		other = "WHIP_HOME"
	}
	if !allowedBaseEnvironment(buildinfo.Env("HOME")) {
		t.Fatal("child loses its distribution home")
	}
	if allowedBaseEnvironment(other) {
		t.Fatal("child inherited the other distribution home")
	}
}
