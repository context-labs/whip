package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browserhost"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

type browserFixture struct {
	peer             *BrowserPeer
	commands         atomic.Int64
	screenshots      atomic.Int64
	blocked          chan struct{}
	block            atomic.Bool
	transferBlock    atomic.Bool
	transferObserved chan *browserhost.Command
}

func startBrowserFixture(t *testing.T, r *Runtime, owner session.Session) *browserFixture {
	t.Helper()
	peer, err := r.BrowserPeer()
	if err != nil {
		t.Fatal(err)
	}
	offer := browserhost.Offer{RootID: string(owner.ID), Version: 2, DesktopID: "desktop", WindowID: "window", OfferRevision: "offer", CreateProfileID: "profile", Tabs: []browserhost.OfferedTab{{TabID: "human-tab", TabGeneration: "generation", ProfileID: "profile", DocumentRevision: "doc1", URL: "https://example.test", Title: "human page"}}, PreviewHosts: []browserhost.Preview{}}
	if _, err := peer.Bind(t.Context(), offer); err != nil {
		t.Fatal(err)
	}
	f := &browserFixture{peer: peer, blocked: make(chan struct{}, 1), transferObserved: make(chan *browserhost.Command, 1)}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			notification, err := peer.Next(ctx)
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, browserhost.ErrClosed) {
					done <- nil
				} else {
					done <- err
				}
				return
			}
			if n := notification.Inventory; n != nil {
				tabs := []browserhost.Tab{}
				for _, target := range n.Tabs {
					tabs = append(tabs, browserhost.Tab{TabID: target.TabID, TabGeneration: target.TabGeneration, DocumentRevision: "doc1", Title: "safe", URL: "https://example.test", State: "available"})
				}
				if err := peer.SettleInventory(browserhost.InventoryResult{RequestID: n.RequestID, RootID: n.Identity.RootID, ProviderEpoch: n.Binding.ProviderEpoch, Tabs: tabs}); err != nil {
					done <- err
					return
				}
			}
			cmd := notification.Command
			if cmd == nil {
				continue
			}
			// This tests the actual runtime/store dispatch boundary, not just fake
			// broker consent. No native request can arrive before this SQL state.
			op, err := r.store.Operation(ctx, session.OperationID(cmd.OperationID))
			if err != nil || op.State != session.OperationDispatched {
				done <- errors.New("browser request arrived before committed dispatch")
				return
			}
			f.commands.Add(1)
			if cmd.Kind == "transfer" && f.transferBlock.Load() {
				f.transferObserved <- cmd
				continue
			}
			result := browserhost.CommandResult{CommandID: cmd.CommandID, RootID: cmd.Identity.RootID, ProviderEpoch: cmd.Scope.ProviderEpoch, AttachmentGeneration: cmd.Scope.AttachmentGeneration, DocumentRevision: "doc1", URL: "https://example.test", Title: "page", Result: json.RawMessage(`{}`)}
			if cmd.Kind == "cdp" {
				var params struct {
					Method string `json:"method"`
				}
				if err := json.Unmarshal(cmd.Arguments, &params); err != nil {
					done <- err
					return
				}
				switch params.Method {
				case "Input.insertText":
					if f.block.Load() {
						f.blocked <- struct{}{}
						continue
					}
				case "Page.getFrameTree":
					result.Result = json.RawMessage(`{"frameTree":{"frame":{"id":"frame","url":"about:blank","securityOrigin":"null","mimeType":"text/html"}}}`)
				case "Runtime.evaluate":
					result.Result = json.RawMessage(`{"result":{"type":"string","value":"{\"url\":\"about:blank\",\"title\":\"page\",\"w\":2,\"h\":2}"}}`)
				case "Page.getLayoutMetrics":
					result.Result = json.RawMessage(`{"cssLayoutViewport":{"clientWidth":2,"clientHeight":2}}`)
				case "Page.captureScreenshot":
					f.screenshots.Add(1)
					data := jpg.Bytes()
					if err := peer.UploadScreenshot(cmd.CommandID, cmd.Identity.RootID, cmd.Scope.ProviderEpoch, cmd.Scope.AttachmentGeneration, 0, data); err != nil {
						done <- err
						return
					}
					sum := sha256.Sum256(data)
					result.Screenshot = &browserhost.Screenshot{Size: len(data), Digest: hex.EncodeToString(sum[:]), MediaType: "image/jpeg"}
				}
			}
			if err := peer.Settle(result); err != nil {
				done <- err
				return
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		peer.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return f
}

