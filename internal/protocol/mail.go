package protocol

import (
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/session"
)

type MailRef struct {
	ID           ID      `json:"id"`
	Revision     Counter `json:"revision"`
	Presentation string  `json:"presentation" enum:"digest,body"`
}

// MailMetadata is a bounded inbox entry. Reading the body is an explicit,
// read-only operation; clients never acknowledge delivery by inspecting it.
type MailMetadata struct {
	ID          ID      `json:"id"`
	Revision    Counter `json:"revision"`
	SenderID    ID      `json:"sender_id"`
	RecipientID ID      `json:"recipient_id"`
	Delivery    string  `json:"delivery" enum:"queued,steer,next_turn"`
	Subject     string  `json:"subject"`
	BodyBytes   Counter `json:"body_bytes"`
	State       string  `json:"state" enum:"pending,delivered,done"`
	AvailableAt string  `json:"available_at"`
	CreatedAt   string  `json:"created_at"`
	RevisedAt   string  `json:"revised_at"`
}

type MailAdmission struct {
	MailID    ID            `json:"mail_id"`
	Mail      *MailMetadata `json:"mail"`
	DeletedAt *string       `json:"deleted_at"`
}

type SendMailParams struct {
	MailID      ID      `json:"mail_id"`
	SenderID    ID      `json:"sender_id"`
	RecipientID ID      `json:"recipient_id"`
	Delivery    string  `json:"delivery" enum:"queued,steer,next_turn"`
	Subject     string  `json:"subject"`
	Body        string  `json:"body"`
	AvailableAt *string `json:"available_at,omitempty"`
}

type ListMailParams struct {
	SessionID ID      `json:"session_id"`
	State     *string `json:"state,omitempty" enum:"pending,delivered,done"`
	After     *ID     `json:"after,omitempty"`
	Limit     int     `json:"limit" min:"1" max:"100"`
}

type ListMailResult struct {
	Items []MailMetadata `json:"items"`
}

type ReadMailParams struct {
	SessionID ID `json:"session_id"`
	MailID    ID `json:"mail_id"`
}

type ReadMailResult struct {
	Mail MailMetadata `json:"mail"`
	Body string       `json:"body"`
}

func (p SendMailParams) Domain() (session.MailSpec, error) {
	value := session.MailSpec{ID: session.MailID(p.MailID), SenderID: session.SessionID(p.SenderID), RecipientID: session.SessionID(p.RecipientID), Delivery: session.MailDelivery(p.Delivery), Subject: p.Subject, Body: p.Body}
	if p.AvailableAt != nil {
		parsed, err := time.Parse(time.RFC3339Nano, *p.AvailableAt)
		if err != nil {
			return value, fmt.Errorf("%w: invalid mail availability timestamp", session.ErrInvalid)
		}
		value.AvailableAt = &parsed
	}
	return value, nil
}

func MailMetadataFromDomain(value session.MailMetadata) MailMetadata {
	return MailMetadata{
		ID: ID(value.ID), Revision: Counter(value.Revision), SenderID: ID(value.SenderID), RecipientID: ID(value.RecipientID),
		Delivery: string(value.Delivery), Subject: value.Subject, BodyBytes: Counter(value.BodyBytes), State: string(value.State),
		AvailableAt: value.AvailableAt.Format(time.RFC3339Nano), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano), RevisedAt: value.RevisedAt.Format(time.RFC3339Nano),
	}
}

func MailAdmissionFromDomain(value session.MailAdmission) MailAdmission {
	result := MailAdmission{MailID: ID(value.ID), DeletedAt: timeString(value.DeletedAt)}
	if value.Mail != nil {
		result.Mail = new(MailMetadataFromDomain(*value.Mail))
	}
	return result
}
