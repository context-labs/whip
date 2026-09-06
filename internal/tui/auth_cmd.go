package tui

import (
	"context"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
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

// authResultMsg contains only the outcome of host-side setup, never a key.
type authResultMsg struct{ err error }

func (m *model) authOpenRouter(key string, envMode bool) {
	if key == "" && !envMode {
		m.append(errStyle.Render("/auth openrouter needs a key (get one at https://openrouter.ai/keys)"))
		return
	}
	m.append(dimStyle.Render("validating and saving the key on the execution host…"))
	if m.prog == nil {
		return
	}
	client, program := m.client, m.prog
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := setupProviderKey(ctx, client, "openrouter", key, envMode)
		program.Send(authResultMsg{err: err})
	}()
}

func setupProviderKey(ctx context.Context, client *Client, provider, key string, environment bool) (daemon.RuntimeConfiguration, error) {
	current, err := client.ReadConfiguration(ctx)
	if err != nil {
		return daemon.RuntimeConfiguration{}, err
	}
	return client.SetProviderKey(ctx, daemon.ProviderKeySetup{Revision: current.Revision, Provider: provider, Key: key, Environment: environment})
}

func (m *model) applyAuthResult(result authResultMsg) bool {
	if result.err != nil {
		m.append(errStyle.Render("OpenRouter setup failed: " + result.err.Error()))
		return false
	}
	m.append(dimStyle.Render("✓ openrouter configured on the execution host"))
	return true
}
