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

func TestExecutionDefaultsRootGoalCaptureRetryAndRestart(t *testing.T) {
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	setHostForTest(t, r, func(h *config.Host) {
		h.Engine = session.QuickJS
		h.Providers["fixture"] = config.Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1"}
		h.Defaults.Model = session.ModelSelection{Provider: "fixture", Name: "chat", Effort: "high"}
		h.Defaults.Compaction.ThresholdPercent = 72
		h.GoalMaxContinuations = new(int64(7))
	})
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	request := session.TreeCreationRequest{ID: "capture", Definition: refs[0], WorkingDirectory: t.TempDir()}
	first, err := r.CreateRoot(t.Context(), request)
	if err != nil || first.Tree.Engine != session.QuickJS || first.Root.Config.Model.Effort != "high" || first.Root.Config.Compaction.ThresholdPercent != 72 {
		t.Fatal("new root did not capture current host defaults", first, err)
	}
	goalRequest := session.GoalRequest{Text: "Objective"}
	goal, err := r.CreateGoal(t.Context(), first.Root.ID, "goal", nil, goalRequest, false)
	if err != nil || goal.Goal.Spec.MaxContinuations != 7 {
		t.Fatal("goal did not capture host allowance", goal, err)
	}
	setHostForTest(t, r, func(h *config.Host) {
		h.Engine, h.Defaults.Model.Effort, h.Defaults.Compaction.ThresholdPercent = session.Starlark, "low", 0
		h.GoalMaxContinuations = new(int64(0))
	})
	child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child"}, store.ChildRequest{ParentID: first.Root.ID, Parts: []session.Part{{Type: "text", Text: "child"}}})
	if err != nil || child.Session.Config.Model.Effort != "high" || child.Session.Config.Compaction.ThresholdPercent != 72 || child.Session.TreeID != first.Tree.ID {
		t.Fatal("child used live host settings instead of its parent", child, err)
	}
	request.ID = "next"
	next, err := r.CreateRoot(t.Context(), request)
	if err != nil || next.Tree.Engine != session.Starlark || next.Root.Config.Model.Effort != "low" || next.Root.Config.Compaction.ThresholdPercent != 50 {
		t.Fatal("new root ignored current defaults", next, err)
	}
	zero, err := r.CreateGoal(t.Context(), next.Root.ID, "zero", nil, goalRequest, false)
	if err != nil || zero.Goal.Spec.MaxContinuations != 0 {
		t.Fatal("host zero goal allowance became built-in default", zero, err)
	}
	explicit, err := r.CreateGoal(t.Context(), next.Root.ID, "explicit", &zero.Goal.GoalRef, session.GoalRequest{Text: "Explicit", MaxContinuations: new(int64(2))}, false)
	if err != nil || explicit.Goal.Spec.MaxContinuations != 2 {
		t.Fatal("explicit allowance ignored", explicit, err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, directory, model.Scripted{})
	if err := os.WriteFile(filepath.Join(directory, config.FileName), []byte("invalid host"), 0o600); err != nil {
		t.Fatal(err)
	}
	request.ID = "capture"
	retried, err := r.CreateRoot(t.Context(), request)
	if err != nil || retried.Tree.Engine != session.QuickJS || retried.Root.Config.Model.Effort != "high" || retried.Root.Config.Compaction.ThresholdPercent != 72 {
		t.Fatal("receipt depended on mutable defaults", retried, err)
	}
	replayedGoal, err := r.CreateGoal(t.Context(), first.Root.ID, "goal", nil, goalRequest, false)
	if err != nil || replayedGoal.Goal.Spec.MaxContinuations != 7 {
		t.Fatal("goal receipt depended on mutable defaults", replayedGoal, err)
	}
	goalRequest.MaxContinuations = new(int64(7))
	if _, err := r.CreateGoal(t.Context(), first.Root.ID, "goal", nil, goalRequest, false); !errors.Is(err, store.ErrConflict) {
		t.Fatal("different intent matched resolved allowance", err)
	}
	if err := r.DeleteSubtree(t.Context(), first.Root.ID); err != nil {
		t.Fatal(err)
	}
	goalRequest.MaxContinuations = nil
	deleted, err := r.CreateGoal(t.Context(), first.Root.ID, "goal", nil, goalRequest, false)
	if err != nil || deleted.DeletedAt == nil || deleted.Goal != nil {
		t.Fatal("deleted goal receipt rechecked host or owner", deleted, err)
	}
}

func TestExecutionDefaultsFormulationCaptureSurvivesRestartAndInvalidHost(t *testing.T) {
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	owner := createTest(t, r)
	submitTest(t, r, owner.ID, "seed")
	seed, err := r.store.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Finish(t.Context(), seed.Turn.ID, session.Succeeded, nil, []session.MessageDraft{{ID: "seed_response", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "seed"}}}}); err != nil {
		t.Fatal(err)
	}
	setHostForTest(t, r, func(h *config.Host) { h.GoalMaxContinuations = new(int64(9)) })
	identity := session.RequestIdentity{ClientID: "test", RequestID: "formulate"}
	request := session.GoalFormulationRequest{GoalID: "formulated"}
	first, err := r.FormulateGoal(t.Context(), identity, owner.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	setHostForTest(t, r, func(h *config.Host) { h.GoalMaxContinuations = new(int64(1)) })
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, directory, model.Scripted{})
	claimed, err := r.store.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := r.store.GoalFormulationInput(t.Context(), claimed.Turn.ID)
	if err != nil || captured.Request.MaxContinuations == nil || *captured.Request.MaxContinuations != 9 {
		t.Fatal("queued formulation read later defaults", captured, err)
	}
	if err := os.WriteFile(filepath.Join(directory, config.FileName), []byte("invalid host"), 0o600); err != nil {
		t.Fatal(err)
	}
	retried, err := r.FormulateGoal(t.Context(), identity, owner.ID, request)
	if err != nil || retried.Input.ID != first.Input.ID {
		t.Fatal("formulation retry rechecked host", retried, err)
	}
	request.MaxContinuations = new(int64(9))
	if _, err := r.FormulateGoal(t.Context(), identity, owner.ID, request); !errors.Is(err, store.ErrConflict) {
		t.Fatal("formulation retry lost original intent", err)
	}
}
