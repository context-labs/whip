package protocol

import (
	"encoding/base64"
	"time"

	"github.com/context-labs/whip/internal/session"
)

type CompactParams struct {
	Identity  RequestIdentity `json:"identity"`
	SessionID ID              `json:"session_id"`
}

type ContextHead struct {
	SessionID    ID      `json:"session_id"`
	Revision     Counter `json:"revision"`
	CompactionID *ID     `json:"compaction_id"`
}

type CompactionMetadata struct {
	HistoryRevision  Counter           `json:"history_revision"`
	Source           *CompactionSource `json:"source"`
	ID               ID                `json:"id"`
	SessionID        ID                `json:"session_id"`
	TurnID           *ID               `json:"turn_id"`
	AttemptID        *ID               `json:"attempt_id"`
	BaseID           *ID               `json:"base_id"`
	ExpectedRevision Counter           `json:"expected_revision"`
	ThroughSequence  Counter           `json:"through_sequence"`
	PinnedMessageIDs []ID              `json:"pinned_message_ids"`
	TextBytes        Counter           `json:"text_bytes"`
	CreatedAt        string            `json:"created_at"`
}

type CompactionParams struct {
	SessionID    ID `json:"session_id"`
	CompactionID ID `json:"compaction_id"`
}

type CompactionsParams struct {
	SessionID ID  `json:"session_id"`
	After     *ID `json:"after,omitempty"`
	Limit     int `json:"limit" min:"1" max:"100"`
}

type CompactionsResult struct {
	Items []CompactionMetadata `json:"items"`
}

type CompactionResult struct {
	Metadata CompactionMetadata `json:"metadata"`
	Text     string             `json:"text"`
}

type SelectCompactionParams struct {
	SessionID        ID      `json:"session_id"`
	ExpectedRevision Counter `json:"expected_revision"`
	CompactionID     *ID     `json:"compaction_id"`
}

type HistorySnapshot struct {
	Revision        Counter `json:"revision"`
	SessionID       ID      `json:"session_id"`
	ThroughSequence Counter `json:"through_sequence"`
	MessageCount    Counter `json:"message_count"`
}

type HistoryMetadata struct {
	Presentation    *MessagePresentation `json:"presentation,omitempty"`
	InputIdentity   *RequestIdentity     `json:"input_identity"`
	GroupID         ID                   `json:"group_id"`
	OpeningInput    bool                 `json:"opening_input"`
	Source          *MessageSource       `json:"source"`
	RetiredBy       *ID                  `json:"retired_by"`
	RetiredRevision *Counter             `json:"retired_revision"`
	ID              ID                   `json:"id"`
	SessionID       ID                   `json:"session_id"`
	TurnID          *ID                  `json:"turn_id"`
	InputID         *ID                  `json:"input_id"`
	Mail            *MailRef             `json:"mail"`
	Sequence        Counter              `json:"sequence"`
	Role            string               `json:"role" enum:"system,user,assistant,tool"`
	PartsBytes      Counter              `json:"parts_bytes"`
}

type ContextHistoryParams struct {
	ExpectedRevision *Counter `json:"expected_revision,omitempty"`
	SessionID        ID       `json:"session_id"`
	After            Counter  `json:"after"`
	ThroughSequence  Counter  `json:"through_sequence"`
	Limit            int      `json:"limit" min:"1" max:"100"`
}

type HistoryMetadataResult struct {
	Revision        Counter           `json:"revision"`
	Items           []HistoryMetadata `json:"items"`
	ThroughSequence Counter           `json:"through_sequence"`
	NextAfter       *Counter          `json:"next_after"`
}

type ReadHistoryParams struct {
	SessionID ID      `json:"session_id"`
	MessageID ID      `json:"message_id"`
	Offset    Counter `json:"offset"`
	Length    int     `json:"length" min:"1" max:"65536"`
}

type ReadHistoryResult struct {
	Message    HistoryMetadata `json:"message"`
	Offset     Counter         `json:"offset"`
	NextOffset *Counter        `json:"next_offset"`
	DataBase64 string          `json:"data_base64"`
}

type SearchHistoryParams struct {
	ExpectedRevision *Counter `json:"expected_revision,omitempty"`
	SessionID        ID       `json:"session_id"`
	After            Counter  `json:"after"`
	ThroughSequence  Counter  `json:"through_sequence"`
	Query            string   `json:"query"`
	Limit            int      `json:"limit" min:"1" max:"100"`
}

