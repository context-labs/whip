package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/config"
)

func TestConfigurationDefaultPermissionMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	cfg := config.Default()
	cfg.Theme = "preserved-theme"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	service := NewProviderService(t.Context(), "permission-defaults")
	defer service.Close()
	before, err := service.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if before.DefaultPermissionMode != "prompt" {
		t.Fatalf("legacy default = %q", before.DefaultPermissionMode)
	}
	encoded, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"default_permission_mode":"prompt"`)) {
		t.Fatalf("resolved default omitted: %s", encoded)
	}
	for _, mode := range []string{"automatic", "prompt"} {
		after, err := service.UpdateConfiguration(ConfigurationUpdate{Revision: before.Revision, DefaultPermissionMode: &mode})
		if err != nil {
			t.Fatal(err)
		}
		if after.DefaultPermissionMode != mode || after.Revision == before.Revision {
			t.Fatalf("update = %+v", after)
		}
		if _, err := service.UpdateConfiguration(ConfigurationUpdate{Revision: before.Revision, DefaultPermissionMode: &mode}); !errors.Is(err, config.ErrRevisionConflict) {
			t.Fatalf("stale update = %v", err)
		}
		persisted, _, err := config.ReadVersioned()
		if err != nil {
			t.Fatal(err)
		}
		if persisted.DefaultPermissionMode != mode || persisted.Theme != cfg.Theme || persisted.DefaultModel != cfg.DefaultModel {
			t.Fatalf("configuration not preserved: %+v", persisted)
		}
		fresh, err := service.ReadConfiguration()
		if err != nil || fresh.DefaultPermissionMode != mode {
			t.Fatalf("read = %+v, %v", fresh, err)
		}
		before = after
	}
	for _, mode := range []string{"", "invalid", "Automatic", " automatic "} {
		t.Run("reject="+mode, func(t *testing.T) {
			path := filepath.Join(home, "config.json")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			brandIcons := !before.BrandIcons
			_, err = service.UpdateConfiguration(ConfigurationUpdate{
				Revision: before.Revision, DefaultPermissionMode: &mode, BrandIcons: &brandIcons,
			})
			if err == nil {
				t.Fatal("invalid permission mode accepted")
			}
			current, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(original, current) {
				t.Fatal("invalid update changed configuration")
			}
			after, err := service.ReadConfiguration()
			if err != nil || after.Revision != before.Revision {
				t.Fatalf("revision changed: %+v, %v", after, err)
			}
		})
	}
}
