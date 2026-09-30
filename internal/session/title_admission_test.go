package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

func titleAdmission(root, agent, id, kind, payload string) CommandAdmission {
	return CommandAdmission{
		ClientID: "title-test", CommandID: id, Scope: CommandScopeRoot,
		RootID: root, AgentID: agent, Kind: kind, RequestDigest: id,
		Payload: RuntimePayload{Data: []byte(payload)},
	}
}

func assertStoredTitle(t *testing.T, store *Store, root, want string) {
	t.Helper()
	var title string
	if err := store.db.QueryRowContext(t.Context(), "SELECT title FROM sessions WHERE id=?", root).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != want {
		t.Fatalf("title = %q, want %q", title, want)
	}
}

func TestTitleInitializedAtAdmission(t *testing.T) {
	for _, test := range []struct {
		name, kind, payload, prompt, title string
	}{
		{"text", "submit", "  Fix\n  the bug  ", "  Fix\n  the bug  ", "Fix the bug"},
		{"unicode", "steer", strings.Repeat("界", provisionalTitleRunes+1), strings.Repeat("界", provisionalTitleRunes+1), strings.Repeat("界", provisionalTitleRunes-1) + "…"},
		{"multipart", "submit.parts", `{"text":"Fix","parts":[{"type":"text","text":"the bug"},{"type":"image_url","image_url":{"url":"ignored"}}],"attachments":[{"kind":"text","name":"ignored.txt"}]}`, "Fix\nthe bug", "Fix the bug"},
		{"parts only", "steer.parts", `{"parts":[{"type":"text","text":"First"},{"type":"text","text":"second"}]}`, "First\nsecond", "First second"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			before, err := store.SessionCatalogRevision(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			admission := titleAdmission(root, agent, "first", test.kind, test.payload)
			result, err := store.AdmitCommand(t.Context(), admission)
			if err != nil {
				t.Fatal(err)
			}
			if result.TitleInitialization == nil || *result.TitleInitialization != (TitleInitialization{Title: test.title, Prompt: test.prompt}) {
				t.Fatalf("initialization = %+v", result.TitleInitialization)
			}
			assertStoredTitle(t, store, root, test.title)
			after, err := store.SessionCatalogRevision(t.Context())
			if err != nil || before == after {
				t.Fatalf("catalog revision did not change: %v", err)
			}
			replay, err := store.AdmitCommand(t.Context(), admission)
			if err != nil || replay.New || replay.TitleInitialization != nil {
				t.Fatalf("replay = %+v, %v", replay, err)
			}
			if err := store.SetTitle(root, ""); err != nil {
				t.Fatal(err)
			}
			later, err := store.AdmitCommand(t.Context(), titleAdmission(root, agent, "later", "submit", "different"))
			if err != nil || later.TitleInitialization != nil {
				t.Fatalf("later = %+v, %v", later, err)
			}
			assertStoredTitle(t, store, root, "")
		})
	}
}

func TestTitleAttachmentOnlyWaitsAcrossTurnAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	first, err := store.AdmitCommand(t.Context(), titleAdmission(root, root, "attachment", "submit.parts", `{"text":" ","attachments":[{"kind":"text","name":"notes.txt"}]}`))
	if err != nil || first.TitleInitialization != nil {
		t.Fatalf("attachment = %+v, %v", first, err)
	}
	assertStoredTitle(t, store, root, "")
	if err := store.StartRootTurn(t.Context(), root, root, first.Command.IngressSeq); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitRootTurn(t.Context(), RootTurnCommit{
		RootID: root, AgentID: root, InboxSeq: first.Command.IngressSeq,
		Messages: []llm.Message{{Role: "user", Content: "expanded attachment text", Authored: true}, {Role: "assistant", Content: "done"}},
	}); err != nil {
		t.Fatal(err)
	}
	assertStoredTitle(t, store, root, "")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.AdmitCommand(t.Context(), titleAdmission(root, root, "text", "submit", "User question"))
	if err != nil || result.TitleInitialization == nil {
		t.Fatalf("text = %+v, %v", result, err)
	}
	assertStoredTitle(t, store, root, "User question")
	if err := store.SetTitle(root, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err = store.AdmitCommand(t.Context(), titleAdmission(root, root, "restart", "submit", "Do not backfill"))
	if err != nil || result.TitleInitialization != nil {
		t.Fatalf("restart = %+v, %v", result, err)
	}
	assertStoredTitle(t, store, root, "")
}

