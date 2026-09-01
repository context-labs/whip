// Package config loads and saves whip's JSONC configuration from ~/.whip.
package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Provider is an API endpoint that can serve models.
type Provider struct {
	Name      string `json:"name,omitempty"`
	BaseURL   string `json:"baseUrl"`
	API       string `json:"api"`              // "openai-completions" is the only supported value for now
	APIKey    string `json:"apiKey,omitempty"` // literal key or a secret reference ("$VAR"/"${VAR}"/"!cmd"); apiKeyEnv is another option
	APIKeyEnv string `json:"apiKeyEnv,omitempty"`
}

// Key returns the resolved API key for the provider, "" when none is
// configured. Unresolvable secret references degrade to "" like a missing
// key; ResolveKey reports the error for callers that can surface it.
func (p Provider) Key() string {
	k, _ := p.ResolveKey()
	return k
}

// ResolveKey is Key with error detail: apiKey/apiKeyEnv may hold a secret
// reference (see ResolveSecret), resolved here at the point of use so the
// config file and session store hold only references and a missing var only
// errors when the provider is actually used. The resolved value never enters
// the event log.
func (p Provider) ResolveKey() (string, error) {
	if p.APIKeyEnv != "" {
		if v := os.Getenv(p.APIKeyEnv); v != "" {
			return v, nil
		}
	}
	if p.APIKey != "" {
		k, err := ResolveSecret(p.APIKey)
		if err != nil {
			return "", fmt.Errorf("provider %q apiKey: %w", p.Name, err)
		}
		return k, nil
	}
	// Inference.net fallbacks: the machine key provisioned by
	// `whip auth inference-net login`, then the inf CLI's stored key.
	if strings.Contains(p.BaseURL, "api.inference.net") {
		if k := whipInferenceNetKey(); k != "" {
			return k, nil
		}
		return infKey(), nil
	}
	return "", nil
}

// whipInferenceNetKey reads the machine key from ~/.whip/inference-net.json
// (written by `whip auth inference-net login`).
func whipInferenceNetKey() string {
	dir, err := Dir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "inference-net.json"))
	if err != nil {
		return ""
	}
	var a struct {
		MachineKey string `json:"machineKey"`
	}
	if json.Unmarshal(data, &a) != nil {
		return ""
	}
	return a.MachineKey
}

// infKey reads apiKey/codingAgentApiKey from ~/.inf/config.json (written by `inf auth set-key`).
func infKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".inf", "config.json"))
	if err != nil {
		return ""
	}
	var c struct {
		APIKey            string `json:"apiKey"`
		CodingAgentAPIKey string `json:"codingAgentApiKey"`
	}
	if json.Unmarshal(data, &c) != nil {
		return ""
	}
	if c.APIKey != "" {
		return c.APIKey
	}
	return c.CodingAgentAPIKey
}

// Model routes a model to one or more providers that serve it.
type Model struct {
	Name      string   `json:"name,omitempty"`
	Providers []string `json:"providers"`    // provider keys, first is the default
	ID        string   `json:"id,omitempty"` // model id sent to the API; defaults to the map key
	// Context is the model's context window (max INPUT tokens). The provider's
	// /models context_length overrides it when advertised; this is the fallback
	// and the value shown for providers that don't report one.
	Context int `json:"context,omitempty"`
	// MaxOut caps OUTPUT tokens (the max_tokens request param). 0 uses the
	// provider's max_completion_tokens when advertised, else a sane default.
	MaxOut int `json:"maxOut,omitempty"`
	// MaxTokens is the legacy field name for Context (it was misnamed: it held
	// the context window, not an output cap). Read on load for back-compat.
	MaxTokens int `json:"maxTokens,omitempty"`
	// Vision reports whether the model accepts image inputs. When false (the
	// default), @image tags are NOT inlined as base64 vision parts — the model
	// gets a pointer note instead, so a text-only model isn't sent a request it
	// would reject. A provider-advertised input_modalities entry overrides this.
	Vision bool `json:"vision,omitempty"`
	// SamplingParams are optional per-model sampling knobs (temperature, top_p)
	// applied to outbound chat-completion requests for this model. Fields are
	// omitted from requests when unset, so provider defaults are preserved.
	SamplingParams *SamplingParams `json:"samplingParams,omitempty"`
}

