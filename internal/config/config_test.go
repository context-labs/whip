package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSaveDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load() // first run writes defaults
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "kimi-k3-fast" || cfg.Providers["inference-net"].BaseURL != "https://api.inference.net/v1" {
		t.Fatalf("defaults: %+v", cfg)
	}
	cfg.DefaultModel = "glm-5.2-fast"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load()
	if err != nil || cfg2.DefaultModel != "glm-5.2-fast" {
		t.Fatalf("reload: %+v %v", cfg2, err)
	}
}

func TestRLMDefaultsEnabledAndCanBeDisabled(t *testing.T) {
	if !Default().RLMEnabled() {
		t.Fatal("RLM should default on")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".whip"), 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"defaultModel":"m","rlm":{"enabled":false,"steps":99,"maxWorkers":2},"providers":{"p":{"baseUrl":"https://example.test","api":"openai-completions"}},"models":{"m":{"providers":["p"]}}}`
	if err := os.WriteFile(filepath.Join(home, ".whip", "config.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RLMEnabled() || cfg.RLM.Steps != 99 || cfg.RLM.MaxWorkers != 2 {
		t.Fatalf("RLM config = %+v", cfg.RLM)
	}
}

func TestLoadRejectsBadJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".whip"), 0o700)
	os.WriteFile(filepath.Join(home, ".whip", "config.json"), []byte("{nope"), 0o600)
	if _, err := Load(); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestProviderKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no ~/.inf fallback available
	t.Setenv("WHIP_TEST_KEY", "from-env")

	if k := (Provider{APIKeyEnv: "WHIP_TEST_KEY", APIKey: "literal"}).Key(); k != "from-env" {
		t.Fatalf("env should win: %q", k)
	}
	if k := (Provider{APIKeyEnv: "WHIP_UNSET_VAR", APIKey: "literal"}).Key(); k != "literal" {
		t.Fatalf("literal fallback: %q", k)
	}
	if k := (Provider{BaseURL: "https://other.example.com"}).Key(); k != "" {
		t.Fatalf("no key expected: %q", k)
	}
}

func TestInfKeyFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := Provider{BaseURL: "https://api.inference.net/v1"}
	if k := p.Key(); k != "" {
		t.Fatalf("missing ~/.inf should yield empty, got %q", k)
	}
	os.MkdirAll(filepath.Join(home, ".inf"), 0o700)
	os.WriteFile(filepath.Join(home, ".inf", "config.json"), []byte(`{"codingAgentApiKey":"inf_sk_x"}`), 0o600)
	if k := p.Key(); k != "inf_sk_x" {
		t.Fatalf("inf fallback: %q", k)
	}
	os.WriteFile(filepath.Join(home, ".inf", "config.json"), []byte(`{"apiKey":"inf_sk_main"}`), 0o600)
	if k := p.Key(); k != "inf_sk_main" {
		t.Fatalf("apiKey should win: %q", k)
	}
}

func TestResolveRouting(t *testing.T) {
	cfg := &Config{
		DefaultModel: "m1",
		Providers: map[string]Provider{
			"a": {BaseURL: "https://a", API: "openai-completions"},
			"b": {BaseURL: "https://b", API: "openai-completions"},
		},
		Models: map[string]Model{
			"m1": {Providers: []string{"a", "b"}, ID: "vendor/m1"},
		},
	}
	p, _, id, err := cfg.Resolve("", "")
	if err != nil || p.BaseURL != "https://a" || id != "vendor/m1" {
		t.Fatalf("default routing: %v %v %v", p.BaseURL, id, err)
	}
	p, _, _, err = cfg.Resolve("m1", "b")
	if err != nil || p.BaseURL != "https://b" {
		t.Fatalf("provider override: %v %v", p.BaseURL, err)
	}
	if _, _, _, err = cfg.Resolve("nope", ""); err == nil {
		t.Fatal("expected unknown model error")
	}
	if _, _, _, err = cfg.Resolve("m1", "nope"); err == nil {
		t.Fatal("expected unknown provider error")
	}
}

func TestHomeUnavailable(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := Dir(); err == nil {
		t.Fatal("expected Dir error")
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected Load error")
	}
	if err := (&Config{}).Save(); err == nil {
		t.Fatal("expected Save error")
	}
	if k := infKey(); k != "" {
		t.Fatalf("infKey with no HOME: %q", k)
	}
}

func TestInfKeyBadJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".inf"), 0o700)
	os.WriteFile(filepath.Join(home, ".inf", "config.json"), []byte("{bad"), 0o600)
	if k := infKey(); k != "" {
		t.Fatalf("bad json should yield empty key, got %q", k)
	}
}

func TestLoadJSONCCommentsAndTrailingCommas(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".whip"), 0o700)
	src := `{
  // default route
  "defaultModel": "m1",
  "defaultProvider": "a", /* block comment */
  "providers": {
    "a": { "baseUrl": "https://a", "api": "openai-completions", }, // trailing comma
  },
  "models": {
    "m1": { "providers": ["a",], "maxTokens": 1024, },
  },
}
`
	os.WriteFile(filepath.Join(home, ".whip", "config.json"), []byte(src), 0o600)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "m1" || cfg.DefaultProvider != "a" {
		t.Fatalf("defaults: %+v", cfg)
	}
	if cfg.Models["m1"].Providers[0] != "a" || cfg.Models["m1"].MaxTokens != 1024 {
		t.Fatalf("model: %+v", cfg.Models["m1"])
	}
}

// TestMCPImportRoundTrip pins the mcpImport block's JSONC shape: absent stays
// nil (import-everything default), and a full block round-trips through
// Save/Load unchanged — including exclude beating only at policy level.
func TestMCPImportRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".whip"), 0o700)
	src := `{
  "defaultModel": "m1",
  "providers": { "a": { "baseUrl": "https://a", "api": "openai-completions" } },
  "models": { "m1": { "providers": ["a"] } },
  "mcpImport": {
    "claude": { "enabled": false },
    "codex": { "enabled": true, "only": ["paper"], "exclude": ["node_repl"] }
  }
}
`
	os.WriteFile(filepath.Join(home, ".whip", "config.json"), []byte(src), 0o600)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPImport == nil || cfg.MCPImport.Claude == nil || cfg.MCPImport.Claude.Enabled == nil || *cfg.MCPImport.Claude.Enabled {
		t.Fatalf("claude should parse as enabled=false, got %+v", cfg.MCPImport)
	}
	if got := cfg.MCPImport.Codex.Exclude; len(got) != 1 || got[0] != "node_repl" {
		t.Fatalf("codex exclude: %+v", cfg.MCPImport.Codex)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.MCPImport == nil || *reloaded.MCPImport.Claude.Enabled || reloaded.MCPImport.Codex.Only[0] != "paper" {
		t.Fatalf("mcpImport did not round-trip: %+v", reloaded.MCPImport)
	}
	// Absent block stays nil — zero-breakage default.
	if err := os.WriteFile(filepath.Join(home, ".whip", "config.json"), []byte(`{
  "defaultModel": "m1",
  "providers": { "a": { "baseUrl": "https://a", "api": "openai-completions" } },
  "models": { "m1": { "providers": ["a"] } }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.MCPImport != nil {
		t.Errorf("absent mcpImport must stay nil, got %+v", cfg2.MCPImport)
	}
}

