package config

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/mcpconfig"
	"github.com/context-labs/whip/internal/session"
)

func TestHostMCPDeclarationsRoundTripWithoutEffects(t *testing.T) {
	directory := t.TempDir()
	host := Default()
	host.MCP.Servers = map[string]mcpconfig.Server{"fixture": {Command: []string{"does-not-exist"}, Env: map[string]string{"TOKEN": "!must-not-run"}}}
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	authority, err := NewAuthority(directory)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Host.MCP.Servers["fixture"].Env["TOKEN"] != "!must-not-run" {
		t.Fatal("declaration changed")
	}
	_, err = authority.Update(t.Context(), snapshot.Revision, func(value *Host) error {
		server := value.MCP.Servers["fixture"]
		server.Origin = "whip"
		value.MCP.Servers["fixture"] = server
		return nil
	})
	if !errors.Is(err, session.ErrInvalid) {
		t.Fatal("provenance accepted", err)
	}
	current, err := authority.Snapshot(t.Context())
	if err != nil || current.Revision != snapshot.Revision {
		t.Fatal("failed update published", err)
	}
}
