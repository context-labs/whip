package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func writeProviderFixture(t *testing.T, directory, name, value string) string {
	t.Helper()
	filename := filepath.Join(directory, name)
	if err := os.WriteFile(filename, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func providerPresetFixture(t *testing.T, id string) Provider {
	t.Helper()
	for _, preset := range ProviderPresets() {
		if preset.ID == id {
			return preset.Provider
		}
	}
	t.Fatalf("missing provider %s", id)
	return Provider{}
}

func TestProviderNamedSourcePrecedenceAndReferences(t *testing.T) {
	isolateProviderEnvironment(t)
	directory := t.TempDir()
	raw := writeProviderFixture(t, directory, "arbitrary.key", " raw-fixture\n")
	first := writeProviderFixture(t, directory, "first.env", "CEREBRAS_API_KEY=first-fixture\n")
	second := writeProviderFixture(t, directory, "second.env", "CEREBRAS_API_KEY=second-fixture\n")
	writeProviderFixture(t, directory, "CEREBRAS_API_KEY", "directory-fixture")
	cfg := &Config{ProviderKeySources: ProviderKeySources{KeyFiles: map[string]string{"CEREBRAS_API_KEY": raw}, EnvFiles: []string{first, second}, KeyDirectories: []string{directory}}}
	provider := providerPresetFixture(t, "cerebras")
	check := func(expected, source, path string) {
		t.Helper()
		snapshot := DiscoverCredentials(cfg)
		key, err := snapshot.ResolveKey(provider)
		status := snapshot.KeyStatus(provider)
		if err != nil || key != expected || status.Source != source || status.Path != path || status.Environment != "CEREBRAS_API_KEY" || !status.Available {
			t.Fatalf("resolution/provenance mismatch: %+v, error %v", status, err)
		}
		for _, reference := range []string{"$CEREBRAS_API_KEY", "${CEREBRAS_API_KEY}"} {
			referenced := Provider{APIKey: reference}
			resolved, err := referenced.ResolveKey(cfg)
			if err != nil || resolved != expected {
				t.Fatalf("reference did not use named source: %v", err)
			}
		}
	}
	t.Setenv("CEREBRAS_API_KEY", "env-fixture")
	check("env-fixture", "environment", "")
	t.Setenv("CEREBRAS_API_KEY", "")
	check("raw-fixture", "key_file", raw)
	cfg.ProviderKeySources.KeyFiles = nil
	check("first-fixture", "env_file", first)
	cfg.ProviderKeySources.EnvFiles = cfg.ProviderKeySources.EnvFiles[1:]
	check("second-fixture", "env_file", second)
	cfg.ProviderKeySources.EnvFiles = nil
	check("directory-fixture", "key_file", filepath.Join(directory, "CEREBRAS_API_KEY"))
}

func TestProviderSourceFailuresDoNotUseLowerPriorityKeys(t *testing.T) {
	for _, kind := range []string{"mapped missing", "mapped multiline", "mapped empty", "env missing", "env invalid", "env empty", "directory escape", "directory nonregular"} {
		t.Run(kind, func(t *testing.T) {
			isolateProviderEnvironment(t)
			directory := t.TempDir()
			lower := t.TempDir()
			writeProviderFixture(t, lower, "CEREBRAS_API_KEY", "lower-fixture")
			cfg := &Config{ProviderKeySources: ProviderKeySources{KeyDirectories: []string{lower}}}
			switch kind {
			case "mapped missing":
				cfg.ProviderKeySources.KeyFiles = map[string]string{"CEREBRAS_API_KEY": filepath.Join(directory, "missing")}
			case "mapped multiline":
				cfg.ProviderKeySources.KeyFiles = map[string]string{"CEREBRAS_API_KEY": writeProviderFixture(t, directory, "bad", "first\nsecond")}
			case "mapped empty":
				cfg.ProviderKeySources.KeyFiles = map[string]string{"CEREBRAS_API_KEY": writeProviderFixture(t, directory, "empty", " \n")}
			case "env missing":
				cfg.ProviderKeySources.EnvFiles = []string{filepath.Join(directory, "missing")}
			case "env invalid":
				cfg.ProviderKeySources.EnvFiles = []string{writeProviderFixture(t, directory, "bad.env", "CEREBRAS_API_KEY=$(cat forbidden)\n")}
			case "env empty":
				cfg.ProviderKeySources.EnvFiles = []string{writeProviderFixture(t, directory, "empty.env", "CEREBRAS_API_KEY=\n")}
			case "directory escape":
				if err := os.Symlink(filepath.Join(lower, "CEREBRAS_API_KEY"), filepath.Join(directory, "CEREBRAS_API_KEY")); err != nil {
					t.Fatal(err)
				}
				cfg.ProviderKeySources.KeyDirectories = []string{directory, lower}
			case "directory nonregular":
				// A directory in place of a raw file is another nonregular source.
				if err := os.Mkdir(filepath.Join(directory, "CEREBRAS_API_KEY"), 0o700); err != nil {
					t.Fatal(err)
				}
				cfg.ProviderKeySources.KeyDirectories = []string{directory, lower}
			}
			provider := providerPresetFixture(t, "cerebras")
			snapshot := DiscoverCredentials(cfg)
			if snapshot.KeyStatus(provider).Available {
				t.Fatal("invalid higher-priority source used lower key")
			}
			key, err := snapshot.ResolveKey(provider)
			if key != "" {
				t.Fatal("invalid higher-priority source resolved a key")
			}
			if !strings.HasSuffix(kind, "empty") && err == nil {
				t.Fatal("missing source-specific error")
			}
			if err != nil && strings.Contains(err.Error(), "forbidden") {
				t.Fatal("source value leaked in error")
			}
		})
	}
}

func TestProviderEnvFileSubsetAndNoExecution(t *testing.T) {
	valid := []struct{ name, input, value string }{
		{"simple", "KEY=value\n", "value"},
		{"comment", "# heading\n\nKEY=value # comment\n", "value"},
		{"tab comment", "KEY=value\t# comment\n", "value"},
		{"empty comment", "KEY= # comment\n", ""},
		{"export", "export KEY=value\r\n", "value"},
		{"single quote", "KEY='literal $OTHER' # comment\n", "literal $OTHER"},
		{"double quote", "KEY=\"literal $OTHER\"\n", "literal $OTHER"},
		{"equals hash", "KEY=part=more#fragment\n", "part=more#fragment"},
		{"first wins", "KEY=first\nKEY=second\n", "first"},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			values, err := parseProviderEnvFile([]byte(test.input), "fixture.env")
			if err != nil || values["KEY"] != test.value {
				t.Fatalf("supported dotenv subset rejected: %v", err)
			}
		})
	}
	invalid := []struct{ name, input string }{
		{"shell command", "source ./secret"}, {"substitution", "KEY=$(touch marker)"}, {"backtick", "KEY=`touch marker`"},
		{"no assignment", "malformed-secret"}, {"missing quote", "KEY='secret"}, {"escaped quote", "KEY=\"secret\\\"quote\""},
		{"multiline", "KEY='first\nsecond'"}, {"invalid name", "BAD-NAME=secret"}, {"control", "KEY=bad\x00secret"},
		{"trailing command", "KEY=secret;touch marker"}, {"quote tail", "KEY='secret' tail"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseProviderEnvFile([]byte(test.input), "fixture.env")
			if err == nil || !strings.Contains(err.Error(), "line 1") || strings.Contains(err.Error(), "secret") {
				t.Fatal("bad syntax lacked safe line diagnostic")
			}
		})
	}
	isolateProviderEnvironment(t)
	directory := t.TempDir()
	marker := filepath.Join(directory, "marker")
	filename := writeProviderFixture(t, directory, "source.env", "CEREBRAS_API_KEY=$(touch "+marker+")")
	snapshot := DiscoverCredentials(&Config{ProviderKeySources: ProviderKeySources{EnvFiles: []string{filename}}})
	if snapshot.Err() == nil {
		t.Fatal("command syntax accepted")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("dotenv executed a command")
	}
}

