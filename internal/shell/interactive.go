package shell

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math"
	"sync"
	"time"
)

const (
	InteractiveOutputBytes = 64 << 10
	MaxInputBytes          = 16 << 10
	inputQueue             = 4
)

var ErrInputConflict = errors.New("shell input sequence conflicts with the active command")

// Interaction belongs to one dispatched foreground operation. Keystrokes are
// never persisted, exposed to the model, or transferred to another operation.
type Interaction struct {
	scope       *Scope
	operationID string
	started     time.Time
	keys        chan []byte
	mu          sync.Mutex
	closed      bool
	output      []byte
	through     int64
	lastInput   int64
	lastDigest  [32]byte
	secondsLeft int
}

type InteractiveView struct {
	OperationID string
	Started     time.Time
	Output      []byte
	From        int64
	Through     int64
	NextInput   int64
	SecondsLeft int
}

func (r *Reservation) Interact(operationID string) (*Interaction, error) {
	s, m := r.scope, r.scope.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.closed || r.used || r.released || r.background || operationID == "" {
		return nil, ErrClosed
	}
	if s.interaction != nil {
		return nil, ErrLimit
	}
	interaction := &Interaction{scope: s, operationID: operationID, started: time.Now().UTC(), keys: make(chan []byte, inputQueue), secondsLeft: 15}
	s.interaction = interaction
	return interaction, nil
}

func (i *Interaction) Keys() <-chan []byte { return i.keys }

func (i *Interaction) Output(chunk string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return
	}
	i.through += int64(len(chunk))
	if len(chunk) >= InteractiveOutputBytes {
		i.output = append(i.output[:0], chunk[len(chunk)-InteractiveOutputBytes:]...)
		return
	}
	if overflow := len(i.output) + len(chunk) - InteractiveOutputBytes; overflow > 0 {
		copy(i.output, i.output[overflow:])
		i.output = i.output[:len(i.output)-overflow]
	}
	i.output = append(i.output, chunk...)
}

func (i *Interaction) AwaitInput(seconds int) {
	i.mu.Lock()
	i.secondsLeft = seconds
	i.mu.Unlock()
}

func (i *Interaction) Close() {
	m := i.scope.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	i.mu.Lock()
	defer i.mu.Unlock()
	i.closed = true
	if i.scope.interaction == i {
		i.scope.interaction = nil
	}
	// The runner has joined its input pump before Close. Clear pending secrets
	// promptly, while keeping the caller-owned channel open.
	for {
		select {
		case data := <-i.keys:
			clear(data)
		default:
			return
		}
	}
}

// Interaction reads only an existing live generation; observation never creates
// a shell scope or process. Cursor zero requests the currently retained tail.
func (m *Manager) Interaction(owner string, cursor int64) (*InteractiveView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.owners[owner]
	if s == nil || s.closed || s.interaction == nil {
		return nil, nil //nolint:nilnil // An absent live interaction is a successful empty observation.
	}
	i := s.interaction
	i.mu.Lock()
	defer i.mu.Unlock()
	if cursor < 0 || cursor > i.through {
		return nil, ErrInputConflict
	}
	from := max(cursor, i.through-int64(len(i.output)))
	return &InteractiveView{OperationID: i.operationID, Started: i.started, Output: bytes.Clone(i.output[from-(i.through-int64(len(i.output))):]), From: from, Through: i.through, NextInput: i.lastInput + 1, SecondsLeft: i.secondsLeft}, nil
}

// Input acknowledges bounded queue admission, not terminal consumption. Only
// the most recent accepted sequence has an exact retry receipt. Older, changed,
// future, foreign-operation and closed-generation writes cannot enqueue bytes.
func (m *Manager) Input(owner, operationID string, sequence int64, data []byte) error {
	if len(data) == 0 || len(data) > MaxInputBytes || sequence <= 0 || sequence == math.MaxInt64 {
		return ErrInputConflict
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.owners[owner]
	if s == nil || s.closed || s.interaction == nil || s.interaction.operationID != operationID {
		return ErrNotFound
	}
	i := s.interaction
	i.mu.Lock()
	defer i.mu.Unlock()
	digest := sha256.Sum256(data)
	if sequence == i.lastInput && digest == i.lastDigest {
		return nil
	}
	if sequence != i.lastInput+1 {
		return ErrInputConflict
	}
	select {
	case i.keys <- bytes.Clone(data):
		i.lastInput, i.lastDigest = sequence, digest
		return nil
	default:
		return ErrLimit
	}
}