// TestLoadPreservesMCPImportOnClobber: regenerating defaults after a clobber
// keeps the user's import gating (same rule as MCP servers).
func TestLoadPreservesMCPImportOnClobber(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".whip")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(
		`{"providers":null,"models":null,"mcpImport":{"codex":{"enabled":false}}}`,
	), 0o600)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPImport == nil || cfg.MCPImport.Codex == nil || *cfg.MCPImport.Codex.Enabled {
		t.Fatalf("mcpImport must survive clobber recovery, got %+v", cfg.MCPImport)
	}
}

func TestLoadRecoversFromClobberedConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".whip")
	os.MkdirAll(dir, 0o700)
	p := filepath.Join(dir, "config.json")
	// a previously-clobbered config: parses fine but has no providers/models
	os.WriteFile(p, []byte(`{"defaultModel":"","providers":null,"models":null}`), 0o600)
	// a healthy backup from before the wipe
	os.WriteFile(p+".bak", []byte(`{"defaultModel":"m1","providers":{"a":{"baseUrl":"https://a","api":"openai-completions"}},"models":{"m1":{"providers":["a"]}}}`), 0o600)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "m1" || len(cfg.Providers) != 1 {
		t.Fatalf("expected restore from .bak, got %+v", cfg)
	}
}