func TestProviderSourceBoundsPathsAndRotation(t *testing.T) {
	isolateProviderEnvironment(t)
	directory := t.TempDir()
	provider := providerPresetFixture(t, "cerebras")
	filename := writeProviderFixture(t, directory, "key", "first-fixture")
	cfg := &Config{ProviderKeySources: ProviderKeySources{KeyFiles: map[string]string{"CEREBRAS_API_KEY": filename}}}
	snapshot := DiscoverCredentials(cfg)
	writeProviderFixture(t, directory, "key", "second-fixture")
	if key, err := snapshot.ResolveKey(provider); err != nil || key != "first-fixture" {
		t.Fatal("snapshot changed mid-operation")
	}
	if key, err := provider.ResolveKey(cfg); err != nil || key != "second-fixture" {
		t.Fatal("new client did not read rotated key")
	}
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	if provider.KeyStatus(cfg).Available {
		t.Fatal("removed file remains available")
	}
	writeProviderFixture(t, directory, "key", strings.Repeat("x", providerKeyFileLimit+1))
	if _, err := provider.ResolveKey(cfg); err == nil {
		t.Fatal("oversized raw key accepted")
	}
	cfg.ProviderKeySources = ProviderKeySources{EnvFiles: []string{writeProviderFixture(t, directory, "oversized.env", strings.Repeat("x", providerEnvFileLimit+1))}}
	if DiscoverCredentials(cfg).Err() == nil {
		t.Fatal("oversized env file accepted")
	}
	cfg.ProviderKeySources = ProviderKeySources{KeyFiles: map[string]string{"CEREBRAS_API_KEY": "relative.key"}}
	if _, err := provider.ResolveKey(cfg); err == nil {
		t.Fatal("relative file accepted")
	}
	home := os.Getenv("HOME")
	writeProviderFixture(t, home, "fixture.key", "home-fixture")
	cfg.ProviderKeySources.KeyFiles["CEREBRAS_API_KEY"] = "~/fixture.key"
	if key, err := provider.ResolveKey(cfg); err != nil || key != "home-fixture" {
		t.Fatal("home-relative source unavailable")
	}
	cfg.ProviderKeySources.KeyDirectories = make([]string, providerSourceLimit+1)
	if DiscoverCredentials(cfg).Err() == nil {
		t.Fatal("source count was not bounded")
	}
}

