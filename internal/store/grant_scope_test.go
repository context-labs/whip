package store

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestGrantScopedRevocationRejectsForeignAndReusedID(t *testing.T) {
	s := fresh(t)
	_, first := create(t, s, nil)
	_, other := create(t, s, nil)
	grant := standingGrant(t, s, first.ID, "reusable-grant")
	if _, err := s.RevokeGrantForOwner(t.Context(), other.ID, grant.ID); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	values, err := s.Grants(t.Context(), first.ID, "", 100)
	if err != nil || len(values) != 1 || values[0].RevokedAt != nil {
		t.Fatal(values, err)
	}
	for range 2 {
		revoked, err := s.RevokeGrantForOwner(t.Context(), first.ID, grant.ID)
		if err != nil || revoked.SessionID != first.ID || revoked.RevokedAt == nil {
			t.Fatal(revoked, err)
		}
	}
	if err := s.DeleteSubtree(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	replacement := standingGrant(t, s, other.ID, string(grant.ID))
	if _, err := s.RevokeGrantForOwner(t.Context(), first.ID, replacement.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("late old-owner revocation changed reused ID", err)
	}
	values, err = s.Grants(t.Context(), other.ID, "", 100)
	if err != nil || len(values) != 1 || values[0].RevokedAt != nil {
		t.Fatal(values, err)
	}
	if _, err := s.RevokeGrantForOwner(t.Context(), session.SessionID(""), replacement.ID); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
	// Existing explicitly global human control remains available.
	if value, err := s.RevokeGrant(t.Context(), replacement.ID); err != nil || value.RevokedAt == nil {
		t.Fatal(value, err)
	}
}
