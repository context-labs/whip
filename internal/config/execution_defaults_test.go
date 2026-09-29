package config

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestExecutionDefaultsCASPersistenceAndIndependentFields(t *testing.T) {
	host := Default()
	host.Providers["fixture"] = Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", CredentialEnv: "PRIVATE_ENV"}
	host.Defaults.Model = session.ModelSelection{Provider: "fixture", Name: "chat", Temperature: new(0.0)}
	host.Defaults.Compaction.Model = &session.ModelSelection{Provider: "fixture", Name: "summary"}
	authority, before := configAuthority(t, host)
	values := before.Host.ExecutionDefaults()
	if values.MaxAttempts != 3 || values.GoalMaxContinuations != 100 || values.CompactionPercent != 0 || values.Engine != session.Starlark {
		t.Fatal("unexpected built-in defaults", values)
	}
	same, err := authority.SetExecutionDefaults(t.Context(), before.Revision, values)
	if err != nil || same.Revision != before.Revision {
		t.Fatal("reading and writing resolved defaults changed raw host", err)
	}
	values.Engine, values.Effort, values.CompactionPercent = session.QuickJS, "high", 73
	values.GoalMaxContinuations, values.MaxAttempts = 9007199254740993, 2
	after, err := authority.SetExecutionDefaults(t.Context(), before.Revision, values)
	if err != nil || after.Host.ExecutionDefaults() != values || after.Revision == before.Revision {
		t.Fatal("default publication failed", after, err)
	}
	expected := before.Host
	expected.Engine, expected.Defaults.Model.Effort, expected.Defaults.Compaction.ThresholdPercent = values.Engine, values.Effort, values.CompactionPercent
	expected.GoalMaxContinuations, expected.MaxAttempts = new(values.GoalMaxContinuations), values.MaxAttempts
	if !reflect.DeepEqual(after.Host, expected) {
		t.Fatal("defaults changed unrelated host declarations")
	}
	loaded, err := Load(authority.directory)
	if err != nil || !reflect.DeepEqual(loaded, expected) {
		t.Fatal("defaults did not persist", err)
	}
	if _, err := authority.SetExecutionDefaults(t.Context(), before.Revision, values); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale same-value write bypassed CAS", err)
	}
	values.GoalMaxContinuations, values.MaxAttempts, values.CompactionPercent, values.Effort = 0, 1, 0, ""
	zero, err := authority.SetExecutionDefaults(t.Context(), after.Revision, values)
	if err != nil || zero.Host.ExecutionDefaults() != values || zero.Host.GoalMaxContinuations == nil {
		t.Fatal("explicit zero or cleared effort was lost", zero, err)
	}
	for _, mutate := range []func(*ExecutionDefaults){
		func(d *ExecutionDefaults) { d.Engine = "invalid" },
		func(d *ExecutionDefaults) { d.GoalMaxContinuations = -1 },
		func(d *ExecutionDefaults) { d.MaxAttempts = 0 },
		func(d *ExecutionDefaults) { d.MaxAttempts = 6 },
		func(d *ExecutionDefaults) { d.CompactionPercent = 101 },
		func(d *ExecutionDefaults) { d.Effort = strings.Repeat("x", 65) },
	} {
		invalid := values
		mutate(&invalid)
		if _, err := authority.SetExecutionDefaults(t.Context(), zero.Revision, invalid); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("invalid default accepted", invalid, err)
		}
	}
}

func TestExecutionDefaultsFreshVersionAndUnconfiguredModel(t *testing.T) {
	host := Default()
	host.Version = 18
	if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("old host version accepted", err)
	}
	host.Version = Version
	authority, before := configAuthority(t, host)
	values := host.ExecutionDefaults()
	values.Engine, values.GoalMaxContinuations = session.QuickJS, 0
	after, err := authority.SetExecutionDefaults(t.Context(), before.Revision, values)
	if err != nil || !after.Host.Defaults.Model.Equal(session.ModelSelection{}) {
		t.Fatal("model-free defaults fabricated model selection", err)
	}
	values.Effort = "high"
	if _, err := authority.SetExecutionDefaults(t.Context(), after.Revision, values); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("effort created incomplete model selection", err)
	}
}
