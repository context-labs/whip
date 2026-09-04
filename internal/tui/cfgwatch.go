package tui

import (
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/config"
)

// cfgSyncMsg carries a freshly observed config-file state into the update
// loop (mod time + the theme it declares, "" = auto).
type cfgSyncMsg struct {
	mod   time.Time
	theme string
}

// cfgSyncTick is the poll timer for the config watcher. Polling (1s) instead
// of fsnotify keeps this dependency-free and immune to atomic-save rename
// semantics; a 1s lag on a rare cross-session setting change is invisible.
// Skipping a poll on failure is fine — the next tick re-stats.
type cfgSyncTick struct{}

// watchConfig schedules the next config-file poll.
func (m *model) watchConfig() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return cfgSyncTick{} })
}

// cfgSync polls the config file after a tick and schedules the next poll.
func (m *model) cfgSync() (tea.Model, tea.Cmd) {
	if m.prog == nil { // headless tests: no loop to feed
		return m, nil
	}
	dir, err := config.Dir()
	if err != nil {
		return m, m.watchConfig()
	}
	fi, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil || !fi.ModTime().After(m.cfgMod) {
		return m, m.watchConfig()
	}
	cfg, err := config.Load()
	if err != nil {
		return m, m.watchConfig() // a half-written file is retried next tick
	}
	return m, tea.Sequence(
		func() tea.Msg { return cfgSyncMsg{mod: fi.ModTime(), theme: cfg.Theme} },
		m.watchConfig(),
	)
}

// applyCfgSync folds a config change from another session into this one.
// Only unpinned keys sync: a setting the user changed HERE (cfgExtra) stays
// local so two sessions tweaking different keys don't fight.
func (m *model) applyCfgSync(msg cfgSyncMsg) {
	m.cfgMod = msg.mod
	if _, pinned := m.cfgExtra["theme"]; pinned {
		return
	}
	if msg.theme != m.cfg.Theme {
		m.cfg.Theme = msg.theme
		m.themeHow = m.applyTheme(msg.theme) // keep /report's detection source current
		m.refreshVP()                        // repaint under the new scheme without a terminal resize
	}
}
