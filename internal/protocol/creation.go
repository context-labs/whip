package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

type TreeCreationParams struct {
	CreationID ID `json:"creation_id"`
}

type TreeCreation struct {
	ID        ID     `json:"id"`
	TreeID    ID     `json:"tree_id"`
	RootID    ID     `json:"root_id"`
	CreatedAt string `json:"created_at"`
}

// Creation is immutable admission evidence. Current Tree and Root are null after
// deletion, and a late exact retry cannot recreate the destination.
type CreateTreeResult struct {
	Creation TreeCreation `json:"creation"`
	Tree     *Tree        `json:"tree"`
	Root     *Session     `json:"root"`
	Deleted  bool         `json:"deleted"`
}

func CreationFromDomain(value session.TreeCreationResult) (CreateTreeResult, error) {
	c := value.Creation
	result := CreateTreeResult{Creation: TreeCreation{ID: ID(c.ID), TreeID: ID(c.TreeID), RootID: ID(c.RootID), CreatedAt: c.CreatedAt.Format(time.RFC3339Nano)}, Deleted: value.Deleted}
	if value.Tree != nil {
		result.Tree = new(TreeFromDomain(*value.Tree))
	}
	if value.Root != nil {
		root, err := SessionFromDomain(*value.Root)
		if err != nil {
			return CreateTreeResult{}, err
		}
		result.Root = &root
	}
	return result, nil
}
