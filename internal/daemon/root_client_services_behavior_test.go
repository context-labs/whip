package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestRootClientWorkspaceHistoryAndContentRemainScoped(t *testing.T) {
	server, client, root, rootID := providerBehaviorFixture(t)
	store := server.daemon.store
	meta, _, err := store.Load(rootID)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(meta.CWD, "docs")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture.md"), []byte("completion fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, filepath.Join(meta.CWD, "linked-docs")); err != nil {
		t.Fatal(err)
	}
	for _, complete := range []func(context.Context, CompletionParams) (CompletionResult, error){client.CompleteWorkspace, root.CompleteWorkspace} {
		result, err := complete(t.Context(), CompletionParams{RootID: rootID, Kind: "path", Prefix: "linked", Limit: 8})
		if err != nil || len(result.Candidates) != 1 || result.Candidates[0].Text != "linked-docs/" || result.Candidates[0].Description != "dir" {
			t.Fatalf("symlink completion=%+v %v", result, err)
		}
		for _, params := range []CompletionParams{{Kind: "path", Limit: 8}, {RootID: rootID, Kind: "path", Limit: 65}, {RootID: rootID, AgentID: "foreign", Kind: "path", Limit: 8}, {RootID: rootID, Kind: "unknown", Limit: 8}} {
			if _, err := complete(t.Context(), params); err == nil {
				t.Fatalf("invalid completion accepted: %+v", params)
			}
		}
	}
	history := []llm.Message{{Role: "system"}, {Role: "user", Content: "first authored message", Authored: true}, {Role: "assistant", Content: "second retained message"}}
	if err := store.Save(rootID, 1, history, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	page, err := root.HistoryPage(t.Context(), HistoryPageParams{RootID: rootID})
	if err != nil || len(page.Messages) != 2 || page.Messages[0].Message.Content != "first authored message" || page.Messages[1].Message.Content != "second retained message" {
		t.Fatalf("retained history=%+v %v", page, err)
	}
	content, err := store.StoreContent(t.Context(), session.ContentGrant{RootID: rootID, Scope: session.ContentGrantRoot}, session.RuntimePayload{Data: []byte("bounded content"), MediaType: "text/plain", Source: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	read, err := root.ReadContent(t.Context(), protocol.ContentReadParams{RootID: rootID, ReferenceID: content.ReferenceID, Offset: 8, Limit: 7})
	if err != nil || string(read.Data) != "content" || read.Content.Size != 15 {
		t.Fatalf("content read=%+v %v", read, err)
	}
	if _, err := root.ReadContent(t.Context(), protocol.ContentReadParams{RootID: "foreign-root", ReferenceID: content.ReferenceID, Limit: 7}); err == nil {
		t.Fatal("content escaped root grant")
	}
	revision, err := client.SessionCatalogRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := client.SessionCatalog(t.Context(), protocol.SessionCatalogParams{Limit: 1, MaxBytes: 4096})
	if err != nil || len(catalog.Items) != 1 || catalog.Items[0].ID != rootID || catalog.Revision != revision.Revision {
		t.Fatalf("catalog=%+v %v", catalog, err)
	}
	if _, err := store.Create(session.SessionKindAgent, t.TempDir(), "other", "provider"); err != nil {
		t.Fatal(err)
	}
	next, err := client.SessionCatalogRevision(t.Context())
	if err != nil || next.Revision <= revision.Revision {
		t.Fatalf("catalog revision did not advance: %+v %v", next, err)
	}
	_ = root.Close()
	if _, err := root.HistoryPage(t.Context(), HistoryPageParams{RootID: rootID}); err == nil {
		t.Fatalf("closed history read=%v", err)
	}
}

type lostProviderReply struct {
	*staticRootConnection
	calls *atomic.Int32
}

func (c *lostProviderReply) Call(context.Context, string, any, any) error {
	c.calls.Add(1)
	_ = c.Close()
	return net.ErrClosed
}

func TestRootClientProviderMutationsAreNotReplayedAfterLostReply(t *testing.T) {
	var calls atomic.Int32
	client, err := NewRootClient(RootClientOptions{ClientID: "ephemeral", RootID: "root", RetryMin: time.Millisecond, RetryMax: time.Millisecond, Connector: func(context.Context, map[string]int64) (RootConnection, error) {
		return &lostProviderReply{staticRootConnection: newStaticRootConnection(), calls: &calls}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := client.WaitLive(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetProviderKey(ctx, ProviderKeySetup{Revision: "expected", Provider: "openrouter", Key: "ephemeral-secret"}); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("lost reply=%v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("credential mutation replayed %d times", calls.Load())
	}
}

func TestRootClientRejectsUnsupportedConnectionServices(t *testing.T) {
	client, err := NewRootClient(RootClientOptions{ClientID: "limited", RootID: "root", Connector: func(context.Context, map[string]int64) (RootConnection, error) { return newStaticRootConnection(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := client.WaitLive(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReadConfiguration(ctx); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("unsupported provider service=%v", err)
	}
	if _, err := client.ValidateProvider(ctx, ProviderValidateParams{}); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("unsupported validation=%v", err)
	}
	if _, err := client.HistoryPage(ctx, HistoryPageParams{}); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("unsupported history=%v", err)
	}
}

func TestHostServiceRPCRejectsMalformedAndUnscopedReads(t *testing.T) {
	_, client, _, rootID := providerBehaviorFixture(t)
	for _, method := range []string{"host.directories.list", "host.attention", "host.themes.resolve", "mailbox.list", "mailbox.read"} {
		t.Run(method, func(t *testing.T) {
			for _, payload := range []json.RawMessage{json.RawMessage(`{"unknown":"value"}`), json.RawMessage(`{}`)} {
				err := client.Call(t.Context(), method, payload, nil)
				var rpc *RPCError
				if !errors.As(err, &rpc) || rpc.Code != -32602 {
					t.Fatalf("invalid read %s: %v", payload, err)
				}
			}
		})
	}
	for _, payload := range []protocol.HostThemeResolveParams{{JSON: "invalid JSON"}, {Name: "dark", JSON: `{}`}} {
		if err := client.Call(t.Context(), "host.themes.resolve", payload, nil); err == nil {
			t.Fatalf("ambiguous or invalid theme accepted: %+v", payload)
		}
	}
	err := client.Call(t.Context(), "mailbox.read", protocol.MailboxReadParams{RootID: rootID, AgentID: "foreign", ID: "missing"}, nil)
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Code != -32003 {
		t.Fatalf("unscoped mailbox read=%v", err)
	}
}

func TestHostCompletionBoundsSkillWarningsAndExpandsHome(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	for i := range 20 {
		dir := filepath.Join(workspace, ".agents", "skills", fmt.Sprintf("broken-%02d", i))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: [invalid\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"valid-a", "valid-b"} {
		dir := filepath.Join(workspace, ".agents", "skills", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: "+strings.Repeat("界", 100)+"\n---\nUse carefully."), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := completeAt(t.Context(), workspace, CompletionParams{Kind: "skill", Prefix: "valid", Limit: 1})
	if err != nil || len(result.Warnings) != 16 || !result.Truncated || len(result.Candidates) != 1 || len([]rune(result.Candidates[0].Description)) != 80 {
		t.Fatalf("skill completion not bounded: %+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := completeAt(ctx, workspace, CompletionParams{Kind: "skill", Limit: 8}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled skill completion=%v", err)
	}
	if err := os.Mkdir(filepath.Join(home, "projects"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "projects"), filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}
	directories, err := hostDirectories(t.Context(), protocol.HostDirectoryParams{Path: "~", Limit: 8})
	if err != nil || directories.Path != home || len(directories.Entries) != 2 {
		t.Fatalf("home directories=%+v %v", directories, err)
	}
	result, err = completeAt(t.Context(), workspace, CompletionParams{Kind: "path", Prefix: "~/pro", Limit: 8})
	if err != nil || len(result.Candidates) != 1 || result.Candidates[0].Text != filepath.ToSlash(filepath.Join(home, "projects"))+"/" {
		t.Fatalf("home completion=%+v %v", result, err)
	}
	result, err = completeAt(t.Context(), workspace, CompletionParams{Kind: "path", Prefix: "missing/path/", Limit: 8})
	if err != nil || len(result.Candidates) != 0 {
		t.Fatalf("missing directory completion=%+v %v", result, err)
	}
	file := filepath.Join(home, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := completeAt(t.Context(), workspace, CompletionParams{Kind: "path", Prefix: file + "/", Limit: 8}); err == nil {
		t.Fatal("non-directory path produced completions")
	}
}
