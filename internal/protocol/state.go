package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

type StateVersion struct {
	ID        ID      `json:"id"`
	TreeID    ID      `json:"tree_id"`
	SessionID *ID     `json:"session_id"`
	Key       string  `json:"key"`
	Revision  Counter `json:"revision"`
	AuthorID  ID      `json:"author_id"`
	Digest    string  `json:"digest" pattern:"^[a-f0-9]{64}$"`
	Size      Counter `json:"size"`
	CreatedAt string  `json:"created_at"`
}

type GetStateParams struct {
	SessionID ID     `json:"session_id"`
	Scope     string `json:"scope" enum:"session,tree"`
	Key       string `json:"key"`
}

type WriteStateParams struct {
	SessionID        ID      `json:"session_id"`
	Scope            string  `json:"scope" enum:"session,tree"`
	VersionID        ID      `json:"version_id"`
	Key              string  `json:"key"`
	ExpectedRevision Counter `json:"expected_revision"`
	DataBase64       string  `json:"data_base64"`
}

type ReadStateParams struct {
	SessionID ID      `json:"session_id"`
	VersionID ID      `json:"version_id"`
	Offset    Counter `json:"offset"`
	Length    int     `json:"length" min:"1" max:"65536"`
}

type ReadStateResult struct {
	Version    StateVersion `json:"version"`
	Offset     Counter      `json:"offset"`
	DataBase64 string       `json:"data_base64"`
}

type ListStateParams struct {
	SessionID ID      `json:"session_id"`
	Scope     string  `json:"scope" enum:"session,tree"`
	After     *string `json:"after,omitempty"`
	Limit     int     `json:"limit" min:"1" max:"100"`
}

type StateHistoryParams struct {
	SessionID ID      `json:"session_id"`
	Scope     string  `json:"scope" enum:"session,tree"`
	Key       string  `json:"key"`
	After     Counter `json:"after"`
	Limit     int     `json:"limit" min:"1" max:"100"`
}

type StateVersionsResult struct {
	Items []StateVersion `json:"items"`
}

func StateVersionFromDomain(v session.StateValue) StateVersion {
	result := StateVersion{ID: ID(v.ID), TreeID: ID(v.TreeID), Key: v.Key, Revision: Counter(v.Revision), AuthorID: ID(v.AuthorID), Digest: v.Digest, Size: Counter(v.Size), CreatedAt: v.CreatedAt.Format(time.RFC3339Nano)}
	if v.SessionID != nil {
		result.SessionID = new(ID(*v.SessionID))
	}
	return result
}