func TestTitleAdmissionPreservesExistingAndHistoricalRoots(t *testing.T) {
	for _, test := range []string{"manual", "fork", "history", "history then attachment", "malformed history", "legacy inbox", "rewound", "previous truncated text"} {
		t.Run(test, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			want := ""
			switch test {
			case "manual":
				want = "My name"
				if err := store.SetTitle(root, want); err != nil {
					t.Fatal(err)
				}
			case "fork":
				exec(t, store, "UPDATE sessions SET forked_from='parent' WHERE id=?", root)
			case "malformed history":
				exec(t, store, "INSERT INTO messages(session_id,seq,role,content) VALUES(?,1,'user','malformed')", root)
			case "rewound":
				exec(t, store, "UPDATE sessions SET history_revision=1 WHERE id=?", root)
			case "history", "history then attachment":
				if err := store.Save(root, 0, []llm.Message{{Role: "user", Content: "old authored text", Authored: true}}, "model", "provider"); err != nil {
					t.Fatal(err)
				}
				if err := store.SetTitle(root, ""); err != nil {
					t.Fatal(err)
				}
				if test == "history then attachment" {
					if _, err := store.AdmitCommand(t.Context(), titleAdmission(root, agent, "attachment", "submit.parts", `{"attachments":[{"kind":"text","name":"notes"}]}`)); err != nil {
						t.Fatal(err)
					}
				}
			case "legacy inbox", "previous truncated text":
				item, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: agent, Kind: "submit", Payload: RuntimePayload{Data: []byte("old")}})
				if err != nil {
					t.Fatal(err)
				}
				if test == "previous truncated text" {
					exec(t, store, "UPDATE inbox SET origin='client',preview=? WHERE root_id=? AND seq=?", `{"text":"`+strings.Repeat(" ", 2048)+`","truncated":true}`, root, item.InboxSeq)
				}
			}
			result, err := store.AdmitCommand(t.Context(), titleAdmission(root, agent, "new", "submit", "New text"))
			if err != nil || result.TitleInitialization != nil {
				t.Fatalf("result = %+v, %v", result, err)
			}
			assertStoredTitle(t, store, root, want)
		})
	}
}

func TestTitleOnlyNamesClientRootInput(t *testing.T) {
	for _, test := range []struct {
		kind, origin string
		child        bool
	}{
		{"submit", "client", true}, {"steer", "internal", false}, {"submit", "", false}, {"goal", "client", false}, {"mailbox", "client", false},
	} {
		t.Run(fmt.Sprintf("%s-%s-%t", test.kind, test.origin, test.child), func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			if test.child {
				admitTestChild(t, store, root, agent, "child")
				agent = "child"
			}
			result, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: agent, Kind: test.kind, Origin: test.origin, Payload: RuntimePayload{Data: []byte("not user text")}})
			if err != nil || result.TitleInitialization != nil {
				t.Fatalf("result = %+v, %v", result, err)
			}
			assertStoredTitle(t, store, root, "")
		})
	}
	store, root, agent := newSwarmFixture(t)
	result, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: agent, Kind: "submit", Origin: "client", Payload: RuntimePayload{Data: []byte("legacy client")}})
	if err != nil || result.TitleInitialization == nil {
		t.Fatalf("legacy result = %+v, %v", result, err)
	}
	assertStoredTitle(t, store, root, "legacy client")
}

