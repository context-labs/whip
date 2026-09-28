package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	MaxInstructionBytes       = 1 << 20
	MaxInstructionSourceBytes = 64 << 10
	MaxInvokedSkillBytes      = 256 << 10
	MaxInstructionSources     = 1152
)

// InstructionSource identifies an exact file read while composing a turn's
// instructions. It contains audit metadata, never file contents or authority.
type InstructionSource struct {
	Kind   string `json:"kind"`
	Scope  string `json:"scope"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes,string"`
	SHA256 string `json:"sha256"`
}

// InstructionManifest describes the composed base instructions captured for a
// turn. Source digests cannot reconstruct files changed after capture. Additional
// request-specific instructions remain covered by the model request digest.
type InstructionManifest struct {
	Bytes   int64               `json:"bytes,string"`
	SHA256  string              `json:"sha256"`
	Sources []InstructionSource `json:"sources"`
}

func (m InstructionManifest) Clone() InstructionManifest {
	m.Sources = slices.Clone(m.Sources)
	return m
}

func (m InstructionManifest) Validate() error {
	if m.Bytes < 0 || m.Bytes > MaxInstructionBytes || len(m.Sources) > MaxInstructionSources {
		return fmt.Errorf("%w: instruction manifest exceeds bounds", ErrInvalid)
	}
	if err := instructionDigest(m.SHA256); err != nil {
		return err
	}
	for _, source := range m.Sources {
		if err := source.Validate(); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(raw) > MaxInstructionBytes {
		return fmt.Errorf("%w: encoded instruction manifest exceeds 1 MiB", ErrInvalid)
	}
	return nil
}

func (s InstructionSource) Validate() error {
	if (s.Kind != "project_file" && s.Kind != "skill_metadata" && s.Kind != "invoked_skill") || s.Scope != "workspace" {
		return fmt.Errorf("%w: unknown instruction source kind or scope", ErrInvalid)
	}
	limit := int64(MaxInstructionSourceBytes)
	if s.Kind == "invoked_skill" {
		limit = MaxInvokedSkillBytes
	}
	if s.Bytes < 0 || s.Bytes > limit {
		return fmt.Errorf("%w: instruction source exceeds its %d-byte limit", ErrInvalid, limit)
	}
	if err := instructionPath(s.Path); err != nil {
		return err
	}
	return instructionDigest(s.SHA256)
}

func (p Instructions) Validate() error {
	if len(p.Text) > MaxInstructionBytes || !utf8.ValidString(p.Text) || strings.ContainsRune(p.Text, 0) || len(p.ProjectFiles) > 32 {
		return fmt.Errorf("%w: instruction policy exceeds bounds or contains invalid text", ErrInvalid)
	}
	seen := make(map[string]bool, len(p.ProjectFiles))
	for _, path := range p.ProjectFiles {
		if err := instructionPath(path); err != nil {
			return err
		}
		if seen[path] {
			return fmt.Errorf("%w: duplicate project instruction path", ErrInvalid)
		}
		seen[path] = true
	}
	return nil
}

func instructionPath(path string) error {
	if err := ValidateText(path, 4096); err != nil {
		return err
	}
	// Canonical slash-separated paths make uniqueness and scope independent of
	// dot-segment cleaning. os.Root still enforces filesystem confinement.
	if path == "." || !fs.ValidPath(path) || !filepath.IsLocal(path) || strings.ContainsRune(path, '\\') {
		return fmt.Errorf("%w: instruction paths must be workspace-relative without traversal", ErrInvalid)
	}
	return nil
}

func instructionDigest(value string) error {
	digest, err := hex.DecodeString(value)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != value {
		return fmt.Errorf("%w: instruction digest must be lowercase SHA-256", ErrInvalid)
	}
	return nil
}
