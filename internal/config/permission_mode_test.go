package config

import (
	"encoding/json"
	"testing"
)

func TestDefaultPermissionModePersistence(t *testing.T) {
	for _, mode := range []string{"", "prompt", "automatic"} {
		t.Run("mode="+mode, func(t *testing.T) {
			t.Setenv("WHIPCODE_HOME", t.TempDir())
			cfg := Default()
			cfg.DefaultPermissionMode = mode
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			got, _, err := ReadVersioned()
			if err != nil {
				t.Fatal(err)
			}
			if got.DefaultPermissionMode != mode {
				t.Fatalf("permission mode = %q, want %q", got.DefaultPermissionMode, mode)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			raw, present := fields["defaultPermissionMode"]
			if mode == "" && present {
				t.Fatal("legacy default should remain omitted")
			}
			if mode != "" && string(raw) != `"`+mode+`"` {
				t.Fatalf("wire field = %s", raw)
			}
		})
	}
}
