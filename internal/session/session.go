// Package session defines durable conversation values. It owns no database,
// provider clients, interpreters, processes, or runtime workers.
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type (
	RuntimeID string
	TreeID    string
	SessionID string
	InputID   string
	TurnID    string
	MessageID string
	Revision  int64
)

const MaxDocumentBytes = 1 << 20

var (
	ErrInvalid = errors.New("invalid value")
	idPattern  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)
)

func ValidateID(value string) error {
	if !idPattern.MatchString(value) {
		return fmt.Errorf("%w: invalid identifier", ErrInvalid)
	}
	return nil
}

type Engine string

const (
	Starlark Engine = "starlark"
	QuickJS  Engine = "quickjs"
)

func (e Engine) Validate() error {
	if e != Starlark && e != QuickJS {
		return fmt.Errorf("%w: unsupported engine %q", ErrInvalid, e)
	}
	return nil
}

// TreePolicy is shared by every session in a tree. These are storage admission
// limits; execution budgets and authority are separate records in later phases.
type TreePolicy struct {
	MaxDepth                  int `json:"max_depth"`
	MaxSessions               int `json:"max_sessions"`
	MaxQueuedInputsPerSession int `json:"max_queued_inputs_per_session"`
}

func DefaultTreePolicy() TreePolicy {
	return TreePolicy{MaxDepth: 8, MaxSessions: 128, MaxQueuedInputsPerSession: 256}
}

func (p TreePolicy) Validate() error {
	if p.MaxDepth < 0 || p.MaxDepth > 128 || p.MaxSessions < 1 || p.MaxSessions > 10000 ||
		p.MaxQueuedInputsPerSession < 1 || p.MaxQueuedInputsPerSession > 10000 {
		return fmt.Errorf("%w: tree limits outside supported bounds", ErrInvalid)
	}
	return nil
}

type TreeMetadata struct {
	Title    *string `json:"title"`
	Archived bool    `json:"archived"`
	Pinned   bool    `json:"pinned"`
}

type Tree struct {
	ID        TreeID
	Metadata  TreeMetadata
	Engine    Engine
	Policy    TreePolicy
	Revision  Revision
	CreatedAt time.Time
}

type Lifecycle string

const (
	Active  Lifecycle = "active"
	Stopped Lifecycle = "stopped"
)

// Session is a read value. Config is loaded from the immutable configuration
// revision selected by ConfigRevision; it is not a second persisted copy.
type Session struct {
	ID               SessionID
	TreeID           TreeID
	ParentID         *SessionID
	Definition       DefinitionRef
	ConfigRevision   Revision
	Config           Configuration
	WorkingDirectory string
	Lifecycle        Lifecycle
	CreatedAt        time.Time
}

type InputSource string

const (
	UserInput      InputSource = "user"
	AgentInput     InputSource = "agent"
	ScheduledInput InputSource = "schedule"
)

type InputState string

const (
	Queued         InputState = "queued"
	Claimed        InputState = "claimed"
	InputCancelled InputState = "cancelled"
)

// Input owns the accepted payload. Its execution outcome is the linked Turn;
// terminal turn states are never copied onto input or receipt rows.
type Input struct {
	ID        InputID
	SessionID SessionID
	Source    InputSource
	Parts     []Part
	State     InputState
	TurnID    *TurnID
	CreatedAt time.Time
}

type RequestIdentity struct {
	ClientID  string
	RequestID string
}

type Receipt struct {
	RequestIdentity
	Digest    string
	InputID   *InputID
	DeletedAt *time.Time
	CreatedAt time.Time
}

type TurnState string

const (
	Running     TurnState = "running"
	Cancelling  TurnState = "cancelling"
	Succeeded   TurnState = "succeeded"
	Failed      TurnState = "failed"
	Cancelled   TurnState = "cancelled"
	Interrupted TurnState = "interrupted"
)

func (s TurnState) Terminal() bool {
	return s == Succeeded || s == Failed || s == Cancelled || s == Interrupted
}

func (s TurnState) CanTransitionTo(next TurnState) bool {
	switch s {
	case Running:
		return next == Cancelling || next.Terminal()
	case Cancelling:
		return next == Cancelled || next == Interrupted
	default:
		return false
	}
}

type Turn struct {
	ID             TurnID
	SessionID      SessionID
	ConfigRevision Revision
	State          TurnState
	Failure        *string
	StartedAt      time.Time
	FinishedAt     *time.Time
}

type Role string

const (
	System    Role = "system"
	User      Role = "user"
	Assistant Role = "assistant"
	Tool      Role = "tool"
)

// Part keeps immutable content references separate from bytes. A reference's
// existence and access grant must be checked by the content boundary before use.
type Part struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	ReferenceID string `json:"reference_id,omitempty"`
}

func ValidateParts(parts []Part) error {
	if len(parts) == 0 || len(parts) > 128 {
		return fmt.Errorf("%w: expected 1–128 message parts", ErrInvalid)
	}
	for _, part := range parts {
		switch part.Type {
		case "text":
			if part.Text == "" || part.ReferenceID != "" {
				return fmt.Errorf("%w: text part requires only text", ErrInvalid)
			}
		case "content":
			if part.Text != "" || ValidateID(part.ReferenceID) != nil {
				return fmt.Errorf("%w: content part requires only a reference", ErrInvalid)
			}
		default:
			return fmt.Errorf("%w: unsupported message part", ErrInvalid)
		}
	}
	data, err := json.Marshal(parts)
	if err != nil {
		return err
	}
	if len(data) > MaxDocumentBytes {
		return fmt.Errorf("%w: message parts exceed size limit", ErrInvalid)
	}
	return nil
}

// Message is the common root/child transcript projection. User entries resolve
// Parts through InputID; assistant/tool entries own their stored parts directly.
type Message struct {
	ID        MessageID
	SessionID SessionID
	TurnID    TurnID
	InputID   *InputID
	Sequence  int64
	Role      Role
	Parts     []Part
	CreatedAt time.Time
}

// MessageDraft describes one completed transcript entry. Its stable ID makes a
// persistence retry independent of repeating the model request or host effect.
type MessageDraft struct {
	ID    MessageID
	Role  Role
	Parts []Part
}

func ValidateText(value string, maxBytes int) error {
	if strings.TrimSpace(value) == "" || len(value) > maxBytes || strings.ContainsRune(value, 0) {
		return fmt.Errorf("%w: text is empty or outside supported bounds", ErrInvalid)
	}
	return nil
}
