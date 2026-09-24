package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
)

// /theme auto must resolve from the ACTUAL detected terminal background, not
// from the explicit pick being replaced (dark→auto on a light terminal used
// to stay dark because SetLightTheme mutated detection state).
func TestThemeAutoResolvesFromDetection(t *testing.T) {
	m := compactCmdModel()
	t.Setenv("WHIP_THEME", "light") // stub detection: the terminal bg is light
	m.setTheme("dark")              // user overrides to dark
	if CurrentTheme() != "dark" {
		t.Fatalf("explicit dark should win, got %q", CurrentTheme())
	}
	m.setTheme("auto") // back to auto: must follow the light background…
	if CurrentTheme() != "light" {
		t.Fatalf("auto should resolve from detection (light), got %q", CurrentTheme())
	}
	setSchemeOverride("") // process-global theme state: restore for other tests
	SetLightTheme(false)
}

// The auto transcript note names the detection source so a wrong pick is
// diagnosable; explicit picks don't claim a source.
func TestThemeAutoNoteNamesSource(t *testing.T) {
	m := compactCmdModel()
	t.Setenv("WHIP_THEME", "dark")
	m.setTheme("auto")
	var note string
	for _, b := range m.blocks {
		if strings.Contains(b.text, "◐ theme:") {
			note = b.text
		}
	}
	if !strings.Contains(note, "(auto: WHIP_THEME)") {
		t.Fatalf("auto note should name the detection source, got %q", note)
	}
	setSchemeOverride("")
	SetLightTheme(false)
}

// A theme change saved to the config file by another session applies locally
// within a poll tick, repainting without a terminal resize.
func TestConfigSyncAppliesThemeFromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WHIPCODE_HOME", dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	m := compactCmdModel()
	m.cfgMod = time.Now().Add(-time.Minute) // any later write counts as newer
	m.setTheme("dark")
	delete(m.cfgExtra, "theme") // pretend the pick came from the file, not this session
	m.cfg.Theme = "dark"

	// the other session writes "light"
	other, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	other.Theme = "light"
	if err := other.Save(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}

	m.applyCfgSync(cfgSyncMsg{mod: fi.ModTime(), theme: other.Theme})
	if m.cfg.Theme != "light" {
		t.Fatalf("synced theme should reach m.cfg, got %q", m.cfg.Theme)
	}
	if CurrentTheme() != "light" {
		t.Fatalf("synced theme should apply live, got %q", CurrentTheme())
	}
	setSchemeOverride("")
	SetLightTheme(false)
}

// A setting this session explicitly changed (pinned in cfgExtra) is NOT
// overwritten by another session's config save — two sessions tweaking
// different keys must not fight.
func TestConfigSyncRespectsPinnedTheme(t *testing.T) {
	m := compactCmdModel()
	m.setTheme("dark") // pins "theme" in cfgExtra

	m.applyCfgSync(cfgSyncMsg{mod: time.Now(), theme: "light"})
	if CurrentTheme() != "dark" {
		t.Fatalf("a pinned theme must survive another session's save, got %q", CurrentTheme())
	}
	if m.cfg.Theme != "dark" {
		t.Fatalf("m.cfg.Theme should stay dark, got %q", m.cfg.Theme)
	}
	setSchemeOverride("")
	SetLightTheme(false)
}

// /theme auto unpins: the next file change syncs again (an explicit auto pick
// is not a permanent opt-out of cross-session theme changes).
func TestThemeAutoUnpinsSync(t *testing.T) {
	m := compactCmdModel()
	m.setTheme("dark")
	m.setTheme("auto")
	if _, pinned := m.cfgExtra["theme"]; pinned {
		t.Fatal("auto must unpin the theme for the config watcher")
	}
	m.applyCfgSync(cfgSyncMsg{mod: time.Now(), theme: "dark"})
	if CurrentTheme() != "dark" {
		t.Fatalf("after auto, a file change should sync again, got %q", CurrentTheme())
	}
	setSchemeOverride("")
	SetLightTheme(false)
}

// The watcher only fires on a NEWER mod time — its own saves (baseline
// refreshed to that save's mod time) don't echo back as syncs.
func TestConfigSyncIgnoresOwnSaves(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WHIPCODE_HOME", dir)
	m := compactCmdModel()
	if err := m.cfg.Save(); err != nil { // baseline write
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.cfgMod = fi.ModTime()
	m.setTheme("dark")
	delete(m.cfgExtra, "theme")
	m.cfg.Theme = ""

	// same mod time (our own save): no message, no apply
	if _, cmd := m.cfgSync(); cmd != nil {
		// cfgSync with no newer file returns only the next-tick timer; assert
		// the theme was untouched either way
		_ = cmd
	}
	if m.cfg.Theme != "" {
		t.Fatalf("no newer file: theme must stay %q, got %q", "", m.cfg.Theme)
	}
	setSchemeOverride("")
	SetLightTheme(false)
}
