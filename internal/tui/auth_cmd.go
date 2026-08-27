package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/codexauth"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

// /auth <provider> starts the provider's login flow without leaving the
// session. OpenRouter validates a pasted API key and pre-fetches its catalog;
// Codex runs device-code OAuth and fetches its account-scoped catalog. Both
// save the provider entry before reporting success, so /model can use it at
// once.
//
// The bare form (/auth openrouter) repurposes the input box as a masked
// one-shot prompt — the same namePrompt machinery as /fork and /rename, with
// mask set so the key never renders on screen or lands in the transcript.

func (m *model) authCommand(args []string) {
	if len(args) == 0 {
		m.append(dimStyle.Render("usage: /auth <provider> [key] — inference-net (bare = browser login), openrouter (bare = masked prompt), or codex (device login)"))
		return
	}
	switch args[0] {
	case "inference-net", "inference":
		m.authInferenceNetCommand(args)
	case "codex":
		if len(args) != 1 {
			m.append(errStyle.Render("usage: /auth codex"))
			return
		}
		m.authCodex()
	case "openrouter":
		m.authOpenRouterCommand(args[1:])
	default:
		m.append(errStyle.Render("unknown provider " + args[0] + " (supported: inference-net, openrouter, codex)"))
	}
}

func (m *model) authOpenRouterCommand(args []string) {
	if len(args) > 0 {
		m.authOpenRouter(config.TrimKey(strings.Join(args, "")), false)
		return
	}
	m.openNamePrompt("🔑 openrouter key (masked, enter to save, esc cancels):", "", func(key string) {
		key = config.TrimKey(key)
		if key == "" {
			m.append(dimStyle.Render("auth cancelled"))
			return
		}
		m.authOpenRouter(key, false)
	})
	m.namePrompt.mask = true
}

// codexLoginResultMsg carries device-code login and account-catalog results
// back to the UI goroutine. Credentials are committed before the catalog is
// fetched, so a transient catalog error never strands a successful login.
type codexLoginResultMsg struct {
	models     []llm.ModelInfo
	catalogErr error
	err        error
}

func (m *model) authCodex() {
	if m.busy {
		m.append(errStyle.Render("/auth codex needs an idle session; wait for the current turn first"))
		return
	}
	if m.prog == nil {
		return // tests drive applyCodexLoginResult directly
	}

	m.append(dimStyle.Render("starting Codex device login…"))
	ctx, cancel := context.WithCancel(context.Background())
	m.busy = true
	m.cancel = cancel
	m.turnStart = time.Now()
	p := m.prog
	go func() {
		source := &codexauth.Source{}
		err := source.DeviceLogin(ctx, func(code codexauth.DeviceCode) {
			p.Send(noticeMsg(fmt.Sprintf(
				"Codex sign-in: open %s and enter code %s. Press esc to cancel.",
				code.VerificationURL,
				code.UserCode,
			)))
		})
		result := codexLoginResultMsg{err: err}
		if err == nil {
			catalogCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			result.models, result.catalogErr = llm.NewCodex(config.CodexBaseURL, source).Models(catalogCtx)
			cancel()
		}
		p.Send(result)
	}()
}

func (m *model) applyCodexLoginResult(result codexLoginResultMsg) {
	m.busy = false
	m.cancel = nil
	m.interrupt1 = false
	m.turnStart = time.Time{}
	if errors.Is(result.err, context.Canceled) {
		m.append(dimStyle.Render("Codex login cancelled"))
		return
	}
	if result.err != nil {
		m.append(errStyle.Render("Codex login failed: " + result.err.Error()))
		return
	}
	m.cfg.UpsertCodex()
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
		return
	}
	if result.catalogErr != nil {
		m.append(dimStyle.Render("✓ Codex configured — gpt-5.4 @ codex is ready in /model"))
		m.append(dimStyle.Render("Codex model catalog will retry on /model refresh: " + result.catalogErr.Error()))
		return
	}
	if err := m.saveCodexCatalog(result.models); err != nil {
		m.append(dimStyle.Render("✓ Codex configured — gpt-5.4 @ codex is ready in /model"))
		m.append(dimStyle.Render("Codex model catalog could not be cached; /model refresh will retry: " + err.Error()))
		return
	}
	m.append(dimStyle.Render(fmt.Sprintf("✓ Codex configured — %d account models are ready in /model", len(result.models))))
}

