package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestHostRoutesRefreshOnlyForNewPreparedCalls(t *testing.T) {
	directory := t.TempDir()
	host := config.Default()
	host.Providers["fixture"] = config.Provider{
		Kind: "openai-chat", BaseURL: "https://first.example/v1", CredentialEnv: "WHIP_V4_FIXTURE_TOKEN",
		Models: map[string]config.Model{"model": {MaxOutputTokens: 77, TimeoutMillis: 1234, MaxAttempts: 2}},
	}
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIP_V4_FIXTURE_TOKEN", "credential-must-not-persist")
	provider := configuredProvider(directory, nil)
	request := model.Request{
		Selection: session.ModelSelection{Provider: "fixture", Name: "model"},
		Messages:  []model.Message{{Role: session.User, Parts: []session.Part{{Type: "text", Text: "hello"}}}},
	}
	first, err := provider.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	route := host.Providers["fixture"]
	route.BaseURL = "https://second.example/v1"
	route.Kind = "openai-responses"
	host.Providers["fixture"] = route
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
	second, err := provider.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Snapshot.Route != "https://first.example/v1/chat/completions" || second.Snapshot.Route != "https://second.example/v1/responses" || second.Snapshot.Adapter != "openai-responses" || second.Snapshot.MaxOutputTokens != 77 || second.Snapshot.TimeoutMillis != 1234 || second.MaxAttempts != 2 {
		t.Fatalf("prepared snapshots: %+v %+v", first.Snapshot, second.Snapshot)
	}
	raw, err := json.Marshal(second.Snapshot)
	if err != nil || strings.Contains(string(raw), "credential-must-not-persist") {
		t.Fatal("credential entered snapshot")
	}
	t.Setenv("WHIP_V4_FIXTURE_TOKEN", "")
	if _, err := provider.Prepare(t.Context(), request); err == nil {
		t.Fatal("unset credential accepted")
	}
}
