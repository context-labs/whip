package protocol

import "github.com/context-labs/whip/internal/session"

// SessionSummariesParams requests a small explicit working set. An empty set is
// valid; root IDs must otherwise be distinct and at most 256 UTF-8 bytes each.
type SessionSummariesParams struct {
	RootIDs []string `json:"root_ids"`
}

// SessionSummariesResult preserves every requested identity in input order.
// Counts are advisory, rather than an atomic snapshot of the recursive runtime.
// The encoded result is bounded to 64 KiB; presentation truncation is explicit.
type SessionSummariesResult struct {
	Items []session.SessionNavigationSummary `json:"items"`
}

// HostDirectoryParams browses directories on the execution machine. Empty path
// starts at its home directory; no session or browser filesystem grant is needed.
type HostDirectoryParams struct {
	Path       string `json:"path,omitempty"`
	After      string `json:"after,omitempty"`
	Prefix     string `json:"prefix,omitempty"`
	ShowHidden bool   `json:"show_hidden,omitempty"`
	Limit      int    `json:"limit"`
}

type HostDirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type HostDirectoryResult struct {
	Path      string               `json:"path"`
	Parent    string               `json:"parent"`
	Entries   []HostDirectoryEntry `json:"entries"`
	NextAfter string               `json:"next_after,omitempty"`
	HasMore   bool                 `json:"has_more"`
	Truncated bool                 `json:"truncated"`
}

// HostDirectoryPickParams opens the OS-native folder chooser on the execution
// machine. Start is an optional absolute directory to preselect; empty starts
// wherever the platform dialog defaults. Not every platform supports a chooser.
type HostDirectoryPickParams struct {
	Start string `json:"start,omitempty"`
}

type HostDirectoryPickResult struct {
	Path      string `json:"path,omitempty"`
	Cancelled bool   `json:"cancelled"`
}

type HostAttentionParams struct {
	AfterID  string `json:"after_id,omitempty"`
	Limit    int    `json:"limit"`
	MaxBytes int    `json:"max_bytes"`
}

type HostAttentionItem struct {
	RootID             string                   `json:"root_id"`
	Title              string                   `json:"title"`
	ActiveAgents       int64                    `json:"active_agents,string"`
	PendingPermissions int64                    `json:"pending_permissions,string"`
	Questions          []session.LifecycleEvent `json:"questions"`
	Truncated          bool                     `json:"truncated"`
}

// Attention is a live advisory index, not a subscription snapshot. Page by root
// identity and refresh from the beginning to observe newly active earlier roots.
type HostAttentionResult struct {
	Items       []HostAttentionItem `json:"items"`
	NextAfterID string              `json:"next_after_id,omitempty"`
	HasMore     bool                `json:"has_more"`
	Truncated   bool                `json:"truncated"`
}

type MailboxPageParams struct {
	RootID   string                 `json:"root_id"`
	AgentID  string                 `json:"agent_id"`
	Status   string                 `json:"status,omitempty"`
	Cursor   *session.MailboxCursor `json:"cursor,omitempty"`
	Limit    int                    `json:"limit"`
	MaxBytes int                    `json:"max_bytes"`
}

type MailboxReadParams struct {
	RootID  string `json:"root_id"`
	AgentID string `json:"agent_id"`
	ID      string `json:"id"`
}

type HostThemeResolveParams struct {
	Name string `json:"name,omitempty"`
	JSON string `json:"json,omitempty"`
}
