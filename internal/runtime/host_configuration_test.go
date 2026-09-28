package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func setHostForTest(t *testing.T, r *Runtime, edit func(*config.Host)) {
	t.Helper()
	authority := r.HostConfiguration()
	current, err := authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Update(t.Context(), current.Revision, func(host *config.Host) error { edit(host); return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestNewRootsCaptureCurrentHostDefaultsWithoutChangingExistingSessions(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	setHostForTest(t, r, func(host *config.Host) {
		host.Providers["explicit"] = config.Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", CredentialSource: "none"}
		host.Defaults.Model = session.ModelSelection{Provider: "explicit", Name: "first", Temperature: new(0.0)}
	})
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	request := store.CreateTree{Engine: session.Starlark, Definition: refs[0], WorkingDirectory: t.TempDir()}
	_, first, err := r.CreateTree(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	setHostForTest(t, r, func(host *config.Host) {
		host.Defaults.Model.Name = "second"
		host.Resources = []session.ResourceLimit{{Kind: session.ResourceDescendants, Limit: new(int64(4))}}
	})
	_, second, err := r.CreateTree(t.Context(), request)
	if err != nil || second.Config.Model.Name != "second" || second.Config.Model.Temperature == nil || *second.Config.Model.Temperature != 0 {
		t.Fatal("new root did not capture current exact defaults", err)
	}
	saved, err := r.Session(t.Context(), first.ID)
	if err != nil || saved.Config.Model.Name != "first" {
		t.Fatal("existing resolved session changed", err)
	}
	resources, err := r.Resources(t.Context(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources {
		if resource.Kind == session.ResourceDescendants && (resource.Limit == nil || *resource.Limit != 4) {
			t.Fatal("new root missed host resource edit")
		}
	}
}

func TestForkRetryPrecedesInvalidCurrentHostAndRetainsDeletedReceipt(t *testing.T) {
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	source := createTest(t, r)
	setHostForTest(t, r, func(host *config.Host) {
		host.Resources = []session.ResourceLimit{{Kind: session.ResourceDescendants, Limit: new(int64(7))}}
	})
	request := session.ForkRequest{ID: "fork", SessionID: source.ID, ExpectedHistoryRevision: source.HistoryRevision, ExpectedConfigRevision: source.ConfigRevision}
	accepted, err := r.Fork(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := r.Resources(t.Context(), accepted.Root.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources {
		if resource.Kind == session.ResourceDescendants && (resource.Limit == nil || *resource.Limit != 7) {
			t.Fatal("new fork missed current host resources")
		}
	}
	if err := os.WriteFile(filepath.Join(directory, config.FileName), []byte("invalid current host"), 0o600); err != nil {
		t.Fatal(err)
	}
	retry, err := r.Fork(t.Context(), request)
	if err != nil || retry.Fork != accepted.Fork {
		t.Fatal("accepted fork depended on current config", err)
	}
	changed := request
	changed.Title = new("different")
	if _, err := r.Fork(t.Context(), changed); !errors.Is(err, store.ErrConflict) {
		t.Fatal("changed retry did not conflict before config lookup", err)
	}
	changed.ID = "new-fork"
	if _, err := r.Fork(t.Context(), changed); err == nil {
		t.Fatal("new fork ignored invalid current host")
	}
	if err := r.DeleteSubtree(t.Context(), accepted.Root.ID); err != nil {
		t.Fatal(err)
	}
	retry, err = r.Fork(t.Context(), request)
	if err != nil || !retry.Deleted || retry.Root != nil || retry.Tree != nil || retry.Fork != accepted.Fork {
		t.Fatal("deleted receipt resurrected destination or needed current config", err)
	}
}
