package session

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestQueueSteerClaimsInPlaceAndLeavesFIFO(t *testing.T) {
	s, root, agent := newSwarmFixture(t)
	first := inputTestCommand(t, s, root, agent, "first", "submit")
	if err := s.StartRootTurn(t.Context(), root, agent, first.Command.IngressSeq); err != nil {
		t.Fatal(err)
	}
	turn, _ := s.RunningTurnID(t.Context(), root, agent)
	a := inputTestCommand(t, s, root, agent, "A", "submit")
	b := inputTestCommand(t, s, root, agent, "B", "submit")
	c := inputTestCommand(t, s, root, agent, "C", "submit")
	for range 2 {
		got, err := s.ControlInbox(t.Context(), root, agent, b.Command.IngressSeq, turn, false, nil)
		if err != nil || got.Status != "steering" {
			t.Fatalf("promote: %+v %v", got, err)
		}
	}
	items, err := s.ClaimSteers(t.Context(), root, agent, turn)
	if err != nil || len(items) != 1 || items[0].Seq != b.Command.IngressSeq || items[0].Kind != "submit" || items[0].DeliverySeq == 0 {
		t.Fatalf("claim: %+v %v", items, err)
	}
	if got, err := s.ClaimSteers(t.Context(), root, agent, turn); err != nil || len(got) != 0 {
		t.Fatalf("duplicate: %+v %v", got, err)
	}
	if got, err := s.ControlInbox(t.Context(), root, agent, b.Command.IngressSeq, "", true, nil); err != nil || got.Status != "already_started" {
		t.Fatalf("remove running: %+v %v", got, err)
	}
	pending, err := s.LoadQueuedInbox(t.Context(), root, agent, 0, 10)
	if err != nil || len(pending) != 2 || pending[0].Seq != a.Command.IngressSeq || pending[1].Seq != c.Command.IngressSeq {
		t.Fatalf("FIFO: %+v %v", pending, err)
	}
	command, _ := s.LoadCommand(t.Context(), root, "B")
	if command.RequestDigest != b.Command.RequestDigest || command.Operation != "submit" {
		t.Fatalf("rewrote command: %+v", command)
	}
	inputCommandStatus(t, s, root, "first", "running")
}

