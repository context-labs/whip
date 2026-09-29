package session

import "time"

type RecentTree struct {
	TreeSummary
	Model          ModelSelection
	LastActivityAt time.Time
}

type RecentTreePage struct {
	CatalogRevision Revision
	Items           []RecentTree
	HasMore         bool
	Next            *RecentTreeCursor
}

// RecentTreeCursor is an advisory position, not a frozen catalog revision.
type RecentTreeCursor struct {
	TreeID         TreeID
	LastActivityAt time.Time
	Pinned         bool
}

type RecentTreeList struct {
	Limit            int
	After            *RecentTreeCursor
	Archived, Pinned *bool
	PinnedFirst      bool
}
