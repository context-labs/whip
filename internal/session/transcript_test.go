package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

func transcriptCommit(t *testing.T, store *Store, rootID, agentID string, turn int, messages []llm.Message, compactions ...RootCompaction) {
	t.Helper()
	item, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: agentID, Kind: "submit", Payload: RuntimePayload{Data: []byte("next")}})
	if err != nil {
		t.Fatal(err)
	}
	if rootID == agentID {
		if err := store.StartRootTurn(t.Context(), rootID, agentID, item.InboxSeq); err != nil {
			t.Fatal(err)
		}
		if err := store.CommitRootTurn(t.Context(), RootTurnCommit{RootID: rootID, AgentID: agentID, InboxSeq: item.InboxSeq, Messages: messages, Compactions: compactions, Model: "model", Provider: "provider"}); err != nil {
			t.Fatal(err)
		}
		return
	}
	turnID := fmt.Sprintf("%s-%d", agentID, turn)
	if _, err := store.StartAgentTurn(t.Context(), rootID, agentID, turnID); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, agentID, AgentTurnCommit{TurnID: turnID, Status: "succeeded", AcknowledgedInbox: []int64{item.InboxSeq}, Messages: messages, Compactions: compactions}); err != nil {
		t.Fatal(err)
	}
}

func TestTranscriptPreservesRootAndChildRawHistoryAcrossTwoCompactionsAndReopen(t *testing.T) {
	for _, child := range []bool{false, true} {
		t.Run(fmt.Sprintf("child=%t", child), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.db")
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			rootID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.EnsureAuthority(t.Context(), rootID); err != nil {
				t.Fatal(err)
			}
			agentID := rootID
			if child {
				agentID = "child"
				admitTestChild(t, store, rootID, rootID, agentID)
			}
			image := llm.ContentPart{Type: "image_url", W: 17, H: 29, ImageURL: &struct {
				URL string `json:"url"`
			}{URL: "data:image/png;base64,aW1hZ2U="}}
			raw := []llm.Message{
				{Role: "user", Content: "original instruction", Authored: true, Parts: []llm.ContentPart{{Type: "text", Text: "original instruction"}, image}},
				{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-1", Type: "function", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "exec", Arguments: `{"code":"print('precise args')"}`}}}},
				{Role: "tool", ToolCallID: "call-1", Name: "exec", Content: strings.Repeat("large result Ω ", 200000)},
				{Role: "assistant", Content: "first answer"},
				{Role: "user", Content: "second instruction"},
				{Role: "assistant", Content: "second answer"},
				{Role: "user", Content: "third instruction"},
				{Role: "assistant", Content: "third answer"},
			}
			transcriptCommit(t, store, rootID, agentID, 1, raw[:4])
			cutoff := 4
			transcriptCommit(t, store, rootID, agentID, 2, raw[4:6], RootCompaction{Summary: "first summary", RawCutoff: &cutoff})
			cutoff = 6
			transcriptCommit(t, store, rootID, agentID, 3, raw[6:], RootCompaction{Summary: "second summary retains first obligations", RawCutoff: &cutoff})
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			bounds, err := store.TranscriptBounds(t.Context(), rootID, agentID)
			if err != nil || bounds != (TranscriptBounds{FirstSeq: 1, LastSeq: 8, Count: 8}) {
				t.Fatalf("bounds = %+v, %v", bounds, err)
			}
			var restoredRaw []llm.Message
			cursor := 0
			for {
				page, err := store.ReadTranscript(t.Context(), rootID, agentID, cursor, bounds.LastSeq, 2)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range page.Messages {
					if item.Message.RawSequence != item.Seq {
						t.Fatalf("missing provenance: %+v", item)
					}
					restoredRaw = append(restoredRaw, item.Message)
				}
				if !page.HasMore {
					break
				}
				if page.NextSeq <= cursor {
					t.Fatal("page did not advance")
				}
				cursor = page.NextSeq
			}
			want, _ := json.Marshal(raw)
			got, _ := json.Marshal(restoredRaw)
			if string(got) != string(want) {
				t.Fatal("raw text, tool call/result, or multipart metadata changed")
			}
			var view []llm.Message
			if child {
				view, err = store.LoadAgentTranscript(t.Context(), rootID, agentID)
			} else {
				_, view, err = store.Load(rootID)
			}
			if err != nil || len(view) != 3 || !strings.Contains(view[0].Content, "second summary") || view[0].RawSequence != 6 || view[1].RawSequence != 7 || view[2].Content != "third answer" {
				t.Fatalf("derived view = %+v, %v", view, err)
			}
		})
	}
}

