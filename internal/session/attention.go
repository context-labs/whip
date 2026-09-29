package session

// AttentionCursor pages exact owners in tree/session identity order. Attention
// is advisory; refresh from the beginning to observe earlier newly active owners.
type AttentionCursor struct {
	TreeID    TreeID
	SessionID SessionID
}
type AttentionItem struct {
	TreeID    TreeID
	RootID    SessionID
	SessionID SessionID
	Title     *string
	Activity  Activity
}
type AttentionPage struct {
	Items      []AttentionItem
	NextCursor *AttentionCursor
}
