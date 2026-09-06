package session

import "testing"

func TestRootAgentViewsDoNotReadTranscriptBodies(t *testing.T) {
	store, rootID, agentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, agentID, "child")
	exec(t, store, `INSERT INTO messages(session_id,seq,role,content) VALUES(?,1,'user','invalid message json')`, rootID)
	views, err := store.RootAgentViews(t.Context(), rootID)
	if err != nil || len(views) != 2 {
		t.Fatalf("views=%+v error=%v", views, err)
	}
	for _, view := range views {
		if view.LifecyclePhase != "idle" {
			t.Fatalf("incorrect lifecycle: %+v", view)
		}
	}
}