type HistoryMatch struct {
	Message   HistoryMetadata `json:"message"`
	PartIndex int             `json:"part_index" min:"0" max:"127"`
	Field     string          `json:"field" enum:"text,arguments,output"`
	Offset    Counter         `json:"offset"`
	Snippet   string          `json:"snippet"`
	Truncated bool            `json:"truncated"`
}

type SearchHistoryResult struct {
	Revision        Counter        `json:"revision"`
	Matches         []HistoryMatch `json:"matches"`
	ThroughSequence Counter        `json:"through_sequence"`
	NextAfter       *Counter       `json:"next_after"`
	ScannedMessages Counter        `json:"scanned_messages"`
	ScannedBytes    Counter        `json:"scanned_bytes"`
}

func ContextHeadFromDomain(v session.ContextHead) ContextHead {
	r := ContextHead{SessionID: ID(v.SessionID), Revision: Counter(v.Revision)}
	if v.CompactionID != nil {
		r.CompactionID = new(ID(*v.CompactionID))
	}
	return r
}

func CompactionFromDomain(v session.CompactionMetadata) CompactionMetadata {
	r := CompactionMetadata{
		HistoryRevision: Counter(v.HistoryRevision),
		ID:              ID(v.ID), SessionID: ID(v.SessionID),
		TurnID: localID(string(v.TurnID)), AttemptID: localID(string(v.AttemptID)),
		ExpectedRevision: Counter(v.ExpectedRevision), ThroughSequence: Counter(v.ThroughSequence),
		PinnedMessageIDs: []ID{}, TextBytes: Counter(v.TextBytes), CreatedAt: v.CreatedAt.Format(time.RFC3339Nano),
	}
	if v.Source != nil {
		r.Source = &CompactionSource{SessionID: ID(v.Source.SessionID), CompactionID: ID(v.Source.CompactionID)}
	}
	if v.BaseID != nil {
		r.BaseID = new(ID(*v.BaseID))
	}
	for _, id := range v.PinnedMessageIDs {
		r.PinnedMessageIDs = append(r.PinnedMessageIDs, ID(id))
	}
	return r
}

func HistoryMetadataFromDomain(v session.HistoryMetadata) HistoryMetadata {
	r := HistoryMetadata{
		Presentation:  PresentationFromDomain(v.Presentation),
		InputIdentity: requestIdentity(v.InputIdentity),
		GroupID:       ID(v.GroupID), OpeningInput: v.OpeningInput, Source: messageSource(v.Source),
		RetiredBy: historyEditID(v.RetiredBy), RetiredRevision: historyRevision(v.RetiredRevision),
		ID: ID(v.ID), SessionID: ID(v.SessionID), TurnID: localID(string(v.TurnID)),
		Sequence: Counter(v.Sequence), Role: string(v.Role), PartsBytes: Counter(v.PartsBytes),
	}
	if v.InputID != nil {
		r.InputID = new(ID(*v.InputID))
	}
	if v.Mail != nil {
		r.Mail = &MailRef{ID: ID(v.Mail.ID), Revision: Counter(v.Mail.Revision), Presentation: string(v.Mail.Presentation)}
	}
	return r
}

func HistoryReadFromDomain(v session.HistoryRead) ReadHistoryResult {
	return ReadHistoryResult{Message: HistoryMetadataFromDomain(v.Message), Offset: Counter(v.Offset), NextOffset: counter(v.NextOffset), DataBase64: base64.StdEncoding.EncodeToString(v.Data)}
}

func HistoryPageFromDomain(v session.HistoryMetadataPage) HistoryMetadataResult {
	r := HistoryMetadataResult{Revision: Counter(v.Revision), Items: []HistoryMetadata{}, ThroughSequence: Counter(v.ThroughSequence), NextAfter: counter(v.NextAfter)}
	for _, value := range v.Items {
		r.Items = append(r.Items, HistoryMetadataFromDomain(value))
	}
	return r
}

func HistorySearchFromDomain(v session.HistorySearchPage) SearchHistoryResult {
	r := SearchHistoryResult{Revision: Counter(v.Revision), Matches: []HistoryMatch{}, ThroughSequence: Counter(v.ThroughSequence), NextAfter: counter(v.NextAfter), ScannedMessages: Counter(v.ScannedMessages), ScannedBytes: Counter(v.ScannedBytes)}
	for _, match := range v.Matches {
		r.Matches = append(r.Matches, HistoryMatch{Message: HistoryMetadataFromDomain(match.Message), PartIndex: match.PartIndex, Field: match.Field, Offset: Counter(match.Offset), Snippet: match.Snippet, Truncated: match.Truncated})
	}
	return r
}
