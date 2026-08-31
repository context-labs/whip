package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func newSwarmFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID, err := store.Create(t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	return store, rootID, authority.AgentID
}

func admitTestChild(t *testing.T, store *Store, rootID, parentID, childID string) {
	t.Helper()
	if _, err := store.AdmitChild(context.Background(), ChildAdmission{
		RootID: rootID, ParentAgentID: parentID, ChildAgentID: childID, ExecutionID: "exec-" + childID,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSwarmChildAdmissionAndRelativeDiscovery(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "parent")
	admitTestChild(t, store, rootID, "parent", "child")
	admitTestChild(t, store, rootID, "child", "grandchild")
	admitTestChild(t, store, rootID, "parent", "sibling")

	relatives, err := store.ListAgentRelatives(context.Background(), rootID, "child")
	if err != nil {
		t.Fatal(err)
	}
	if relatives.Parent == nil || relatives.Parent.ID != "parent" {
		t.Fatalf("parent = %+v", relatives.Parent)
	}
	if len(relatives.Children) != 1 || relatives.Children[0].ID != "grandchild" {
		t.Fatalf("children = %+v", relatives.Children)
	}
	if len(relatives.Siblings) != 1 || relatives.Siblings[0].ID != "sibling" {
		t.Fatalf("siblings = %+v", relatives.Siblings)
	}

	var childRoot, childParent, executionRoot, executionParent, executionChild, eventKind string
	if err := store.db.QueryRow(`SELECT root_id,parent_id FROM agents WHERE id='grandchild'`).Scan(&childRoot, &childParent); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT root_id,parent_agent_id,child_agent_id FROM child_executions WHERE id='exec-grandchild'`).Scan(&executionRoot, &executionParent, &executionChild); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT kind FROM events WHERE root_id=? ORDER BY seq DESC LIMIT 1`, rootID).Scan(&eventKind); err != nil {
		t.Fatal(err)
	}
	if childRoot != rootID || executionRoot != rootID || childParent != "child" || executionParent != "child" || executionChild != "grandchild" || eventKind != "child.admitted" {
		t.Fatalf("child=(%q,%q) execution=(%q,%q,%q) event=%q", childRoot, childParent, executionRoot, executionParent, executionChild, eventKind)
	}
	if _, err := store.AdmitChild(context.Background(), ChildAdmission{
		RootID: rootID, ParentAgentID: "parent", ChildAgentID: "rolled-back", ExecutionID: "exec-child",
	}); err == nil {
		t.Fatal("duplicate execution should fail child admission")
	}
	var rolledBack int
	if err := store.db.QueryRow(`SELECT count(*) FROM agents WHERE id='rolled-back'`).Scan(&rolledBack); err != nil || rolledBack != 0 {
		t.Fatalf("rolled-back child rows=%d err=%v", rolledBack, err)
	}
}

func TestSwarmSiblingMessageSharesAuthorizedEvidence(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "tester")
	admitTestChild(t, store, rootID, rootAgentID, "implementer")
	evidenceBody := bytes.Repeat([]byte("failure evidence"), 1024)
	evidence, err := store.StoreContent(context.Background(), ContentGrant{
		RootID: rootID, AgentID: "tester", Scope: ContentGrantAgent,
	}, RuntimePayload{Data: evidenceBody, MediaType: "text/plain", Source: "test failure"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReadContent(context.Background(), evidence.ReferenceID, rootID, "implementer", 0, 1); !errors.Is(err, ErrContentAccess) {
		t.Fatalf("recipient read before share = %v", err)
	}

	if _, err := store.SendAgentMessage(context.Background(), rootID, "tester", "implementer", AgentMessage{
		Delivery: DeliveryQueued, Body: "the test failed", EvidenceReferenceID: evidence.ReferenceID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SendAgentMessage(context.Background(), rootID, "tester", "implementer", AgentMessage{
		Delivery: DeliveryNextTurn, Body: "check this next",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SendAgentMessage(context.Background(), rootID, "tester", "implementer", AgentMessage{
		Delivery: DeliveryImmediate, Body: "steer now",
	}); err != nil {
		t.Fatal(err)
	}

	items, err := store.LoadQueuedInbox(context.Background(), rootID, "implementer", 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Kind != "peer.message" || items[1].Kind != "peer.message" || items[2].Kind != "steer" {
		t.Fatalf("message inbox = %+v", items)
	}
	for i, want := range []MessageDelivery{DeliveryQueued, DeliveryNextTurn, DeliveryImmediate} {
		var envelope AgentMessageEnvelope
		if err := json.Unmarshal(items[i].Payload.Inline, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.SenderAgentID != "tester" || envelope.RecipientAgentID != "implementer" || envelope.Delivery != want {
			t.Fatalf("envelope %d = %+v", i, envelope)
		}
	}
	if got, _, err := store.ReadContent(context.Background(), evidence.ReferenceID, rootID, "implementer", 0, len(evidenceBody)); err != nil || !bytes.Equal(got, evidenceBody) {
		t.Fatalf("shared evidence bytes=%d err=%v", len(got), err)
	}
	var objects, references, recipientGrants int
	if err := store.db.QueryRow(`SELECT count(*) FROM content_objects`).Scan(&objects); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM content_references`).Scan(&references); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM content_grants WHERE reference_id=? AND root_id=? AND agent_id='implementer' AND scope='agent'`, evidence.ReferenceID, rootID).Scan(&recipientGrants); err != nil {
		t.Fatal(err)
	}
	if objects != 1 || references != 1 || recipientGrants != 1 {
		t.Fatalf("content objects=%d references=%d recipient grants=%d", objects, references, recipientGrants)
	}
}

func TestSwarmRejectsCrossRootUnrelatedAndSenderSpoofing(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "left")
	admitTestChild(t, store, rootID, rootAgentID, "right")
	admitTestChild(t, store, rootID, "left", "grandchild")

	otherRoot, err := store.Create(t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	otherAuthority, err := store.EnsureClassicAuthority(context.Background(), otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	admitTestChild(t, store, otherRoot, otherAuthority.AgentID, "outsider")

	if _, err := store.ListAgentRelatives(context.Background(), rootID, "outsider"); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("cross-root discovery error = %v", err)
	}
	if _, err := store.AdmitChild(context.Background(), ChildAdmission{
		RootID: rootID, ParentAgentID: "outsider", ChildAgentID: "cross-root-child", ExecutionID: "cross-root-exec",
	}); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("cross-root admission error = %v", err)
	}
	if _, err := store.SendAgentMessage(context.Background(), rootID, "left", "outsider", AgentMessage{Delivery: DeliveryQueued, Body: "cross root"}); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("cross-root message error = %v", err)
	}
	if _, err := store.SendAgentMessage(context.Background(), rootID, rootAgentID, "grandchild", AgentMessage{Delivery: DeliveryQueued, Body: "not a direct relative"}); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("unrelated message error = %v", err)
	}
	foreignEvidence, err := store.StoreContent(context.Background(), ContentGrant{
		RootID: otherRoot, AgentID: "outsider", Scope: ContentGrantAgent,
	}, RuntimePayload{Data: []byte("private")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SendAgentMessage(context.Background(), rootID, "left", "right", AgentMessage{
		Delivery: DeliveryQueued, Body: "stolen", EvidenceReferenceID: foreignEvidence.ReferenceID,
	}); !errors.Is(err, ErrContentAccess) {
		t.Fatalf("unauthorized evidence error = %v", err)
	}

	spoofBody := `{"sender_agent_id":"outsider"}`
	if _, err := store.SendAgentMessage(context.Background(), rootID, "left", "right", AgentMessage{Delivery: DeliveryQueued, Body: spoofBody}); err != nil {
		t.Fatal(err)
	}
	items, err := store.LoadQueuedInbox(context.Background(), rootID, "right", 0, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("spoof inbox=%+v err=%v", items, err)
	}
	var envelope AgentMessageEnvelope
	if err := json.Unmarshal(items[0].Payload.Inline, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.SenderAgentID != "left" || envelope.Body != spoofBody {
		t.Fatalf("spoofed envelope = %+v", envelope)
	}
}

func TestSwarmTerminalizationPreservesLineage(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "parent")
	admitTestChild(t, store, rootID, "parent", "child")
	admitTestChild(t, store, rootID, "child", "grandchild")
	admitTestChild(t, store, rootID, rootAgentID, "sibling")

	if _, err := store.TerminalizeSubtree(context.Background(), rootID, "parent", "child", "stopped"); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"parent": "idle", "child": "stopped", "grandchild": "stopped", "sibling": "idle"} {
		var got string
		if err := store.db.QueryRow(`SELECT status FROM agents WHERE id=?`, id).Scan(&got); err != nil || got != want {
			t.Fatalf("agent %s status=%q err=%v", id, got, err)
		}
	}
	if _, err := store.TerminalizeSubtree(context.Background(), rootID, "parent", "child", "deleted"); err != nil {
		t.Fatal(err)
	}
	var agentRows, executionRows int
	if err := store.db.QueryRow(`SELECT count(*) FROM agents WHERE root_id=?`, rootID).Scan(&agentRows); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM child_executions WHERE root_id=?`, rootID).Scan(&executionRows); err != nil {
		t.Fatal(err)
	}
	if agentRows != 5 || executionRows != 4 {
		t.Fatalf("preserved rows agents=%d executions=%d", agentRows, executionRows)
	}
	for id, parent := range map[string]string{"child": "parent", "grandchild": "child"} {
		var gotParent, gotStatus string
		if err := store.db.QueryRow(`SELECT parent_id,status FROM agents WHERE id=?`, id).Scan(&gotParent, &gotStatus); err != nil || gotParent != parent || gotStatus != "deleted" {
			t.Fatalf("agent %s parent=%q status=%q err=%v", id, gotParent, gotStatus, err)
		}
	}
	for _, id := range []string{"exec-child", "exec-grandchild"} {
		var status string
		if err := store.db.QueryRow(`SELECT status FROM child_executions WHERE id=?`, id).Scan(&status); err != nil || status != "deleted" {
			t.Fatalf("execution %s status=%q err=%v", id, status, err)
		}
	}
	if _, err := store.TerminalizeSubtree(context.Background(), rootID, "sibling", "parent", "stopped"); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("unrelated terminalization error = %v", err)
	}
}
