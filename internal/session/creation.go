package session

import "time"

type CreationID string

// TreeCreationRequest contains caller intent only. Mutable host defaults never
// participate in delivery identity and are captured only on first admission.
type TreeCreationRequest struct {
	ID               CreationID      `json:"id"`
	Metadata         TreeMetadata    `json:"metadata"`
	Engine           Engine          `json:"engine"`
	Resources        []ResourceLimit `json:"resources"`
	Definition       DefinitionRef   `json:"definition"`
	Overrides        ConfigPatch     `json:"overrides"`
	WorkingDirectory string          `json:"working_directory"`
	PermissionMode   *PermissionMode `json:"permission_mode"`
}

type TreeCreationDefaults struct {
	Configuration  Configuration
	Resources      []ResourceLimit
	PermissionMode PermissionMode
}

// TreeCreation survives deletion. Its identity never becomes available again.
type TreeCreation struct {
	ID        CreationID
	TreeID    TreeID
	RootID    SessionID
	CreatedAt time.Time
}

type TreeCreationResult struct {
	Creation TreeCreation
	Tree     *Tree
	Root     *Session
	Deleted  bool
}
