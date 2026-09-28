package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ClientPreferencesFile is separate from host-owned runtime configuration so a
// stale UI cannot overwrite newly saved provider credentials or runtime options.
const ClientPreferencesFile = "client-preferences.json"

type clientPreferences struct {
	Theme         string `json:"theme"`
	Sidebar       *bool  `json:"sidebar"`
	Repl          *bool  `json:"repl"`
	Panel         string `json:"panel"`
	Mouse         *bool  `json:"mouse"`
	Thinking      *bool  `json:"thinking"`
	CollapsePaste *bool  `json:"collapse_paste"`
}

func preferencesFromConfig(c *Config) clientPreferences {
	return clientPreferences{Theme: c.Theme, Sidebar: c.Sidebar, Repl: c.Repl, Panel: c.Panel, Mouse: c.Mouse, Thinking: c.Thinking, CollapsePaste: c.CollapsePaste}
}

func loadClientPreferences(c *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, ClientPreferencesFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var prefs clientPreferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return err
	}
	c.Theme, c.Sidebar, c.Repl, c.Panel, c.Mouse, c.Thinking, c.CollapsePaste = prefs.Theme, prefs.Sidebar, prefs.Repl, prefs.Panel, prefs.Mouse, prefs.Thinking, prefs.CollapsePaste
	return nil
}

// SavePreferences saves client presentation state without reading or writing
// the runtime configuration. Existing global preferences are fallback values
// until the client first writes this file.
func (c *Config) SavePreferences() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(preferencesFromConfig(c), "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".client-preferences-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(dir, ClientPreferencesFile))
}
