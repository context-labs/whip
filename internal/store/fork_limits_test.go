package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

// Bounded SQL fixtures reach rejected import sizes without creating unrelated
// execution or weakening production limits on normal history authorship.
func forkRowsTest(t *testing.T, s *Store, owner session.SessionID, messages, groups int, parts string, continuation *string) {
	t.Helper()
	execTest(t, s, `WITH RECURSIVE numbers(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n<?)
 INSERT INTO history_groups(id,session_id,source_session_id,source_group_id,created_at)
 SELECT 'group_'||n,?,'old','source_group_'||n,? FROM numbers`, groups, owner, now())
	execTest(t, s, `WITH RECURSIVE numbers(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n<?)
 INSERT INTO messages(id,session_id,group_id,sequence,role,parts,created_at,source_session_id,source_message_id,source_sequence,model_continuation)
 SELECT 'message_'||n,?,'group_'||(1+(n-1)%?),n,'assistant',?,?,'old','source_message_'||n,n,? FROM numbers`, messages, owner, groups, parts, now(), continuation)
}

func TestForkImportLimitsRejectAtomically(t *testing.T) {
	for _, test := range []struct {
		name string
		seed func(*testing.T, *Store, session.SessionID) int64
	}{
		{"messages", func(t *testing.T, s *Store, owner session.SessionID) int64 {
			t.Helper()
			forkRowsTest(t, s, owner, session.MaxForkMessages+1, 1, `[]`, nil)
			return session.MaxForkMessages + 1
		}},
		{"groups", func(t *testing.T, s *Store, owner session.SessionID) int64 {
			t.Helper()
			forkRowsTest(t, s, owner, session.MaxForkGroups+1, session.MaxForkGroups+1, `[]`, nil)
			return session.MaxForkGroups + 1
		}},
		{"history bytes", func(t *testing.T, s *Store, owner session.SessionID) int64 {
			t.Helper()
			forkRowsTest(t, s, owner, 65, 1, `[{"type":"text","text":"`+strings.Repeat("x", 1<<20)+`"}]`, nil)
			return 65
		}},
		{"private bytes", func(t *testing.T, s *Store, owner session.SessionID) int64 {
			t.Helper()
			value, err := encode(session.ModelContinuation{Scope: strings.Repeat("a", 64), Data: `[{"opaque":"` + strings.Repeat("x", 900000) + `"}]`})
			if err != nil {
				t.Fatal(err)
			}
			forkRowsTest(t, s, owner, 5, 1, `[]`, &value)
			return 5
		}},
		{"content references", func(t *testing.T, s *Store, owner session.SessionID) int64 {
			t.Helper()
			execTest(t, s, "INSERT INTO content_bodies VALUES(?,1)", strings.Repeat("a", 64))
			execTest(t, s, `WITH RECURSIVE numbers(n) AS(SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n<?)
 INSERT INTO content_references SELECT 'ref_'||n,?,?,'text/plain',? FROM numbers`, session.MaxContentReferences+1, owner, strings.Repeat("a", 64), now())
			return 0
		}},
		{"content bytes", func(t *testing.T, s *Store, owner session.SessionID) int64 {
			t.Helper()
			execTest(t, s, "INSERT INTO content_bodies VALUES(?,?)", strings.Repeat("a", 64), session.MaxContentBytes)
			execTest(t, s, `WITH RECURSIVE numbers(n) AS(SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n<17)
 INSERT INTO content_references SELECT 'ref_'||n,?,?,'text/plain',? FROM numbers`, owner, strings.Repeat("a", 64), now())
			return 0
		}},
		{"compaction chain", func(t *testing.T, s *Store, owner session.SessionID) int64 {
			t.Helper()
			forkRowsTest(t, s, owner, 1, 1, `[]`, nil)
			for i := 0; i <= session.MaxForkCompactions; i++ {
				var base *string
				if i > 0 {
					base = new(fmt.Sprintf("summary_%d", i-1))
				}
				execTest(t, s, `INSERT INTO compactions(id,session_id,history_revision,source_session_id,source_compaction_id,base_id,expected_revision,through_sequence,pinned_message_ids,text,created_at)
 VALUES(?,?,1,'old',?,?,0,1,'[]','summary',?)`, fmt.Sprintf("summary_%d", i), owner, fmt.Sprintf("source_%d", i), base, now())
			}
			execTest(t, s, "INSERT INTO context_heads VALUES(?,1,?)", owner, fmt.Sprintf("summary_%d", session.MaxForkCompactions))
			return 1
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := fresh(t)
			_, source := create(t, s, nil)
			keep := test.seed(t, s, source.ID)
			messages, refs, summaries := count(t, s, "messages"), count(t, s, "content_references"), count(t, s, "compactions")
			_, err := s.Fork(t.Context(), forkRequestTest(t, s, source.ID, "oversize", keep), forkDefaultsTest())
			if !errors.Is(err, ErrLimit) {
				t.Fatal("oversize fork accepted or wrong rejection", err)
			}
			if count(t, s, "sessions") != 1 || count(t, s, "forks") != 0 || count(t, s, "messages") != messages || count(t, s, "content_references") != refs || count(t, s, "compactions") != summaries {
				t.Fatal("oversize fork committed partial data")
			}
		})
	}
}

func TestForkValidatesContinuationAndFreshDefaultsBeforeCommit(t *testing.T) {
	s := fresh(t)
	_, source := create(t, s, nil)
	invalid := `{"scope":"bad","data":"[]"}`
	forkRowsTest(t, s, source.ID, 1, 1, `[]`, &invalid)
	request := forkRequestTest(t, s, source.ID, "invalid", 1)
	if _, err := s.Fork(t.Context(), request, forkDefaultsTest()); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("invalid private envelope copied", err)
	}
	if count(t, s, "sessions") != 1 || count(t, s, "forks") != 0 {
		t.Fatal("invalid private envelope left destination")
	}
	request.KeepThrough = 0
	for _, test := range []struct {
		name     string
		defaults session.ForkDefaults
	}{
		{"missing root caps", session.ForkDefaults{}},
		{"unbounded write cap", session.ForkDefaults{Budgets: []session.BudgetLimit{{Kind: session.BudgetLogicalWrites}, {Kind: session.BudgetLogicalWriteBytes, Limit: new(int64(1))}}}},
		{"duplicate budgets", session.ForkDefaults{Budgets: []session.BudgetLimit{{Kind: session.BudgetLogicalWrites, Limit: new(int64(1))}, {Kind: session.BudgetLogicalWrites, Limit: new(int64(1))}}}},
		{"unbounded resource", session.ForkDefaults{Resources: []session.ResourceLimit{{Kind: session.ResourceDepth}}, Budgets: forkDefaultsTest().Budgets}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := s.Fork(t.Context(), request, test.defaults); !errors.Is(err, session.ErrInvalid) {
				t.Fatal("invalid defaults admitted", err)
			}
		})
	}
	result := forkTest(t, s, request)
	request.Title = new(strings.Repeat("x", 1025))
	if _, err := s.Fork(t.Context(), request, session.ForkDefaults{}); !errors.Is(err, ErrConflict) {
		t.Fatal("receipt digest did not precede content validation", err)
	}
	if count(t, s, "sessions") != 2 || result.Fork.RootID == source.ID {
		t.Fatal("defaults validation mutated source")
	}
}

func TestForkSchemaRejectsPreviousVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	var version int
	if err := s.db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatal(version, err)
	}
	execTest(t, s, "PRAGMA user_version=32")
	if _, err := Open(t.Context(), path); !errors.Is(err, ErrSchema) {
		t.Fatal("pre-fork schema accepted", err)
	}
}