func TestQueueExpiredSteerAndRecovery(t *testing.T) {
	for _, ending := range []string{"succeeded", "failed", "cancelled", "interrupted", "restart"} {
		t.Run(ending, func(t *testing.T) {
			s, root, agent := newSwarmFixture(t)
			first := inputTestCommand(t, s, root, agent, "first", "submit")
			if err := s.StartRootTurn(t.Context(), root, agent, first.Command.IngressSeq); err != nil {
				t.Fatal(err)
			}
			turn, _ := s.RunningTurnID(t.Context(), root, agent)
			queued := inputTestCommand(t, s, root, agent, "later", "submit")
			if _, err := s.ControlInbox(t.Context(), root, agent, queued.Command.IngressSeq, turn, false, nil); err != nil {
				t.Fatal(err)
			}
			if ending == "restart" {
				s = reopenMailboxStore(t, s)
				if err := s.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
			} else {
				exec(t, s, `UPDATE turns SET status=? WHERE id=?`, ending, turn)
			}
			pending, err := s.LoadQueuedInbox(t.Context(), root, agent, 0, 10)
			if err != nil || len(pending) != 1 || pending[0].SteerTurnID != "" {
				t.Fatalf("expired: %+v %v", pending, err)
			}
			got, err := s.ControlInbox(t.Context(), root, agent, queued.Command.IngressSeq, turn, false, nil)
			if err != nil || got.Status != "turn_ended" {
				t.Fatalf("stale: %+v %v", got, err)
			}
			if err := s.StartRootTurn(t.Context(), root, agent, queued.Command.IngressSeq); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQueueRemovalIsScopedAndAtomic(t *testing.T) {
	s, root, agent := newSwarmFixture(t)
	rootItem := inputTestCommand(t, s, root, agent, "root", "submit")
	admitTestChild(t, s, root, agent, "child")
	child, err := s.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: "child", Kind: "submit", Origin: "client", Payload: RuntimePayload{Data: []byte("child")}})
	if err != nil {
		t.Fatal(err)
	}
	internal, err := s.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: "child", Kind: "submit", Payload: RuntimePayload{Data: []byte("parent follow-up")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ControlInbox(t.Context(), root, "child", internal.InboxSeq, "", true, nil); err == nil {
		t.Fatal("removed internal input")
	}
	exec(t, s, `CREATE TRIGGER reject_queue_removal BEFORE INSERT ON events WHEN NEW.kind='inbox.removed' BEGIN SELECT RAISE(ABORT,'failure'); END`)
	if _, err := s.ControlInbox(t.Context(), root, agent, rootItem.Command.IngressSeq, "", true, nil); err == nil {
		t.Fatal("ignored event failure")
	}
	inputCommandStatus(t, s, root, "root", "queued")
	inputTestStatus(t, s, root, agent, rootItem.Command.IngressSeq, "queued")
	exec(t, s, `DROP TRIGGER reject_queue_removal`)
	for _, expected := range []string{"removed", "already_removed"} {
		got, err := s.ControlInbox(t.Context(), root, "child", child.InboxSeq, "", true, nil)
		if err != nil || got.Status != expected {
			t.Fatalf("child remove: %+v %v", got, err)
		}
	}
	inputCommandStatus(t, s, root, "root", "queued")
	if _, err := s.ControlInbox(t.Context(), root, agent, rootItem.Command.IngressSeq, "", true, []byte(`{"message":"removed"}`)); err != nil {
		t.Fatal(err)
	}
	inputCommandStatus(t, s, root, "root", "cancelled")
}

func TestQueueConcurrentClaimAndRemove(t *testing.T) {
	for range 12 {
		s, root, agent := newSwarmFixture(t)
		first := inputTestCommand(t, s, root, agent, "first", "submit")
		if err := s.StartRootTurn(t.Context(), root, agent, first.Command.IngressSeq); err != nil {
			t.Fatal(err)
		}
		turn, _ := s.RunningTurnID(t.Context(), root, agent)
		item := inputTestCommand(t, s, root, agent, "queued", "submit")
		if _, err := s.ControlInbox(t.Context(), root, agent, item.Command.IngressSeq, turn, false, nil); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := s.ClaimSteers(t.Context(), root, agent, turn); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := s.ControlInbox(t.Context(), root, agent, item.Command.IngressSeq, "", true, nil); err != nil {
				t.Error(err)
			}
		}()
		wg.Wait()
		inputCommandStatus(t, s, root, "first", "running")
		got, _ := s.LoadCommand(t.Context(), root, "queued")
		if got.Status != "running" && got.Status != "cancelled" {
			t.Fatalf("unresolved race: %+v", got)
		}
	}
}

func TestQueueInterruptKeepsOtherRootsSteeringIntent(t *testing.T) {
	s, root, agent := newSwarmFixture(t)
	other, err := s.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureAuthority(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{root, other} {
		first := inputTestCommand(t, s, id, id, "first", "submit")
		if err := s.StartRootTurn(t.Context(), id, id, first.Command.IngressSeq); err != nil {
			t.Fatal(err)
		}
		turn, err := s.RunningTurnID(t.Context(), id, id)
		if err != nil {
			t.Fatal(err)
		}
		queued := inputTestCommand(t, s, id, id, "later", "submit")
		if _, err := s.ControlInbox(t.Context(), id, id, queued.Command.IngressSeq, turn, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.InterruptRoot(t.Context(), root, "fixture interruption"); err != nil {
		t.Fatal(err)
	}
	cleared, err := s.LoadQueuedInbox(t.Context(), root, agent, 0, 10)
	if err != nil || len(cleared) != 1 || cleared[0].SteerTurnID != "" {
		t.Fatalf("interrupted queue: %+v %v", cleared, err)
	}
	retained, err := s.LoadQueuedInbox(t.Context(), other, other, 0, 10)
	if err != nil || len(retained) != 1 || retained[0].SteerTurnID == "" {
		t.Fatalf("other root lost intent: %+v %v", retained, err)
	}
}

func TestQueuePreviewPreservesPayloadAndSurvivesPaging(t *testing.T) {
	s, root, agent := newSwarmFixture(t)
	body := []byte(`{"text":"` + strings.Repeat("🙂", 3000) + `","attachments":[{"kind":"image","name":"one.png","content":{"reference_id":"original","digest":"digest","size":"99","media_type":"image/png"}},{"kind":"text","name":"two.txt","content":{"reference_id":"second","digest":"other","size":"10","media_type":"text/plain"}}]}`)
	item, err := s.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: agent, Kind: "submit.parts", Origin: "client", Payload: RuntimePayload{Data: body, MediaType: "application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.SnapshotRoot(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	p := snapshot.Inbox[0].Preview
	if p == nil || !p.Truncated || len(p.Text) > 2048 || !utf8.ValidString(p.Text) || len(p.Attachments) != 2 || p.AttachmentCount != 2 {
		t.Fatalf("preview: %+v", p)
	}
	resolved, err := s.ResolveInboxPayload(t.Context(), snapshot.Inbox[0])
	if err != nil || string(resolved) != string(body) {
		t.Fatalf("payload changed: %v", err)
	}
	page, err := s.RootCollectionPage(t.Context(), root, "inbox", CollectionPageOptions{Limit: 10, MaxBytes: 256 << 10})
	if err != nil || len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0].Inbox.Preview, p) {
		t.Fatalf("page: %+v %v", page, err)
	}
	encoded, _ := json.Marshal(p)
	if len(encoded) > 16<<10 {
		t.Fatal("unbounded preview")
	}
	if got, err := s.ControlInbox(t.Context(), root, agent, item.InboxSeq, "", true, nil); err != nil || got.Status != "removed" {
		t.Fatalf("remove: %+v %v", got, err)
	}
}

const dropQueueSchema = `ALTER TABLE compactions DROP COLUMN pinned;
DROP TRIGGER inbox_steer_turn_finished;
ALTER TABLE inbox DROP COLUMN origin;
ALTER TABLE inbox DROP COLUMN command_client_id;
ALTER TABLE inbox DROP COLUMN command_id;
ALTER TABLE inbox DROP COLUMN steer_turn_id;
ALTER TABLE inbox DROP COLUMN delivery_seq;
ALTER TABLE inbox DROP COLUMN preview;`

func TestQueueMigrationBackfillsOnlyProvenRootInputs(t *testing.T) {
	s, root, agent := newSwarmFixture(t)
	original := inputTestCommand(t, s, root, agent, "old-client", "submit")
	admitTestChild(t, s, root, agent, "child")
	if _, err := s.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: "child", Kind: "submit", Payload: RuntimePayload{Data: []byte("unknown legacy provenance")}}); err != nil {
		t.Fatal(err)
	}
	exec(t, s, dropQueueSchema+`UPDATE runtime_schema SET identity='whip-recursive-runtime-v19'; PRAGMA user_version=19`)
	s = reopenMailboxStore(t, s)
	items, err := s.LoadQueuedInbox(t.Context(), root, agent, 0, 10)
	if err != nil || len(items) != 1 || items[0].CommandID != "old-client" || items[0].Origin != "client" {
		t.Fatalf("backfill: %+v %v", items, err)
	}
	command, _ := s.LoadCommand(t.Context(), root, "old-client")
	if command.RequestDigest != original.Command.RequestDigest {
		t.Fatal("changed original request")
	}
	children, err := s.LoadQueuedInbox(t.Context(), root, "child", 0, 10)
	if err != nil || len(children) != 1 || children[0].Origin != "" {
		t.Fatalf("guessed child origin: %+v %v", children, err)
	}
}
