package session

import (
	"fmt"
	"time"
	"unicode/utf8"
)

type (
	MailID           string
	MailDelivery     string
	MailState        string
	MailPresentation string
)

const (
	MailQueued          MailDelivery     = "queued"
	MailSteer           MailDelivery     = "steer"
	MailNextTurn        MailDelivery     = "next_turn"
	MailPending         MailState        = "pending"
	MailDelivered       MailState        = "delivered"
	MailDone            MailState        = "done"
	MailDigest          MailPresentation = "digest"
	MailBody            MailPresentation = "body"
	MaxMailBodyBytes                     = 16 << 10
	MaxMailPerSession                    = 1024
	MaxPendingMail                       = 256
	MaxMailBacklog                       = 20
	MaxMailRevisions                     = 128
	MaxMailObservations                  = 1024
)

type MailReceipt struct {
	ID       MailID `json:"id"`
	Revision int64  `json:"revision,string"`
}

type MailRef struct {
	MailReceipt
	Presentation MailPresentation `json:"presentation"`
}

type MailSource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type MailMetadata struct {
	MailReceipt
	Source      MailSource   `json:"source"`
	RecipientID SessionID    `json:"recipient_id"`
	Delivery    MailDelivery `json:"delivery"`
	Subject     string       `json:"subject"`
	BodyBytes   int64        `json:"body_bytes,string"`
	EvidenceRef *string      `json:"evidence_ref"`
	State       MailState    `json:"state"`
	AvailableAt time.Time    `json:"available_at"`
	CreatedAt   time.Time    `json:"created_at"`
	RevisedAt   time.Time    `json:"revised_at"`
}

type Mail struct {
	MailMetadata
	Body string `json:"body"`
}

type MailSend struct {
	RecipientID SessionID    `json:"recipient_id"`
	Delivery    MailDelivery `json:"delivery"`
	Subject     string       `json:"subject"`
	Body        string       `json:"body"`
	EvidenceRef *string      `json:"evidence_ref"`
	AvailableAt *time.Time   `json:"available_at"`
}

type MailSpec struct {
	MailSend
	ID       MailID    `json:"id"`
	SenderID SessionID `json:"sender_id"`
}

type MailAdmission struct {
	ID        MailID        `json:"id"`
	Mail      *MailMetadata `json:"mail"`
	DeletedAt *time.Time    `json:"deleted_at"`
}

type MailList struct {
	State MailState `json:"state"`
	After MailID    `json:"after"`
	Limit int       `json:"limit"`
}

type MailRead struct {
	ID MailID `json:"id"`
}
type MailComplete struct {
	Receipts []MailReceipt `json:"receipts"`
}
type MailDefer struct {
	Receipt     MailReceipt `json:"receipt"`
	AvailableAt time.Time   `json:"available_at"`
}

func (s MailSend) Validate() error {
	if err := ValidateID(string(s.RecipientID)); err != nil {
		return err
	}
	if s.Delivery != MailQueued && s.Delivery != MailSteer && s.Delivery != MailNextTurn {
		return fmt.Errorf("%w: invalid mail delivery", ErrInvalid)
	}
	if s.Subject != "" {
		if err := ValidateText(s.Subject, 256); err != nil {
			return err
		}
	}
	if s.EvidenceRef != nil {
		if err := ValidateID(*s.EvidenceRef); err != nil {
			return err
		}
	}
	if s.Body != "" || s.EvidenceRef == nil {
		if err := ValidateText(s.Body, MaxMailBodyBytes); err != nil {
			return err
		}
	}
	if !utf8.ValidString(s.Subject) || !utf8.ValidString(s.Body) {
		return fmt.Errorf("%w: mail must be UTF-8", ErrInvalid)
	}
	if s.AvailableAt != nil && (s.AvailableAt.IsZero() || s.AvailableAt.Year() < 1 || s.AvailableAt.Year() > 9999) {
		return fmt.Errorf("%w: invalid mail availability", ErrInvalid)
	}
	return nil
}

func (r MailReceipt) Validate() error {
	if err := ValidateID(string(r.ID)); err != nil {
		return err
	}
	if r.Revision < 1 {
		return fmt.Errorf("%w: invalid mail revision", ErrInvalid)
	}
	return nil
}
