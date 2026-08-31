package tui

import (
	"strings"
	"testing"
)

// Every slash command in the registry must route through the dispatch switch
// to a real handler — the "registry says it exists but the switch 404s" drift
// class. The probe runs the bare command on a scratch model and fails if the
// transcript reports an unknown command.
func TestRegistryEntriesDispatch(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, e := range slashRegistry() {
		if !compactCmdModel().dispatches(e.Name) {
			t.Errorf("%s is in the registry but the command switch doesn't handle it", e.Name)
		}
	}
}

// /help renders from the registry: every entry's hint (and the shell escape)
// must appear in its output.
func TestHelpContainsEveryRegistryHint(t *testing.T) {
	help := helpText()
	for _, e := range registry {
		if !strings.Contains(help, e.Hint) {
			t.Errorf("/help missing hint for %s: %q", e.Name, e.Hint)
		}
		if !strings.Contains(help, e.Name) {
			t.Errorf("/help missing command name %s", e.Name)
		}
	}
	// and the actual /help command prints it
	m := compactCmdModel()
	m.command("/help")
	if !strings.Contains(m.blocks[len(m.blocks)-1].text, "/compact") {
		t.Fatalf("/help output missing registry content: %q", m.blocks[len(m.blocks)-1].text)
	}
}

// The palette's slash-command rows take their description from the registry:
// for every row whose hint is a slash name, the rendered description must
// contain the registry hint.
func TestPaletteListsRegistryCommands(t *testing.T) {
	m := compactCmdModel()
	m.openPalette()
	rows := 0
	for _, it := range m.palette.all {
		if it.dynHint == nil || it.dynDesc == nil {
			continue
		}
		hint := it.dynHint(m)
		if !strings.HasPrefix(hint, "/") || strings.ContainsAny(hint, " ·<") {
			continue // keybind-only or usage-form hints ("/model · tab", "/compact <model>")
		}
		e := registryFind(hint)
		if e == nil {
			t.Errorf("palette row %q hints %q, which is not in the registry", it.title, hint)
			continue
		}
		if !strings.Contains(it.dynDesc(m), e.Hint) {
			t.Errorf("palette row %q desc %q doesn't come from the registry hint %q", it.title, it.dynDesc(m), e.Hint)
		}
		rows++
	}
	if rows < 8 {
		t.Fatalf("expected the palette to surface registry commands, found %d rows", rows)
	}
}

// The completion table is derived from the registry, never hand-maintained.
func TestCompletionMatchesRegistry(t *testing.T) {
	slash := slashRegistry()
	if len(commands) != len(slash) {
		t.Fatalf("completion table has %d entries, registry has %d slash commands", len(commands), len(slash))
	}
	for _, e := range slash {
		found := false
		for _, c := range commands {
			if c.Text == e.Name {
				found = true
				if c.Desc != e.Hint {
					t.Errorf("completion desc for %s = %q, registry hint is %q", e.Name, c.Desc, e.Hint)
				}
			}
		}
		if !found {
			t.Errorf("%s missing from the completion table", e.Name)
		}
	}
}
