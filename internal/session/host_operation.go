package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// HostOperation is accepted human work, not a prompt or a guest cell. The fixed
// surface cannot invoke arbitrary coordination or acquire caller-chosen scope.
type HostOperation struct {
	Module    string          `json:"module"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (h HostOperation) Validate() error {
	allowed := false
	switch h.Module {
	case "agents":
		// Only the dedicated child-transfer admission can create this input.
		allowed = h.Name == "spawn"
	case "browser":
		switch h.Name {
		case "list_tabs", "open", "attach", "run", "detach", "allow_preview_port":
			allowed = true
		}
	case "computer":
		allowed = h.Name == "run"
	case "shell":
		allowed = h.Name == "run"
	case "files":
		switch h.Name {
		case "read", "write", "patch", "list", "search", "diagnostics":
			allowed = true
		}
	case "tools":
		allowed = toolIdentifier.MatchString(h.Name)
	}
	if !allowed {
		return fmt.Errorf("%w: unsupported direct host operation", ErrInvalid)
	}
	var object map[string]json.RawMessage
	if len(h.Arguments) > MaxDocumentBytes/2 || !utf8.Valid(h.Arguments) || json.Unmarshal(h.Arguments, &object) != nil || object == nil {
		return fmt.Errorf("%w: host arguments require a bounded JSON object", ErrInvalid)
	}
	return nil
}

func (h HostOperation) Normalize() (HostOperation, error) {
	if err := h.Validate(); err != nil {
		return HostOperation{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(h.Arguments))
	decoder.UseNumber()
	var args map[string]any
	if err := decoder.Decode(&args); err != nil {
		return HostOperation{}, err
	}
	raw, err := json.Marshal(args)
	h.Arguments = raw
	return h, err
}

// DirectCapability identifies only the base intent. Preparation may narrow its
// resource and diagnostics may add one standing-only observation.
func (h HostOperation) DirectCapability(capability string) bool {
	return h.Module == "browser" && (h.Name == "list_tabs" && capability == "browser.catalog" || h.Name != "list_tabs" && capability == "browser.control" || (h.Name == "run" || h.Name == "detach") && (capability == "browser.external" || capability == "browser.external.upload")) || h.Module == "computer" && h.Name == "run" && (capability == "computer.run" || capability == "computer.run.trusted" || capability == "computer.applescript") || capability == h.Module+"."+h.Name || h.Module == "files" && (h.Name == "write" || h.Name == "patch" || h.Name == "diagnostics") && capability == "lsp.diagnostics"
}
