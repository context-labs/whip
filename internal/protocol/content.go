package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

type ContentReference struct {
	ID        ID      `json:"id"`
	SessionID ID      `json:"session_id"`
	Digest    string  `json:"digest" pattern:"^[a-f0-9]{64}$"`
	Size      Counter `json:"size"`
	MediaType string  `json:"media_type"`
	CreatedAt string  `json:"created_at"`
}
type PutContentParams struct {
	SessionID   ID     `json:"session_id"`
	ReferenceID ID     `json:"reference_id"`
	MediaType   string `json:"media_type"`
	DataBase64  string `json:"data_base64"`
}
type ReadContentParams struct {
	SessionID   ID `json:"session_id"`
	ReferenceID ID `json:"reference_id"`
}
type ReadContentResult struct {
	Reference  ContentReference `json:"reference"`
	DataBase64 string           `json:"data_base64"`
}

func ContentReferenceFromDomain(value session.ContentReference) ContentReference {
	return ContentReference{
		ID: ID(value.ID), SessionID: ID(value.SessionID), Digest: value.Digest, Size: Counter(value.Size),
		MediaType: value.MediaType, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano),
	}
}
