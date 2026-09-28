package config

import (
	"errors"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestInferenceSetupPreservesExplicitDefaultsAndModelDeclarations(t *testing.T) {
	host := Default()
	host.Providers["other"] = Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1"}
	host.Defaults.Model = session.ModelSelection{Provider: "other", Name: "chosen", Effort: "high"}
	authority, before := configAuthority(t, host)
	after, err := authority.Update(t.Context(), before.Revision, (*Host).EnsureInference)
	if err != nil || after.Revision == before.Revision {
		t.Fatal("managed route was not published", err)
	}
	if !reflect.DeepEqual(after.Host.Defaults, before.Host.Defaults) || after.Host.Providers["inference-net"].CredentialSource != "inference-net" {
		t.Fatal("account setup changed defaults or failed to bind machine key")
	}
	after, err = authority.Update(t.Context(), after.Revision, func(h *Host) error {
		route := h.Providers["inference-net"]
		route.Models = map[string]Model{"explicit": {Prices: session.ModelPrices{Input: new(int64(0))}, TimeoutMillis: 1234}}
		h.Providers["inference-net"] = route
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := authority.Update(t.Context(), after.Revision, (*Host).EnsureInference)
	if err != nil || !reflect.DeepEqual(after, retry) {
		t.Fatal("setup changed existing models or rewrote an unchanged route", err)
	}
	_, err = authority.Update(t.Context(), before.Revision, (*Host).EnsureInference)
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale revision accepted", err)
	}
}

func TestInferenceSetupCannotOverwriteAnotherCredentialOrEndpoint(t *testing.T) {
	for _, route := range []Provider{
		{Kind: "openai-chat", BaseURL: "https://api.inference.net/v1", CredentialEnv: "EXPLICIT_KEY"},
		{Kind: "openai-chat", BaseURL: "https://custom.example/v1"},
		{Kind: "openai-codex"},
	} {
		host := Default()
		host.Providers["inference-net"] = route
		authority, before := configAuthority(t, host)
		if _, err := authority.Update(t.Context(), before.Revision, (*Host).EnsureInference); !errors.Is(err, ErrRevisionConflict) {
			t.Fatal("setup replaced another route", err)
		}
		after, err := authority.Snapshot(t.Context())
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Fatal("conflicting setup changed configuration", err)
		}
	}
}