func TestTitleAdmissionRollback(t *testing.T) {
	for _, test := range []string{"invalid parts", "cancelled", "title write", "inbox write", "event write", "command write"} {
		t.Run(test, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			ctx := t.Context()
			admission := titleAdmission(root, agent, "first", "submit", "Do not title")
			switch test {
			case "invalid parts":
				admission.Kind = "submit.parts"
				admission.Payload.Data = []byte("invalid")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "title write":
				exec(t, store, `CREATE TRIGGER fail_title BEFORE UPDATE OF title ON sessions BEGIN SELECT RAISE(ABORT,'title failure'); END`)
			default:
				table := map[string]string{"inbox write": "inbox", "event write": "events", "command write": "commands"}[test]
				exec(t, store, "CREATE TRIGGER fail_title_test BEFORE INSERT ON "+table+" BEGIN SELECT RAISE(ABORT,'input failure'); END")
			}
			result, err := store.AdmitCommand(ctx, admission)
			if err == nil || result.TitleInitialization != nil {
				t.Fatalf("result = %+v, %v", result, err)
			}
			assertStoredTitle(t, store, root, "")
			for _, table := range []string{"inbox", "commands"} {
				var count int
				if err := store.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("%s rows = %d, %v", table, count, err)
				}
			}
		})
	}
}

func TestConcurrentInputsInitializeOneTitle(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	const callers = 12
	results := make(chan *TitleInitialization, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			result, err := store.AdmitCommand(t.Context(), titleAdmission(root, agent, strconv.Itoa(i), "submit", fmt.Sprintf("Prompt %d", i)))
			results <- result.TitleInitialization
			errs <- err
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var initialized []*TitleInitialization
	for result := range results {
		if result != nil {
			initialized = append(initialized, result)
		}
	}
	if len(initialized) != 1 {
		t.Fatalf("initializations = %d", len(initialized))
	}
	assertStoredTitle(t, store, root, initialized[0].Title)
}

func TestTitleWaitsForQueuedTextAndSurvivesTurnFailure(t *testing.T) {
	for _, status := range []string{"succeeded", "failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			for i, payload := range []string{"", "\t\n\u2003", strings.Repeat(" ", 3000)} {
				result, err := store.AdmitCommand(t.Context(), titleAdmission(root, agent, strconv.Itoa(i), "submit", payload))
				if err != nil || result.TitleInitialization != nil {
					t.Fatalf("empty admission = %+v, %v", result, err)
				}
			}
			first, err := store.AdmitCommand(t.Context(), titleAdmission(root, agent, "text", "submit", "Actual text"))
			if err != nil || first.TitleInitialization == nil {
				t.Fatalf("text = %+v, %v", first, err)
			}
			if err := store.StartRootTurn(t.Context(), root, agent, first.Command.IngressSeq); err != nil {
				t.Fatal(err)
			}
			if err := store.CommitRootTurn(t.Context(), RootTurnCommit{
				RootID: root, AgentID: agent, InboxSeq: first.Command.IngressSeq, Status: status,
				Messages: []llm.Message{{Role: "user", Content: "expanded text", Authored: true}},
			}); err != nil {
				t.Fatal(err)
			}
			assertStoredTitle(t, store, root, "Actual text")
		})
	}
}

func TestLegacyTitleAdmissionRollsBackAfterTitleWrite(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	exec(t, store, `CREATE TRIGGER fail_legacy_inbox BEFORE INSERT ON inbox BEGIN SELECT RAISE(ABORT,'input failure'); END`)
	result, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: agent, Origin: "client", Kind: "submit", Payload: RuntimePayload{Data: []byte("No title")}})
	if err == nil || result.TitleInitialization != nil {
		t.Fatalf("result = %+v,%v", result, err)
	}
	assertStoredTitle(t, store, root, "")
}

func TestSetTitleIfContext(t *testing.T) {
	store, root, _ := newSwarmFixture(t)
	if err := store.SetTitle(root, "Fallback"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if changed, err := store.SetTitleIfContext(ctx, root, "Fallback", "Generated"); changed || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled = %t,%v", changed, err)
	}
	assertStoredTitle(t, store, root, "Fallback")
	if changed, err := store.SetTitleIfContext(t.Context(), root, "Stale", "Generated"); changed || err != nil {
		t.Fatalf("stale = %t,%v", changed, err)
	}
	if changed, err := store.SetTitleIfContext(t.Context(), root, "Fallback", "Generated"); !changed || err != nil {
		t.Fatalf("write = %t,%v", changed, err)
	}
	if err := store.DeleteSession(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.SetTitleIfContext(t.Context(), root, "Generated", "Late"); changed || err != nil {
		t.Fatalf("deleted = %t,%v", changed, err)
	}
}
