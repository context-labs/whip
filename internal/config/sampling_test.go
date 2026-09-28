package config

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestSamplingDefaultsCaptureAndHostRoundTrip(t *testing.T) {
	directory := t.TempDir()
	host := Default()
	host.Providers["chat"] = Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1"}
	host.Defaults.Model = session.ModelSelection{Provider: "chat", Name: "model", Temperature: new(0.0), TopP: new(0.75)}
	host.Defaults.Compaction.Model = &session.ModelSelection{Provider: "chat", Name: "summary", Temperature: new(0.5)}
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(directory)
	if err != nil || !loaded.Defaults.Model.Equal(host.Defaults.Model) || !loaded.Defaults.Compaction.Model.Equal(*host.Defaults.Compaction.Model) {
		t.Fatalf("host sampling did not round trip: %+v %v", loaded.Defaults, err)
	}
	captured, err := session.Resolve(loaded.Defaults, session.DefinitionDocument{}, session.ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	*loaded.Defaults.Model.Temperature = 1
	loaded.Defaults.Compaction.Model.Temperature = nil
	if !captured.Model.Equal(host.Defaults.Model) || !captured.Compaction.Model.Equal(*host.Defaults.Compaction.Model) {
		t.Fatal("captured model preferences followed mutable host defaults")
	}
	host.Defaults.Model.Temperature, host.Defaults.Model.TopP = nil, nil
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	loaded, err = Load(directory)
	if err != nil || loaded.Defaults.Model.Temperature != nil || loaded.Defaults.Model.TopP != nil {
		t.Fatalf("unset sampling acquired numeric defaults: %+v %v", loaded.Defaults.Model, err)
	}
}

func TestHostSamplingRejectsInvalidCapturedValues(t *testing.T) {
	host := Default()
	host.Providers["chat"] = Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1"}
	host.Defaults.Model = session.ModelSelection{Provider: "chat", Name: "model", TopP: new(1.1)}
	if !errors.Is(host.Validate(), session.ErrInvalid) {
		t.Fatal("invalid main sampling accepted")
	}
	host.Defaults.Model.TopP = nil
	host.Defaults.Compaction.Model = &session.ModelSelection{Provider: "chat", Name: "summary", Temperature: new(2.1)}
	if !errors.Is(host.Validate(), session.ErrInvalid) {
		t.Fatal("invalid compaction sampling accepted")
	}
	host.Defaults.Compaction.Model = nil
	host.Version = Version - 1
	if !errors.Is(host.Validate(), session.ErrInvalid) {
		t.Fatal("previous host version accepted")
	}
}