func TestProviderSourceOpenAIEndpointGuard(t *testing.T) {
	for _, source := range []string{"env_file", "key_file", "directory"} {
		for _, endpoint := range []string{"https://api.openai.com/v1/", "https://proxy.example/v1"} {
			t.Run(source+endpoint, func(t *testing.T) {
				isolateProviderEnvironment(t)
				directory := t.TempDir()
				cfg := &Config{}
				switch source {
				case "env_file":
					cfg.ProviderKeySources.EnvFiles = []string{writeProviderFixture(t, directory, "source.env", "OPENAI_API_KEY=fixture-key\nOPENAI_BASE_URL="+endpoint+"\n")}
				case "key_file":
					cfg.ProviderKeySources.KeyFiles = map[string]string{"OPENAI_API_KEY": writeProviderFixture(t, directory, "key", "fixture-key"), "OPENAI_BASE_URL": writeProviderFixture(t, directory, "base", endpoint)}
				case "directory":
					writeProviderFixture(t, directory, "OPENAI_API_KEY", "fixture-key")
					writeProviderFixture(t, directory, "OPENAI_BASE_URL", endpoint)
					cfg.ProviderKeySources.KeyDirectories = []string{directory}
				}
				provider := providerPresetFixture(t, "openai")
				want := endpoint == "https://api.openai.com/v1/"
				snapshot := DiscoverCredentials(cfg)
				if snapshot.KeyStatus(provider).Available != want {
					t.Fatal("source endpoint guard mismatch")
				}
				key, err := snapshot.ResolveKey(provider)
				if err != nil || (key != "") != want {
					t.Fatal("runtime disagrees with guard")
				}
				provider.APIKeyEnv, provider.APIKey = "", "explicit-fixture"
				if key, _ := provider.ResolveKey(cfg); key != "explicit-fixture" {
					t.Fatal("guard replaced explicit saved key")
				}
			})
		}
	}
}

