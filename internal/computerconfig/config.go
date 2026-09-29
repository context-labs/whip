// Package computerconfig contains explicit computer availability declarations.
// These values never grant agent authority or start native processes.
package computerconfig

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Config struct {
	Enabled          bool     `json:"enabled"`
	HelperExecutable string   `json:"helper_executable"`
	Allow            []string `json:"allow"`
	Deny             []string `json:"deny"`
	DefaultDeny      bool     `json:"default_deny"`
}

// CanonicalApp accepts exact names and bundle IDs, not fuzzy app selectors.
func CanonicalApp(value string) (string, error) {
	if len(value) > 256 || !utf8.ValidString(value) || strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0 {
		return "", errors.New("invalid computer application name")
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", errors.New("computer application name required")
	}
	return value, nil
}

func (c Config) Normalize() (Config, error) {
	if c.HelperExecutable != "" && (!utf8.ValidString(c.HelperExecutable) || len(c.HelperExecutable) > 4096 || strings.ContainsAny(c.HelperExecutable, "\x00\r\n") || !filepath.IsAbs(c.HelperExecutable) || filepath.Clean(c.HelperExecutable) != c.HelperExecutable) {
		return Config{}, errors.New("computer helper requires a clean absolute executable path")
	}
	lists := []*[]string{&c.Allow, &c.Deny}
	for _, list := range lists {
		if len(*list) > 64 {
			return Config{}, errors.New("computer application policy limit exceeded")
		}
		normalized := make([]string, 0, len(*list))
		for _, value := range *list {
			name, err := CanonicalApp(value)
			if err != nil {
				return Config{}, err
			}
			if slices.Contains(normalized, name) {
				return Config{}, errors.New("duplicate computer application policy")
			}
			normalized = append(normalized, name)
		}
		slices.Sort(normalized)
		*list = normalized
	}
	return c, nil
}

// Check rejects hard policy restrictions. An unlisted app may still receive
// one-off SQL consent; NeedsConsent tells the host to require that path.
func (c Config) Check(names ...string) error {
	if !c.Enabled {
		return errors.New("computer control is disabled")
	}
	for _, name := range names {
		canonical, err := CanonicalApp(name)
		if err != nil {
			return err
		}
		if slices.Contains(c.Deny, canonical) {
			return errors.New("computer application is blocked by host policy")
		}
	}
	return nil
}

func (c Config) NeedsConsent(names []string) bool {
	if !c.DefaultDeny {
		return false
	}
	for _, name := range names {
		if !slices.Contains(c.Allow, name) {
			return true
		}
	}
	return false
}
