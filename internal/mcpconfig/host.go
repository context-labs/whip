package mcpconfig

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

// Host contains declarations only. Publishing these values neither resolves
// credentials nor connects to a server. Nil BrandIcons preserves the default on.
type Host struct {
	Servers    map[string]Server `json:"servers"`
	Imports    Import            `json:"imports"`
	BrandIcons *bool             `json:"brand_icons"`
}

func (h Host) Validate() error {
	if len(h.Servers) > 64 {
		return errors.New("too many native MCP servers")
	}
	for name, value := range h.Servers {
		if !ValidName(name) {
			return errors.New("invalid native MCP server name")
		}
		if value.Origin != "" || value.Source != "" {
			return errors.New("native MCP declarations cannot claim imported provenance")
		}
		if problem := value.Valid(); problem != "" {
			return errors.New(problem)
		}
	}
	return h.Imports.Validate()
}

func ValidName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 256 && utf8.ValidString(name) && !strings.ContainsRune(name, 0)
}

func (i Import) Validate() error {
	for _, source := range []*ImportSource{i.Claude, i.Codex, i.Project, i.Opencode} {
		if source == nil {
			continue
		}
		for _, names := range [][]string{source.Only, source.Exclude} {
			if len(names) > 256 {
				return errors.New("too many MCP source filters")
			}
			for index, name := range names {
				if !ValidName(name) || slices.Contains(names[:index], name) {
					return errors.New("invalid or repeated MCP source filter")
				}
			}
		}
	}
	return nil
}
