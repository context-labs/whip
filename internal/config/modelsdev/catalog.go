// Package modelsdev contains the reviewed, build-time Models.dev snapshot.
// No catalog lookup performs network or credential access.
package modelsdev

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
)

const SchemaVersion = 1
const SourceURL = "https://models.dev/api.json"

//go:embed catalog.json
var bundled []byte

// Snapshot records provenance for the exact upstream input, before normalization.
type Snapshot struct {
	SchemaVersion int                     `json:"schemaVersion"`
	SourceURL     string                  `json:"sourceURL"`
	InputSHA256   string                  `json:"inputSHA256"`
	RetrievedAt   string                  `json:"retrievedAt"`
	Providers     map[string]ProviderInfo `json:"providers"`
}

type ProviderInfo struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	API    string           `json:"api,omitempty"`
	Env    []string         `json:"env"`
	Models map[string]Model `json:"models"`
}

// Pointers and non-omitted slices preserve unknown, false, zero and empty values.
type Model struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	ContextLength       *int     `json:"contextLength,omitempty"`
	MaxCompletionTokens *int     `json:"maxCompletionTokens,omitempty"`
	SupportsTools       *bool    `json:"supportsTools,omitempty"`
	Reasoning           *bool    `json:"reasoning,omitempty"`
	ReasoningEfforts    []string `json:"reasoningEfforts"`
	InputModalities     []string `json:"inputModalities"`
	OutputModalities    []string `json:"outputModalities"`
	Pricing             *Pricing `json:"pricing,omitempty"`
}

// Pricing contains decimal USD per token, not upstream's per-million units.
type Pricing struct {
	Prompt         string `json:"prompt,omitempty"`
	Completion     string `json:"completion,omitempty"`
	InputCacheRead string `json:"inputCacheRead,omitempty"`
}

// Decode checks the format; the importer additionally validates all fields/policy.
func Decode(data []byte) (Snapshot, error) {
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return snapshot, fmt.Errorf("decode Models.dev snapshot: %w", err)
	}
	if snapshot.SchemaVersion != SchemaVersion || snapshot.Providers == nil {
		return snapshot, fmt.Errorf("unsupported Models.dev snapshot schema")
	}
	return snapshot, nil
}

var load = sync.OnceValues(func() (Snapshot, error) { return Decode(bundled) })

// Bundled returns a fresh copy for generation and validation tools.
func Bundled() (Snapshot, error) { return Decode(bundled) }

// Metadata returns provider-level metadata without copying the model catalog.
func Metadata(id string) (ProviderInfo, bool) {
	snapshot, err := load()
	if err != nil {
		return ProviderInfo{}, false
	}
	p, ok := snapshot.Providers[id]
	if !ok {
		return ProviderInfo{}, false
	}
	p.Env = slices.Clone(p.Env)
	p.Models = nil
	return p, true
}

// Provider returns an independent copy; callers cannot mutate the embedded data.
func Provider(id string) (ProviderInfo, bool) {
	snapshot, err := load()
	if err != nil {
		return ProviderInfo{}, false
	}
	p, ok := snapshot.Providers[id]
	if !ok {
		return ProviderInfo{}, false
	}
	p.Env = slices.Clone(p.Env)
	p.Models = maps.Clone(p.Models)
	for id, model := range p.Models {
		model.ContextLength = clonePointer(model.ContextLength)
		model.MaxCompletionTokens = clonePointer(model.MaxCompletionTokens)
		model.SupportsTools = clonePointer(model.SupportsTools)
		model.Reasoning = clonePointer(model.Reasoning)
		model.Pricing = clonePointer(model.Pricing)
		model.ReasoningEfforts = slices.Clone(model.ReasoningEfforts)
		model.InputModalities = slices.Clone(model.InputModalities)
		model.OutputModalities = slices.Clone(model.OutputModalities)
		p.Models[id] = model
	}
	return p, true
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	return new(*value)
}