// SamplingParams holds optional per-model sampling knobs sent on outbound
// chat-completion requests. Pointer fields: a 0.0 temperature/top_p is a
// legitimate value, and nil means "don't send" (provider default).
type SamplingParams struct {
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
}

// ContextWindow returns the model's context (input) size, honoring the legacy
// maxTokens field for configs written before the rename.
func (m Model) ContextWindow() int {
	if m.Context > 0 {
		return m.Context
	}
	return m.MaxTokens
}

// DefaultCompactModel is the built-in compaction-model default: the
// deepseek-v4-flash route wired into the default inference.net config. An
// empty compactModel resolves to this at apply time, falling back to the
// conversation's model when it's not in the user's config.
const DefaultCompactModel = "deepseek-v4-flash-0731"

// DefaultTaskModel is the built-in subagent-model default: subagents are
// high-volume, self-contained work, so they run on the same cheap fast route
// compaction uses unless the user pins taskModel or the main model overrides
// per task. Falls back to the conversation's model when it's not resolvable
// (an openrouter-only config resolves it via a catalog suffix match first).
const DefaultTaskModel = DefaultCompactModel

// DefaultCompactPct is the built-in compaction threshold: compact once the
// estimated context use crosses this percent of the model's context window.
// 50% keeps compaction deterministic instead of letting the context bloat.
const DefaultCompactPct = 50

// Config is the root of ~/.whip/config.json (JSONC: comments allowed).
type Config struct {
	DefaultModel    string `json:"defaultModel"`
	DefaultProvider string `json:"defaultProvider,omitempty"` // override the model's first provider
	DefaultEffort   string `json:"defaultEffort,omitempty"`   // reasoning effort for new sessions: "" defaults to "low"; "off", "low", "medium", "high"
	CompactModel    string `json:"compactModel,omitempty"`    // model for compaction summaries; "" = the built-in default
	CompactProvider string `json:"compactProvider,omitempty"` // provider for the compaction model; "" = the model's default routing
	CompactPct      int    `json:"compactPct,omitempty"`      // compact at this % of the context window; 0 = DefaultCompactPct
	TaskModel       string `json:"taskModel,omitempty"`       // model subagents (the task tool) run on; "" = the built-in default
	TaskProvider    string `json:"taskProvider,omitempty"`    // provider for the subagent model; "" = the model's default routing
	Theme           string `json:"theme,omitempty"`           // "light", "dark", or "" (auto-detect at startup)
	UIMode          string `json:"uiMode,omitempty"`          // "" (default whip look) or "opencode" (reproduces opencode's TUI palette/glyphs/logo)
	Mouse           *bool  `json:"mouse,omitempty"`           // false disables capture so native terminal selection works
	Thinking        *bool  `json:"thinking,omitempty"`        // nil defaults to on; false hides reasoning tokens (ctrl+o)
	CollapsePaste   *bool  `json:"collapsePaste,omitempty"`   // nil/false: pastes land verbatim; true collapses ≥3-line pastes into a [Pasted ~N lines] placeholder
	GoalMaxRounds   int    `json:"goalMaxRounds,omitempty"`   // global goal-loop round cap; 0 = DefaultGoalMaxRounds; projects.json may override per folder
	// WorktreeSubagents defaults background subagents to run in their own git
	// worktree so their file edits stay isolated from the parent's tree and
	// from each other. The subagent tool's per-call `worktree` arg overrides this.
	WorktreeSubagents *bool               `json:"worktreeSubagents,omitempty"`
	MaxRetries        int                 `json:"maxRetries,omitempty"` // attempts per provider request on transient failures (429/5xx/network); 0 = llm.DefaultMaxAttempts, 1 = no retries
	Providers         map[string]Provider `json:"providers"`
	Models            map[string]Model    `json:"models"`
	// MCPServers is whip's own MCP server block (whip-native shape; see
	// internal/mcp.ServerConfig for the normalized semantics). On load it is
	// merged over imported claude/codex configs: whip always wins per name.
	MCPServers map[string]MCPServer `json:"mcp,omitempty"`
	// MCPImport gates which imported MCP server definitions whip picks up
	// (claude-style .mcp.json, codex-style ~/.codex/config.toml). nil imports
	// both sources, preserving the pre-gating behavior.
	MCPImport *MCPImport `json:"mcpImport,omitempty"`
	// LSPServers is whip's own LSP server block (whip-native shape; see
	// internal/lsp.FromConfigMap for the merge semantics). Entries extend or
	// disable the built-in registry (gopls).
	LSPServers map[string]LSPServer `json:"lsp,omitempty"`
	// Browser configures the native browser subsystem (internal/browser).
	Browser BrowserConfig `json:"browser,omitzero"`
	// Computer configures computer-use (internal/computer): which apps
	// computer_exec may drive.
	Computer ComputerConfig `json:"computer,omitzero"`
}

