package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

func authTestModel(t *testing.T) *model {
	t.Helper()
	t.Setenv("WHIP_HOME", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := &model{
		input:    newInput(),
		cfg:      cfg,
		queueSel: -1,
	}
	m.width = 80
	m.input.SetWidth(m.width - 2)
	return m
}

// transcriptText concatenates every appended block's text for assertions.
func (m *model) transcriptText() string {
	var sb strings.Builder
	for _, b := range m.blocks {
		sb.WriteString(b.text + "\n")
	}
	return sb.String()
}

func TestAuthCommandUsageAndUnknownProvider(t *testing.T) {
	m := authTestModel(t)
	m.authCommand(nil)
	m.authCommand([]string{"anthropic"})
	out := m.transcriptText()
	if !strings.Contains(out, "usage: /auth") || !strings.Contains(out, "codex") {
		t.Errorf("bare /auth should print usage:\n%s", out)
	}
	if !strings.Contains(out, "unknown provider anthropic") {
		t.Errorf("unknown provider should be rejected:\n%s", out)
	}
}

func TestAuthCommandCodexUsage(t *testing.T) {
	m := authTestModel(t)
	m.authCommand([]string{"codex", "unexpected"})
	if out := m.transcriptText(); !strings.Contains(out, "usage: /auth codex") {
		t.Errorf("codex with arguments should print usage:\n%s", out)
	}
}

func TestCodexLoginResultConfiguresAndMakesModelPickable(t *testing.T) {
	m := authTestModel(t)
	m.busy = true
	m.cancel = func() {}
	m.applyCodexLoginResult(codexLoginResultMsg{models: []llm.ModelInfo{
		{ID: "gpt-5.6-sol", ContextLength: 1050000, ReasoningEfforts: []string{"low", "high"}, InputModalities: []string{"text", "image"}},
		{ID: "gpt-5.6-terra", ContextLength: 1050000, InputModalities: []string{"text", "image"}},
	}})

	if m.busy || m.cancel != nil {
		t.Fatal("completed Codex login should clear its busy state")
	}
	p, ok := m.cfg.Providers[config.CodexProviderName]
	if !ok || p.Auth != "codex" {
		t.Fatalf("Codex provider was not configured: %+v", p)
	}
	m.openModelPicker()
	if m.mpicker == nil {
		t.Fatal("Codex login should make the model picker available")
	}
	var foundDefault, foundCatalog bool
	for _, item := range m.mpicker.items {
		if item.model == config.CodexDefaultModel && item.provider == config.CodexProviderName {
			foundDefault = true
		}
		if item.model == "gpt-5.6-sol" && item.provider == config.CodexProviderName && item.fromCatalog {
			foundCatalog = true
		}
	}
	if !foundDefault {
		t.Fatalf("%s @ %s missing from picker: %+v", config.CodexDefaultModel, config.CodexProviderName, m.mpicker.items)
	}
	if !foundCatalog {
		t.Fatalf("account catalog model missing from picker: %+v", m.mpicker.items)
	}
	if out := m.transcriptText(); !strings.Contains(out, "Codex configured") {
		t.Errorf("success should be reported:\n%s", out)
	}
}

func TestCodexLoginResultFailureLeavesRouteUntouched(t *testing.T) {
	m := authTestModel(t)
	m.applyCodexLoginResult(codexLoginResultMsg{err: errors.New("device login rejected")})
	if _, ok := m.cfg.Providers[config.CodexProviderName]; ok {
		t.Fatal("failed Codex login must not configure a provider")
	}
	if _, ok := m.cfg.Models[config.CodexDefaultModel]; ok {
		t.Fatal("failed Codex login must not add a model route")
	}
	if out := m.transcriptText(); !strings.Contains(out, "Codex login failed") {
		t.Errorf("failure should be reported:\n%s", out)
	}
}

func TestAuthCommandBareOpensMaskedPrompt(t *testing.T) {
	m := authTestModel(t)
	m.authCommand([]string{"openrouter"})
	if m.namePrompt == nil {
		t.Fatal("bare /auth openrouter should open the key prompt")
	}
	if !m.namePrompt.mask {
		t.Error("key prompt must mask input")
	}
	if got := m.namePrompt.maskedValue("sk-or-secret"); strings.Contains(got, "secret") {
		t.Errorf("masked render leaked the key: %q", got)
	}
	if got := m.namePrompt.maskedValue("sk-or-secret"); got != strings.Repeat("•", len("sk-or-secret")) {
		t.Errorf("mask should be one bullet per rune, got %q", got)
	}

	// esc cancels: prompt closes, nothing was configured.
	m.closeNamePrompt()
	if m.namePrompt != nil {
		t.Error("esc should close the prompt")
	}
	if m.cfg.OpenRouterConfigured() {
		t.Error("cancelled prompt must not configure the provider")
	}
}

func TestAuthResultGoodKeyConfiguresAndRefreshes(t *testing.T) {
	m := authTestModel(t)

	// prog == nil, so applyAuthResult is driven directly (the goroutine
	// dispatch in authOpenRouter is a no-op without a running program).
	m.applyAuthResult(authResultMsg{
		key:    "sk-or-good",
		models: []llm.ModelInfo{{ID: "openai/gpt-5", ContextLength: 400000, InputModalities: []string{"text", "image"}}},
	})

	if !m.cfg.OpenRouterConfigured() {
		t.Fatal("provider should be configured after a good auth")
	}
	if got := m.cfg.Providers["openrouter"].APIKey; got != "sk-or-good" {
		t.Errorf("literal key not stored: %q", got)
	}
	// Catalog seeding is a live-runtime side effect (guarded on m.prog); the
	// dispatch-level test asserts the config + transcript, not the cache.
	if cats := config.LoadCatalogs(); len(cats) != 0 {
		t.Error("dispatch-level auth must not write the on-disk catalog cache")
	}
	out := m.transcriptText()
	if !strings.Contains(out, "openrouter configured") {
		t.Errorf("success should be reported:\n%s", out)
	}
	if strings.Contains(out, "sk-or-good") {
		t.Error("the key must never appear in the transcript")
	}
}

func TestAuthResultBadKeyWritesNothing(t *testing.T) {
	m := authTestModel(t)
	m.applyAuthResult(authResultMsg{err: errors.New("401 invalid key")})

	if m.cfg.OpenRouterConfigured() {
		t.Error("a rejected key must not configure the provider")
	}
	if cats := config.LoadCatalogs(); len(cats) != 0 {
		t.Error("a rejected key must not write the catalog")
	}
	out := m.transcriptText()
	if !strings.Contains(out, "rejected") {
		t.Errorf("failure should be reported:\n%s", out)
	}
}

func TestAuthResultRekeysLiveSession(t *testing.T) {
	m := authTestModel(t)
	m.cfg.UpsertOpenRouter("sk-or-old", false)
	if err := m.cfg.Save(); err != nil {
		t.Fatal(err)
	}
	// Session currently routed through openrouter with a config-entry model.
	m.cfg.Models["gpt-5"] = config.Model{Providers: []string{"openrouter"}, ID: "openai/gpt-5", Context: 400000}
	m.modelName, m.provName = "gpt-5", "openrouter"
	ag, _, _, err := buildAgent(m.cfg, m.modelName, m.provName, "sys")
	if err != nil {
		t.Fatal(err)
	}
	m.agent = ag
	m.agent.Messages = append(m.agent.Messages, llm.Message{Role: "user", Content: "hi", Authored: true})

	m.applyAuthResult(authResultMsg{key: "sk-or-new", models: []llm.ModelInfo{{ID: "openai/gpt-5", ContextLength: 400000}}})

	client, ok := m.agent.Client.(*llm.OpenAI)
	if !ok {
		t.Fatalf("client = %T, want *llm.OpenAI", m.agent.Client)
	}
	if client.APIKey != "sk-or-new" {
		t.Error("live agent should be rebuilt with the new key")
	}
	if len(m.agent.Messages) != 2 || m.agent.Messages[1].Content != "hi" {
		t.Error("history must survive the hot-swap")
	}
}

// Keys must never enter input history (idle or busy), never queue as a chat
// message while the agent is busy, and the masked prompt's esc-esc draft
// stash must not record them either.
func TestAuthKeyNeverLeaksIntoHistoryOrQueue(t *testing.T) {
	m := authTestModel(t)

	// Idle submit of an inline key: runs the command, skips history.
	m.input.SetValue("/auth openrouter sk-or-idle")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	for _, h := range m.hist {
		if strings.Contains(h, "sk-or-idle") {
			t.Fatalf("idle submit recorded a key in history: %v", m.hist)
		}
	}

	// Busy submit: /auth must run immediately (busyCmd), not queue the key
	// as a message for the model, and skip history too.
	m.busy = true
	m.input.SetValue("/auth openrouter sk-or-busy")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	m.busy = false
	for _, q := range m.queue {
		if strings.Contains(q, "sk-or-busy") {
			t.Fatalf("busy submit queued a key as a chat message: %v", m.queue)
		}
	}
	for _, h := range m.hist {
		if strings.Contains(h, "sk-or-busy") {
			t.Fatalf("busy submit recorded a key in history: %v", m.hist)
		}
	}
	if !busyCmd("/auth openrouter sk-or-x") {
		t.Error("/auth must be a busyCmd — queueing an inline key sends it to the model")
	}

	// Masked prompt: esc cancels without the esc-esc draft stash recording.
	m.authCommand([]string{"openrouter"})
	m.input.SetValue("sk-or-masked")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(*model)
	if m.namePrompt != nil {
		t.Fatal("esc should close the masked prompt")
	}
	for _, h := range m.hist {
		if strings.Contains(h, "sk-or-masked") {
			t.Fatalf("masked prompt cancel recorded a key in history: %v", m.hist)
		}
	}
	if m.escClr {
		t.Error("esc-esc clear must not arm after a masked prompt")
	}
}

// catalogLites carries context/output caps, reasoning efforts, vision
// modalities, and pricing from the provider's /models into the cache shape;
// a nil Pricing stays a zero-rated entry (callers hide $0 costs).
func TestCatalogLites(t *testing.T) {
	lites := catalogLites([]llm.ModelInfo{
		{
			ID: "openai/gpt-5", ContextLength: 400000, MaxCompletionTokens: 128000,
			ReasoningEfforts: []string{"low", "high"}, InputModalities: []string{"text", "image"},
			Pricing: &llm.Pricing{Prompt: "0.00000125", Completion: "0.00001", InputCacheRead: "0.000000125"},
		},
		{ID: "meta/llama-4", ContextLength: 131072}, // no pricing, text-only
	})
	if len(lites) != 2 {
		t.Fatalf("want 2 lites, got %d", len(lites))
	}
	a := lites[0]
	if a.ContextLength != 400000 || a.MaxCompletionTokens != 128000 {
		t.Errorf("caps not carried: %+v", a)
	}
	if len(a.ReasoningEfforts) != 2 || len(a.InputModalities) != 2 {
		t.Errorf("efforts/modalities not carried: %+v", a)
	}
	if a.InPrice == 0 || a.OutPrice == 0 || a.CacheReadPrice == 0 {
		t.Errorf("pricing not parsed: %+v", a)
	}
	if b := lites[1]; b.InPrice != 0 || b.OutPrice != 0 || len(b.InputModalities) != 0 {
		t.Errorf("pricing-less model should stay zero-rated: %+v", b)
	}
}

// The masked render shows the bullet mask with the textarea's prompt, never
// the key.
func TestAuthMaskedRender(t *testing.T) {
	m := authTestModel(t)
	m.agent = &agent.Agent{} // View reads usage off the agent
	m.authCommand([]string{"openrouter"})
	m.input.SetValue("sk-or-hidden")
	out := m.View()
	if strings.Contains(out, "sk-or-hidden") {
		t.Fatalf("masked prompt leaked the key into the view")
	}
	if !strings.Contains(out, "┃ "+strings.Repeat("•", len("sk-or-hidden"))) {
		t.Errorf("masked prompt should render bullets after the prompt:\n%s", out)
	}
}

// The user-visible promise: after auth, the openrouter provider entry plus
// its seeded catalog make every advertised model appear in the /model picker
// with its capabilities — no per-model config. (The HTTP fetch/validate half
// is covered with an httptest server in cmd/whip; this pins the TUI half:
// applyAuthResult's seeded state flowing into the picker.)
func TestAuthMakesCatalogModelsPickable(t *testing.T) {
	m := authTestModel(t)
	m.cfg.UpsertOpenRouter("sk-or-live", false)
	if err := m.cfg.Save(); err != nil {
		t.Fatal(err)
	}
	// What applyAuthResult seeds on the live path (guarded on m.prog).
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"openrouter": {
			FetchedAt: time.Now(),
			BaseURL:   config.OpenRouterBaseURL,
			Models: catalogLites([]llm.ModelInfo{
				{ID: "openai/gpt-5", ContextLength: 400000, InputModalities: []string{"text", "image"}},
				{ID: "anthropic/claude-sonnet-4.5", ContextLength: 1000000, InputModalities: []string{"text"}},
			}),
		},
	}); err != nil {
		t.Fatal(err)
	}

	m.openModelPicker()
	if m.mpicker == nil {
		t.Fatal("picker should open on a non-empty catalog")
	}
	byModel := map[string]modelItem{}
	for _, it := range m.mpicker.items {
		byModel[it.model] = it
	}
	gpt, ok := byModel["openai/gpt-5"]
	if !ok {
		names := make([]string, 0, len(m.mpicker.items))
		for _, it := range m.mpicker.items {
			names = append(names, it.model+"@"+it.provider)
		}
		t.Fatalf("openai/gpt-5 missing from picker; items: %v", names)
	}
	if gpt.provider != "openrouter" || !gpt.fromCatalog {
		t.Errorf("catalog model should route to openrouter and be marked (new): %+v", gpt)
	}
	if _, ok := byModel["anthropic/claude-sonnet-4.5"]; !ok {
		t.Error("every advertised catalog model should be pickable, not just one")
	}

	// And the picked model resolves + builds an agent through the catalog —
	// the actual switch a user makes next.
	ag, name, prov, err := buildAgent(m.cfg, "openai/gpt-5", "openrouter", "sys")
	if err != nil {
		t.Fatalf("catalog model should build an agent: %v", err)
	}
	if name != "openai/gpt-5" || prov != "openrouter" {
		t.Errorf("route should stay on openrouter: %q @ %q", name, prov)
	}
	if ag.ContextLimit != 400000 {
		t.Errorf("context window should come from the catalog: %d", ag.ContextLimit)
	}
	client, ok := ag.Client.(*llm.OpenAI)
	if !ok {
		t.Fatalf("client = %T, want *llm.OpenAI", ag.Client)
	}
	if client.APIKey != "sk-or-live" {
		t.Error("agent should carry the authed key")
	}
}
