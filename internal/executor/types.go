// Package executor owns bounded, process-local definition leases and invocations.
// It owns no transport, session persistence, interpreter or external effect.
package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

const (
	MaxPeers         = 64
	MaxLeases        = 128
	MaxPeerLeases    = 16
	MaxCalls         = 128
	MaxBindWaiters   = 64
	MaxPeerCalls     = 16
	MaxQueuedEvents  = 32
	MaxQueuedBytes   = 8 << 20
	MaxRequestBytes  = 1 << 20
	MaxResultBytes   = 512 << 10
	MaxProgressBytes = 2 << 10
	BindWait         = 5 * time.Second
)

var (
	ErrClosed       = errors.New("executor registry closed")
	ErrDisconnected = errors.New("executor disconnected")
	ErrReplaced     = errors.New("executor lease replaced")
	ErrUnavailable  = errors.New("executor unavailable")
	ErrCapacity     = errors.New("executor capacity exhausted")
	ErrConflict     = errors.New("executor invocation or lease is stale, completed, or owned by another peer")
)

type Kind string

const (
	Tool Kind = "tool"
	Hook Kind = "hook"
)

// Lease identifies one process-local holder of an immutable definition.
type Lease struct {
	Epoch      string
	Definition session.DefinitionRef
	Generation int64
	Tools      []string
	Hooks      []string
}

// Request contains only captured call context. Registry validates its bounded
// shape; runtime owns schema validation, narrowing, permission and SQL dispatch.
type Request struct {
	SessionID      session.SessionID
	TurnID         session.TurnID
	CellID         session.CellID
	OperationID    session.OperationID
	Operation      string
	Arguments      json.RawMessage
	Spawn          json.RawMessage
	Input          string
	PermissionMode string
	Timeout        time.Duration
}

type Invocation struct {
	ID       string
	Lease    Lease
	Kind     Kind
	Name     string
	Request  Request
	Deadline time.Time
}

// Event is one invocation or cancellation on the same leased peer. Next skips
// invocations that ended before delivery; no ended invocation is replayed.
type Event struct {
	Type       string
	Invocation *Invocation
	ID         string
	Epoch      string
	Generation int64
}

type Result struct {
	Value     json.RawMessage
	Failure   string
	Decision  string
	Reason    string
	Arguments json.RawMessage
	Spawn     json.RawMessage
	Context   string
}

type Page struct {
	Items []Invocation
	Next  string
}

func boundedText(value string, limit int) bool {
	return len(value) <= limit && utf8.ValidString(value)
}

func object(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	return len(raw) > 0 && utf8.Valid(raw) && json.Unmarshal(raw, &value) == nil && value != nil
}

func (request Request) validate(kind Kind, name string) (Request, error) {
	for _, value := range []string{string(request.SessionID), string(request.TurnID)} {
		if err := session.ValidateID(value); err != nil {
			return request, err
		}
	}
	if !boundedText(request.Input, 2<<10) || !boundedText(request.PermissionMode, 64) {
		return request, fmt.Errorf("%w: executor context exceeds bounds", session.ErrInvalid)
	}
	if len(request.Arguments)+len(request.Spawn)+len(request.Input) > MaxRequestBytes {
		return request, fmt.Errorf("%w: executor request exceeds bounds", session.ErrInvalid)
	}
	switch kind {
	case Tool:
		for _, value := range []string{string(request.CellID), string(request.OperationID)} {
			if err := session.ValidateID(value); err != nil {
				return request, err
			}
		}
		if request.Operation != "tools."+name || !object(request.Arguments) || len(request.Spawn) != 0 || request.Input != "" {
			return request, fmt.Errorf("%w: invalid custom tool request", session.ErrInvalid)
		}
		if request.Timeout == 0 {
			request.Timeout = 5 * time.Minute
		}
		if request.Timeout < 0 || request.Timeout > 15*time.Minute {
			return request, fmt.Errorf("%w: invalid custom tool deadline", session.ErrInvalid)
		}
	case Hook:
		if request.OperationID != "" {
			return request, fmt.Errorf("%w: hooks do not own dispatched operations", session.ErrInvalid)
		}
		if request.Timeout == 0 {
			request.Timeout = 30 * time.Second
		}
		if request.Timeout < 0 || request.Timeout > time.Minute {
			return request, fmt.Errorf("%w: invalid hook deadline", session.ErrInvalid)
		}
		switch name {
		case "before_tool":
			if session.ValidateID(string(request.CellID)) != nil || session.ValidateID(request.Operation) != nil || !object(request.Arguments) || len(request.Spawn) != 0 || request.Input != "" {
				return request, fmt.Errorf("%w: invalid tool-hook request", session.ErrInvalid)
			}
		case "before_spawn":
			if session.ValidateID(string(request.CellID)) != nil || request.Operation != "agents.spawn" || !object(request.Spawn) || len(request.Arguments) != 0 || request.Input != "" {
				return request, fmt.Errorf("%w: invalid spawn-hook request", session.ErrInvalid)
			}
		case "turn_start":
			if request.CellID != "" || request.Operation != "" || len(request.Arguments) != 0 || len(request.Spawn) != 0 {
				return request, fmt.Errorf("%w: invalid turn-hook request", session.ErrInvalid)
			}
		default:
			return request, fmt.Errorf("%w: unknown hook", session.ErrInvalid)
		}
	default:
		return request, fmt.Errorf("%w: unknown executor invocation kind", session.ErrInvalid)
	}
	return request, nil
}

func (result Result) validate(kind Kind, name string) error {
	if !boundedText(result.Failure, MaxProgressBytes) || !boundedText(result.Reason, MaxProgressBytes) || !boundedText(result.Context, 4<<10) {
		return fmt.Errorf("%w: executor result text exceeds bounds", session.ErrInvalid)
	}
	if len(result.Value)+len(result.Arguments)+len(result.Spawn) > MaxResultBytes {
		return fmt.Errorf("%w: executor result exceeds bounds", session.ErrInvalid)
	}
	if kind == Tool {
		if result.Decision != "" || result.Reason != "" || result.Context != "" || len(result.Arguments) != 0 || len(result.Spawn) != 0 || result.Failure == "" && (!utf8.Valid(result.Value) || !json.Valid(result.Value)) || result.Failure != "" && len(result.Value) != 0 {
			return fmt.Errorf("%w: invalid custom tool result", session.ErrInvalid)
		}
		return nil
	}
	if len(result.Value) != 0 || (result.Decision != "" && result.Decision != "allow" && result.Decision != "deny") || (len(result.Arguments) > 0 && !object(result.Arguments)) || (len(result.Spawn) > 0 && !object(result.Spawn)) {
		return fmt.Errorf("%w: invalid hook result", session.ErrInvalid)
	}
	if result.Failure != "" && (result.Decision != "" || result.Reason != "" || result.Context != "" || len(result.Arguments) != 0 || len(result.Spawn) != 0) {
		return fmt.Errorf("%w: failed hook cannot rewrite", session.ErrInvalid)
	}
	if (name != "before_tool" && len(result.Arguments) != 0) || (name != "before_spawn" && len(result.Spawn) != 0) || (name != "turn_start" && result.Context != "") {
		return fmt.Errorf("%w: hook result has fields for another hook", session.ErrInvalid)
	}
	return nil
}
