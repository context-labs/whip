package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestExplicitFreshHostAndAtomicInitialization(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte("invalid legacy configuration"), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		wg.Go(func() { _, err := Initialize(directory); failures <- err })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Error(err)
		}
	}
	host, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := json.Marshal(host)
	expected, _ := json.Marshal(Default())
	if string(actual) != string(expected) {
		t.Fatalf("unexpected defaults: %+v", host)
	}
	info, err := os.Stat(filepath.Join(directory, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions: %v", info.Mode())
	}
}

func TestCredentialsRemainHostReferences(t *testing.T) {
	directory := t.TempDir()
	host := Default()
	host.Providers["test"] = Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", CredentialEnv: "WHIP_TEST_KEY"}
	host.Defaults.Model = session.ModelSelection{Provider: "test", Name: "scripted"}
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	read, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := read.Providers["test"].Credential(func(name string) (string, bool) {
		if name != "WHIP_TEST_KEY" {
			t.Fatal(name)
		}
		return "secret-for-test", true
	})
	if err != nil || credential != "secret-for-test" {
		t.Fatal("credential lookup", err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-for-test") {
		t.Fatal("resolved credential persisted")
	}
	if _, err := read.Providers["test"].Credential(func(string) (string, bool) { return "", false }); err == nil {
		t.Fatal("unset credential accepted")
	}
	again, err := Initialize(directory)
	if err != nil || again.Defaults.Model.Name != "scripted" {
		t.Fatal("initialization replaced host", err)
	}
}

func TestProviderDispatchLimitsAndUnknownPrices(t *testing.T) {
	settings, err := (Model{}).Resolve()
	if err != nil || settings.MaxOutputTokens != 4096 || settings.TimeoutMillis != 120000 || settings.MaxAttempts != 3 || settings.Prices.Input != nil {
		t.Fatalf("resolved defaults: %+v %v", settings, err)
	}
	for _, settings := range []Model{
		{MaxOutputTokens: -1},
		{MaxOutputTokens: 1000001},
		{TimeoutMillis: -1},
		{TimeoutMillis: 600001},
		{MaxAttempts: -1},
		{MaxAttempts: 6},
		{Prices: session.ModelPrices{Input: new(int64(-1))}},
	} {
		if _, err := settings.Resolve(); err == nil {
			t.Fatalf("accepted invalid dispatch configuration: %+v", settings)
		}
	}
}

func TestStrictConfigurationBoundary(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatal("empty directory implicitly loaded cwd")
	}
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["api_key"] = "secret" },
		func(v map[string]any) { v["version"] = 999 },
		func(v map[string]any) {
			v["providers"] = map[string]any{"test": map[string]any{"kind": "openai-chat", "base_url": "https://user:password@example.test", "credential_env": ""}}
		},
		func(v map[string]any) {
			v["providers"] = map[string]any{"test": map[string]any{"kind": "openai-chat", "base_url": "https://example.test?key=secret", "credential_env": ""}}
		},
		func(v map[string]any) {
			v["providers"] = map[string]any{"test": map[string]any{"kind": "openai-chat", "base_url": "https://example.test", "credential_env": "", "api_key": "secret"}}
		},
	} {
		raw, err := json.Marshal(Default())
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		mutate(v)
		raw, err = json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, FileName), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(directory); err == nil {
			t.Fatalf("accepted invalid config: %s", raw)
		}
	}
}

func TestSaveIncludesNewlineInSizeLimit(t *testing.T) {
	host := Default()
	raw, err := json.MarshalIndent(host, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	host.Defaults.Instructions.Text = strings.Repeat("x", session.MaxDocumentBytes-len(raw))
	directory := t.TempDir()
	if err := Save(directory, host); err == nil {
		t.Fatal("saved a file larger than Load accepts")
	}
	host.Defaults.Instructions.Text = host.Defaults.Instructions.Text[:len(host.Defaults.Instructions.Text)-1]
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(directory); err != nil {
		t.Fatal("accepted save cannot be read", err)
	}
}
