package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestHostStandingEditsUseActivePublicationWithoutCreatingGrants(t *testing.T) {
	requests := make(chan model.Request, 2)
	r := openTest(t, t.TempDir(), providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		requests <- request
		return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
	}))
	owner := createTest(t, r)
	policy := session.Instructions{StandingInstructions: true}
	if _, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "standing.md")
	writeInstructionFile(t, path, "# before\nOriginal rule.\n")
	r.host.StandingInstructionsFile = path
	before, err := r.HostStandingInstructions(t.Context())
	if err != nil || !before.Published || before.Text == nil || before.Revision == nil {
		t.Fatal(before, err)
	}
	after, err := r.WriteHostStandingInstructions(t.Context(), *before.Revision, "# edited\nNew rule.\n")
	if err != nil || *after.Revision == *before.Revision {
		t.Fatal(after, err)
	}
	// A disk config edit cannot silently change the already active publication.
	host, err := r.HostConfiguration().Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.HostConfiguration().Update(t.Context(), host.Revision, func(host *config.Host) error {
		host.StandingInstructionsFile = filepath.Join(t.TempDir(), "not-active.md")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if active, err := r.HostStandingInstructions(t.Context()); err != nil || *active.Revision != *after.Revision {
		t.Fatal(active, err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, owner.ID, "without-standing-grant")
	if result := waitTest(t, r, "without-standing-grant", terminal); result.Turn.State != session.Succeeded {
		t.Fatal(result.Turn)
	}
	if request := nextInstructionRequest(t, requests); strings.Contains(request.Instructions, "New rule.") {
		t.Fatal("human edit created agent authority")
	}
	if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "standing", SessionID: owner.ID, Capability: "instructions.read", Resource: "standing"}); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, owner.ID, "with-standing-grant")
	result := waitTest(t, r, "with-standing-grant", terminal)
	if result.Turn.State != session.Succeeded {
		t.Fatal(result.Turn)
	}
	if request := nextInstructionRequest(t, requests); !strings.Contains(request.Instructions, "New rule.") || strings.Contains(request.Instructions, "# edited") {
		t.Fatal("updated source did not use existing capture/filtering")
	}
	manifest, err := r.InstructionManifest(t.Context(), result.Turn.ID)
	if err != nil || manifest == nil || len(manifest.Sources) != 1 || manifest.Sources[0].Bytes != int64(len(*after.Text)) {
		t.Fatal(manifest, err)
	}
}

func TestHostStandingMissingPublicationAndConcurrentCAS(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	absent, err := r.HostStandingInstructions(t.Context())
	if err != nil || absent.Published || absent.Revision != nil || absent.Text != nil {
		t.Fatal(absent, err)
	}
	if _, err := r.WriteHostStandingInstructions(t.Context(), strings.Repeat("a", 64), "do not publish"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "standing.md")
	writeInstructionFile(t, path, "before")
	r.host.StandingInstructionsFile = path
	before, err := r.HostStandingInstructions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var joined sync.WaitGroup
	for _, text := range []string{"first", "second"} {
		joined.Go(func() {
			<-start
			_, err := r.WriteHostStandingInstructions(t.Context(), *before.Revision, text)
			results <- err
		})
	}
	close(start)
	joined.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, store.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal(successes, conflicts)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.HostStandingInstructions(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	after, err := r.HostStandingInstructions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.WriteHostStandingInstructions(ctx, *after.Revision, "cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != *after.Text {
		t.Fatal(string(raw), err)
	}
}
