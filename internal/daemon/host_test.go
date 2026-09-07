package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/theme"
)

func TestHostServicesAcrossTransports(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	t.Setenv("INFERENCE_API_KEY", "")
	themes := filepath.Join(home, "themes")
	if err := os.MkdirAll(themes, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, "custom.json"), []byte(`{"name":"custom","dark":true,"palette":{"primary":"#abcdef"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, "broken.json"), []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newV2Fixture(t, &fakeRunner{})
	if _, err := f.store.EnsureAuthority(t.Context(), f.rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AdmitAgent(t.Context(), session.AgentAdmission{RootID: f.rootID, ParentAgentID: f.rootID, ChildAgentID: "inspect-child", Name: "child", Model: "model", Provider: "provider"}); err != nil {
		t.Fatal(err)
	}
	message, err := f.store.SendMailboxMessage(t.Context(), f.rootID, f.rootID, "inspect-child", session.MailboxSend{Subject: "hello", Body: "read without delivery", Delivery: session.MessageDeliveryNextTurn})
	if err != nil {
		t.Fatal(err)
	}
	var firstCatalog *theme.CatalogResult
	for _, transport := range []string{"unix", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			client := f.dial(transport, "host-"+transport)
			catalogs, err := client.Query(t.Context(), protocol.QueryParams{Operation: "provider.catalogs", Payload: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatalf("rootless provider catalogs: %v", err)
			}
			var providers protocol.ProviderCatalogsResult
			if err := json.Unmarshal(catalogs.Result, &providers); err != nil || len(providers.Models) == 0 {
				t.Fatalf("provider descriptors missing: %v", err)
			}
			var directories protocol.HostDirectoryResult
			if err := client.Call(t.Context(), "host.directories.list", protocol.HostDirectoryParams{Path: home, Limit: 16}, &directories); err != nil || !slices.ContainsFunc(directories.Entries, func(e protocol.HostDirectoryEntry) bool { return e.Name == "themes" }) {
				t.Fatalf("host directories %+v: %v", directories, err)
			}
			var catalog theme.CatalogResult
			if err := client.Call(t.Context(), "host.themes.list", protocol.Empty{}, &catalog); err != nil {
				t.Fatal(err)
			}
			if len(catalog.Themes) != len(theme.Builtins())+1 || len(catalog.Errors) != 1 {
				t.Fatalf("theme catalog %+v", catalog)
			}
			if firstCatalog == nil {
				firstCatalog = &catalog
			} else if !reflect.DeepEqual(*firstCatalog, catalog) {
				t.Fatal("transport theme catalog differs")
			}
			var resolved theme.Resolved
			if err := client.Call(t.Context(), "host.themes.resolve", protocol.HostThemeResolveParams{Name: "custom"}, &resolved); err != nil || resolved.Colors.Primary != "#abcdef" {
				t.Fatalf("custom resolution %+v: %v", resolved, err)
			}
			if err := client.Call(t.Context(), "host.themes.resolve", protocol.HostThemeResolveParams{JSON: `{"name":"imported","dark":false,"palette":{"primary":"#123456"}}`}, &resolved); err != nil || resolved.Colors.Primary != "#123456" {
				t.Fatalf("imported resolution %+v: %v", resolved, err)
			}
			if err := client.Call(t.Context(), "host.themes.resolve", protocol.HostThemeResolveParams{Name: "../config.json"}, &resolved); err == nil {
				t.Fatal("theme path used as arbitrary file read")
			}
			var page session.MailboxPage
			if err := client.Call(t.Context(), "mailbox.list", protocol.MailboxPageParams{RootID: f.rootID, AgentID: "inspect-child", Limit: 4, MaxBytes: 4096}, &page); err != nil || len(page.Items) != 1 || page.Items[0].ID != message.ID {
				t.Fatalf("mailbox page %+v %v", page, err)
			}
			var read session.MailboxInspection
			if err := client.Call(t.Context(), "mailbox.read", protocol.MailboxReadParams{RootID: f.rootID, AgentID: "inspect-child", ID: message.ID}, &read); err != nil || string(read.Body.Inline) != "read without delivery" {
				t.Fatalf("mailbox read %+v %v", read, err)
			}
			var attention protocol.HostAttentionResult
			if err := client.Call(t.Context(), "host.attention", protocol.HostAttentionParams{Limit: 4, MaxBytes: 4096}, &attention); err != nil || len(attention.Items) != 0 {
				t.Fatalf("idle host attention %+v %v", attention, err)
			}
		})
	}
	read, err := f.store.ReadMailboxMessage(t.Context(), f.rootID, "inspect-child", message.ID)
	if err != nil || read.Status != "pending" || read.Revision != message.Revision {
		t.Fatalf("transport inspection affected delivery: %+v %v", read, err)
	}
}

func TestHostReadsDoNotOpenRootsAndQuestionsDisappear(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv("INFERENCE_API_KEY", "")
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	opens := 0
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		opens++
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	server := &Server{daemon: owner}
	// A fresh host catalog is usable before the first session exists.
	if _, err := server.query(t.Context(), protocol.QueryParams{Operation: "provider.catalogs", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for range 20 {
		ids = append(ids, createRoot(t, store))
	}
	page, err := server.hostAttention(t.Context(), protocol.HostAttentionParams{Limit: 4, MaxBytes: 4096})
	if err != nil || len(page.Items) != 0 || opens != 0 {
		t.Fatalf("inspection opened roots: %+v %d %v", page, opens, err)
	}
	root, err := owner.Open(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	root.questions.mu.Lock()
	root.questions.pending = map[string]*questionWaiter{"question": {event: session.LifecycleEvent{QuestionID: "question", AgentID: root.AgentID(), Question: "Which option?"}}}
	root.questions.mu.Unlock()
	page, err = server.hostAttention(t.Context(), protocol.HostAttentionParams{Limit: 4, MaxBytes: 4096})
	if err != nil || len(page.Items) != 1 || len(page.Items[0].Questions) != 1 || opens != 1 {
		t.Fatalf("live question index %+v %d %v", page, opens, err)
	}
	root.questions.mu.Lock()
	delete(root.questions.pending, "question")
	root.questions.mu.Unlock()
	page, err = server.hostAttention(t.Context(), protocol.HostAttentionParams{Limit: 4, MaxBytes: 4096})
	if err != nil || len(page.Items) != 0 || opens != 1 {
		t.Fatalf("answered question retained %+v %d %v", page, opens, err)
	}
	for _, id := range ids {
		root, err := owner.Open(id)
		if err != nil {
			t.Fatal(err)
		}
		root.questions.mu.Lock()
		root.questions.pending = map[string]*questionWaiter{"question": {event: session.LifecycleEvent{QuestionID: "question", AgentID: id, Question: strings.Repeat("界", 500)}}}
		root.questions.mu.Unlock()
	}
	seen := map[string]bool{}
	after := ""
	for {
		page, err := server.hostAttention(t.Context(), protocol.HostAttentionParams{AfterID: after, Limit: 10, MaxBytes: 4096})
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(page)
		if len(encoded) > 4096 || len(page.Items) > 10 {
			t.Fatalf("unbounded attention page: %d bytes, %d items", len(encoded), len(page.Items))
		}
		for _, item := range page.Items {
			if seen[item.RootID] || !item.Truncated {
				t.Fatalf("duplicate or unmarked shortened question: %+v", item)
			}
			seen[item.RootID] = true
		}
		if !page.HasMore {
			break
		}
		after = page.NextAfterID
	}
	if len(seen) != len(ids) || opens != len(ids) {
		t.Fatalf("attention pagination found %d roots and opened %d actors", len(seen), opens)
	}
}

func TestHostDirectoryBoundsAndFilters(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"alpha", "beta", ".hidden"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "file"), []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := hostDirectories(t.Context(), protocol.HostDirectoryParams{Path: directory, Limit: 1})
	if err != nil || len(first.Entries) != 1 || first.Entries[0].Name != "alpha" || first.NextAfter != "alpha" || !first.HasMore {
		t.Fatalf("first page %+v %v", first, err)
	}
	next, err := hostDirectories(t.Context(), protocol.HostDirectoryParams{Path: directory, After: first.NextAfter, Limit: 1})
	if err != nil || len(next.Entries) != 1 || next.Entries[0].Name != "beta" || next.HasMore {
		t.Fatalf("next page %+v %v", next, err)
	}
	filtered, err := hostDirectories(t.Context(), protocol.HostDirectoryParams{Path: directory, Prefix: ".", ShowHidden: true, Limit: 1})
	if err != nil || len(filtered.Entries) != 1 || filtered.Entries[0].Name != ".hidden" {
		t.Fatalf("hidden filter %+v %v", filtered, err)
	}
	for _, params := range []protocol.HostDirectoryParams{{Path: directory, Limit: 129}, {Path: "relative", Limit: 1}, {Path: directory, Prefix: "../", Limit: 1}, {Path: strings.Repeat("/", 4097), Limit: 1}} {
		if _, err := hostDirectories(t.Context(), params); err == nil {
			t.Fatalf("invalid bounds accepted %+v", params)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := hostDirectories(ctx, protocol.HostDirectoryParams{Path: directory, Limit: 1}); err == nil {
		t.Fatal("cancelled directory read succeeded")
	}
}
