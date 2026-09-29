package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// BrowserScope is the immutable, host-resolved intent recorded with permission.
// Its opaque handles describe a live connection; persistence cannot restore it.
type BrowserScope struct {
	ProviderID           string               `json:"provider_id"`
	ProviderEpoch        string               `json:"provider_epoch"`
	TabID                string               `json:"tab_id"`
	TabGeneration        string               `json:"tab_generation"`
	ProfileID            string               `json:"profile_id"`
	ControlLineage       string               `json:"control_lineage"`
	AttachmentID         string               `json:"attachment_id"`
	AttachmentGeneration string               `json:"attachment_generation"`
	Preview              *BrowserPreviewScope `json:"preview,omitempty"`
}

type BrowserPreviewScope struct {
	HostID               string `json:"host_id"`
	HostIdentity         string `json:"host_identity"`
	ConnectionGeneration string `json:"connection_generation"`
	EnvironmentID        string `json:"environment_id"`
	Loopback             string `json:"loopback"`
	Ports                []int  `json:"ports"`
}

// Resource is immutable across an explicit child handoff. Private attachments
// still require exact owner and generation; this hash alone never grants control.
func (v BrowserScope) Resource() string {
	v.AttachmentID = ""
	v.AttachmentGeneration = ""
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return "browser:" + hex.EncodeToString(sum[:])
}

// BrowserIntent is assigned by host preparation, never accepted as guest scope.
// PreviousResource names the narrower scope retired by preview expansion.
type BrowserIntent struct {
	SessionID        SessionID       `json:"session_id"`
	TreeID           TreeID          `json:"tree_id"`
	ConfigRevision   Revision        `json:"config_revision"`
	Kind             string          `json:"kind"`
	TargetURL        string          `json:"target_url"`
	TargetTitle      string          `json:"target_title"`
	TargetDocument   string          `json:"target_document"`
	Scope            BrowserScope    `json:"scope"`
	PreviousResource string          `json:"previous_resource"`
	Arguments        json.RawMessage `json:"arguments"`
}

func (s BrowserScope) Validate() error {
	for _, id := range []string{s.ProviderID, s.ProviderEpoch, s.TabID, s.TabGeneration, s.ProfileID, s.ControlLineage, s.AttachmentID, s.AttachmentGeneration} {
		if err := validateBrowserToken(id); err != nil {
			return err
		}
	}
	if p := s.Preview; p != nil {
		for _, id := range []string{p.HostID, p.HostIdentity, p.ConnectionGeneration, p.EnvironmentID} {
			if err := validateBrowserToken(id); err != nil {
				return err
			}
		}
		if (p.Loopback != "127.0.0.1" && p.Loopback != "::1") || len(p.Ports) > 64 {
			return ErrInvalid
		}
		previous := 0
		for _, port := range p.Ports {
			if port <= previous || port > 65535 {
				return ErrInvalid
			}
			previous = port
		}
	}
	return nil
}

func (r BrowserIntent) Validate() error {
	if err := ValidateID(string(r.SessionID)); err != nil {
		return err
	}
	if err := ValidateID(string(r.TreeID)); err != nil {
		return err
	}
	for value, limit := range map[string]int{r.TargetURL: 8192, r.TargetTitle: 512, r.TargetDocument: 128} {
		if len(value) > limit || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return ErrInvalid
		}
	}
	if r.ConfigRevision < 1 || !json.Valid(r.Arguments) || len(r.Arguments) > 256<<10 {
		return ErrInvalid
	}
	if !slices.Contains([]string{"open", "attach", "run", "detach", "allow_preview_port"}, r.Kind) {
		return ErrInvalid
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if r.Kind == "allow_preview_port" {
		if len(r.PreviousResource) != 72 || !strings.HasPrefix(r.PreviousResource, "browser:") || r.Scope.Preview == nil {
			return ErrInvalid
		}
		if _, err := hex.DecodeString(strings.TrimPrefix(r.PreviousResource, "browser:")); err != nil {
			return ErrInvalid
		}
	} else if r.PreviousResource != "" {
		return ErrInvalid
	}
	return nil
}

type BrowserCatalogRequest struct {
	SessionID      SessionID `json:"session_id"`
	TreeID         TreeID    `json:"tree_id"`
	ConfigRevision Revision  `json:"config_revision"`
	Action         string    `json:"action"`
}

func validateBrowserToken(value string) error {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0 {
		return ErrInvalid
	}
	return nil
}
