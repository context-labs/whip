package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
)

var configurationMu sync.Mutex

// ErrRevisionConflict means the caller must read the latest configuration.
var ErrRevisionConflict = errors.New("configuration changed; read the current revision and retry")

func configurationRevision(c *Config) string {
	data, _ := json.Marshal(c)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// ReadVersioned returns one coherent configuration and its opaque revision.
func ReadVersioned() (*Config, string, error) {
	configurationMu.Lock()
	defer configurationMu.Unlock()
	c, err := loadUnlocked()
	if err != nil {
		return nil, "", err
	}
	return c, configurationRevision(c), nil
}

// UpdateVersioned serializes a patch against fresh disk state, preserving fields
// the caller does not own. Empty expected is reserved for host-owned updates.
// The callback must not call Load, Save, or another configuration update.
func UpdateVersioned(expected string, patch func(*Config) error) (*Config, string, error) {
	return updateVersioned(expected, patch, false)
}

// UpdateVersionedIfChanged skips disk writes when the patch leaves configuration unchanged.
func UpdateVersionedIfChanged(expected string, patch func(*Config) error) (*Config, string, error) {
	return updateVersioned(expected, patch, true)
}

func updateVersioned(expected string, patch func(*Config) error, skipUnchanged bool) (*Config, string, error) {
	configurationMu.Lock()
	defer configurationMu.Unlock()
	c, err := loadUnlocked()
	if err != nil {
		return nil, "", err
	}
	before := configurationRevision(c)
	if expected != "" && expected != before {
		return nil, "", ErrRevisionConflict
	}
	if err := patch(c); err != nil {
		return nil, "", err
	}
	if skipUnchanged && before == configurationRevision(c) {
		return c, before, nil
	}
	if err := c.saveUnlocked(); err != nil {
		return nil, "", err
	}
	return c, configurationRevision(c), nil
}
