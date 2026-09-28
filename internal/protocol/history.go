package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

// Source identities are immutable provenance, not authorization to read them.
type MessageSource struct {
	SessionID ID      `json:"session_id"`
	MessageID ID      `json:"message_id"`
	Sequence  Counter `json:"sequence"`
}

type CompactionSource struct {
	SessionID    ID `json:"session_id"`
	CompactionID ID `json:"compaction_id"`
}

type RewindParams struct {
	EditID           ID      `json:"edit_id"`
	SessionID        ID      `json:"session_id"`
	ExpectedRevision Counter `json:"expected_revision"`
	ObservedThrough  Counter `json:"observed_through"`
	KeepThrough      Counter `json:"keep_through"`
}

type HistoryEdit struct {
	ID               ID      `json:"id"`
	SessionID        ID      `json:"session_id"`
	Digest           string  `json:"digest" pattern:"^[a-f0-9]{64}$"`
	ExpectedRevision Counter `json:"expected_revision"`
	Revision         Counter `json:"revision"`
	ObservedThrough  Counter `json:"observed_through"`
	KeepThrough      Counter `json:"keep_through"`
	CreatedAt        string  `json:"created_at"`
}

func HistorySnapshotFromDomain(value session.HistorySnapshot) HistorySnapshot {
	return HistorySnapshot{
		SessionID: ID(value.SessionID), Revision: Counter(value.Revision),
		ThroughSequence: Counter(value.ThroughSequence), MessageCount: Counter(value.MessageCount),
	}
}

func HistoryEditFromDomain(value session.HistoryEdit) HistoryEdit {
	return HistoryEdit{
		ID: ID(value.ID), SessionID: ID(value.SessionID), Digest: value.Digest,
		ExpectedRevision: Counter(value.ExpectedRevision), Revision: Counter(value.Revision),
		ObservedThrough: Counter(value.ObservedThrough), KeepThrough: Counter(value.KeepThrough),
		CreatedAt: value.CreatedAt.Format(time.RFC3339Nano),
	}
}

func messageSource(value *session.MessageSource) *MessageSource {
	if value == nil {
		return nil
	}
	return &MessageSource{SessionID: ID(value.SessionID), MessageID: ID(value.MessageID), Sequence: Counter(value.Sequence)}
}

func localID(value string) *ID {
	if value == "" {
		return nil
	}
	return new(ID(value))
}

func historyEditID(value *session.HistoryEditID) *ID {
	if value == nil {
		return nil
	}
	return new(ID(*value))
}

func historyRevision(value *session.Revision) *Counter {
	if value == nil {
		return nil
	}
	return new(Counter(*value))
}
