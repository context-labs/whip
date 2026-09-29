package hostcmd

import (
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestExecutionDefaultAttemptsCapturedAtPreparationAndModelOverride(t *testing.T) {
	directory := t.TempDir()
	host := config.Default()
	host.Providers["fixture"] = config.Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", Models: map[string]config.Model{"explicit": {MaxAttempts: 1}}}
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
	provider := configuredProvider(directory, nil, nil)
	request := model.Request{Selection: session.ModelSelection{Provider: "fixture", Name: "inherited"}, Messages: []model.Message{{Role: session.User, Parts: []session.Part{{Type: "text", Text: "hello"}}}}}
	first, err := provider.Prepare(t.Context(), request)
	if err != nil || first.MaxAttempts != 3 {
		t.Fatal("missing global attempts did not select bounded default", first.MaxAttempts, err)
	}
	host.MaxAttempts = 5
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
	next, err := provider.Prepare(t.Context(), request)
	if err != nil || next.MaxAttempts != 5 || first.MaxAttempts != 3 {
		t.Fatal("global change ignored or rewrote existing prepared call", next.MaxAttempts, err)
	}
	request.Selection.Name = "explicit"
	explicit, err := provider.Prepare(t.Context(), request)
	if err != nil || explicit.MaxAttempts != 1 {
		t.Fatal("host default overrode explicit model policy", explicit.MaxAttempts, err)
	}
}
