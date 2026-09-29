package rpc_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestSkillRootPublicationRPCMetadataNeverGrants(t *testing.T) {
	_, c := fixture(t)
	before := call[protocol.HostSkillRoots](t, c, "host.skills.roots", protocol.EmptyParams{})
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "review", "SKILL.md"), []byte("---\nname: review\ndescription: Review carefully\n---\nBODY_SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	published := call[protocol.HostSkillRoots](t, c, "host.skills.publish", protocol.PublishSkillRootParams{ExpectedRevision: before.Revision, ID: "personal", Path: root})
	if len(published.Defaults) != 0 || len(published.Roots) != 1 {
		t.Fatal(published)
	}
	empty := call[protocol.HostSkillsResult](t, c, "host.skills.complete", protocol.HostSkillsParams{Scope: "global", Limit: 10})
	if len(empty.Candidates) != 0 {
		t.Fatal("publication selected roots", empty)
	}
	requireHistoryError(t, c, "host.skills.set_defaults", protocol.SetDefaultSkillRootsParams{ExpectedRevision: before.Revision, Roots: []protocol.ID{"personal"}}, "CONFLICT")
	selected := call[protocol.HostSkillRoots](t, c, "host.skills.set_defaults", protocol.SetDefaultSkillRootsParams{ExpectedRevision: published.Revision, Roots: []protocol.ID{"personal"}})
	if len(selected.Defaults) != 1 {
		t.Fatal(selected)
	}
	preview := call[protocol.HostSkillsResult](t, c, "host.skills.complete", protocol.HostSkillsParams{Scope: "global", Limit: 10})
	if len(preview.Candidates) != 1 || preview.Candidates[0].Text != "$review" || preview.Candidates[0].Description != "Review carefully" {
		t.Fatal(preview)
	}
	requireHistoryError(t, c, "host.skills.publish", protocol.PublishSkillRootParams{ExpectedRevision: selected.Revision, ID: "personal", Path: t.TempDir()}, "CONFLICT")
}