func TestTranscriptChildCommitRollsBackAndRetryAppendsOnlyItsDelta(t *testing.T) {
	store, rootID, _ := newMailboxFixture(t)
	item, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: "child", Kind: "submit", Payload: RuntimePayload{Data: []byte("work")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "failed-turn"); err != nil {
		t.Fatal(err)
	}
	failed := AgentTurnCommit{TurnID: "failed-turn", Status: "failed", RetryInput: true, Messages: []llm.Message{{Role: "user", Content: "work"}, {Role: "assistant", Content: "observed partial"}}}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", failed); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", failed); err == nil {
		t.Fatal("repeated failed-turn commit accepted")
	}
	if _, err := store.StartAgentTurn(t.Context(), rootID, "child", "retry-turn"); err != nil {
		t.Fatal(err)
	}
	cutoff := 2
	commit := AgentTurnCommit{TurnID: "retry-turn", Status: "succeeded", AcknowledgedInbox: []int64{item.InboxSeq}, Messages: []llm.Message{{Role: "user", Content: "work retry"}, {Role: "assistant", Content: "finished"}}, Compactions: []RootCompaction{{Summary: "observed partial", RawCutoff: &cutoff}}}
	if _, err := store.db.ExecContext(t.Context(), `CREATE TRIGGER fail_history_commit BEFORE INSERT ON events WHEN NEW.kind='agent.turn.succeeded' BEGIN SELECT RAISE(ABORT,'injected commit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", commit); err == nil {
		t.Fatal("injected failure did not abort")
	}
	page, err := store.ReadTranscript(t.Context(), rootID, "child", 0, -1, 128)
	if err != nil || len(page.Messages) != 2 {
		t.Fatalf("failed commit appended rows: %+v, %v", page, err)
	}
	var compactions int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM compactions WHERE session_id=? AND agent_id='child'`, rootID).Scan(&compactions); err != nil || compactions != 0 {
		t.Fatalf("failed commit appended summary: %d, %v", compactions, err)
	}
	if _, err := store.db.ExecContext(t.Context(), `DROP TRIGGER fail_history_commit`); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", commit); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", commit); err == nil {
		t.Fatal("repeated successful commit accepted")
	}
	page, err = store.ReadTranscript(t.Context(), rootID, "child", 0, -1, 128)
	if err != nil || len(page.Messages) != 4 || page.Messages[1].Message.Content != "observed partial" || page.Messages[3].Message.Content != "finished" {
		t.Fatalf("journal deltas = %+v, %v", page, err)
	}
}

