package session

import (
	"path/filepath"
	"testing"
)

func TestAgentReportModeSurvivesStoreReopenAndMetadataViews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(t.Context(), rootID); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"default": "notice", "notice": "notice", "inline": "inline", "message": "message"}
	cwd := t.TempDir()
	for childID, expected := range want {
		report := expected
		if childID == "default" {
			report = ""
		}
		if _, err := store.AdmitAgent(t.Context(), AgentAdmission{
			RootID: rootID, ParentAgentID: rootID, ChildAgentID: childID,
			Name: childID, Model: "child-model", Provider: "child-provider", Effort: "high", CWD: cwd, Report: report,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.AdmitAgent(t.Context(), AgentAdmission{
		RootID: rootID, ParentAgentID: "inline", ChildAgentID: "grandchild", Report: "message",
	}); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"admitted", "reopened"} {
		if stage == "reopened" {
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
		}
		check := func(records []RuntimeAgent) {
			t.Helper()
			seen := make(map[string]bool)
			for _, record := range records {
				expected, ok := want[record.ID]
				if !ok {
					continue
				}
				seen[record.ID] = true
				if record.Report != expected || record.Name != record.ID || record.Model != "child-model" ||
					record.Provider != "child-provider" || record.Effort != "high" || record.CWD != cwd || record.Status != "idle" {
					t.Fatalf("%s metadata changed: %+v", stage, record)
				}
			}
			if len(seen) != len(want) {
				t.Fatalf("%s metadata omitted agents: %v", stage, seen)
			}
		}
		var loaded []RuntimeAgent
		for id := range want {
			record, err := store.LoadAgent(t.Context(), rootID, id)
			if err != nil {
				t.Fatal(err)
			}
			loaded = append(loaded, record)
		}
		check(loaded)
		relatives, err := store.ListAgentRelatives(t.Context(), rootID, rootID)
		if err != nil {
			t.Fatal(err)
		}
		check(relatives.Children)
		retained, err := store.LoadRetainedAgents(t.Context(), rootID)
		if err != nil {
			t.Fatal(err)
		}
		check(retained)
		snapshot, err := store.SnapshotRoot(t.Context(), rootID)
		if err != nil {
			t.Fatal(err)
		}
		check(snapshot.Agents)
		relatives, err = store.ListAgentRelatives(t.Context(), rootID, "grandchild")
		if err != nil || relatives.Parent == nil || relatives.Parent.Report != "inline" {
			t.Fatalf("%s parent metadata lost report: %+v, %v", stage, relatives, err)
		}
		relatives, err = store.ListAgentRelatives(t.Context(), rootID, "default")
		if err != nil {
			t.Fatal(err)
		}
		check(append(relatives.Siblings, reportRecord(loaded, "default")))
	}
}

func reportRecord(records []RuntimeAgent, id string) RuntimeAgent {
	for _, record := range records {
		if record.ID == id {
			return record
		}
	}
	return RuntimeAgent{}
}

func TestAgentReportModeRejectsInvalidAdmission(t *testing.T) {
	store, rootID, _ := newSwarmFixture(t)
	before := lifecycleRows(t, store, rootID)
	for _, report := range []string{"none", "Inline", " notice "} {
		if _, err := store.AdmitAgent(t.Context(), AgentAdmission{
			RootID: rootID, ParentAgentID: rootID, ChildAgentID: "invalid", Report: report,
		}); err == nil {
			t.Fatalf("accepted invalid report %q", report)
		}
	}
	after := lifecycleRows(t, store, rootID)
	for table, count := range before {
		if after[table] != count {
			t.Fatalf("invalid report admission changed %s rows: %d -> %d", table, count, after[table])
		}
	}
}
