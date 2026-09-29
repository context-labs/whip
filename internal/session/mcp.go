package session

import (
	"fmt"
	"slices"
	"unicode/utf8"
)

// MCPSelection publishes server names; it never grants connection or tool
// authority. All and an explicit list are mutually exclusive, including empty.
type MCPSelection struct {
	All     bool     `json:"all"`
	Servers []string `json:"servers"`
}

func (s MCPSelection) Validate() error {
	if len(s.Servers) > 64 || s.All && len(s.Servers) != 0 {
		return fmt.Errorf("%w: invalid MCP server selection", ErrInvalid)
	}
	for index, name := range s.Servers {
		if err := ValidateText(name, 256); err != nil {
			return err
		}
		if !utf8.ValidString(name) || slices.Contains(s.Servers[:index], name) {
			return fmt.Errorf("%w: invalid or repeated MCP server name", ErrInvalid)
		}
	}
	return nil
}

func (s *MCPSelection) Contains(name string) bool {
	return s == nil || s.All || slices.Contains(s.Servers, name)
}

// Allowed returns the pure discovery filter: nil is all; a non-nil empty
// slice selects none. The returned list belongs to the caller.
func (s *MCPSelection) Allowed() []string {
	if s == nil || s.All {
		return nil
	}
	return append([]string{}, s.Servers...)
}

func narrowMCP(ceiling, candidate *MCPSelection) error {
	if ceiling == nil || ceiling.All {
		return nil
	}
	if candidate == nil || candidate.All {
		return fmt.Errorf("%w: MCP selection exceeds initial binding ceiling", ErrInvalid)
	}
	for _, name := range candidate.Servers {
		if !ceiling.Contains(name) {
			return fmt.Errorf("%w: MCP server exceeds initial binding ceiling", ErrInvalid)
		}
	}
	return nil
}