// ComputerConfig gates computer_exec per app (codex's per-bundle-id model).
type ComputerConfig struct {
	// Allow lists app names/bundle ids approved without prompting
	// (e.g. ["Google Chrome", "com.google.Chrome"]).
	Allow []string `json:"allow,omitempty"`
	// Deny lists apps never drivable (wins over allow).
	Deny []string `json:"deny,omitempty"`
	// DefaultDeny, when true, gates unlisted apps behind approval. Default
	// is false — allow-all, and users build blocklists via `deny` (or
	// /computer-use deny <app> in-session).
	DefaultDeny *bool `json:"defaultDeny,omitempty"`
	// Enabled false hides computer_exec entirely.
	Enabled *bool `json:"enabled,omitempty"`
}

// BrowserConfig controls browser_exec: which Chrome to drive and the
// private-address policy for non-live backends.
type BrowserConfig struct {
	// Mode: "live" (attach to the user's running Chrome, default),
	// "dedicated" (whip-owned profile), "headless", or "extension" (drive
	// the user's real logged-in tab through the whip extension — the only
	// way onto the default profile on Chrome ≥ 136).
	Mode string `json:"mode,omitempty"`
	// CDPURL attaches live mode to an explicit DevTools endpoint instead of
	// the profile scan (http:// or ws://).
	CDPURL string `json:"cdpUrl,omitempty"`
	// AllowPrivateURLs permits private/LAN targets on dedicated/headless
	// backends (default false; live mode is always exempt — the user's own
	// browser on their own network).
	AllowPrivateURLs bool `json:"allowPrivateUrls,omitempty"`
	// Enabled false hides the browser_exec tool entirely.
	Enabled *bool `json:"enabled,omitempty"`
}

