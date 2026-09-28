package store

import (
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestCompactionPolicyCopiesChildrenAndCapturesTurnsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, root := create(t, s, nil)
	if root.Config.Compaction.Model != nil || root.Config.Compaction.ThresholdPercent != 50 {
		t.Fatalf("fresh session did not capture explicit defaults: %+v", root.Config.Compaction)
	}
	helper := session.ModelSelection{Provider: "helper", Name: "original", Effort: "low"}
	var err error
	root, err = s.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Compaction: &session.CompactionPolicy{Model: &helper, ThresholdPercent: 60}})
	if err != nil {
		t.Fatal(err)
	}
	helper.Name = "mutated caller"
	child := *controlChild(t, s, root.ID, "child").Session
	if child.Config.Compaction.Model == nil || child.Config.Compaction.Model.Name != "original" || child.Config.Compaction.ThresholdPercent != 60 {
		t.Fatalf("child did not copy parent effective policy: %+v", child.Config.Compaction)
	}
	submit(t, s, root.ID, "parent-turn")
	active := []Claim{claim(t, s, root.ID), claim(t, s, child.ID)}
	conversation := session.ModelSelection{Provider: "conversation", Name: "new-chat"}
	root, err = s.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Model: &conversation, Compaction: &session.CompactionPolicy{}})
	if err != nil {
		t.Fatal(err)
	}
	unchangedChild, err := s.Session(t.Context(), child.ID)
	if err != nil || unchangedChild.Config.Compaction.Model == nil || unchangedChild.Config.Compaction.Model.Name != "original" || unchangedChild.Config.Compaction.ThresholdPercent != 60 {
		t.Fatalf("parent reset changed existing child: %+v err=%v", unchangedChild.Config.Compaction, err)
	}
	newHelper := session.ModelSelection{Provider: "helper", Name: "next"}
	child, err = s.UpdateConfiguration(t.Context(), child.ID, child.ConfigRevision, session.ConfigPatch{Compaction: &session.CompactionPolicy{Model: &newHelper, ThresholdPercent: 80}})
	if err != nil {
		t.Fatal(err)
	}
	for _, claimed := range active {
		captured, err := s.Configuration(t.Context(), claimed.Turn.SessionID, claimed.Turn.ConfigRevision)
		if err != nil || captured.Compaction.Model == nil || captured.Compaction.Model.Name != "original" || captured.Compaction.ThresholdPercent != 60 || claimed.Configuration.Compaction.Model == nil || claimed.Configuration.Compaction.Model.Name != "original" {
			t.Fatalf("active turn capture changed: %+v err=%v", captured.Compaction, err)
		}
		if _, err := s.Finish(t.Context(), claimed.Turn.ID, session.Succeeded, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	for _, target := range []session.Session{root, child} {
		submit(t, s, target.ID, "next-"+string(target.ID))
		next := claim(t, s, target.ID)
		policy := next.Configuration.Compaction
		if target.ID == root.ID {
			if policy.Model != nil || policy.ThresholdPercent != 50 || next.Configuration.Model != conversation {
				t.Fatalf("reset/fallback policy did not reach next turn: %+v", next.Configuration)
			}
		} else if policy.Model == nil || *policy.Model != newHelper || policy.ThresholdPercent != 80 {
			t.Fatalf("child policy did not reach next turn: %+v", policy)
		}
	}
	for _, claimed := range active {
		captured, err := s.Configuration(t.Context(), claimed.Turn.SessionID, claimed.Turn.ConfigRevision)
		if err != nil || captured.Compaction.Model == nil || captured.Compaction.Model.Name != "original" || captured.Compaction.ThresholdPercent != 60 {
			t.Fatalf("restart re-resolved old turn: %+v err=%v", captured.Compaction, err)
		}
	}
}
