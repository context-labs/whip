package protocol

import "github.com/context-labs/whip/internal/session"

// Limit bounds examined input records, including non-human records. Continue
// with next_cursor even when items is empty. Reads never admit or replay work.
type RecentInputTextParams struct {
	Before *Counter `json:"before_ordinal,omitempty"`
	Limit  int      `json:"limit" min:"1" max:"500"`
}

type InputText struct {
	SessionID ID      `json:"session_id"`
	InputID   ID      `json:"input_id"`
	Ordinal   Counter `json:"ordinal"`
	Text      string  `json:"text"`
}

type InputTextPage struct {
	Items        []InputText `json:"items"`
	NextCursor   *Counter    `json:"next_cursor"`
	ScannedCount int         `json:"scanned_count" min:"0" max:"500"`
	SkippedCount int         `json:"skipped_count" min:"0" max:"500"`
}

func InputTextPageFromDomain(value session.InputTextPage) InputTextPage {
	page := InputTextPage{Items: []InputText{}, NextCursor: counter(value.NextCursor), ScannedCount: value.ScannedCount, SkippedCount: value.SkippedCount}
	for _, item := range value.Items {
		page.Items = append(page.Items, InputText{SessionID: ID(item.SessionID), InputID: ID(item.InputID), Ordinal: Counter(item.Ordinal), Text: item.Text})
	}
	return page
}