func TestLoadRegeneratesDefaultsWhenEmptyAndNoBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".whip")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"providers":null,"models":null}`), 0o600)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "kimi-k3-fast" || len(cfg.Providers) == 0 {
		t.Fatalf("expected regenerated defaults, got %+v", cfg)
	}
}

func TestSaveRefusesToClobberHealthyConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".whip")
	os.MkdirAll(dir, 0o700)
	p := filepath.Join(dir, "config.json")
	healthy := `{"defaultModel":"m1","providers":{"a":{"baseUrl":"https://a","api":"openai-completions"}},"models":{"m1":{"providers":["a"]}}}`
	os.WriteFile(p, []byte(healthy), 0o600)

	if err := (&Config{}).Save(); err == nil {
		t.Fatal("expected refusal to overwrite a healthy config with an empty one")
	}
	// original untouched
	data, _ := os.ReadFile(p)
	if string(data) != healthy {
		t.Fatalf("config should be unchanged, got %q", data)
	}
}

func TestSaveWritesBackupAndIsAtomic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := Load() // writes defaults
	if err != nil {
		t.Fatal(err)
	}
	p, _ := path()
	first, _ := os.ReadFile(p)

	cfg.DefaultModel = "glm-5.2-fast"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(p + ".bak")
	if err != nil {
		t.Fatal("expected a .bak of the previous contents")
	}
	if string(bak) != string(first) {
		t.Fatalf("backup should hold the previous contents")
	}
	// no temp file left behind
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file should be renamed away")
	}
}

func TestSaveWritesJSONCHeader(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	p, _ := path()
	data, _ := os.ReadFile(p)
	if len(data) == 0 || data[0] != '/' {
		t.Fatalf("expected a // header comment, got:\n%s", data)
	}
	// and it still parses back via the JSONC loader
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCatalogsAlwaysNonNil(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no ~/.whip/models.json exists
	cats := LoadCatalogs()
	if cats == nil {
		t.Fatal("LoadCatalogs must return a non-nil map so callers can write into it")
	}
	cats["inference"] = Catalog{} // must not panic
	if len(cats) != 1 {
		t.Fatalf("expected to hold the written entry, got %d", len(cats))
	}
}

func TestLogEventWritesAndRotates(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())

	LogEvent("config.save", "before=(providers=1) after=(providers=1)")
	LogEvent("catalog.fetch", "inference ok: 42 models")
	dir, _ := Dir()
	b, err := os.ReadFile(filepath.Join(dir, "whip.log"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "config.save") || !strings.Contains(s, "catalog.fetch") || !strings.Contains(s, "pid=") {
		t.Fatalf("log content: %q", s)
	}

	// rotation: oversize log rolls to whip.log.1
	os.WriteFile(filepath.Join(dir, "whip.log"), make([]byte, logMaxBytes+1), 0o600)
	LogEvent("config.load", "after rotation")
	if _, err := os.Stat(filepath.Join(dir, "whip.log.1")); err != nil {
		t.Fatalf("expected rotation: %v", err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "whip.log"))
	if !strings.Contains(string(b), "after rotation") {
		t.Fatalf("fresh log should hold the new event: %q", b)
	}
}

func TestLogEventNeverFails(t *testing.T) {
	t.Setenv("WHIP_HOME", "/nonexistent-\x7f-impossible") // Dir() will fail MkdirAll
	LogEvent("config.load", "should not panic or error")
}

// The first-run signal the wizard triggers on: no config file AND no
// setup-done marker. A subcommand's Load on a fresh install creates the
// config but never the marker — so the wizard still offers on the first
// interactive launch.
func TestSetupDoneMarker(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if SetupDone() {
		t.Fatal("a fresh WHIP_HOME should report setup-not-done")
	}
	if _, err := Load(); err != nil { // what a subcommand does
		t.Fatal(err)
	}
	if !Exists() {
		t.Fatal("Load should have written the config")
	}
	if SetupDone() {
		t.Fatal("a subcommand's Load must not mark setup done — the wizard would never run")
	}
	MarkSetupDone()
	if !SetupDone() {
		t.Fatal("the marker should report done")
	}
	// And the wizard completing is what writes it: prove the file shape.
	dir, _ := Dir()
	if _, err := os.Stat(filepath.Join(dir, "setup.done")); err != nil {
		t.Fatalf("setup.done should exist: %v", err)
	}
}

// ContextWindow prefers the new `context` field but falls back to the legacy
// `maxTokens` for configs written before the rename.
func TestContextWindowBackCompat(t *testing.T) {
	if got := (Model{Context: 200000}).ContextWindow(); got != 200000 {
		t.Fatalf("context field: %d", got)
	}
	if got := (Model{MaxTokens: 131072}).ContextWindow(); got != 131072 {
		t.Fatalf("legacy maxTokens: %d", got)
	}
	if got := (Model{Context: 200000, MaxTokens: 131072}).ContextWindow(); got != 200000 {
		t.Fatalf("context should win over legacy: %d", got)
	}
	if got := (Model{}).ContextWindow(); got != 0 {
		t.Fatalf("empty: %d", got)
	}
}

// The catalog reports the provider's output cap (max_completion_tokens)
// separately from the input window (context_length).
func TestCatalogMaxCompletionTokens(t *testing.T) {
	c := Catalog{Models: []ModelInfoLite{
		{ID: "a", ContextLength: 1000000, MaxCompletionTokens: 128000},
		{ID: "b", ContextLength: 200000}, // no output cap advertised
	}}
	if got := c.MaxCompletionTokens("a"); got != 128000 {
		t.Fatalf("a: %d", got)
	}
	if got := c.MaxCompletionTokens("b"); got != 0 {
		t.Fatalf("b should be 0 when unadvertised: %d", got)
	}
	if got := c.MaxCompletionTokens("nope"); got != 0 {
		t.Fatalf("unknown: %d", got)
	}
	if got := c.ContextLength("a"); got != 1000000 {
		t.Fatalf("ctx a: %d", got)
	}
}

// A config mixing old maxTokens with new context/maxOut parses both.
func TestLoadMixedTokenFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".whip"), 0o700)
	src := `{
  "defaultModel": "m1",
  "providers": { "a": { "baseUrl": "https://a", "api": "openai-completions" } },
  "models": {
    "m1": { "providers": ["a"], "maxTokens": 131072 },
    "m2": { "providers": ["a"], "context": 200000, "maxOut": 64000 }
  }
}
`
	os.WriteFile(filepath.Join(home, ".whip", "config.json"), []byte(src), 0o600)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Models["m1"].ContextWindow(); got != 131072 {
		t.Fatalf("m1 legacy context: %d", got)
	}
	m2 := cfg.Models["m2"]
	if m2.ContextWindow() != 200000 || m2.MaxOut != 64000 {
		t.Fatalf("m2: %+v", m2)
	}
}

// Snapshot hands worker goroutines an isolated view: mutating the original's
// maps after the snapshot must not show through.
func TestSnapshotIsolatesMaps(t *testing.T) {
	cfg := Default()
	snap := cfg.Snapshot()
	cfg.Providers["late"] = Provider{BaseURL: "http://late"}
	cfg.Models["late-model"] = Model{Providers: []string{"late"}}
	cfg.DefaultModel = "changed"
	if _, ok := snap.Providers["late"]; ok {
		t.Fatal("snapshot providers must not see later mutations")
	}
	if _, ok := snap.Models["late-model"]; ok {
		t.Fatal("snapshot models must not see later mutations")
	}
	if snap.DefaultModel == "changed" {
		t.Fatal("snapshot scalars are copies")
	}
	if len(snap.Providers) != len(Default().Providers) || len(snap.Models) != len(Default().Models) {
		t.Fatal("snapshot should carry the original entries")
	}
}