func browserCall(owner session.Session, cell session.Cell, id, name string, args map[string]any) tool.Invocation {
	return tool.Invocation{SessionID: owner.ID, CellID: cell.ID, RequestID: id, Module: "browser", Name: name, Arguments: args}
}

func attachBrowserFixture(t *testing.T, r *Runtime, owner session.Session, cell session.Cell) browserhost.Attachment {
	t.Helper()
	if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: owner.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.tools.Call(t.Context(), browserCall(owner, cell, "attach", "attach", map[string]any{"tab_id": "human-tab"})); err != nil {
		t.Fatal(err)
	}
	entries, err := r.BrowserAttachments(t.Context(), owner.ID)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	return entries[0]
}

func TestBrowserOfferAndCatalogueDoNotGrantControl(t *testing.T) {
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	fake := startBrowserFixture(t, r, owner)
	if _, _, err := r.tools.Call(t.Context(), browserCall(owner, cell, "list", "list_tabs", map[string]any{})); err != nil {
		t.Fatal(err)
	}
	grants, err := r.Grants(t.Context(), owner.ID, "", 100)
	if err != nil || len(grants) != 0 || fake.commands.Load() != 0 {
		t.Fatal(grants, err)
	}
	type outcome struct {
		id  session.OperationID
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		_, id, err := r.tools.Call(t.Context(), browserCall(owner, cell, "attach", "attach", map[string]any{"tab_id": "human-tab"}))
		done <- outcome{id, err}
	}()
	var op session.Operation
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		operations, err := r.Operations(t.Context(), cell.TurnID, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range operations {
			if value.Capability == "browser.control" {
				op = value
			}
		}
		if op.ID != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if op.State != session.OperationWaiting || fake.commands.Load() != 0 {
		t.Fatal("offer authorized control", op)
	}
	if _, err := r.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	op, err = r.Operation(t.Context(), got.id)
	if err != nil || op.State != session.OperationSucceeded {
		t.Fatal(op, err)
	}
	grants, err = r.Grants(t.Context(), owner.ID, "", 100)
	if err != nil || len(grants) != 2 {
		t.Fatal(grants, err)
	}
	if fake.commands.Load() != 1 {
		t.Fatal("unexpected native replay", fake.commands.Load())
	}
}

func TestBrowserRunUsesCapturedControlAndTypedImages(t *testing.T) {
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	fake := startBrowserFixture(t, r, owner)
	attachment := attachBrowserFixture(t, r, owner, cell)
	if _, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "scripted", Name: "changed"}}); err != nil {
		t.Fatal(err)
	}
	call := browserCall(owner, cell, "run", "run", map[string]any{"attachment_id": attachment.Scope.AttachmentID, "code": `type("café"); screenshot()`})
	_, id, err := r.tools.Call(t.Context(), call)
	if err != nil {
		t.Fatal(err)
	}
	op, err := r.Operation(t.Context(), id)
	if err != nil || op.State != session.OperationSucceeded || len(op.Result.ContentReferences) != 1 || fake.screenshots.Load() != 1 {
		t.Fatal(op, err)
	}
	before := fake.commands.Load()
	if _, _, err := r.tools.Call(t.Context(), call); err == nil || fake.commands.Load() != before {
		t.Fatal("replayed accepted browser effect", err)
	}
	if _, err := r.store.SettleCell(t.Context(), cell.ID, session.CellSucceeded, session.ToolResult{CallID: cell.CallID, Output: "canonical browser result"}, nil); err != nil {
		t.Fatal(err)
	}
	parts, err := r.store.CellResultParts(t.Context(), owner.ID, cell.ID)
	if err != nil || len(parts) != 2 || parts[0].Result.Output != "canonical browser result" {
		t.Fatal(parts, err)
	}
}

