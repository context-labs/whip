package tui

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
)

// TestAskYN pins the parsing contract: Enter takes the default, y/n parse in
// any case, garbage re-asks once then falls back to the default.
func TestAskYN(t *testing.T) {
	cases := []struct {
		input string
		def   bool
		want  bool
	}{
		{"\n", true, true},
		{"\n", false, false},
		{"y\n", false, true},
		{"yes\n", false, true},
		{"Y\n", false, true},
		{"n\n", true, false},
		{"No\n", true, false},
		{"garbage\n\n", true, true},           // re-ask, then Enter
		{"garbage\nnonsense\n", false, false}, // two bad answers: default
		{"", false, false},                    // EOF takes the default
	}
	for _, tc := range cases {
		r := bufio.NewReader(strings.NewReader(tc.input))
		var out bytes.Buffer
		if got := askYN(r, &out, "q?", tc.def); got != tc.want {
			t.Errorf("askYN(%q, def=%v) = %v, want %v", tc.input, tc.def, got, tc.want)
		}
	}
}

// driveWizard runs the wizard against a fixture config with scripted answers
// and a throwaway WHIP_HOME. Returns the saved-on-disk config (reloaded, so
// the test proves persistence, not just in-memory mutation).
func driveWizard(t *testing.T, input string) *config.Config {
	t.Helper()
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	cfg := config.Default()
	if err := runSetupWizard(cfg, strings.NewReader(input), &bytes.Buffer{}); err != nil {
		t.Fatalf("runSetupWizard: %v", err)
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return saved
}

// TestWizardDefaultsOptOut pins the Enter-through path: skip provider, default
// thinking (yes → nothing written), no MCP imports — the config file gains
// only an explicit mcpImport block with both sources off.
func TestWizardDefaultsOptOut(t *testing.T) {
	// answers: provider=Enter(skip), thinking=Enter(yes), claude=Enter(no), codex=Enter(no)
	saved := driveWizard(t, "\n\n\n\n")
	if saved.MCPImport == nil || saved.MCPImport.Claude == nil || saved.MCPImport.Codex == nil {
		t.Fatalf("mcpImport should be recorded explicitly, got %+v", saved.MCPImport)
	}
	if *saved.MCPImport.Claude.Enabled || *saved.MCPImport.Codex.Enabled {
		t.Fatalf("Enter should default imports off, got claude=%v codex=%v",
			*saved.MCPImport.Claude.Enabled, *saved.MCPImport.Codex.Enabled)
	}
	if saved.Thinking != nil {
		t.Fatalf("default thinking answer should leave the block absent, got %v", *saved.Thinking)
	}
	if _, ok := saved.Providers["openrouter"]; ok {
		t.Fatal("skipping the provider step must not register openrouter")
	}
}

// TestWizardOptIn pins the yes answers: thinking stays on (nil), imports on.
func TestWizardOptIn(t *testing.T) {
	// provider=skip, thinking=y, claude=y, codex=y
	saved := driveWizard(t, "3\ny\ny\ny\n") //nolint:dupword // scripted yes-answers, one per wizard question
	if !*saved.MCPImport.Claude.Enabled || !*saved.MCPImport.Codex.Enabled {
		t.Fatalf("explicit yes should import, got %+v", saved.MCPImport)
	}
}

// TestWizardThinkingOff pins that "n" writes the thinking block.
func TestWizardThinkingOff(t *testing.T) {
	// provider=skip, thinking=n, claude=n, codex=n
	saved := driveWizard(t, "3\nn\nn\nn\n") //nolint:dupword // scripted no-answers, one per wizard question
	if saved.Thinking == nil || *saved.Thinking {
		t.Fatalf("thinking off should persist as false, got %+v", saved.Thinking)
	}
}

func TestWizardProviderOpenRouterStoresKeyWithoutConstructingProvider(t *testing.T) {
	saved := driveWizard(t, "2\nsk-or-good\n\nn\nn\n") //nolint:dupword // scripted answers, one per question
	p, ok := saved.Providers["openrouter"]
	if !ok {
		t.Fatal("the key should register openrouter")
	}
	if p.APIKey != "sk-or-good" {
		t.Fatalf("provider key = %q", p.APIKey)
	}
}
