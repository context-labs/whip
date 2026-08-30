package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

// /auth <provider> [key] turns a pasted API key into a working provider
// without leaving the session: the key is validated against the provider's
// live /models, the provider entry is upserted into ~/.whip/config.json
// (guarded atomic save), and the model catalog is refreshed so /model lists
// the new catalog immediately. OpenRouter is the first (and ponytail: only,
// until a second provider wants one) supported provider.
//
// The bare form (/auth openrouter) repurposes the input box as a masked
// one-shot prompt — the same namePrompt machinery as /fork and /rename, with
// mask set so the key never renders on screen or lands in the transcript.

func (m *model) authCommand(args []string) {
	if len(args) == 0 {
		m.append(dimStyle.Render("usage: /auth <provider> [key] — inference-net (bare = browser login) or openrouter (bare = masked prompt)"))
		return
	}
	switch args[0] {
	case "inference-net", "inference":
		m.authInferenceNetCommand(args)
		return
	case "openrouter":
	default:
		m.append(errStyle.Render("unknown provider " + args[0] + " (supported: inference-net, openrouter)"))
		return
	}
	if len(args) > 1 {
		m.authOpenRouter(config.TrimKey(strings.Join(args[1:], "")), false)
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
	if m.provName == "openrouter" && m.modelName != "" && m.replacementBlocked() == "" {
		if ag, _, _, err := buildAgent(m.cfg, m.modelName, m.provName, m.sysPrompt, m.agent.Services); err == nil {
			ag.Effort = m.agent.Effort
			ag.WorkingDir = m.agent.WorkingDir
			ag.Messages = append(ag.Messages, m.agent.Messages[1:]...)
			ag.CompactClient, ag.CompactModel = m.agent.CompactClient, m.agent.CompactModel
			ag.CompactThreshold = m.agent.CompactThreshold
			m.agent = ag
			m.bindToolServices(m.agent)
			m.applyTaskModel()
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
