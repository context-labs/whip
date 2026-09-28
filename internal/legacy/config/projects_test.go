package config

import (
	"testing"
)

func TestProjectGoalMaxRounds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()

	if n := ProjectGoalMaxRounds(dir); n != 0 {
		t.Fatalf("no override should be 0, got %d", n)
	}
	if err := SetProjectGoalMaxRounds(dir, 42); err != nil {
		t.Fatal(err)
	}
	if n := ProjectGoalMaxRounds(dir); n != 42 {
		t.Fatalf("override should be 42, got %d", n)
	}
	// a second project is independent
	other := t.TempDir()
	if n := ProjectGoalMaxRounds(other); n != 0 {
		t.Fatalf("other project should be 0, got %d", n)
	}
	// clearing removes it
	if err := SetProjectGoalMaxRounds(dir, 0); err != nil {
		t.Fatal(err)
	}
	if n := ProjectGoalMaxRounds(dir); n != 0 {
		t.Fatalf("cleared override should be 0, got %d", n)
	}
}

func TestGoalMaxRoundsConfigRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Default()
	cfg.GoalMaxRounds = 250
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GoalMaxRounds != 250 {
		t.Fatalf("goalMaxRounds should round-trip as 250, got %d", loaded.GoalMaxRounds)
	}
}
