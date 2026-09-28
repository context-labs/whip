package session

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
)

var agentIDEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// ErrAgentIDCollision means admission found an existing session or agent with
// this identity. A caller may generate a new ID and rebuild its admission.
var ErrAgentIDCollision = errors.New("agent identity already exists")

// NewAgentID returns an opaque, filename-safe ID with 96 random bits. Root and
// parent relationships belong in the store, not in the identifier.
func NewAgentID() string {
	var data [12]byte
	rand.Read(data[:])
	return agentIDEncoding.EncodeToString(data[:])
}

func checkAgentID(ctx context.Context, q commandQueryer, id string) error {
	var exists bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=? UNION ALL SELECT 1 FROM agents WHERE id=?)`, id, id).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrAgentIDCollision
	}
	return nil
}

// unusedAgentID must run in the same transaction as the subsequent insert.
func unusedAgentID(ctx context.Context, q commandQueryer, generate func() string) (string, error) {
	for range 3 {
		id := generate()
		if err := checkAgentID(ctx, q, id); !errors.Is(err, ErrAgentIDCollision) {
			return id, err
		}
	}
	return "", ErrAgentIDCollision
}
