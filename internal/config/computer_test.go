package config

import (
	"context"
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/computerconfig"
	"github.com/context-labs/whip/internal/session"
)

func TestComputerConfigurationExplicitPrivateVersionAndValidation(t *testing.T) {
	dir := t.TempDir()
	host, err := Initialize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if host.Version != Version || host.Computer.Enabled || host.Computer.HelperExecutable != "" {
		t.Fatal(host.Computer)
	}
	authority, err := NewAuthority(dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := authority.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []computerconfig.Config{{HelperExecutable: "relative"}, {Allow: []string{"app", "APP"}}, {Deny: []string{"\nprivate"}}} {
		if _, err := authority.Update(t.Context(), snapshot.Revision, func(h *Host) error { h.Computer = cfg; return nil }); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("invalid computer settings accepted", err)
		}
	}
	if _, err := authority.Update(t.Context(), snapshot.Revision, func(h *Host) error {
		h.Computer = computerconfig.Config{Enabled: true, HelperExecutable: "/explicit/helper", Allow: []string{"app"}, DefaultDeny: true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(dir)
	if err != nil || !reloaded.Computer.Enabled || reloaded.Computer.HelperExecutable != "/explicit/helper" {
		t.Fatal(reloaded.Computer, err)
	}
}
