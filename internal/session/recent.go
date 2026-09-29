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
}
