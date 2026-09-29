package session

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// MCPSelection publishes server names; it never grants connection or tool
// authority. All and an explicit list are mutually exclusive, including empty.
type MCPSelection struct {
	All     bool     `json:"all"`
	Servers []string `json:"servers"`
}

// MCPCatalogRequest is fixed read intent assigned by the host adapter. The
// store verifies its copied owner and selection against the owning turn.
type MCPCatalogRequest struct {
	SessionID      SessionID     `json:"session_id"`
	TreeID         TreeID        `json:"tree_id"`
	ConfigRevision Revision      `json:"config_revision"`
	Selection      *MCPSelection `json:"selection"`
	Action         string        `json:"action"`
	Server         string        `json:"server"`
	Tool           string        `json:"tool"`
	Query          string        `json:"query"`
	Limit          int           `json:"limit"`
}

func (r MCPCatalogRequest) Validate() error {
	if r.Selection == nil || r.ConfigRevision < 1 {
		return fmt.Errorf("%w: missing captured MCP selection", ErrInvalid)
	}
	if err := r.Selection.Validate(); err != nil {
		return err
	}
	for _, id := range []string{string(r.SessionID), string(r.TreeID)} {
		if err := ValidateID(id); err != nil {
			return err
		}
	}
	for _, name := range []string{r.Server, r.Tool} {
		if len(name) > 256 || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
			return fmt.Errorf("%w: invalid MCP catalog name", ErrInvalid)
		}
	}
	if len(r.Query) > 4096 || !utf8.ValidString(r.Query) || strings.ContainsRune(r.Query, 0) || r.Limit < 1 || r.Limit > 100 {
		return fmt.Errorf("%w: invalid MCP catalog query", ErrInvalid)
	}
	if r.Server != "" && !r.Selection.Contains(r.Server) {
		return fmt.Errorf("%w: MCP server not selected", ErrInvalid)
	}
	switch r.Action {
	case "list_servers":
		if r.Server != "" || r.Tool != "" || r.Query != "" {
			return fmt.Errorf("%w: invalid server listing", ErrInvalid)
		}
	case "list_tools", "instructions":
		if r.Server == "" || r.Tool != "" || r.Query != "" {
			return fmt.Errorf("%w: invalid server metadata request", ErrInvalid)
		}
	case "describe":
		if r.Server == "" || r.Tool == "" || r.Query != "" {
			return fmt.Errorf("%w: invalid tool metadata request", ErrInvalid)
		}
	case "search":
		if r.Tool != "" || strings.TrimSpace(r.Query) == "" {
			return fmt.Errorf("%w: invalid tool search", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown MCP metadata operation", ErrInvalid)
	}
	return nil
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
