package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestStateLargeValueRestartIsolationAndCollection(t *testing.T) {
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	root := createTest(t, r)
	child := mailSender(t, r, root)
	stranger := createTest(t, r)
	data := append([]byte{'"'}, bytes.Repeat([]byte{'a'}, session.MaxStateValueBytes-2)...)
	data = append(data, '"')
	shared, err := r.WriteState(t.Context(), child.ID, session.TreeState, "large", "key", 0, data)
	if err != nil {
		t.Fatal(err)
	}
	private, err := r.WriteState(t.Context(), child.ID, session.SessionState, "private", "key", 0, []byte(`"private"`))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		actor session.SessionID
		id    string
	}{{root.ID, private.ID}, {stranger.ID, shared.ID}, {root.ID, shared.Digest}} {
		if _, _, err := r.ReadStateRange(t.Context(), test.actor, test.id, 0, 10); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("state escaped authorization: %+v %v", test, err)
		}
	}
	if _, _, err := r.ReadContent(t.Context(), root.ID, shared.ID, session.MaxContentBytes); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("state became ordinary content", err)
	}
	if err := r.DeleteSubtree(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, directory, model.Scripted{})
	value, part, err := r.ReadStateRange(t.Context(), root.ID, shared.ID, shared.Size-20, session.MaxStateReadBytes)
	if err != nil || value.ID != shared.ID || !bytes.Equal(part, data[len(data)-20:]) {
		t.Fatalf("large state restart: %+v %q %v", value, part, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "artifacts", "sha256", private.Digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("private state not collected", err)
	}
	if err := r.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, directory, model.Scripted{})
	if _, err := os.Stat(filepath.Join(directory, "artifacts", "sha256", shared.Digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted tree state not collected", err)
	}
	if _, _, err := r.ReadStateRange(t.Context(), root.ID, shared.ID, 0, 10); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("deleted owner read", err)
	}
}

func TestInvalidStateWriteDoesNotPublishContent(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner := createTest(t, r)
	for _, test := range []struct {
		id, key  string
		scope    session.StateScope
		revision int64
		data     string
	}{
		{"bad id", "key", session.TreeState, 0, `true`},
		{"id", "", session.TreeState, 0, `true`},
		{"id", "key", "invalid", 0, `true`},
		{"id", "key", session.TreeState, -1, `true`},
		{"id", "key", session.TreeState, 0, `"\ud800"`},
	} {
		if _, err := r.WriteState(t.Context(), owner.ID, test.scope, test.id, test.key, test.revision, []byte(test.data)); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("invalid write", err)
		}
	}
	bodies, err := r.content.Bodies()
	if err != nil || len(bodies) != 0 {
		t.Fatalf("invalid writes published content: %+v %v", bodies, err)
	}
}

func TestBothEnginesStateHelpersPreserveExactNumbersAndRevisions(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `first=state.write(scope="session",key="exact",expected_revision="0",value=[9007199254740993,-2])
second=state.append(scope="session",key="exact",expected_revision=first["revision"],value=[True])
entry=state.get(scope="session",key="exact")
if entry["value"] != [9007199254740993,-2,True]:
    fail("value changed")
versions=state.history(scope="session",key="exact",after="0")
if len(versions["items"]) != 2:
    fail("missing immutable versions")
state.write(scope="tree",key="public",expected_revision="0",value=entry["value"])
print("state-ok")`
			if engine == session.QuickJS {
				code = `var first=await state.write({scope:'session',key:'exact',expected_revision:'0',value:[9007199254740993n,-2]});
var second=await state.append({scope:'session',key:'exact',expected_revision:first.revision,value:[true]});
var entry=await state.get({scope:'session',key:'exact'});
if(entry.value[0]!==9007199254740993n || entry.value[2]!==true) throw Error('value changed');
var versions=await state.history({scope:'session',key:'exact',after:'0'});
if(versions.items.length!==2) throw Error('missing immutable versions');
await state.write({scope:'tree',key:'public',expected_revision:'0',value:entry.value});
print('state-ok');`
			}
			calls := 0
			r := openMailRuntime(t, t.TempDir(), providerFunc(func(_ context.Context, _ model.Request) (model.Response, error) {
				calls++
				if calls == 1 {
					return model.Response{Parts: []session.Part{mailCode("state", code)}}, nil
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
			}))
			owner := createEngineSession(t, r, engine)
			for _, name := range []string{"write", "append", "get", "history"} {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID("state_" + name), SessionID: owner.ID, Capability: "state." + name, Resource: string(owner.TreeID)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "state"}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "state"}}}); err != nil {
				t.Fatal(err)
			}
			result := waitTestWithin(t, r, "state", terminal, 30*time.Second)
			cells, err := r.Cells(t.Context(), result.Turn.ID, "", 100)
			if err != nil || len(cells) != 1 || cells[0].State != session.CellSucceeded {
				t.Fatalf("state cell=%+v %v", cells, err)
			}
			value, err := r.State(t.Context(), owner.ID, session.TreeState, "public")
			if err != nil {
				t.Fatal(err)
			}
			_, data, err := r.ReadState(t.Context(), owner.ID, value.ID)
			if err != nil || string(data) != `[9007199254740993,-2,true]` {
				t.Fatalf("exact state %s %v", data, err)
			}
		})
	}
}

func TestBothEnginesStateSubscriptionHelpersUseScopedOperations(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `watch=state.subscribe(key="topic",after="0",delivery="next_turn")
items=state.subscriptions()
if len(items["items"]) != 1:
    fail("missing subscription")
state.unsubscribe(id=watch["id"])
print("unsubscribed")`
			if engine == session.QuickJS {
				code = `var watch=await state.subscribe({key:'topic',after:'0',delivery:'next_turn'}); var items=await state.subscriptions({}); if(items.items.length!==1) throw Error('missing subscription'); await state.unsubscribe({id:watch.id}); print('unsubscribed');`
			}
			calls := 0
			r := openMailRuntime(t, t.TempDir(), providerFunc(func(_ context.Context, _ model.Request) (model.Response, error) {
				calls++
				if calls == 1 {
					return model.Response{Parts: []session.Part{mailCode("subscribe", code)}}, nil
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
			}))
			owner := createEngineSession(t, r, engine)
			for _, name := range []string{"subscribe", "subscriptions", "unsubscribe"} {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID("state_" + name), SessionID: owner.ID, Capability: "state." + name, Resource: string(owner.TreeID)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "subscription"}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "subscribe"}}}); err != nil {
				t.Fatal(err)
			}
			result := waitTestWithin(t, r, "subscription", terminal, 30*time.Second)
			cells, err := r.Cells(t.Context(), result.Turn.ID, "", 100)
			if err != nil || len(cells) != 1 || cells[0].State != session.CellSucceeded {
				t.Fatalf("subscription helpers failed: %+v %v", cells, err)
			}
			subscriptions, err := r.StateSubscriptions(t.Context(), owner.ID, "", 100)
			if err != nil || len(subscriptions) != 1 || subscriptions[0].CancelledAt == nil {
				t.Fatalf("subscription cancellation %+v %v", subscriptions, err)
			}
		})
	}
}
