package protocol

import "github.com/context-labs/whip/internal/session"

type InstructionSource struct {
	Kind   string  `json:"kind" enum:"project_file,skill_metadata"`
	Scope  string  `json:"scope" enum:"workspace"`
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
		result.Sources = append(result.Sources, InstructionSource{
			Kind: source.Kind, Scope: source.Scope, Path: source.Path, Bytes: Counter(source.Bytes), SHA256: source.SHA256,
		})
	}
	return InstructionManifestResult{Manifest: result}
}
