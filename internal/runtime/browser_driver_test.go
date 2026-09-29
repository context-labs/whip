package runtime

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestBrowserDriverPinIsCapturedOnceAndSharedCAS(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "chromedp")
	r, _, _ := modelHelperFixture(t, model.Scripted{})
	before, err := r.BrowserDriver(t.Context())
	if err != nil || !before.Pinned || before.Driver != "chromedp" || before.ConfiguredDriver != "rod" {
		t.Fatal(before, err)
	}
	t.Setenv("WHIP_BROWSER_DRIVER", "rod")
	if got, err := r.BrowserDriver(t.Context()); err != nil || got != before {
		t.Fatal("mutable env changed pin", got, err)
	}
	if _, err = r.SetBrowserDriver(t.Context(), before.Revision, "rod"); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("pin overridden", err)
	}
	written, err := r.SetBrowserDriver(t.Context(), before.Revision, "chromedp")
	if err != nil || written.Driver != "chromedp" || written.ConfiguredDriver != "chromedp" || written.Revision == before.Revision {
		t.Fatal(written, err)
	}
	if _, err = r.SetBrowserDriver(t.Context(), before.Revision, "chromedp"); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatal("stale no-op accepted", err)
	}
	if same, err := r.SetBrowserDriver(t.Context(), written.Revision, "chromedp"); err != nil || same != written {
		t.Fatal("same value changed revision", same, err)
	}
}

func TestBrowserDriverRejectsInvalidPinBeforeCreatingHome(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "other")
	if _, err := Open(t.Context(), t.TempDir()+"/absent", model.Scripted{}, Options{}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestBrowserDriverHandoverKeepsAcceptedBatchCapture(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "")
	r, owner, cell := modelHelperFixture(t, model.Scripted{})
	fake := startBrowserFixture(t, r, owner)
	attachment := attachBrowserFixture(t, r, owner, cell)
	for i, driver := range []string{"rod", "chromedp"} {
		selection, err := r.BrowserDriver(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.SetBrowserDriver(t.Context(), selection.Revision, driver); err != nil {
			t.Fatal(err)
		}
		call := browserCall(owner, cell, "captured_"+driver, "run", map[string]any{"attachment_id": attachment.Scope.AttachmentID, "code": `screenshot()`})
		prepared, err := r.prepareBrowser(t.Context(), owner, call)
		if err != nil {
			t.Fatal(err)
		}
		var intent session.BrowserIntent
		if err = json.Unmarshal(prepared.Arguments, &intent); err != nil || intent.Driver != driver {
			t.Fatal(intent, err)
		}
		op, err := r.store.AdmitOperation(t.Context(), session.OperationSpec{ID: call.OperationID(), CellID: cell.ID, RequestID: call.RequestID, Capability: prepared.Capability, Resource: prepared.Resource, Arguments: prepared.Arguments})
		if err != nil {
			t.Fatal(err)
		}
		next := "chromedp"
		if i == 1 {
			next = "rod"
		}
		selection, err = r.BrowserDriver(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.SetBrowserDriver(t.Context(), selection.Revision, next); err != nil {
			t.Fatal(err)
		}
		// The accepted closure must keep its captured driver after the host edit.
		release, err := prepared.Acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := r.store.DispatchOperation(t.Context(), op.ID); err != nil || !ok {
			release()
			t.Fatal(ok, err)
		}
		beforeStarts := fake.clippedScreenshots.Load()
		_, err = prepared.Run(t.Context(), op.ID)
		release()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.store.SettleOperation(t.Context(), op.ID, session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
		if (fake.clippedScreenshots.Load() > beforeStarts) != (driver == "rod") {
			t.Fatal("dispatch used the new driver instead of the captured driver", driver)
		}
		current, err := r.Operation(t.Context(), op.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(current.Arguments, &intent); err != nil || intent.Driver != driver {
			t.Fatal("accepted intent changed", intent, err)
		}
	}
	if fake.commands.Load() == 0 {
		t.Fatal("no captured batches dispatched")
	}
}
