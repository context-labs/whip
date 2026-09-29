package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestCellPageUsesOrdinalAndExactTurnCursor(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	submit(t, s, owner.ID, "cells")
	turn := claim(t, s, owner.ID).Turn
	// Bound-only metadata fixture; lexical IDs intentionally reverse execution order.
	for i := range 131 {
		id := fmt.Sprintf("cell_%03d", 130-i)
		message := fmt.Sprintf("call_message_%d", i)
		result := fmt.Sprintf("result_message_%d", i)
		execTest(t, s, `INSERT INTO messages(id,session_id,turn_id,group_id,sequence,role,parts,created_at) VALUES(?,?,?,?,?,'assistant','[{"type":"text","text":"fixture"}]',?)`, message, owner.ID, turn.ID, turn.ID, 2*i+2, now())
		execTest(t, s, `INSERT INTO messages(id,session_id,turn_id,group_id,sequence,role,parts,created_at) VALUES(?,?,?,?,?,'tool','[{"type":"tool_result","result":{"call_id":"call","output":"","is_error":false}}]',?)`, result, owner.ID, turn.ID, turn.ID, 2*i+3, now())
		execTest(t, s, `INSERT INTO cells(id,turn_id,call_message_id,call_id,state,result_message_id,created_at,finished_at) VALUES(?,?,?,'call','succeeded',?,?,?)`, id, turn.ID, message, result, now(), now())
	}
	seen := []session.Cell{}
	var before session.CellID
	for {
		page, err := s.CellPage(t.Context(), turn.ID, before, 64)
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, page.Items...)
		if page.NextCursor == nil {
			break
		}
		if len(page.Items) != 64 || *page.NextCursor != page.Items[63].ID {
			t.Fatal(page)
		}
		before = *page.NextCursor
	}
	if len(seen) != 131 || seen[0].ID != "cell_000" || seen[130].ID != "cell_130" {
		t.Fatal("not canonical ordinal order", len(seen), seen[0], seen[len(seen)-1])
	}
	_, other := create(t, s, nil)
	submit(t, s, other.ID, "other")
	foreign := claim(t, s, other.ID).Turn
	if _, err := s.CellPage(t.Context(), foreign.ID, "cell_000", 10); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign cursor", err)
	}
	if _, err := s.CellPage(t.Context(), "missing", "", 10); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing turn", err)
	}
	if _, err := s.CellPage(t.Context(), turn.ID, "", 101); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("unbounded page", err)
	}
}
