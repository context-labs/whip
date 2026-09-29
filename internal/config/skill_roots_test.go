package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestSkillPublicationCASImmutableNamesAndSelection(t *testing.T) {
	directory := t.TempDir()
	host := Default()
	host.Defaults.Instructions.Text = "retained"
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	authority, err := NewAuthority(directory)
	if err != nil {
		t.Fatal(err)
	}
	before, err := authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "published-link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	first, err := authority.PublishSkillRoot(t.Context(), before.Revision, "personal", link)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if first.Host.SkillRoots["personal"] != canonical || len(first.Host.Defaults.Instructions.SkillRoots) != 0 {
		t.Fatal("publication selected a root", first)
	}
	if _, err := authority.SetDefaultSkillRoots(t.Context(), before.Revision, []string{"personal"}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale accepted", err)
	}
	if _, err := authority.PublishSkillRoot(t.Context(), first.Revision, "personal", t.TempDir()); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("name rebound", err)
	}
	for _, roots := range [][]string{{"missing"}, {"personal", "personal"}} {
		if _, err := authority.SetDefaultSkillRoots(t.Context(), first.Revision, roots); !errors.Is(err, session.ErrInvalid) {
			t.Fatal(roots, err)
		}
	}
	selected, err := authority.SetDefaultSkillRoots(t.Context(), first.Revision, []string{"personal"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Host.Defaults.Instructions.Text != "retained" || !reflect.DeepEqual(selected.Host.Defaults.Instructions.SkillRoots, []string{"personal"}) {
		t.Fatal(selected)
	}
	reopened, err := Load(directory)
	if err != nil || !reflect.DeepEqual(reopened.SkillRoots, selected.Host.SkillRoots) {
		t.Fatal(reopened, err)
	}
	again, err := authority.PublishSkillRoot(t.Context(), selected.Revision, "personal", root)
	if err != nil || again.Revision != selected.Revision {
		t.Fatal("same mapping not idempotent", again, err)
	}
	cleared, err := authority.SetDefaultSkillRoots(t.Context(), again.Revision, []string{})
	if err != nil || len(cleared.Host.Defaults.Instructions.SkillRoots) != 0 || len(cleared.Host.SkillRoots) != 1 {
		t.Fatal(cleared, err)
	}
	revision := cleared.Revision
	for i := 1; i < session.MaxSkillRoots; i++ {
		result, err := authority.PublishSkillRoot(t.Context(), revision, fmt.Sprintf("root-%d", i), root)
		if err != nil {
			t.Fatal(err)
		}
		revision = result.Revision
	}
	if _, err := authority.PublishSkillRoot(t.Context(), revision, "overflow", root); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("publication bound", err)
	}
	if _, err := authority.PublishSkillRoot(t.Context(), revision, "relative", "relative"); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
}
