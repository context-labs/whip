package hostcmd

import (
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestDisabledProviderStopsBeforeCredentialCommand(t *testing.T) {
	directory := t.TempDir()
	host := config.Default()
	host.Providers["disabled"] = config.Provider{Kind: "openai-chat", BaseURL: "https://fixture.test/v1", Disabled: true, CredentialSource: "command", CredentialCommand: &config.CredentialCommand{Executable: "/must-not-run"}}
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
	provider := configuredProvider(directory, nil, nil)
	_, err := provider.Prepare(t.Context(), model.Request{Selection: session.ModelSelection{Provider: "disabled", Name: "model"}, Messages: []model.Message{{Role: session.User, Parts: []session.Part{{Type: "text", Text: "hello"}}}}})
	if err == nil || !strings.Contains(err.Error(), "is disabled") {
		t.Fatalf("disabled route reached credential preparation: %v", err)
	}
}
