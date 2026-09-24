package main

import (
	"testing"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/session"
)

// Host integrations follow the definition's capabilities: an agent without the
// browser or computer capability gets no browser manager or computer policy.
func TestDaemonToolServicesFollowCapabilities(t *testing.T) {
	cfg := config.Default()
	meta := session.Meta{ID: "root"}
	narrow := daemonToolServices(cfg, meta, "model", []string{"read", "write", "shell"})
	t.Cleanup(narrow.Close)
	if narrow.Browser() != nil || narrow.ComputerPolicy() != nil {
		t.Fatal("browser or computer wired without the capability")
	}
	full := daemonToolServices(cfg, meta, "model", agentdef.Coding().Capabilities)
	t.Cleanup(full.Close)
	if full.Browser() == nil || full.ComputerPolicy() == nil {
		t.Fatal("coding definition must wire browser and computer")
	}
	disabled := config.Default()
	off := false
	disabled.Browser.Enabled, disabled.Computer.Enabled = &off, &off
	host := daemonToolServices(disabled, meta, "model", agentdef.Coding().Capabilities)
	t.Cleanup(host.Close)
	if host.Browser() != nil || host.ComputerPolicy() != nil {
		t.Fatal("host configuration must still disable integrations")
	}
}
