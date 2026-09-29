package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestTurnPageUsesExactOwnerKeysetAcrossTiedTimesAndNewTurns(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	child := controlChild(t, s, root.ID, "child").Session
	const started = int64(9007199254740993)
	insert := func(owner session.Session, id string, at int64) {
		t.Helper()
		execTest(t, s, `INSERT INTO turns(id,session_id,config_revision,history_revision,state,started_at,finished_at)
		 VALUES(?,?,?,?,'succeeded',?,?)`, id, owner.ID, owner.ConfigRevision, owner.HistoryRevision, at, at)
	}
	for _, id := range []string{"a", "b", "c"} {
		insert(root, id, started)
	}
	insert(*child, "foreign", started+1)
	first, err := s.TurnPage(t.Context(), root.ID, "", 2)
	if err != nil || len(first.Items) != 2 || first.Items[0].ID != "c" || first.Items[1].ID != "b" || first.NextCursor == nil || *first.NextCursor != "b" || first.Items[0].StartedAt.UnixMicro() != started {
		t.Fatal(first, err)
	}
	insert(root, "new", started+1)
	older, err := s.TurnPage(t.Context(), root.ID, *first.NextCursor, 2)
	if err != nil || len(older.Items) != 1 || older.Items[0].ID != "a" || older.NextCursor != nil {
		t.Fatal(older, err)
	}
	for _, before := range []session.TurnID{"foreign", "missing"} {
		if _, err := s.TurnPage(t.Context(), root.ID, before, 1); !errors.Is(err, ErrNotFound) {
			t.Fatal(before, err)
		}
	}
	if _, err := s.TurnPage(t.Context(), "missing", "", 1); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := s.TurnPage(t.Context(), root.ID, "", limit); err == nil {
			t.Fatal("accepted invalid limit", limit)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.TurnPage(ctx, root.ID, "", 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	rows, err := s.db.QueryContext(t.Context(), `EXPLAIN QUERY PLAN SELECT `+turnColumns+` FROM turns
	 WHERE session_id=? AND (started_at,id)<(?,?) ORDER BY started_at DESC,id DESC LIMIT ?`, root.ID, started, "b", 3)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var indexed bool
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		indexed = indexed || strings.Contains(detail, "turns_by_session_start")
		if strings.Contains(detail, "TEMP B-TREE") {
			t.Fatal("unbounded turn sort", detail)
		}
	}
	if err := rows.Err(); err != nil || !indexed {
		t.Fatal("missing bounded owner index", err)
	}
}

func TestTurnPageRetainsDirectWorkWithoutHistoryAfterStopAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	_, err := s.AdmitHostOperation(t.Context(), session.RequestIdentity{ClientID: "human", RequestID: "direct"}, owner.ID,
		session.HostOperation{Module: "shell", Name: "run", Arguments: json.RawMessage(`{"command":"echo example"}`)})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.TurnPage(t.Context(), owner.ID, "", 1)
	if err != nil || len(before.Items) != 0 || before.Items == nil || count(t, s, "turns") != 0 {
		t.Fatal("read claimed queued work", before, err)
	}
	turn := claim(t, s, owner.ID).Turn
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	want, err := s.Turn(t.Context(), turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	page, err := reopened.TurnPage(t.Context(), owner.ID, "", 1)
	if err != nil || len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0], want) || page.Items[0].Kind != session.HostOperationInputKind || page.NextCursor != nil || count(t, reopened, "messages") != 0 {
		t.Fatal(page, err)
	}
}
