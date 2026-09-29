package session

import "time"

// TreeSummary is a read projection. Inspecting the catalog loads no configuration,
// history, or worker; metadata and root identity remain owned by their rows.
type TreeSummary struct {
	Tree
	RootID           SessionID
	WorkingDirectory string
}

// DefinitionSummary describes one immutable revision, never a mutable latest alias.
type DefinitionSummary struct {
	Ref       DefinitionRef
	Name      string
	CreatedAt time.Time
}