func TestProviderCustomNamesAndInventoryCommands(t *testing.T) {
	isolateProviderEnvironment(t)
	directory := t.TempDir()
	provider := Provider{BaseURL: "http://localhost:1234/v1", APIKeyEnv: "FIXTURE_CUSTOM_KEY"}
	t.Setenv("FIXTURE_CUSTOM_KEY", "")
	writeProviderFixture(t, directory, "FIXTURE_CUSTOM_KEY", "custom-fixture")
	cfg := &Config{ProviderKeySources: ProviderKeySources{KeyDirectories: []string{directory}}}
	if key, err := provider.ResolveKey(cfg); err != nil || key != "custom-fixture" {
		t.Fatal("new provider could not resolve directory source before save")
	}
	cfg.ProviderKeySources = ProviderKeySources{EnvFiles: []string{writeProviderFixture(t, directory, "new.env", "FIXTURE_CUSTOM_KEY=custom-fixture\n")}}
	snapshot := DiscoverCredentials(cfg, &Config{Providers: map[string]Provider{"pending": provider}})
	if key, err := snapshot.ResolveKey(provider); err != nil || key != "custom-fixture" {
		t.Fatal("new provider could not resolve env file before save")
	}
	marker := filepath.Join(directory, "command")
	provider.APIKeyEnv, provider.APIKey = "", "!touch "+marker
	if snapshot.KeyStatus(provider).Available {
		t.Fatal("secret command marked ready")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inventory executed credential command")
	}
	if _, err := snapshot.ResolveKey(provider); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("runtime lost command credential semantics")
	}
}

