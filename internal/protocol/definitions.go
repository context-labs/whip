package protocol

import "github.com/context-labs/whip/internal/agentdef"

// DefinitionRegisterParams carries an authored agent definition document. The
// daemon validates it, computes its revision, and stores it.
type DefinitionRegisterParams struct {
	Definition agentdef.Definition `json:"definition"`
}

// DefinitionRegisterResult identifies the stored revision. Created is false
// when the same document was already registered.
type DefinitionRegisterResult struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Created  bool   `json:"created"`
}

// DefinitionParams selects a definition; an empty revision means the latest.
type DefinitionParams struct {
	ID       string `json:"id"`
	Revision string `json:"revision,omitempty"`
}

// DefinitionRecord is one resolved definition. Built-ins have no revision or
// registration provenance.
type DefinitionRecord struct {
	Definition   agentdef.Definition `json:"definition"`
	Revision     string              `json:"revision"`
	BuiltIn      bool                `json:"built_in"`
	RegisteredBy string              `json:"registered_by"`
	CreatedAt    string              `json:"created_at"`
}

type DefinitionSummary struct {
	ID           string `json:"id"`
	Revision     string `json:"revision"`
	BuiltIn      bool   `json:"built_in"`
	RegisteredBy string `json:"registered_by"`
	CreatedAt    string `json:"created_at"`
}

// DefinitionList holds built-ins first, then the latest revision of every
// registered id.
type DefinitionList struct {
	Items []DefinitionSummary `json:"items"`
}
