package config

import "testing"

func TestGoalHostEligibilityRoundTrip(t *testing.T) {
	directory := t.TempDir()
	host := Default()
	for _, enabled := range []bool{true, false} {
		host.Defaults.GoalsEnabled = enabled
		if err := Save(directory, host); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(directory)
		if err != nil || loaded.Defaults.GoalsEnabled != enabled || loaded.Version != Version {
			t.Fatal(loaded, err)
		}
	}
}
