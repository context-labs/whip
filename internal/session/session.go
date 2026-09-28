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
	"unicode/utf8"
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

type TreeMetadata struct {
	Title    *string `json:"title"`
	Archived bool    `json:"archived"`
	Pinned   bool    `json:"pinned"`
}

type Tree struct {
	ID        TreeID
	Metadata  TreeMetadata
	Engine    Engine
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
	GoalInput      InputSource = "goal"
)

// InputKind distinguishes conversation work from maintenance. Source identifies
// who admitted that work; maintenance creates no synthetic prompt.
type InputKind string

const (
	PromptInput              InputKind = "prompt"
	CompactInput             InputKind = "compact"
	GoalFormulationInputKind InputKind = "goal_formulation"
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
	Goal      *GoalRef
	Schedule  *ScheduleOccurrence
	ID        InputID
	SessionID SessionID
	Source    InputSource
	Kind      InputKind
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
	Goal      *GoalRef
	ID        TurnID
	SessionID SessionID
	// Kind is derived from the accepted input. Mail-only turns are prompt turns.
	Kind           InputKind
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
	Type        string      `json:"type"`
	Text        string      `json:"text,omitempty"`
	ReferenceID string      `json:"reference_id,omitempty"`
	Call        *ToolCall   `json:"call,omitempty"`
	Result      *ToolResult `json:"result,omitempty"`
}

const MaxToolCalls = 16

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolResult struct {
	CallID  string `json:"call_id"`
	Output  string `json:"output"`
	IsError bool   `json:"is_error"`
}

var toolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func ValidateToolName(name string) error {
	if !toolName.MatchString(name) {
		return fmt.Errorf("%w: invalid tool name", ErrInvalid)
	}
	return nil
}

func (call ToolCall) Validate() error {
	if err := ValidateID(call.ID); err != nil {
		return err
	}
	if err := ValidateToolName(call.Name); err != nil {
		return err
	}
	var object map[string]json.RawMessage
	if len(call.Arguments) > MaxDocumentBytes || !utf8.Valid(call.Arguments) || json.Unmarshal(call.Arguments, &object) != nil || object == nil {
		return fmt.Errorf("%w: tool arguments require a bounded JSON object", ErrInvalid)
	}
	return nil
}

func ValidateParts(parts []Part) error {
	if len(parts) == 0 || len(parts) > 128 {
		return fmt.Errorf("%w: expected 1–128 message parts", ErrInvalid)
	}
	calls := map[string]bool{}
	for _, part := range parts {
		switch part.Type {
		case "text":
			if part.Text == "" || part.ReferenceID != "" || part.Call != nil || part.Result != nil {
				return fmt.Errorf("%w: text part requires only text", ErrInvalid)
			}
		case "content":
			if part.Text != "" || ValidateID(part.ReferenceID) != nil || part.Call != nil || part.Result != nil {
				return fmt.Errorf("%w: content part requires only a reference", ErrInvalid)
			}
		case "tool_call":
			if part.Call == nil || part.Text != "" || part.ReferenceID != "" || part.Result != nil {
				return fmt.Errorf("%w: tool call part requires only a call", ErrInvalid)
			}
			if err := part.Call.Validate(); err != nil {
				return err
			}
			if calls[part.Call.ID] || len(calls) >= MaxToolCalls {
				return fmt.Errorf("%w: expected at most 16 calls with unique IDs", ErrInvalid)
			}
			calls[part.Call.ID] = true
		case "tool_result":
			if part.Result == nil || part.Text != "" || part.ReferenceID != "" || part.Call != nil {
				return fmt.Errorf("%w: tool result part requires only a result", ErrInvalid)
			}
			if err := ValidateID(part.Result.CallID); err != nil {
				return err
			}
			if len(part.Result.Output) > MaxDocumentBytes || !utf8.ValidString(part.Result.Output) {
				return fmt.Errorf("%w: tool output exceeds supported bounds or is not UTF-8", ErrInvalid)
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

// ValidateMessage checks role ownership as well as each part's representation.
// Cross-message call/result pairing belongs to the execution/transcript boundary.
func ValidateMessage(role Role, parts []Part) error {
	if err := ValidateParts(parts); err != nil {
		return err
	}
	if role != User && role != Assistant && role != System && role != Tool {
		return fmt.Errorf("%w: unsupported message role", ErrInvalid)
	}
	if role == Tool {
		if len(parts) != 1 || parts[0].Type != "tool_result" {
			return fmt.Errorf("%w: tool messages require exactly one result", ErrInvalid)
		}
		return nil
	}
	for _, part := range parts {
		if part.Type == "tool_result" || (part.Type == "tool_call" && role != Assistant) {
			return fmt.Errorf("%w: message role cannot own this part", ErrInvalid)
		}
	}
	return nil
}

func ValidateInputParts(parts []Part) error { return ValidateMessage(User, parts) }

// Message is the common root/child transcript projection. User entries resolve
// Parts through an input or immutable mail revision; authored entries own parts.
type Message struct {
	ID        MessageID
	SessionID SessionID
	TurnID    TurnID
	InputID   *InputID
	Mail      *MailRef
	Sequence  int64
	Role      Role
	Parts     []Part
	CreatedAt time.Time
}

// MessageDraft describes one completed transcript entry. Its stable ID makes a
// persistence retry independent of repeating the model request or host effect.
type MessageDraft struct {
	ID           MessageID
	Role         Role
	Parts        []Part
	Continuation *ModelContinuation `json:"-"`
}

func ValidateText(value string, maxBytes int) error {
	if strings.TrimSpace(value) == "" || len(value) > maxBytes || strings.ContainsRune(value, 0) {
		return fmt.Errorf("%w: text is empty or outside supported bounds", ErrInvalid)
	}
	return nil
}
