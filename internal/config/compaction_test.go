package config

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestHostCompactionPolicyRoundTripAndResolveBoundary(t *testing.T) {
	directory := t.TempDir()
	host := Default()
	host.Providers["conversation"] = Provider{Kind: "openai-chat", BaseURL: "https://conversation.example/v1"}
	host.Providers["helper"] = Provider{Kind: "openai-chat", BaseURL: "https://helper.example/v1"}
	host.Defaults.Model = session.ModelSelection{Provider: "conversation", Name: "chat"}
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(directory)
	if err != nil || loaded.Defaults.Compaction.Model != nil || loaded.Defaults.Compaction.ThresholdPercent != 0 {
		t.Fatalf("host loading silently resolved session defaults: %+v err=%v", loaded.Defaults.Compaction, err)
	}
	resolved, err := session.Resolve(loaded.Defaults, session.DefinitionDocument{}, session.ConfigPatch{})
	if err != nil || resolved.Compaction.ThresholdPercent != 50 || loaded.Defaults.Compaction.ThresholdPercent != 0 {
		t.Fatalf("session capture default=%+v err=%v", resolved.Compaction, err)
	}
	helper := session.ModelSelection{Provider: "helper", Name: "summary", Effort: "low"}
	host.Defaults.Compaction = session.CompactionPolicy{Model: &helper, ThresholdPercent: 75}
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	helper.Name = "changed after save"
	loaded, err = Load(directory)
	if err != nil || loaded.Defaults.Compaction.Model == nil || loaded.Defaults.Compaction.Model.Name != "summary" || loaded.Defaults.Compaction.ThresholdPercent != 75 {
		t.Fatalf("helper policy round trip=%+v err=%v", loaded.Defaults.Compaction, err)
	}
	// A host may configure the helper before choosing the conversation route.
	host.Defaults.Model = session.ModelSelection{}
	if err := host.Validate(); err != nil {
		t.Fatal("unconfigured conversation rejected a valid helper policy", err)
	}
}

func TestHostCompactionPolicyRejectsMissingRouteAndInvalidValues(t *testing.T) {
	for _, policy := range []session.CompactionPolicy{
		{Model: &session.ModelSelection{Provider: "missing", Name: "summary"}},
		{ThresholdPercent: -1},
		{ThresholdPercent: 101},
		{Model: &session.ModelSelection{Provider: "helper"}},
	} {
		host := Default()
		host.Providers["helper"] = Provider{Kind: "openai-chat", BaseURL: "https://helper.example/v1"}
		host.Defaults.Compaction = policy
		if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid host policy accepted: %+v err=%v", policy, err)
		}
	}
}
