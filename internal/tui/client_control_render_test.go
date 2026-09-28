package tui

import (
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestRenderCompactionSettings(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		settings protocol.CompactionSettingsResult
		want     string
	}{
		{
			name:     "automatic",
			settings: protocol.CompactionSettingsResult{BuiltinDefault: true},
			want:     "compaction model: Automatic (this conversation’s model and provider)",
		},
		{
			name:     "custom former default",
			settings: protocol.CompactionSettingsResult{Model: "deepseek-v4-flash-0731", Provider: "inference-net"},
			want:     "compaction model: deepseek-v4-flash-0731 inference-net",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := json.Marshal(test.settings)
			if err != nil {
				t.Fatal(err)
			}
			text, handled, err := renderRuntimeControl("compaction.configure", string(output))
			if err != nil || !handled || text != test.want {
				t.Fatalf("render = %q %t %v, want %q", text, handled, err, test.want)
			}
		})
	}
}
