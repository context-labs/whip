package daemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestV2BoundedViewsAcrossTransports(t *testing.T) {
	f := newV2Fixture(t, &fakeRunner{})
	body := strings.TrimSpace(strings.Repeat("large history ", 50000))
	if err := f.store.Save(f.rootID, 0, []llm.Message{{Role: "user", Content: body, Authored: true}}, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err := f.store.AddSchedule(f.rootID, "@every 1h", "prompt", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, transport := range []string{"unix", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			c := f.dial(transport, "views-"+transport)
			defer c.Close()
			params := protocol.QueryParams{RootID: f.rootID, Operation: "history.user.list", Payload: json.RawMessage(`{}`)}
			var wire protocol.QueryResult
			if err := c.Call(t.Context(), "query", params, &wire); err != nil {
				t.Fatal(err)
			}
			if wire.Content == nil || len(wire.Result) != 0 || wire.RootID != f.rootID {
				t.Fatalf("large result inline: %+v", wire)
			}
			resolved, err := c.Query(t.Context(), params)
			if err != nil {
				t.Fatal(err)
			}
			var history []string
			if err = json.Unmarshal(resolved.Result, &history); err != nil || len(history) != 1 || history[0] != body {
				t.Fatalf("resolved history: %v", err)
			}
			page, err := c.RootCollection(t.Context(), protocol.RootCollectionParams{RootID: f.rootID, Collection: "schedules", Limit: 2, MaxBytes: 4096})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 2 || !page.HasMore {
				t.Fatalf("page %+v", page)
			}
			var catalog session.SessionCatalogPage
			if err = c.Call(t.Context(), "sessions.list", protocol.SessionCatalogParams{Limit: 8, MaxBytes: 4096}, &catalog); err != nil {
				t.Fatal(err)
			}
			if len(catalog.Items) != 1 || catalog.Items[0].ID != f.rootID {
				t.Fatalf("catalog %+v", catalog)
			}
		})
	}
}
