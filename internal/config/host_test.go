package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	credential, err := read.Providers["test"].Credential(t.Context(), func(name string) (string, bool) {
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
	if _, err := read.Providers["test"].Credential(t.Context(), func(string) (string, bool) { return "", false }); err == nil {
		t.Fatal("unset credential accepted")
	}
	again, err := Initialize(directory)
	if err != nil || again.Defaults.Model.Name != "scripted" {
		t.Fatal("initialization replaced host", err)
	}
}

func TestProviderDispatchLimitsAndUnknownPrices(t *testing.T) {
	settings, err := (Model{}).Resolve()
	if err != nil || settings.MaxOutputTokens != 4096 || settings.TimeoutMillis != 120000 || settings.MaxAttempts != 3 || settings.Prices.Input != nil || settings.ContextWindowTokens != nil {
		t.Fatalf("resolved defaults: %+v %v", settings, err)
	}
	for _, settings := range []Model{
		{MaxOutputTokens: -1},
		{MaxOutputTokens: 1000001},
		{TimeoutMillis: -1},
		{TimeoutMillis: 600001},
		{MaxAttempts: -1},
		{MaxAttempts: 6},
		{ContextWindowTokens: new(int64(0))},
		{ContextWindowTokens: new(int64(-1))},
		{ContextWindowTokens: new(int64(1000000001))},
		{ContextWindowTokens: new(int64(4095))},
		{Prices: session.ModelPrices{Input: new(int64(-1))}},
	} {
		if _, err := settings.Resolve(); err == nil {
			t.Fatalf("accepted invalid dispatch configuration: %+v", settings)
		}
	}
}

func TestResolvedContextWindowIsIndependentHostEvidence(t *testing.T) {
	window := int64(1000000000)
	settings := Model{ContextWindowTokens: &window}
	resolved, err := settings.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	window = 8192
	if resolved.ContextWindowTokens == nil || *resolved.ContextWindowTokens != 1000000000 {
		t.Fatal("resolved context window aliases mutable host settings")
	}
	host := Default()
	host.Providers["fixture"] = Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", Models: map[string]Model{"model": settings}}
	directory := t.TempDir()
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Providers["fixture"].Models["model"].ContextWindowTokens
	if got == nil || *got != 8192 {
		t.Fatalf("context window did not survive host persistence: %v", got)
	}
}

func TestStrictConfigurationBoundary(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatal("empty directory implicitly loaded cwd")
	}
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["api_key"] = "secret" },
		func(v map[string]any) { v["version"] = 999 },
		func(v map[string]any) { v["version"] = 1 },
		func(v map[string]any) { v["policy"] = map[string]any{"max_depth": 8} },
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

func TestResourceDefaultsResolveFromFreshHost(t *testing.T) {
	host := Default()
	host.Resources = []session.ResourceLimit{{Kind: session.ResourceDescendants, Limit: new(int64(7))}}
	directory := t.TempDir()
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != Version || len(loaded.Resources) != len(session.ResourceKinds()) {
		t.Fatalf("unresolved resource defaults: %+v", loaded)
	}
	*host.Resources[0].Limit = 99
	for _, limit := range loaded.Resources {
		if limit.Limit == nil || (limit.Kind == session.ResourceDescendants && *limit.Limit != 7) {
			t.Fatalf("missing or mutable default: %+v", limit)
		}
	}
	for _, limits := range [][]session.ResourceLimit{
		{{Kind: session.ResourceDepth, Limit: new(int64(1))}, {Kind: session.ResourceDepth, Limit: new(int64(2))}},
		{{Kind: session.ResourceDepth, Limit: nil}},
		{{Kind: session.ResourceDepth, Limit: new(int64(129))}},
	} {
		host.Resources = limits
		if err := Save(directory, host); err == nil {
			t.Fatalf("accepted invalid root defaults: %+v", limits)
		}
	}
	host = Default()
	host.Version = 1
	if err := Save(directory, host); err == nil {
		t.Fatal("accepted old host version")
	}
}

func TestHostProjectRootsExplicitPublicationAndBounds(t *testing.T) {
	directory := t.TempDir()
	host := Default()
	host.ProjectRoots["missing"] = filepath.Join(directory, "not-created")
	host.ProjectRoots["file"] = filepath.Join(directory, "regular-file")
	if err := os.WriteFile(host.ProjectRoots["file"], []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	host.Defaults.Instructions.ProjectRoot = new("missing")
	if err := Save(directory, host); err != nil {
		t.Fatal("registry validation must not inspect the filesystem", err)
	}
	loaded, err := Load(directory)
	if err != nil || loaded.Version != Version || !reflect.DeepEqual(loaded.ProjectRoots, host.ProjectRoots) || loaded.Defaults.Instructions.ProjectRoot == nil || *loaded.Defaults.Instructions.ProjectRoot != "missing" {
		t.Fatalf("project publication round trip=%+v %v", loaded, err)
	}
	loaded.ProjectRoots["missing"] = "/changed"
	*loaded.Defaults.Instructions.ProjectRoot = "changed"
	again, err := Load(directory)
	if err != nil || !reflect.DeepEqual(again.ProjectRoots, host.ProjectRoots) || *again.Defaults.Instructions.ProjectRoot != "missing" {
		t.Fatal("loaded project policy aliases caller", err)
	}
	for _, path := range []string{"", "relative", "/a/../project", "/a//project", "/a/", "/bad\x00path", "/bad\xffpath", "/" + strings.Repeat("a", 4096)} {
		host.ProjectRoots = map[string]string{"team": path}
		if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid project directory %q accepted: %v", path, err)
		}
	}
	for _, id := range []string{"", "../root", "bad root", strings.Repeat("a", 129)} {
		host.ProjectRoots = map[string]string{id: "/project"}
		if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid project ID %q accepted: %v", id, err)
		}
	}
	host.ProjectRoots = make(map[string]string, session.MaxProjectRoots)
	for i := range session.MaxProjectRoots {
		host.ProjectRoots[fmt.Sprintf("root_%d", i)] = "/個人/project"
	}
	if err := host.Validate(); err != nil {
		t.Fatal("exact project registry bound rejected", err)
	}
	host.ProjectRoots["extra"] = "/project"
	if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("oversized project registry accepted: %v", err)
	}
	host = Default()
	host.Version = Version - 1
	if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("previous host format accepted: %v", err)
	}
}