// saveCodexCatalog makes freshly authenticated subscription models available
// before the next background refresh. The caller has already persisted the
// provider route, so catalog-cache failure is recoverable.
func (m *model) saveCodexCatalog(infos []llm.ModelInfo) error {
	cats := config.LoadCatalogs()
	cats[config.CodexProviderName] = config.Catalog{
		FetchedAt: time.Now(),
		BaseURL:   config.CodexBaseURL,
		Models:    catalogLites(infos),
	}
	if err := config.SaveCatalogs(cats); err != nil {
		return err
	}
	if m.agent != nil {
		m.updateCatalogs(cats)
	} else {
		m.catalogs = cats
	}
	return nil
}

// authResultMsg carries a finished key validation back to the UI goroutine.
type authResultMsg struct {
	key     string
	envMode bool
	models  []llm.ModelInfo
	err     error
}

// authOpenRouter validates key against OpenRouter in the background, then
// persists provider + catalog and hot-swaps the live agent's routing when
// the session is currently on the openrouter provider (so a refreshed key
// fixes a 401ing session without a /model round-trip).
func (m *model) authOpenRouter(key string, envMode bool) {
	if key == "" && !envMode {
		m.append(errStyle.Render("/auth openrouter needs a key (get one at https://openrouter.ai/keys)"))
		return
	}
	m.append(dimStyle.Render("validating key against OpenRouter…"))
	if m.prog == nil {
		return // tests drive applyAuthResult directly; no program to report to
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		infos, err := llm.New(config.OpenRouterBaseURL, key).Models(ctx)
		cancel()
		m.prog.Send(authResultMsg{key: key, envMode: envMode, models: infos, err: err})
	}()
}

// applyAuthResult commits a validated auth: config upsert, then the
// live-session rewiring. Runs on the UI goroutine (via authResultMsg).
// Catalog seeding and the background refresh are live-runtime side effects
// (m.prog != nil) so driving the command directly in tests writes no cache
// and spawns no network fetch.
func (m *model) applyAuthResult(res authResultMsg) {
	if res.err != nil {
		m.append(errStyle.Render("OpenRouter rejected the key: " + res.err.Error()))
		return
	}
	m.cfg.UpsertOpenRouter(res.key, res.envMode)
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
		return
	}
	// If the current session routes through openrouter, rebuild the agent so
	// the new key takes effect on the very next turn.
	if m.provName == "openrouter" && m.modelName != "" {
		if ag, _, _, err := buildAgent(m.cfg, m.modelName, m.provName, m.sysPrompt); err == nil {
			ag.Effort = m.agent.Effort
			ag.Messages = append(ag.Messages, m.agent.Messages[1:]...)
			ag.CompactClient, ag.CompactModel = m.agent.CompactClient, m.agent.CompactModel
			ag.CompactThreshold = m.agent.CompactThreshold
			m.agent = ag
			m.wireTasks()
		}
	}
	m.append(dimStyle.Render(fmt.Sprintf("✓ openrouter configured — %d models in the catalog; /model lists them all (e.g. /model openai/gpt-5 openrouter)", len(res.models))))

	if m.prog == nil {
		return // test dispatch: stop before on-disk/network side effects
	}
	if len(res.models) > 0 { // a fresh list came with the validation; seed the cache
		cats := config.LoadCatalogs()
		cats["openrouter"] = config.Catalog{
			FetchedAt: time.Now(),
			BaseURL:   config.OpenRouterBaseURL,
			Models:    catalogLites(res.models),
		}
		if err := config.SaveCatalogs(cats); err != nil {
			m.append(dimStyle.Render("(catalog cache write failed; /model refresh will retry)"))
		}
	}
	go m.fetchCatalogs(true) // refresh all providers; the openrouter entry is already fresh
}
