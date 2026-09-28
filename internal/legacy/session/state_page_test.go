package session

import "testing"

func TestStatePagesRespectScopeAndCursor(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "left")
	admitTestChild(t, store, rootID, rootAgentID, "right")
	for _, key := range []string{"a", "b", "c"} {
		if _, err := store.SetPrivateState(t.Context(), rootID, "left", key, RuntimePayload{Data: []byte("null"), MediaType: "application/json"}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ListPrivateStatePage(t.Context(), rootID, "left", "a", 1)
	if err != nil || len(page) != 1 || page[0].Key != "b" {
		t.Fatalf("page=%v err=%v", page, err)
	}
	page, err = store.ListPrivateStatePage(t.Context(), rootID, "right", "", 10)
	if err != nil || len(page) != 0 {
		t.Fatalf("scope page=%v err=%v", page, err)
	}
	if _, err = store.ListPrivateStatePage(t.Context(), "another-root", "left", "", 10); err == nil {
		t.Fatal("cross-root accepted")
	}
	for range 3 {
		if _, err := store.SetBlackboard(t.Context(), rootID, "left", "shared", RuntimePayload{Data: []byte("null"), MediaType: "application/json"}); err != nil {
			t.Fatal(err)
		}
	}
	page, err = store.BlackboardHistoryPage(t.Context(), rootID, "right", "shared", 1, 1)
	if err != nil || len(page) != 1 || page[0].Version != 2 {
		t.Fatalf("history=%v err=%v", page, err)
	}
	if _, err = store.BlackboardHistoryPage(t.Context(), "another-root", "right", "shared", 0, 10); err == nil {
		t.Fatal("cross-root history accepted")
	}
}