func TestTranscriptPaginationSnapshotIsolationAndStrictErrors(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "child")
	admitTestChild(t, store, rootID, rootAgentID, "sibling")
	otherID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	empty, err := store.ReadTranscript(t.Context(), rootID, "child", 0, -1, 1)
	if err != nil || empty.ThroughSeq != 0 || empty.HasMore || len(empty.Messages) != 0 {
		t.Fatalf("empty history = %+v, %v", empty, err)
	}
	transcriptCommit(t, store, rootID, "child", 1, []llm.Message{{Role: "user", Content: "child private"}, {Role: "assistant", Content: "child result"}})
	first, err := store.ReadTranscript(t.Context(), rootID, "child", 0, -1, 1)
	if err != nil || first.NextSeq != 1 || first.ThroughSeq != 2 || !first.HasMore {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	transcriptCommit(t, store, rootID, "child", 2, []llm.Message{{Role: "user", Content: "later"}})
	last, err := store.ReadTranscript(t.Context(), rootID, "child", first.NextSeq, first.ThroughSeq, 1)
	if err != nil || last.NextSeq != 2 || last.HasMore || len(last.Messages) != 1 || last.Messages[0].Message.Content != "child result" {
		t.Fatalf("frozen page = %+v, %v", last, err)
	}
	stillEmpty, err := store.ReadTranscript(t.Context(), rootID, "child", 0, empty.ThroughSeq, 1)
	if err != nil || len(stillEmpty.Messages) != 0 || stillEmpty.HasMore {
		t.Fatalf("empty snapshot shifted = %+v, %v", stillEmpty, err)
	}
	for _, agentID := range []string{rootID, "sibling"} {
		page, err := store.ReadTranscript(t.Context(), rootID, agentID, 0, -1, 128)
		if err != nil || len(page.Messages) != 0 {
			t.Fatalf("%s inherited private transcript: %+v, %v", agentID, page, err)
		}
	}
	for _, ids := range [][2]string{{otherID, "child"}, {"missing", rootID}, {rootID, "missing"}, {"", "child"}} {
		if _, err := store.ReadTranscript(t.Context(), ids[0], ids[1], 0, -1, 1); !errors.Is(err, ErrAgentAccess) {
			t.Fatalf("foreign transcript %v = %v", ids, err)
		}
	}
	for _, query := range [][3]int{{-1, -1, 1}, {0, -2, 1}, {0, -1, 0}, {0, -1, 129}} {
		if _, err := store.ReadTranscript(t.Context(), rootID, "child", query[0], query[1], query[2]); err == nil {
			t.Fatalf("invalid page %v accepted", query)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.ReadTranscript(ctx, rootID, "child", 0, -1, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled page = %v", err)
	}
	for _, broken := range []string{"{", "null"} {
		if _, err := store.db.ExecContext(t.Context(), `UPDATE transcript_messages SET content=? WHERE root_id=? AND agent_id='child' AND seq=2`, broken, rootID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReadTranscript(t.Context(), rootID, "child", 0, -1, 128); err == nil {
			t.Fatalf("malformed row %q silently skipped", broken)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadTranscript(t.Context(), rootID, "child", 0, -1, 1); err == nil {
		t.Fatal("closed storage treated as empty")
	}
	if _, err := store.TranscriptBounds(t.Context(), rootID, "child"); err == nil {
		t.Fatal("closed bounds treated as empty")
	}
}

func TestTranscriptRootOperationsPreserveChildSummariesAndForkScope(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "child")
	cutoff := 2
	for _, agentID := range []string{rootID, "child"} {
		transcriptCommit(t, store, rootID, agentID, 1, []llm.Message{{Role: "user", Content: agentID + " private"}, {Role: "assistant", Content: "answer"}})
		transcriptCommit(t, store, rootID, agentID, 2, []llm.Message{{Role: "user", Content: "tail"}}, RootCompaction{Summary: agentID + " summary", RawCutoff: &cutoff})
	}
	before, err := store.LoadAgentTranscript(t.Context(), rootID, "child")
	if err != nil {
		t.Fatal(err)
	}
	for _, upto := range []int{1, 3} {
		forkID, err := store.Fork(rootID, upto, "fork")
		if err != nil {
			t.Fatal(err)
		}
		page, err := store.ReadTranscript(t.Context(), forkID, forkID, 0, -1, 128)
		if err != nil || len(page.Messages) != upto {
			t.Fatalf("fork transcript = %+v, %v", page, err)
		}
		compactions := store.Compactions(forkID)
		if (upto == 1 && len(compactions) != 0) || (upto == 3 && (len(compactions) != 1 || compactions[0].Summary != rootID+" summary")) {
			t.Fatalf("fork summaries at %d = %+v", upto, compactions)
		}
		if _, err := store.ReadTranscript(t.Context(), forkID, "child", 0, -1, 128); !errors.Is(err, ErrAgentAccess) {
			t.Fatalf("fork child history access = %v", err)
		}
	}
	for _, clear := range []func() error{
		func() error { return store.DeleteCompaction(rootID, 1) },
		func() error { return store.ClearCompactions(rootID) },
		func() error { _, err := store.RewindHistory(t.Context(), rootID, 3); return err },
		func() error { return store.ClearMessages(rootID) },
	} {
		if err := clear(); err != nil {
			t.Fatal(err)
		}
		after, err := store.LoadAgentTranscript(t.Context(), rootID, "child")
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("root history operation changed child view = %+v, %v", after, err)
		}
	}
}

func TestTranscriptRawCutoffUsesSequenceAndCompactionFailureIsAtomic(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	// Legacy Save permits holes. A focused runner's sequence identifies the
	// covered raw message even when a slice index or count cannot identify it.
	seed := []llm.Message{{}, {Role: "user", Content: "one"}, {}, {Role: "assistant", Content: "three"}}
	if err := store.Save(rootID, 1, seed, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	item, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: rootAgentID, Kind: "submit", Payload: RuntimePayload{Data: []byte("next")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StartRootTurn(t.Context(), rootID, rootAgentID, item.InboxSeq); err != nil {
		t.Fatal(err)
	}
	cutoff := 3
	commit := RootTurnCommit{RootID: rootID, AgentID: rootAgentID, InboxSeq: item.InboxSeq, Messages: []llm.Message{{Role: "user", Content: "four"}}, Compactions: []RootCompaction{{Summary: "one and three", RawCutoff: &cutoff}}, Model: "model", Provider: "provider"}
	commitFailure := errors.New("injected after all transcript writes")
	if err := store.commitRootTurn(t.Context(), commit, func() error { return commitFailure }); !errors.Is(err, commitFailure) {
		t.Fatalf("injected failure = %v", err)
	}
	page, err := store.ReadTranscript(t.Context(), rootID, rootAgentID, 0, -1, 128)
	if err != nil || len(page.Messages) != 2 || len(store.Compactions(rootID)) != 0 {
		t.Fatalf("root commit partially persisted = %+v, %v", page, err)
	}
	cutoff = 2
	if err := store.CommitRootTurn(t.Context(), commit); err == nil {
		t.Fatal("missing raw sequence accepted as cutoff")
	}
	cutoff = 3
	if err := store.CommitRootTurn(t.Context(), commit); err != nil {
		t.Fatal(err)
	}
	_, view, err := store.Load(rootID)
	if err != nil || len(view) != 2 || view[0].RawSequence != 3 || view[1].RawSequence != 4 || view[1].Content != "four" {
		t.Fatalf("sequence-mapped view = %+v, %v", view, err)
	}
	if err := store.CommitRootTurn(t.Context(), commit); err == nil {
		t.Fatal("repeated root journal commit accepted")
	}
}

func TestExplicitRawCompactionValidatesSequenceBeforeWriting(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	if err := store.Save(rootID, 1, []llm.Message{{}, {Role: "user", Content: "one"}, {}, {Role: "assistant", Content: "three"}}, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	for _, cutoff := range []int{0, 2, 4} {
		if err := store.RecordRawCompaction(t.Context(), rootID, rootAgentID, cutoff, "invalid"); err == nil {
			t.Fatalf("explicit compaction accepted missing sequence %d", cutoff)
		}
		if got := store.Compactions(rootID); len(got) != 0 {
			t.Fatalf("invalid explicit compaction persisted: %+v", got)
		}
	}
	if err := store.RecordRawCompaction(t.Context(), rootID, rootAgentID, 3, "valid"); err != nil {
		t.Fatal(err)
	}
	compactions := store.Compactions(rootID)
	if len(compactions) != 1 || compactions[0].Cutoff != 2 {
		t.Fatalf("explicit sequence-to-row mapping: %+v", compactions)
	}
	_, view, err := store.Load(rootID)
	if err != nil || len(view) != 1 || view[0].RawSequence != 3 || !strings.Contains(view[0].Content, "valid") {
		t.Fatalf("explicit compaction restoration: %+v, %v", view, err)
	}
}
