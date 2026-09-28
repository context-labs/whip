package protocol

import "github.com/context-labs/whip/internal/session"

type InstructionSource struct {
	Kind   string  `json:"kind" enum:"project_file,skill_metadata,invoked_skill,standing_instructions"`
	Scope  string  `json:"scope" enum:"workspace,host"`
	RootID *string `json:"root_id"`
	Path   string  `json:"path"`
	Bytes  Counter `json:"bytes"`
	SHA256 string  `json:"sha256" pattern:"^[a-f0-9]{64}$"`
}

type InstructionManifest struct {
	Bytes   Counter             `json:"bytes"`
	SHA256  string              `json:"sha256" pattern:"^[a-f0-9]{64}$"`
	Sources []InstructionSource `json:"sources"`
}

type InstructionManifestResult struct {
	Manifest *InstructionManifest `json:"manifest"`
}

func InstructionManifestFromDomain(value *session.InstructionManifest) InstructionManifestResult {
	if value == nil {
		return InstructionManifestResult{}
	}
	result := &InstructionManifest{Bytes: Counter(value.Bytes), SHA256: value.SHA256, Sources: []InstructionSource{}}
	for _, source := range value.Sources {
		result.Sources = append(result.Sources, InstructionSourceFromDomain(source))
	}
	return InstructionManifestResult{Manifest: result}
}

func InstructionSourceFromDomain(source session.InstructionSource) InstructionSource {
	return InstructionSource{Kind: source.Kind, Scope: source.Scope, RootID: source.RootID, Path: source.Path, Bytes: Counter(source.Bytes), SHA256: source.SHA256}
}

type ListSkillsParams struct {
	SessionID ID     `json:"session_id"`
	Prefix    string `json:"prefix,omitempty"`
	After     string `json:"after,omitempty"`
	Limit     int    `json:"limit" min:"1" max:"100"`
}

type SkillMetadata struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Disabled    bool              `json:"disabled"`
	Source      InstructionSource `json:"source"`
}

type ListSkillsResult struct {
	Items     []SkillMetadata `json:"items"`
	NextAfter *string         `json:"next_after"`
}
