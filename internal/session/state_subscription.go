package session

import "time"

const MaxStateSubscriptions = 1000

// Cursor is the latest revision enqueued (or authored by this subscriber), not
// an acknowledgement that the recipient has processed its notification.
type StateSubscription struct {
	ID          string       `json:"id"`
	TreeID      TreeID       `json:"tree_id"`
	SessionID   SessionID    `json:"session_id"`
	Key         string       `json:"key"`
	Delivery    MailDelivery `json:"delivery"`
	Cursor      int64        `json:"cursor,string"`
	CancelledAt *time.Time   `json:"cancelled_at"`
	CreatedAt   time.Time    `json:"created_at"`
}

type StateSubscribe struct {
	Key      string       `json:"key"`
	After    int64        `json:"after,string"`
	Delivery MailDelivery `json:"delivery"`
}

func (s StateSubscribe) Validate() error {
	if err := ValidateStateKey(s.Key); err != nil {
		return err
	}
	if s.After < 0 || s.Delivery != MailQueued && s.Delivery != MailSteer && s.Delivery != MailNextTurn {
		return ErrInvalid
	}
	return nil
}

type StateChange struct {
	VersionID string    `json:"version_id"`
	Key       string    `json:"key"`
	Revision  int64     `json:"revision,string"`
	AuthorID  SessionID `json:"author_id"`
}

type StateSubscriptionID struct {
	ID string `json:"id"`
}

type StateSubscriptionList struct {
	After string `json:"after"`
	Limit int    `json:"limit"`
}

type StateSubscriptionItems struct {
	Items []StateSubscription `json:"items"`
}
