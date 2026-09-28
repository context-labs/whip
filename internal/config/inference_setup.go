package config

import (
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

// EnsureInference installs only the managed gateway declaration. Account setup
// cannot replace another route or select defaults on behalf of the user.
func (h *Host) EnsureInference() error {
	if route, ok := h.Providers["inference-net"]; ok && (route.Kind != "openai-chat" || route.BaseURL != "https://api.inference.net/v1" || route.CredentialSource != "inference-net") {
		return fmt.Errorf("%w: inference-net is configured as another provider route", ErrRevisionConflict)
	}
	if err := h.Validate(); err != nil {
		return err
	}
	if h.Providers == nil {
		h.Providers = map[string]Provider{}
	}
	if _, ok := h.Providers["inference-net"]; !ok {
		if len(h.Providers) == 128 {
			return fmt.Errorf("%w: too many provider routes", session.ErrInvalid)
		}
		h.Providers["inference-net"] = Provider{Kind: "openai-chat", BaseURL: "https://api.inference.net/v1", CredentialSource: "inference-net"}
	}
	return nil
}
