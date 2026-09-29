package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestRootCreationRetryPrecedesMutableHostAndPreservesCapturedDefaults(t *testing.T) {
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	setHostForTest(t, r, func(host *config.Host) {
		host.Defaults.Model = session.ModelSelection{Provider: "explicit", Name: "original", Temperature: new(0.0)}
		host.Providers["explicit"] = config.Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", CredentialSource: "none"}
		host.DefaultPermissionMode = session.PermissionAutomatic
		host.Resources = []session.ResourceLimit{{Kind: session.ResourceDescendants, Limit: new(int64(7))}}
	})
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	request := session.TreeCreationRequest{ID: "Root:Creation", Definition: refs[0], Engine: session.QuickJS, WorkingDirectory: t.TempDir()}
	accepted, err := r.CreateRoot(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	setHostForTest(t, r, func(host *config.Host) {
		host.Defaults.Model.Name = "new-default"
		host.DefaultPermissionMode = session.PermissionPrompt
		host.Engine = session.Starlark
	})
	retry, err := r.CreateRoot(t.Context(), request)
	if err != nil || !reflect.DeepEqual(retry, accepted) {
		t.Fatal("changed host reinterpreted receipt", retry, err)
	}
	if err := os.WriteFile(filepath.Join(directory, config.FileName), []byte("invalid host"), 0o600); err != nil {
		t.Fatal(err)
	}
	retry, err = r.CreateRoot(t.Context(), request)
	if err != nil || !reflect.DeepEqual(retry, accepted) {
		t.Fatal("retry depends on live config", err)
	}
	changed := request
	changed.WorkingDirectory = "/changed"
	if _, err := r.CreateRoot(t.Context(), changed); !errors.Is(err, store.ErrConflict) {
		t.Fatal("changed request did not conflict before host", err)
	}
	changed.ID = "new-request"
	if _, err := r.CreateRoot(t.Context(), changed); err == nil {
		t.Fatal("fresh creation ignored invalid host")
	}
	if err := r.DeleteSubtree(t.Context(), accepted.Root.ID); err != nil {
		t.Fatal(err)
	}
	retry, err = r.CreateRoot(t.Context(), request)
	if err != nil || !retry.Deleted || retry.Root != nil || retry.Tree != nil || retry.Creation != accepted.Creation {
		t.Fatal("tombstone depended on live host", retry, err)
	}
}