// LSPServer is the config-file form of an LSP server entry. It mirrors
// MCPServer minus the remote fields (LSP is stdio-only here).
type LSPServer struct {
	Command     []string          `json:"command,omitempty"`
	Extensions  []string          `json:"extensions,omitempty"`
	RootMarkers []string          `json:"rootMarkers,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
}

// MCPImport selects which claude/codex MCP server definitions whip imports.
// A nil source entry (or nil Enabled) leaves that source on. Example:
//
//	"mcpImport": {
//	  "codex": { "enabled": true, "exclude": ["node_repl"] }
//	}
type MCPImport struct {
	Claude *MCPImportSource `json:"claude,omitempty"`
	Codex  *MCPImportSource `json:"codex,omitempty"`
}

// MCPImportSource gates one import source. Enabled nil means on; Only, when
// non-empty, is an allowlist of server names; Exclude is a denylist and wins
// over Only when both are set (documented behavior, no validation error).
type MCPImportSource struct {
	Enabled *bool    `json:"enabled,omitempty"`
	Only    []string `json:"only,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// MCPServer is the config-file form of an MCP server entry. It mirrors
// mcp.ServerConfig without importing that package (config is a leaf).
type MCPServer struct {
	Command        []string          `json:"command,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Cwd            string            `json:"cwd,omitempty"`
	URL            string            `json:"url,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Enabled        *bool             `json:"enabled,omitempty"`
	Note           string            `json:"note,omitempty"`
	StartupTimeout int               `json:"startupTimeout,omitempty"`
	ToolTimeout    int               `json:"toolTimeout,omitempty"`
}

// Dir returns the whip home directory (~/.whip), creating it if needed.
// WHIP_HOME overrides the location — used by tests to keep fixture writes
// far away from the real config.
func Dir() (string, error) {
	if d := os.Getenv("WHIP_HOME"); d != "" {
		return d, os.MkdirAll(d, 0o700) //nolint:gosec // G703: WHIP_HOME is the user's own env override for the config dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".whip")
	return dir, os.MkdirAll(dir, 0o700)
}

func path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Exists reports whether the config file is already on disk. Load creates it
// when missing, so callers that want to detect a first run (the setup wizard)
// must check Exists before Load.
func Exists() bool {
	p, err := path()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// setupDonePath is the marker the first-run wizard leaves when it completes.
// The wizard triggers on "no config file AND no marker": any whip subcommand
// (auth/run/mcp/…) that calls Load on a fresh install creates the config
// without running the wizard, and the marker keeps that from permanently
// consuming the first run.
func setupDonePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "setup.done"), nil
}

// SetupDone reports whether the first-run wizard has completed (or a previous
// version ran enough times to have one).
func SetupDone() bool {
	p, err := setupDonePath()
	if err != nil {
		return true // can't stat the marker: don't risk a surprise wizard
	}
	_, err = os.Stat(p)
	return err == nil
}

// MarkSetupDone leaves the wizard-completed marker. Best-effort: a failed
// write just means the wizard offers again next launch.
func MarkSetupDone() {
	p, err := setupDonePath()
	if err != nil {
		return
	}
	_ = os.WriteFile(p, []byte("ok\n"), 0o600)
}

// fingerprint summarizes a config for the operation log: enough to spot a
// clobbering write (providers/models collapsing, fixture values appearing)
// without logging secrets.
func (c *Config) fingerprint() string {
	return fmt.Sprintf("providers=%d models=%d default=%q compact=%q",
		len(c.Providers), len(c.Models), c.DefaultModel, c.CompactModel)
}

// Load reads ~/.whip/config.json, writing a default config on first run. The
// file is JSONC: comments and trailing commas are allowed.
func Load() (*Config, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		cfg := Default()
		logf("config.load", "missing file, writing defaults (%s)", cfg.fingerprint())
		return cfg, cfg.Save()
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := parseJSONC(data, &cfg); err != nil {
		logf("config.load", "PARSE FAILURE %s: %v (%d bytes)", p, err, len(data))
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	// Recover from a clobbered/empty config: no providers and no models is
	// never a usable state, so prefer the backup, else regenerate defaults —
	// BUT preserve any MCP server/import entries: an mcp-only config is valid
	// (the user may configure servers before providers), and regenerating
	// defaults would silently wipe them.
	if len(cfg.Providers) == 0 && len(cfg.Models) == 0 {
		logf("config.load", "CLOBBERED/EMPTY config detected (%d bytes on disk), attempting recovery", len(data))
		if bak, err := os.ReadFile(p + ".bak"); err == nil {
			var restored Config
			if parseJSONC(bak, &restored) == nil && (len(restored.Providers) > 0 || len(restored.Models) > 0) {
				logf("config.load", "restored from .bak (%s)", restored.fingerprint())
				if len(restored.MCPServers) == 0 && len(cfg.MCPServers) > 0 {
					restored.MCPServers = cfg.MCPServers // keep the user's servers
				}
				if restored.MCPImport == nil {
					restored.MCPImport = cfg.MCPImport // keep import gating too
				}
				return &restored, restored.Save()
			}
		}
		def := Default()
		def.MCPServers = cfg.MCPServers // mcp-only configs are valid; keep them
		def.MCPImport = cfg.MCPImport
		logf("config.load", "no usable .bak; regenerated defaults (%s), keeping %d mcp entries", def.fingerprint(), len(cfg.MCPServers))
		return def, def.Save()
	}
	logf("config.load", "ok (%s)", cfg.fingerprint())
	cfg.normalize()
	return &cfg, nil
}

// normalize upgrades legacy config shapes in place. Today: the provider key
// "inference" was renamed to "inference-net" — old configs (and model routes
// pointing at it) are migrated transparently. No file write happens here;
// the next Save persists the rename.
func (c *Config) normalize() {
	p, ok := c.Providers["inference"]
	if !ok {
		return
	}
	if _, clash := c.Providers["inference-net"]; !clash {
		c.Providers["inference-net"] = p
	}
	delete(c.Providers, "inference")
	for name, m := range c.Models {
		for i, prov := range m.Providers {
			if prov == "inference" {
				m.Providers[i] = "inference-net"
			}
		}
		c.Models[name] = m
	}
	if c.DefaultProvider == "inference" {
		c.DefaultProvider = "inference-net"
	}
	if c.CompactProvider == "inference" {
		c.CompactProvider = "inference-net"
	}
}

// Save writes the config back to ~/.whip/config.json. The write is atomic
// (temp file + rename) and the previous contents are kept in config.json.bak.
// As a safety net, Save refuses to overwrite an existing healthy config (one
// with providers/models) with a structurally empty one — that path has only
// ever been reached by a bug, never intentionally.
func (c *Config) Save() error {
	p, err := path()
	if err != nil {
		return err
	}
	if len(c.Providers) == 0 && len(c.Models) == 0 {
		if existing, err := os.ReadFile(p); err == nil {
			var cur Config
			if parseJSONC(existing, &cur) == nil && (len(cur.Providers) > 0 || len(cur.Models) > 0) {
				logf("config.save", "REFUSED empty overwrite of healthy config (disk had providers=%d models=%d)", len(cur.Providers), len(cur.Models))
				return fmt.Errorf("refusing to overwrite %s: existing config has providers/models but the value being saved is empty", p)
			}
		}
	}
	data, err := marshalConfig(c)
	if err != nil {
		return err
	}
	// log the before/after fingerprint so a bad write is attributable
	if existing, err := os.ReadFile(p); err == nil && len(existing) > 0 {
		var cur Config
		if parseJSONC(existing, &cur) == nil {
			logf("config.save", "before=(%s) after=(%s)", cur.fingerprint(), c.fingerprint())
		} else {
			logf("config.save", "before=(unparseable, %d bytes) after=(%s)", len(existing), c.fingerprint())
		}
		// best-effort backup before replacing
		_ = os.WriteFile(p+".bak", existing, 0o600) //nolint:gosec // G703: p is the whip-owned config path
	} else {
		logf("config.save", "first write (%s)", c.fingerprint())
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logf("config.save", "write tmp failed: %v", err)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		logf("config.save", "rename failed: %v", err)
		return err
	}
	return nil
}

// marshalConfig renders the config as JSONC with a header comment.
func marshalConfig(c *Config) ([]byte, error) {
	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	header := "// whip configuration — JSONC: comments and trailing commas are allowed.\n" +
		"// providers: declare each API endpoint once. models: route each model to one or\n" +
		"// more providers (first is the default). defaultModel/defaultProvider pick the route.\n" +
		"// mcp: whip's own MCP servers; mcpImport: gate claude/codex imports, e.g.\n" +
		"//   \"mcpImport\": { \"codex\": { \"enabled\": true, \"exclude\": [\"node_repl\"] } }\n"
	out := append([]byte(header), body...)
	return append(out, '\n'), nil
}

// Resolve picks the provider and API model id for a model name.
// provider may be "" to use the config default routing.
func (c *Config) Resolve(model, provider string) (Provider, Model, string, error) {
	if model == "" {
		model = c.DefaultModel
	}
	m, ok := c.Models[model]
	if !ok {
		// Catalog fallback: a provider-advertised model needs no config entry;
		// config entries stay authoritative overrides when present.
		var err error
		m, provider, err = c.resolveFromCatalog(model, provider)
		if err != nil {
			return Provider{}, Model{}, "", err
		}
	}
	if provider == "" {
		provider = c.DefaultProvider
	}
	if provider == "" && len(m.Providers) > 0 {
		provider = m.Providers[0]
	}
	p, ok := c.Providers[provider]
	if !ok {
		return Provider{}, Model{}, "", fmt.Errorf("unknown provider %q (providers: %s)", provider, keys(c.Providers))
	}
	id := m.ID
	if id == "" {
		id = model
	}
	return p, m, id, nil
}

// UnknownModelError flags a Resolve miss the caller may recover from by
// refreshing the provider catalogs and retrying: the model is absent from
// both cfg.Models and every provider's cached /models list, which a stale
// (or deleted) ~/.whip/models.json causes even for live models.
type UnknownModelError struct {
	Model string
	known string // config model names, for the message
}

func (e *UnknownModelError) Error() string {
	return fmt.Sprintf("unknown model %q (models: %s)", e.Model, e.known)
}

// resolveFromCatalog synthesizes a Model for an id advertised in a provider's
// cached /models catalog but absent from cfg.Models. Capabilities (context,
// max output, vision) come from the catalog entry; the provider routing is the
// catalog's owner. provider may pin the choice ("" scans all providers); when
// several providers advertise the id and none is pinned, it errors naming the
// candidates so the user can disambiguate with -p / a provider argument.
func (c *Config) resolveFromCatalog(model, provider string) (Model, string, error) {
	type hit struct {
		prov string
		mi   *ModelInfoLite
	}
	var hits []hit
	for name, cat := range LoadCatalogs() {
		if provider != "" && name != provider {
			continue
		}
		if _, ok := c.Providers[name]; !ok {
			continue // catalog for a provider no longer configured
		}
		if mi := cat.Find(model); mi != nil {
			hits = append(hits, hit{name, mi})
		}
	}
	if len(hits) == 0 {
		return Model{}, "", &UnknownModelError{Model: model, known: keys(c.Models)}
	}
	if len(hits) > 1 {
		names := make([]string, len(hits))
		for i, h := range hits {
			names[i] = h.prov
		}
		return Model{}, "", fmt.Errorf("model %q is advertised by multiple providers (%s); pass a provider to disambiguate (-p / /model %s <provider>)",
			model, strings.Join(names, ", "), model)
	}
	h := hits[0]
	m := Model{
		Providers: []string{h.prov},
		ID:        model,
		Context:   h.mi.ContextLength,
		MaxOut:    h.mi.MaxCompletionTokens,
	}
	if slices.Contains(h.mi.InputModalities, "image") {
		m.Vision = true
	}
	return m, h.prov, nil
}

// Snapshot returns a copy of the config safe to read from another goroutine
// while the original keeps being mutated on the UI goroutine (the subagent
// model resolver runs on tool workers). Maps are copied one level deep —
// their struct values are plain data; nested slices are never mutated in
// place, only replaced wholesale with the map entry.
func (c *Config) Snapshot() *Config {
	snap := *c
	snap.Providers = make(map[string]Provider, len(c.Providers))
	maps.Copy(snap.Providers, c.Providers)
	snap.Models = make(map[string]Model, len(c.Models))
	maps.Copy(snap.Models, c.Models)
	return &snap
}

func keys[V any](m map[string]V) string {
	s := ""
	for k := range m {
		if s != "" {
			s += ", "
		}
		s += k
	}
	return s
}

// Default returns the first-run config, wired for inference.net.
func Default() *Config {
	return &Config{
		DefaultModel: "kimi-k3-fast",
		CompactModel: DefaultCompactModel,
		//nolint:gosec // G101: APIKeyEnv holds env var NAMES, not credentials
		Providers: map[string]Provider{
			"inference-net": {
				Name:      "Inference.net",
				BaseURL:   "https://api.inference.net/v1",
				API:       "openai-completions",
				APIKeyEnv: "INFERENCE_API_KEY",
			},
		},
		Models: map[string]Model{
			"kimi-k3":                {Providers: []string{"inference-net"}, Context: 1048576, Vision: true},
			"kimi-k3-fast":           {Providers: []string{"inference-net"}, Context: 1048576, Vision: true},
			"glm-5.2-fast":           {Providers: []string{"inference-net"}, Context: 128000},
			"deepseek-v4-flash-0731": {Providers: []string{"inference-net"}, Context: 384000},
		},
	}
}