func TestPersistDiscoveredProvidersReferencesAndNoop(t *testing.T) {
	isolateProviderEnvironment(t)
	directory := t.TempDir()
	source := writeProviderFixture(t, directory, "source.env", "OPENROUTER_API_KEY=router-private-fixture\nDEEPINFRA_TOKEN=alias-private-fixture\nINFERENCE_API_KEY=inference-private-fixture\n")
	cfg := Default()
	cfg.ProviderKeySources = ProviderKeySources{EnvFiles: []string{source}}
	cfg.Theme = "my-theme"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	updated, revision, err := PersistDiscoveredProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Providers["openrouter"].APIKeyEnv != "OPENROUTER_API_KEY" || updated.Providers["deepinfra"].APIKeyEnv != "DEEPINFRA_TOKEN" || updated.Theme != "my-theme" || updated.DefaultModel != cfg.DefaultModel {
		t.Fatal("discovery did not preserve routes/defaults/reference aliases")
	}
	filename, _ := path()
	before, _ := os.ReadFile(filename)
	info, _ := os.Stat(filename)
	if strings.Contains(string(before), "private-fixture") {
		t.Fatal("discovery persisted credential material")
	}
	again, nextRevision, err := PersistDiscoveredProviders(context.Background())
	if err != nil || nextRevision != revision {
		t.Fatal("repeat discovery changed revision")
	}
	after, _ := os.ReadFile(filename)
	nextInfo, _ := os.Stat(filename)
	if string(before) != string(after) || !info.ModTime().Equal(nextInfo.ModTime()) {
		t.Fatal("no-op discovery wrote config")
	}
	if !again.OpenRouterConfigured() {
		t.Fatal("restart source reference did not resolve")
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	missing, _, err := PersistDiscoveredProviders(context.Background())
	if err == nil || missing.Providers["openrouter"].APIKeyEnv == "" || missing.Providers["openrouter"].KeyStatus(missing).Available {
		t.Fatal("missing key deleted route or remained available")
	}
}

func TestPersistDiscoveredProvidersPreservesOverridesAndConcurrentEdits(t *testing.T) {
	isolateProviderEnvironment(t)
	t.Setenv("OPENROUTER_API_KEY", "router-fixture")
	t.Setenv("CEREBRAS_API_KEY", "cerebras-fixture")
	cfg := Default()
	cfg.DisabledProviders = []string{"openrouter"}
	custom := Provider{BaseURL: "https://custom.example", APIKey: "saved-fixture"}
	cfg.Providers["cerebras"] = custom
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errs := make(chan error, 12)
	for i := range 12 {
		group.Go(func() {
			if i%2 == 0 {
				_, _, err := PersistDiscoveredProviders(context.Background())
				errs <- err
			} else {
				_, _, err := UpdateVersioned("", func(latest *Config) error { latest.MaxRetries = 7; return nil })
				errs <- err
			}
		})
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := reloaded.Providers["openrouter"]; exists {
		t.Fatal("disabled provider resurrected")
	}
	if reloaded.Providers["cerebras"] != custom || reloaded.MaxRetries != 7 {
		t.Fatal("concurrent discovery overwrote user changes")
	}
}

func TestProviderSourcesSurviveSnapshotRecoveryAndFailure(t *testing.T) {
	isolateProviderEnvironment(t)
	source := writeProviderFixture(t, t.TempDir(), "source.env", "OPENROUTER_API_KEY=fixture-key\n")
	cfg := &Config{ProviderKeySources: ProviderKeySources{EnvFiles: []string{source}, KeyFiles: map[string]string{"CUSTOM_KEY": source}, KeyDirectories: []string{filepath.Dir(source)}}, DisabledProviders: []string{"openrouter"}}
	snapshot := cfg.Snapshot()
	cfg.ProviderKeySources.EnvFiles[0] = "changed"
	cfg.ProviderKeySources.KeyFiles["CUSTOM_KEY"] = "changed"
	cfg.ProviderKeySources.KeyDirectories[0] = "changed"
	if snapshot.ProviderKeySources.EnvFiles[0] != source || snapshot.ProviderKeySources.KeyFiles["CUSTOM_KEY"] != source || snapshot.ProviderKeySources.KeyDirectories[0] != filepath.Dir(source) {
		t.Fatal("snapshot aliases source configuration")
	}
	// Existing usable backup must not replace a new sources-only config.
	if err := Default().Save(); err != nil {
		t.Fatal(err)
	}
	filename, _ := path()
	defaultData, _ := os.ReadFile(filename)
	if err := os.WriteFile(filename+".bak", defaultData, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot.ProviderKeySources.KeyFiles = nil
	data, _ := json.Marshal(snapshot)
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := Load()
	if err != nil || restored.ProviderKeySources.EnvFiles[0] != source || len(restored.DisabledProviders) != 1 {
		t.Fatal("recovery discarded key sources or disabled providers")
	}
	restored.DisabledProviders = nil
	if err := restored.Save(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filename)
	if err := os.Mkdir(filename+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	current, _, err := PersistDiscoveredProviders(context.Background())
	if err == nil || current == nil {
		t.Fatal("failed write did not return prior usable state and error")
	}
	after, _ := os.ReadFile(filename)
	if string(before) != string(after) {
		t.Fatal("failed discovery changed original file")
	}
	if _, ok := current.Providers["openrouter"]; ok {
		t.Fatal("failed discovery reported persisted route")
	}
	if !current.EffectiveProviders()["openrouter"].KeyStatus(current).Available {
		t.Fatal("failed persistence hid usable effective provider")
	}
}

func TestDiscoverCredentialsIgnoresOpenCodeStores(t *testing.T) {
	isolateProviderEnvironment(t)
	directory := filepath.Join(os.Getenv("XDG_DATA_HOME"), "opencode")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeProviderFixture(t, directory, "auth.json", `{"groq":{"type":"api","key":"old-import-fixture"}}`)
	if providerPresetFixture(t, "groq").KeyStatus().Available {
		t.Fatal("OpenCode credential store was imported")
	}
}

func TestProviderMixedLiteralSurvivesUnavailableNamedSource(t *testing.T) {
	isolateProviderEnvironment(t)
	cfg := &Config{ProviderKeySources: ProviderKeySources{KeyFiles: map[string]string{"CEREBRAS_API_KEY": filepath.Join(t.TempDir(), "missing")}}}
	provider := providerPresetFixture(t, "cerebras")
	provider.APIKey = "explicit-fixture"
	credentials := DiscoverCredentials(cfg)
	key, err := credentials.ResolveKey(provider)
	if err != nil || key != "explicit-fixture" || credentials.KeyStatus(provider).Source != "literal" {
		t.Fatal("unavailable named source displaced existing explicit fallback")
	}
	if credentials.Err() == nil {
		t.Fatal("unavailable named source diagnostic disappeared")
	}
}