func TestBrowserRevocationCancelsInflightNativeCallAndDoesNotReplay(t *testing.T) {
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	fake := startBrowserFixture(t, r, owner)
	attachment := attachBrowserFixture(t, r, owner, cell)
	fake.block.Store(true)
	done := make(chan session.OperationID, 1)
	go func() {
		_, id, _ := r.tools.Call(t.Context(), browserCall(owner, cell, "block", "run", map[string]any{"attachment_id": attachment.Scope.AttachmentID, "code": `type("block"); screenshot()`}))
		done <- id
	}()
	select {
	case <-fake.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("native call did not block")
	}
	grants, err := r.Grants(t.Context(), owner.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if grant.Capability == "browser.control" && grant.OperationID == nil {
			if _, err := r.RevokeGrant(t.Context(), grant.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	var id session.OperationID
	select {
	case id = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("revocation did not join browser call")
	}
	op, err := r.Operation(t.Context(), id)
	if err != nil || op.State != session.OperationUncertain || fake.screenshots.Load() != 0 {
		t.Fatal(op, err)
	}
	entries, err := r.BrowserAttachments(t.Context(), owner.ID)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

func TestBrowserNativeSuccessThenPublicationFailureRetiresHandle(t *testing.T) {
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	fake := startBrowserFixture(t, r, owner)
	if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: owner.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(r.directory, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_browser BEFORE INSERT ON grants WHEN NEW.capability='browser.control' BEGIN SELECT RAISE(ABORT,'injected publication failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, id, err := r.tools.Call(t.Context(), browserCall(owner, cell, "attach", "attach", map[string]any{"tab_id": "human-tab"}))
	if err == nil || fake.commands.Load() != 1 {
		t.Fatal("native success not accounted", err)
	}
	op, err := r.Operation(t.Context(), id)
	if err != nil || op.State != session.OperationUncertain {
		t.Fatal(op, err)
	}
	entries, err := r.BrowserAttachments(t.Context(), owner.ID)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed publication retained control", entries, err)
	}
}

func TestBrowserRejectsExpansionAndForeignTargetBeforeNativeEffect(t *testing.T) {
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	fake := startBrowserFixture(t, r, owner)
	attachment := attachBrowserFixture(t, r, owner, cell)
	before := fake.commands.Load()
	for _, args := range []map[string]any{
		{"attachment_id": attachment.Scope.AttachmentID, "code": `type("prefix"); useTab("foreign")`},
		{"attachment_id": attachment.Scope.AttachmentID, "code": `type("prefix"); upload("input",["/tmp/private"])`},
		{"attachment_id": attachment.Scope.AttachmentID, "code": `type("prefix")`, "timeout": 121},
	} {
		if _, _, err := r.tools.Call(t.Context(), browserCall(owner, cell, "invalid", "run", args)); err == nil {
			t.Fatal("invalid batch accepted")
		}
	}
	if fake.commands.Load() != before {
		t.Fatal("invalid batch ran prefix")
	}
	child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "browser-test", RequestID: "child"}, store.ChildRequest{ParentID: owner.ID, Parts: []session.Part{{Type: "text", Text: "child"}}})
	if err != nil {
		t.Fatal(err)
	}
	foreign := browserhost.Offer{RootID: string(child.Session.ID), Version: 2, DesktopID: "desktop", WindowID: "window", OfferRevision: "offer", CreateProfileID: "profile", Tabs: []browserhost.OfferedTab{}, PreviewHosts: []browserhost.Preview{}}
	if _, err := fake.peer.Bind(t.Context(), foreign); !errors.Is(err, store.ErrConflict) {
		t.Fatal("child invented root offer", err)
	}
}

func TestBothEnginesBrowserBindingsImagesAndRestartDoNotRestoreControl(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{
				"attach": `a=browser.attach(tab_id="human-tab"); print(a["tab_id"])`,
				"run":    `v=browser.run(attachment_id=a["attachment_id"],code='print("done"); screenshot()'); print(v["output"])`,
				"read":   `print(a["tab_id"])`,
			}
			if engine == session.QuickJS {
				codes = map[string]string{
					"attach": `var a=await browser.attach({tab_id:"human-tab"}); console.log(a.tab_id)`,
					"run":    `var v=await browser.run({attachment_id:a.attachment_id,code:'print("done"); screenshot()'}); console.log(v.output)`,
					"read":   `console.log(a.tab_id)`,
				}
			}
			directory := t.TempDir()
			provider := cellProvider(codes)
			r := openEngineTest(t, directory, provider)
			root := createEngineSession(t, r, engine)
			fake := startBrowserFixture(t, r, root)
			if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: root.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
				t.Fatal(err)
			}
			runCellTurn(t, r, root.ID, "attach", "human-tab\n")
			if _, err := r.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "scripted", Name: "different"}}); err != nil {
				t.Fatal(err)
			}
			runCellTurn(t, r, root.ID, "run", "done\n(screenshot captured: 597 bytes, jpeg, ≤1568px)\n\n")
			latest, err := r.store.LatestCell(t.Context(), root.ID)
			if err != nil {
				t.Fatal(err)
			}
			parts, err := r.store.CellResultParts(t.Context(), root.ID, latest.ID)
			if err != nil || len(parts) != 2 || fake.screenshots.Load() != 1 {
				t.Fatal(parts, err)
			}
			// Closing the connection retires control, while checkpoint values remain
			// ordinary data. Restart cannot turn a saved attachment ID back into access.
			fake.peer.Close()
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			restarted := openEngineTest(t, directory, provider)
			runCellTurn(t, restarted, root.ID, "read", "human-tab\n")
			if entries, err := restarted.BrowserAttachments(t.Context(), root.ID); err != nil || len(entries) != 0 {
				t.Fatal(entries, err)
			}
			owner, err := restarted.store.Session(t.Context(), root.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := restarted.prepareBrowser(t.Context(), owner, tool.Invocation{Name: "run", Arguments: map[string]any{"attachment_id": "saved", "code": "info()"}}); err == nil {
				t.Fatal("checkpoint restored browser authority")
			}
		})
	}
}

func TestBrowserDirectHumanWorkHasNoModelCellOrConversation(t *testing.T) {
	r, calls := directRuntime(t)
	root := createTest(t, r)
	startBrowserFixture(t, r, root)
	if _, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "auto", SessionID: root.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
		t.Fatal(err)
	}
	a := admitDirect(t, r, root.ID, "catalog", "browser", "list_tabs", `{}`)
	complete := waitTest(t, r, "catalog", terminal)
	if complete.Input.ID != a.Input.ID || complete.Turn.State != session.Succeeded {
		t.Fatal(complete)
	}
	a = admitDirect(t, r, root.ID, "attach", "browser", "attach", `{"tab_id":"human-tab"}`)
	complete = waitTest(t, r, "attach", terminal)
	if complete.Input.ID != a.Input.ID || complete.Turn.State != session.Succeeded {
		t.Fatal(complete)
	}
	history, err := r.History(t.Context(), root.ID, 0, 100)
	if err != nil || len(history) != 0 || calls.Load() != 0 {
		t.Fatal(history, err)
	}
	cells, err := r.Cells(t.Context(), complete.Turn.ID, "", 100)
	if err != nil || len(cells) != 0 {
		t.Fatal(cells, err)
	}
}
