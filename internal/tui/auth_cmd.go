package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
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

// authOpenRouter stores provider credentials. Provider construction and
// validation belong to the daemon, which will report an invalid key on use.
func (m *model) authOpenRouter(key string, envMode bool) {
	if key == "" && !envMode {
		m.append(errStyle.Render("/auth openrouter needs a key (get one at https://openrouter.ai/keys)"))
		return
	}
	m.append(dimStyle.Render("validating key against OpenRouter…"))
	if m.prog == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, err := m.client.ValidateProvider(ctx, daemon.ProviderValidateParams{
			Name: "openrouter", BaseURL: config.OpenRouterBaseURL, Key: key,
		})
		m.prog.Send(authResultMsg{key: key, envMode: envMode, models: result.Models, err: err})
	}()
}

// applyAuthResult commits auth configuration on the UI goroutine. The daemon
// remains the only component that constructs providers or rewires sessions.
func (m *model) applyAuthResult(res authResultMsg) bool {
	if res.err != nil {
		m.append(errStyle.Render("OpenRouter rejected the key: " + res.err.Error()))
		return false
	}
	m.cfg.UpsertOpenRouter(res.key, res.envMode)
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
		return false
	}
	m.append(dimStyle.Render(fmt.Sprintf("✓ openrouter configured — %d models available", len(res.models))))
	return true
}
