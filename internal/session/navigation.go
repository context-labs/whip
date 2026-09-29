package session

// TreeNavigation is a current SQL projection for bounded human navigation.
// Counts include every descendant; they are not execution permit usage.
type TreeNavigation struct {
	TreeSummary
	ActiveTurnCount            int64
	QueuedInputCount           int64
	PendingPermissionCount     int64
	PendingQuestionCount       int64
	ActiveWorkspaceActionCount int64
}

type TreeNavigationPage struct {
	Items   []TreeNavigation
	Missing []SessionID
}
